package discovery_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/discovery"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

func TestAnimeGardenListFiltersSubjectsBySearchText(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/subjects":
			_, _ = w.Write([]byte(`{"subjects":[{"id":"123","name":"目标作品","keywords":["English Title"],"activedAt":"2025-01-06T00:00:00Z"},{"id":"456","name":"另一个作品","activedAt":"2025-01-07T00:00:00Z"}]}`))
		case "/v0/subjects/123":
			_, _ = w.Write([]byte(`{"id":"123","name":"目标作品","images":{"small":"https://img/cover.jpg"},"rating":{"score":8.5}}`))
		case "/v0/subjects/456":
			_, _ = w.Write([]byte(`{"id":"456","name":"另一个作品","rating":{"score":7.5}}`))
		default:
			t.Fatalf("意外的 AnimeGarden 请求：%s", r.URL.String())
		}
	}))
	defer upstream.Close()

	store := &memoryStore{config: map[string]any{
		"animeGardenBaseURL": upstream.URL,
		"bgmApi":             upstream.URL,
	}, subs: nil}
	client := discovery.New(store, upstream.Client(), nil)
	var route server.Route
	for _, candidate := range client.Routes() {
		if candidate.Path == "/api/animeGardenList" {
			route = candidate
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/api/animeGardenList?text="+url.QueryEscape("english"), nil).WithContext(context.Background())
	recorder := httptest.NewRecorder()
	route.Handler.ServeHTTP(recorder, request)

	var response struct {
		Code int `json:"code"`
		Data []struct {
			Subjects []struct {
				ID string `json:"id"`
			} `json:"subjects"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(response.Data) != 1 || len(response.Data[0].Subjects) != 1 || response.Data[0].Subjects[0].ID != "123" {
		t.Fatalf("AnimeGarden 搜索结果错误：%s", recorder.Body.String())
	}
}
