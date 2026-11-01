package discovery_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/discovery"
)

func TestHalfEpisodeKeepsDefaultEpisodeTitlesAndSubscriptionMetadata(t *testing.T) {
	var episodeTitleRequests, subscriptionMetadataRequests int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/tv/456/season/2", "/v0/episodes":
			episodeTitleRequests++
			_, _ = w.Write([]byte(`{"data":[],"episodes":[{"episode_number":4,"name":"第4集错误标题"}]}`))
		case "/v0/subjects/123":
			subscriptionMetadataRequests++
			_, _ = w.Write([]byte(`{"id":"123","name":"订阅日文标题","name_cn":"订阅中文标题"}`))
		default:
			t.Errorf("意外请求 %s", r.URL.String())
		}
	}))
	defer upstream.Close()
	client := discovery.New(&memoryStore{config: map[string]any{"bgmApi": upstream.URL, "tmdbApi": upstream.URL}}, upstream.Client(), nil)
	ani := map[string]any{
		"bgmUrl": "https://bgm.tv/subject/123", "themoviedbName": "订阅 TMDB 标题",
		"season": 2, "tmdb": map[string]any{"id": "456"},
	}
	metadata, err := client.Metadata(context.Background(), ani, 4.5)
	if err != nil {
		t.Fatal(err)
	}
	if episodeTitleRequests != 0 {
		t.Fatalf("半集不应请求 TMDB 或 Bangumi 单集标题，实际请求 %d 次", episodeTitleRequests)
	}
	if subscriptionMetadataRequests != 1 {
		t.Fatalf("半集仍应获取订阅级 JP 标题，实际请求 %d 次", subscriptionMetadataRequests)
	}
	want := map[string]string{
		"tmdbid": "456", "bgmId": "123", "jpTitle": "订阅日文标题", "themoviedbName": "订阅 TMDB 标题",
		"episodeTitle": "第4.5集", "bgmEpisodeTitle": "第4.5集", "bgmJpEpisodeTitle": "第4.5集",
	}
	for key, expected := range want {
		if metadata[key] != expected {
			t.Errorf("metadata[%q]=%q，期望 %q", key, metadata[key], expected)
		}
	}
}
