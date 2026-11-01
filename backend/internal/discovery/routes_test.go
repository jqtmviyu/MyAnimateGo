package discovery_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/discovery"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

type memoryStore struct {
	config map[string]any
	subs   []map[string]any
}

func (s *memoryStore) GetConfig(context.Context) (map[string]any, error) { return s.config, nil }
func (s *memoryStore) UpdateConfigFields(_ context.Context, fields map[string]any) error {
	for key, value := range fields {
		s.config[key] = value
	}
	return nil
}
func (s *memoryStore) SaveConfig(_ context.Context, v map[string]any) error { s.config = v; return nil }
func (s *memoryStore) ListSubscriptions(context.Context) ([]map[string]any, error) {
	return s.subs, nil
}
func (*memoryStore) PutSubscription(context.Context, map[string]any) error        { return nil }
func (*memoryStore) DeleteSubscriptions(context.Context, []string) error          { return nil }
func (*memoryStore) ReplaceSubscriptions(context.Context, []map[string]any) error { return nil }

func TestRoutesExposeAllDiscoveryPOSTContracts(t *testing.T) {
	client := discovery.New(&memoryStore{config: map[string]any{}}, nil, nil)
	routes := client.Routes()
	want := []string{
		"/api/mikan", "/api/mikanGroup", "/api/aniBT", "/api/aniBTGroup",
		"/api/animeGardenList", "/api/animeGardenGroup", "/api/searchBgm",
		"/api/getAniBySubjectId", "/api/getBgmTitle",
		"/api/getThemoviedbName", "/api/getThemoviedbGroup", "/api/rssToAni",
		"/api/updateTotalEpisodeNumber",
	}
	if len(routes) != len(want) {
		t.Fatalf("Routes() 有 %d 条，期望 %d", len(routes), len(want))
	}
	got := map[string]server.Route{}
	for _, route := range routes {
		got[route.Path] = route
	}
	for _, path := range want {
		route, ok := got[path]
		if !ok || route.Method != http.MethodPost || route.Public || route.Handler == nil {
			t.Fatalf("route %q 不符合受保护 POST 契约：%+v", path, route)
		}
	}
}

func TestMikanSearchRouteParsesHTMLAndReturnsNormalizedResult(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Home/Bangumi/123" {
			_, _ = w.Write([]byte(`<html></html>`))
			return
		}
		if r.URL.Path != "/Home/Search" || r.URL.Query().Get("searchstr") != "星之旅" {
			t.Errorf("上游请求 = %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<div class="date-select"><span class="date-text">2025 春</span><div class="dropdown-menu"><ul><li>title</li><li><a data-year="2025" data-season="春">2025 春</a></li></ul></div></div><ul class="an-ul"><li><span data-src="/covers/show.jpg"></span><a href="/Home/Bangumi/123">星之旅</a></li></ul>`))
	}))
	defer upstream.Close()
	client := discovery.New(&memoryStore{config: map[string]any{"mikanHost": upstream.URL}}, upstream.Client(), nil)
	route := findRoute(t, client.Routes(), "/api/mikan")
	body := strings.NewReader(`{"year":2025,"season":"春"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/mikan?text="+url.QueryEscape("星之旅"), body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	route.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP状态=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var result struct {
		Code int `json:"code"`
		Data struct {
			Total   int   `json:"totalItem"`
			Seasons []any `json:"seasons"`
			Weeks   []struct {
				Label string `json:"weekLabel"`
				Items []struct {
					Title string `json:"title"`
					URL   string `json:"url"`
					Cover string `json:"cover"`
				} `json:"items"`
			} `json:"weeks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Code != 200 || result.Data.Total != 1 || len(result.Data.Weeks) != 1 || result.Data.Weeks[0].Label != "Search" || len(result.Data.Seasons) != 1 {
		t.Fatalf("响应结构不符：%s", recorder.Body.String())
	}
	item := result.Data.Weeks[0].Items[0]
	if item.Title != "星之旅" || item.URL != upstream.URL+"/Home/Bangumi/123" || item.Cover != upstream.URL+"/covers/show.jpg" {
		t.Fatalf("归一后的 Mikan 条目不符：%+v", item)
	}
}

func findRoute(t *testing.T, routes []server.Route, path string) server.Route {
	t.Helper()
	for _, route := range routes {
		if route.Path == path {
			return route
		}
	}
	t.Fatalf("找不到 route %s", path)
	return server.Route{}
}

func TestMikanPosterCatalogReturnsWeekAndSubscriptionStateInOneRequest(t *testing.T) {
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`<html><div class="sk-bangumi"><h2>星期日</h2><ul class="an-ul"><li><span data-src="/cover.jpg"></span><div class="an-info"><div class="date-text">2026/10/18 更新</div><a href="/Home/Bangumi/123">星期日作品</a></div></li></ul></div></html>`))
	}))
	defer upstream.Close()
	store := &memoryStore{config: map[string]any{"mikanHost": upstream.URL}, subs: []map[string]any{{"url": "https://mikanani.me/RSS/Bangumi?bangumiId=123&subgroupid=8"}}}
	client := discovery.New(store, upstream.Client(), nil)
	route := findRoute(t, client.Routes(), "/api/mikan")
	recorder := httptest.NewRecorder()
	route.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/mikan?metadata=false", strings.NewReader(`{}`)))
	var response struct {
		Code int `json:"code"`
		Data struct {
			Weeks []struct {
				Label string `json:"weekLabel"`
				Items []struct {
					Title      string `json:"title"`
					UpdateDate string `json:"updateDate"`
					Exists     bool   `json:"exists"`
				} `json:"items"`
			} `json:"weeks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || len(response.Data.Weeks) != 1 || len(response.Data.Weeks[0].Items) != 1 {
		t.Fatalf("目录响应 = %s", recorder.Body.String())
	}
	week := response.Data.Weeks[0]
	if requests != 1 || week.Label != "星期日" || week.Items[0].Title != "星期日作品" || !week.Items[0].Exists || week.Items[0].UpdateDate != "2026/10/18 更新" {
		t.Fatalf("轻量目录必须从单次目录请求带回星期和订阅状态：requests=%d, week=%+v", requests, week)
	}
}
