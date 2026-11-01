package collections_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/collections"
)

func TestOriginalVueUploadDetectsSubgroupAndStartReportsQueued(t *testing.T) {
	meta, err := os.ReadFile(filepath.Join("..", "torrent", "testdata", "v1-multifile-utf8.torrent"))
	if err != nil {
		t.Fatal(err)
	}
	name := "[识别组] Example - 01.mkv"
	meta = bytes.Replace(meta, []byte("12:第01集.mkv"), []byte(fmt.Sprintf("%d:%s", len([]byte(name)), name)), 1)
	q := &queue{}
	service, err := collections.New(collections.Options{Enqueuer: q, Config: func(context.Context) (map[string]any, error) {
		return map[string]any{"automaticTasksPaused": true}, nil
	}, DownloadPath: func(context.Context, map[string]any) (string, error) { return t.TempDir(), nil }})
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := os.ReadFile(filepath.Join("..", "server", "ani-defaults.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ani map[string]any
	if err := json.Unmarshal(defaults, &ani); err != nil {
		t.Fatal(err)
	}
	ani["title"], ani["season"], ani["subgroup"], ani["customEpisode"] = "Example", 1, "未知字幕组", true
	ani["match"], ani["exclude"] = []any{}, []any{}
	body, _ := json.Marshal(map[string]any{"torrent": base64.StdEncoding.EncodeToString(meta), "ani": ani})
	results := map[string]map[string]any{}
	for _, route := range service.Routes() {
		if route.Path == "/api/previewCollection" {
			continue
		}
		res := httptest.NewRecorder()
		route.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodPost, route.Path, bytes.NewReader(body)))
		var value map[string]any
		if err := json.Unmarshal(res.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if value["code"] != float64(200) {
			t.Fatalf("%s: %v", route.Path, value)
		}
		results[route.Path] = value
	}
	if got := results["/api/getCollectionSubgroup"]["data"]; got != "识别组" {
		t.Errorf("Vue placeholder masked detected subgroup: %v", got)
	}
	if got := results["/api/startCollection"]["message"]; got != "合集任务已加入队列" {
		t.Errorf("paused queue incorrectly reported download started: %v", got)
	}
	if q.job.Kind != collections.JobKind || q.job.ID != "fixture" {
		t.Fatal("start did not persist collection job")
	}
}
