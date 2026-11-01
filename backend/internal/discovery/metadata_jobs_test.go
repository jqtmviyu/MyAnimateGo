package discovery_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/discovery"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestManualEpisodeCountJobSurvivesReopenAndKeepsOtherSubscriptionFields(t *testing.T) {
	ctx := context.Background()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/episodes" {
			_, _ = io.WriteString(w, `{"data":[{"id":1,"ep":1,"type":0},{"id":2,"ep":2,"type":0}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":123,"name":"Example","eps":24,"rating":{"score":8.8}}`)
	}))
	defer origin.Close()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(ctx, map[string]any{"apiKey": "metadata-fixture-key", "bgmApi": origin.URL, "cacheBaseURL": origin.URL}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{"id": "selected", "title": "Example", "season": 1, "enable": false, "totalEpisodeNumber": 0, "score": 1, "bgmUrl": "https://bgm.tv/subject/123", "customField": "preserved"}); err != nil {
		t.Fatal(err)
	}
	api, err := server.New(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	catalog := discovery.New(store, origin.Client(), nil)
	if err := api.Register(catalog.Routes()...); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/updateTotalEpisodeNumber?force=true", strings.NewReader(`["selected"]`))
	req.Header.Set("x-api-key", "metadata-fixture-key")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["code"] != float64(200) {
		t.Fatalf("手动更新未持久接收：%v", result)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	catalog = discovery.New(store, origin.Client(), nil)
	processed, err := jobs.New(store, catalog.MetadataJobHandlers(), jobs.Options{}).RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("重开后未恢复元数据任务：%v %v", processed, err)
	}
	items, err := store.ListSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0]["totalEpisodeNumber"] != float64(2) || items[0]["score"] != float64(1) || items[0]["customField"] != "preserved" {
		t.Fatalf("手动集数更新结果：%v", items)
	}
}

func TestAutomaticMetadataContinuesAfterFailureAndHonorsEpisodeCountPolicy(t *testing.T) {
	ctx := context.Background()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "999") {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/v0/episodes" {
			_, _ = io.WriteString(w, `{"data":[{"id":1,"ep":1,"type":0},{"id":2,"ep":2,"type":0}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":123,"eps":24,"rating":{"score":8.8}}`)
	}))
	defer origin.Close()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := map[string]any{"bgmApi": origin.URL, "cacheBaseURL": origin.URL, "updateTotalEpisodeNumber": true, "forceUpdateTotalEpisodeNumber": false}
	if err := store.SaveConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	for _, item := range []map[string]any{
		{"id": "broken", "enable": true, "bgmUrl": "https://bgm.tv/subject/999"},
		{"id": "healthy", "enable": true, "score": 1, "totalEpisodeNumber": 7, "bgmUrl": "https://bgm.tv/subject/123"},
		{"id": "disabled", "enable": false, "score": 1, "totalEpisodeNumber": 9, "bgmUrl": "https://bgm.tv/subject/123"},
	} {
		if err := store.PutSubscription(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	catalog := discovery.New(store, origin.Client(), nil)
	if err := catalog.RefreshAutomaticMetadata(ctx); err == nil {
		t.Fatal("应汇总失败订阅的上游错误")
	}
	items, err := store.ListSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if items[1]["score"] != 8.8 || items[1]["totalEpisodeNumber"] != float64(7) || items[2]["score"] != float64(1) {
		t.Fatalf("自动刷新策略未保留已有集数/禁用订阅：%v", items)
	}
	config["forceUpdateTotalEpisodeNumber"] = true
	if err := store.SaveConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	_ = catalog.RefreshAutomaticMetadata(ctx)
	items, err = store.ListSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if items[1]["totalEpisodeNumber"] != float64(2) {
		t.Fatalf("强制更新应使用Bangumi普通集列表：%v", items)
	}
}
