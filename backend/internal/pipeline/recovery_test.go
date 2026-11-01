package pipeline_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestOpaqueTorrentURLReconcilesUsingMetainfoHash(t *testing.T) {
	metainfo, err := os.ReadFile("../torrent/testdata/v1-single.torrent")
	if err != nil {
		t.Fatal(err)
	}
	const hash = "bcf2e8247f61dd5871b7de2f04236d84b32706f0"
	writes := 0
	removed := false
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Method == "torrent-add" {
			writes++
			w.WriteHeader(503)
			return
		}
		torrents := []any{}
		if writes > 0 && !removed {
			torrents = append(torrents, map[string]any{"hashString": hash, "totalSize": 5, "status": 4})
		}
		writeRPC(w, map[string]any{"torrents": torrents})
	}))
	defer rpc.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(metainfo)
	}))
	defer source.Close()
	engine, _ := recoveryEngine(t, rpc.URL, pipeline.Options{RSSFetcher: func(context.Context, string) ([]byte, error) {
		return []byte(`<rss><channel><item><title>Example - 01</title><guid>rssitem000001</guid><enclosure url="` + source.URL + `/download?id=opaque"/></item></channel></rss>`), nil
	}})
	if err := engine.RefreshSubscription(context.Background(), "recover"); err != nil {
		t.Fatalf("应通过种子实际hash确认已添加任务：%v", err)
	}
	removed = true
	if err := engine.RefreshSubscription(context.Background(), "recover"); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("历史种子不能在远端任务清理后重新提交：%d", writes)
	}
}

func TestRefreshAllContinuesAfterOneSubscriptionFails(t *testing.T) {
	fetchedHealthy := false
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRPC(w, map[string]any{"torrents": []any{}})
	}))
	defer rpc.Close()
	feedError := errors.New("RSS fixture unavailable")
	engine, store := recoveryEngine(t, rpc.URL, pipeline.Options{RSSFetcher: func(ctx context.Context, rawURL string) ([]byte, error) {
		if rawURL == "https://example.invalid/rss" {
			return nil, feedError
		}
		fetchedHealthy = true
		return []byte(`<rss><channel></channel></rss>`), nil
	}})
	if err := store.PutSubscription(context.Background(), map[string]any{"id": "healthy", "title": "Healthy", "season": 1, "url": "https://healthy.invalid/rss", "enable": true}); err != nil {
		t.Fatal(err)
	}
	if err := engine.RefreshAll(context.Background()); !errors.Is(err, feedError) {
		t.Fatalf("应返回失败订阅的原因：%v", err)
	}
	if !fetchedHealthy {
		t.Fatal("一条坏 RSS 阻止了后续健康订阅刷新")
	}
}

func TestUncertainAddWaitsForReconciliationAndRestoresProgress(t *testing.T) {
	added, recovered, removed := false, false, false
	writes := 0
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Method == "torrent-add" {
			writes++
			added = true
			w.WriteHeader(503)
			return
		}
		if added && !recovered {
			w.WriteHeader(503)
			return
		}
		torrents := []any{}
		if added && !removed {
			torrents = append(torrents, map[string]any{"hashString": sampleHash, "labels": []string{"ani-rss"}, "totalSize": 1, "status": 4})
		}
		writeRPC(w, map[string]any{"torrents": torrents})
	}))
	defer rpc.Close()
	now := time.Date(2025, 4, 6, 12, 0, 0, 0, time.UTC)
	engine, store := recoveryEngine(t, rpc.URL, pipeline.Options{Now: func() time.Time { return now }})
	if err := engine.RefreshSubscription(context.Background(), "recover"); err == nil {
		t.Fatal("对账暂时不可用时应保留待恢复状态")
	}
	if writes != 1 {
		t.Fatalf("结果未知时不得盲目重写，实际写了%d次", writes)
	}
	recovered = true
	now = now.Add(time.Minute)
	if err := engine.RefreshSubscription(context.Background(), "recover"); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListSubscriptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if items[0]["currentEpisodeNumber"] != float64(1) || items[0]["lastDownloadTime"] != float64(now.UnixMilli()) {
		t.Fatalf("对账后进度应恢复：%v", items[0])
	}
	stamp := items[0]["lastDownloadTime"]
	removed = true
	now = now.Add(time.Hour)
	if err := engine.RefreshSubscription(context.Background(), "recover"); err != nil {
		t.Fatal(err)
	}
	items, err = store.ListSubscriptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if writes != 1 || items[0]["lastDownloadTime"] != stamp {
		t.Fatal("已提交历史不应重新添加或刷新下载时间")
	}
}

func recoveryEngine(t *testing.T, rpc string, options pipeline.Options) (*pipeline.Engine, *storage.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SaveConfig(context.Background(), map[string]any{"automaticTasksPaused": false, "downloadToolHost": rpc, "downloadPathTemplate": filepath.Join(dir, "${title}"), "downloadRetry": 3}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(context.Background(), map[string]any{"id": "recover", "title": "Example", "season": 1, "url": "https://example.invalid/rss", "enable": true}); err != nil {
		t.Fatal(err)
	}
	if options.RSSFetcher == nil {
		options.RSSFetcher = func(context.Context, string) ([]byte, error) {
			return []byte(fmt.Sprintf(`<rss><channel><item><title>Example - 01</title><enclosure url="magnet:?xt=urn:btih:%s"/></item></channel></rss>`, sampleHash)), nil
		}
	}
	engine, err := pipeline.New(store, options)
	if err != nil {
		t.Fatal(err)
	}
	return engine, store
}
