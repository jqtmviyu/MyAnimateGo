package pipeline_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestDownloadListIncludesOwnedCollectionAlongsideOrdinaryDownload(t *testing.T) {
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"result": "success", "arguments": map[string]any{"torrents": []any{
			map[string]any{"hashString": "ordinary", "name": "普通下载", "labels": []string{"ani-rss"}, "totalSize": 10, "haveValid": 5},
			map[string]any{"hashString": sampleHash, "name": "合集下载", "labels": []string{"MyAnimeGo合集下载", "ani-rss-collection:" + sampleHash}, "totalSize": 10, "haveValid": 10, "status": 6},
			map[string]any{"hashString": "foreign", "name": "其他任务", "labels": []string{"MyAnimeGo合集下载"}},
		}}})
	}))
	defer rpc.Close()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(context.Background(), map[string]any{"downloadToolHost": rpc.URL}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range engine.Routes() {
		if route.Path != "/api/torrentsInfos" {
			continue
		}
		res := httptest.NewRecorder()
		route.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodPost, route.Path, nil))
		var got struct {
			Code int `json:"code"`
			Data []struct {
				Hash     string  `json:"hash"`
				Progress float64 `json:"progress"`
			} `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Code != 200 || len(got.Data) != 2 {
			t.Fatalf("ordinary and collection downloads must both be listed: %s", res.Body.String())
		}
		if got.Data[1].Hash != sampleHash || got.Data[1].Progress != 100 {
			t.Fatal("collection identity or progress differs")
		}
		return
	}
	t.Fatal("download list route missing")
}
