package discovery_test

import (
	"bytes"
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

type entitlementStub bool

func (e entitlementStub) Verify(context.Context, map[string]any) (bool, error) { return bool(e), nil }

func TestDiscoveryRoutesUseFixturesForAllExternalBoundaries(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/seasons/anime":
			if r.URL.Query().Get("query") != "query" || r.URL.Query().Get("season") != "" || r.URL.Query().Get("bgmId") != "" {
				t.Errorf("AniBT query 参数错误：%s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":{"currentSeason":"2025春","requestedSeason":"","availableSeasons":["2025春"],"byWeekday":[{"weekday":1,"weekdayLabel":"星期一","animes":[{"animeId":"a1","bgmId":123,"rating":8.5,"rssReleaseCount":2,"title":{"primary":"作品"}}]}]}}`))
		case r.URL.Path == "/api/anime/groups":
			_, _ = w.Write([]byte(`{"data":{"groups":[{"slug":"group-x","name":"组","items":[{"title":"[组] 第01话 1080p","size":1048576}]}]}}`))
		case r.URL.Path == "/subjects":
			_, _ = w.Write([]byte(`{"subjects":[{"id":123,"name":"作品","isArchived":false,"activedAt":"2025-01-06T00:00:00Z"}]}`))
		case r.URL.Path == "/resources":
			_, _ = w.Write([]byte(`{"resources":[{"id":9007199254740993,"subjectId":123,"title":"[组] 第01话 720p","size":1048576,"createdAt":"2025-01-06T00:00:00Z","publisher":{"id":42,"name":"发布者"},"fansub":{"id":789,"name":"组"}}]}`))
		case r.URL.Path == "/bgm/cover":
			_, _ = w.Write([]byte(`{}`))
		case r.URL.Path == "/bgm/subjects/123":
			_, _ = w.Write([]byte(`{"id":"123","name":"作品","name_cn":"作品中文","eps":12,"date":"2025-01-01","images":{"small":"https://img/cover.jpg"},"platform":"TV","rating":{"score":8.1}}`))
		case strings.HasPrefix(r.URL.Path, "/search/subject/"):
			_, _ = w.Write([]byte(`{"list":[{"id":"123","url":"https://bgm.tv/subject/123","name":"作品","name_cn":"作品中文","eps":12,"date":"2025-01-01","images":{"small":"https://img/cover.jpg"},"platform":"TV","rating":{"score":8.1}}]}`))
		case r.URL.Path == "/v0/subjects/123":
			_, _ = w.Write([]byte(`{"id":"123","name":"作品","name_cn":"作品中文","eps":12,"date":"2025-01-01","images":{"small":"https://img/cover.jpg"},"platform":"TV","tags":[{"name":"Season 2"}],"rating":{"score":8.1}}`))
		case r.URL.Path == "/v0/episodes":
			_, _ = w.Write([]byte(`{"data":[],"total":0,"limit":1000,"offset":0}`))
		case r.URL.Path == "/3/tv/123":
			_, _ = w.Write([]byte(`{"id":123,"name":"TV 名","original_name":"Original","first_air_date":"2025-01-01"}`))
		case r.URL.Path == "/3/tv/123/episode_groups":
			_, _ = w.Write([]byte(`{"results":[{"id":"g1","name":"Season One"}]}`))
		case r.URL.Path == "/3/search/tv":
			_, _ = w.Write([]byte(`{"results":[{"id":123,"name":"TV 名","original_name":"Original","first_air_date":"2025-01-01"}]}`))
		case r.URL.Path == "/graphql":
			var request struct {
				Variables struct {
					Search string `json:"search"`
				} `json:"variables"`
			}
			_ = json.NewDecoder(r.Body).Decode(&request)
			if request.Variables.Search != `名称 "safe"` {
				t.Errorf("AniList search 未正确 JSON 编码：%q", request.Variables.Search)
			}
			_, _ = w.Write([]byte(`{"data":{"Page":{"media":[{"title":{"romaji":"Name Romaji","native":"名前"}}]}}}`))
		default:
			t.Errorf("意外的 fixture 请求：%s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	store := &memoryStore{config: map[string]any{
		"mikanHost": upstream.URL, "aniBTBaseURL": upstream.URL, "animeGardenBaseURL": upstream.URL,
		"bgmApi": upstream.URL, "tmdbApi": upstream.URL, "tmdbApiKey": "fixture-key", "tmdbLanguage": "zh-CN", "cacheBaseURL": upstream.URL,
		"aniListBaseURL": upstream.URL + "/graphql",
	}, subs: []map[string]any{{"id": "sub1", "bgmUrl": "https://bgm.tv/subject/123", "url": "https://mikanani.me/RSS?bangumiId=123"}}}
	client := discovery.New(store, upstream.Client(), nil)
	routes := map[string]server.Route{}
	for _, route := range client.Routes() {
		routes[route.Path] = route
	}

	call := func(path, query string, body any) map[string]any {
		t.Helper()
		encoded, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path+query, bytes.NewReader(encoded))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		routes[path].Handler.ServeHTTP(recorder, req)
		var result map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatalf("%s JSON: %v %s", path, err, recorder.Body.String())
		}
		if result["code"] != float64(200) {
			t.Fatalf("%s failed: %s", path, recorder.Body.String())
		}
		return result
	}
	if got := call("/api/aniBT", "", map[string]any{"title": "query", "season": "ignored", "bgmUrl": "https://bgm.tv/subject/88"}); len(got["data"].(map[string]any)["byWeekday"].([]any)) != 1 {
		t.Fatalf("AniBT 结果：%v", got)
	} else {
		anime := got["data"].(map[string]any)["byWeekday"].([]any)[0].(map[string]any)["animes"].([]any)[0].(map[string]any)
		if anime["bgmId"] != "123" || anime["exists"] != true {
			t.Fatalf("AniBT 数字 ID 未保持前端字符串及订阅关联：%v", anime)
		}
	}
	if got := call("/api/aniBTGroup", "?bgmId=123", nil); len(got["data"].([]any)) != 1 {
		t.Fatalf("AniBT group：%v", got)
	}
	if got := call("/api/animeGardenList", "", nil); len(got["data"].([]any)) != 1 {
		t.Fatalf("AnimeGarden list：%v", got)
	} else {
		subject := got["data"].([]any)[0].(map[string]any)["subjects"].([]any)[0].(map[string]any)
		if subject["id"] != "123" || subject["isArchived"] != "false" || subject["exists"] != true {
			t.Fatalf("AnimeGarden 字段未保持前端字符串及订阅关联：%v", subject)
		}
	}
	if got := call("/api/animeGardenGroup", "?bgmId=123", nil); len(got["data"].([]any)) != 1 {
		t.Fatalf("AnimeGarden group：%v", got)
	} else {
		group := got["data"].([]any)[0].(map[string]any)
		item := group["items"].([]any)[0].(map[string]any)
		if group["id"] != "789" || item["id"] != "9007199254740993" || item["subjectId"] != "123" || item["publisher"].(map[string]any)["id"] != "42" {
			t.Fatalf("AnimeGarden 资源 ID 未精确保留：%v", group)
		}
	}
	if got := call("/api/searchBgm", "?name=query", nil); len(got["data"].([]any)) != 1 {
		t.Fatalf("BGM search：%v", got)
	}
	if got := call("/api/getBgmTitle", "", map[string]any{"bgmUrl": "https://bgm.tv/subject/123"}); got["data"] != "作品中文" {
		t.Fatalf("BGM title：%v", got)
	}
	if got := call("/api/getThemoviedbName", "", map[string]any{"tmdbId": "123", "ova": false}); got["data"].(map[string]any)["themoviedbName"] != "TV 名" {
		t.Fatalf("TMDB name：%v", got)
	}
	if got := call("/api/getThemoviedbGroup", "", map[string]any{"tmdb": map[string]any{"id": "123"}}); len(got["data"].([]any)) != 1 {
		t.Fatalf("TMDB groups：%v", got)
	}
	if got := call("/api/getAniBySubjectId", "?id=123", nil); got["data"].(map[string]any)["bgmUrl"] != "https://bgm.tv/subject/123" {
		t.Fatalf("Ani 草稿：%v", got)
	}
	if _, err := client.AniListRomaji(context.Background(), `名称 "safe"`); err != nil {
		t.Fatalf("AniList 查询：%v", err)
	}
}

func TestBgmSearchDistinguishesNotFoundFromUpstreamFailures(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantCode   int
		wantLength int
	}{
		{name: "http not found", status: http.StatusNotFound, body: `{"error":"not found"}`, wantCode: 200, wantLength: 0},
		{name: "business not found", status: http.StatusOK, body: `{"code":404}`, wantCode: 200, wantLength: 0},
		{name: "server failure", status: http.StatusInternalServerError, body: `{"error":"unavailable"}`, wantCode: 500},
		{name: "malformed success", status: http.StatusOK, body: `{broken`, wantCode: 500},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer upstream.Close()
			client := discovery.New(&memoryStore{config: map[string]any{"bgmApi": upstream.URL}}, upstream.Client(), nil)
			var search server.Route
			for _, route := range client.Routes() {
				if route.Path == "/api/searchBgm" {
					search = route
				}
			}
			recorder := httptest.NewRecorder()
			search.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/searchBgm?name=title", nil))
			var result struct {
				Code int               `json:"code"`
				Data []json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Code != test.wantCode {
				t.Fatalf("code=%d, want %d; response=%s", result.Code, test.wantCode, recorder.Body.String())
			}
			if test.wantCode == 200 && len(result.Data) != test.wantLength {
				t.Fatalf("data length=%d, want %d; response=%s", len(result.Data), test.wantLength, recorder.Body.String())
			}
		})
	}
}

func TestMikanGroupRouteParsesGroupsWithoutUsingOtherHosts(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<div class="bangumi-info">Bangumi番组计划链接：<a href="https://bgm.tv/subject/123">link</a></div><div class="leftbar-item"><a class="subgroup-name" data-anchor="#g1">Fansub</a><span class="date">今日</span></div><div id="g1"><a class="mikan-rss" href="/RSS?subgroupid=8"></a></div><table><tbody><tr><td><a>标题 1080p</a><a data-clipboard-text="magnet:?xt=urn:btih:aa">magnet</a><a href="/seed.torrent">torrent</a></td><td></td><td>1 GB</td><td>2025-01-02</td></tr></tbody></table>`))
	}))
	defer upstream.Close()
	client := discovery.New(&memoryStore{config: map[string]any{"mikanHost": upstream.URL}}, upstream.Client(), nil)
	var route server.Route
	for _, item := range client.Routes() {
		if item.Path == "/api/mikanGroup" {
			route = item
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mikanGroup?url="+url.QueryEscape(upstream.URL+"/Home/Bangumi/123"), nil)
	recorder := httptest.NewRecorder()
	route.Handler.ServeHTTP(recorder, req)
	var result struct {
		Code int `json:"code"`
		Data []struct {
			Label string `json:"label"`
			RSS   string `json:"rss"`
			Items []struct {
				Magnet  string `json:"magnet"`
				Torrent string `json:"torrent"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Code != 200 || len(result.Data) != 1 || result.Data[0].Label != "Fansub" || len(result.Data[0].Items) != 1 || result.Data[0].Items[0].Magnet != "magnet:?xt=urn:btih:aa" || !strings.HasPrefix(result.Data[0].RSS, upstream.URL) {
		t.Fatalf("Mikan groups：%s", recorder.Body.String())
	}
}

func TestPublicAnimeGardenMetadataWithoutDonation(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/subjects":
			_, _ = w.Write([]byte(`{"subjects":[{"id":"123","name":"show","activedAt":"2025-01-06T00:00:00Z"}]}`))
		case "/v0/subjects/123":
			_, _ = w.Write([]byte(`{"id":123,"rating":{"score":8.8},"images":{"small":"https://img/cover.jpg"}}`))
		default:
			t.Errorf("不应请求作者缓存：%s", r.URL.Path)
		}
	}))
	defer upstream.Close()
	config := map[string]any{"animeGardenBaseURL": upstream.URL, "bgmApi": upstream.URL, "cacheBaseURL": upstream.URL}
	client := discovery.New(&memoryStore{config: config}, upstream.Client(), entitlementStub(false))
	var route server.Route
	for _, item := range client.Routes() {
		if item.Path == "/api/animeGardenList" {
			route = item
		}
	}
	for _, state := range []map[string]any{{}, {"outTradeNo": "expired", "expirationTime": 1}, {"tryOut": true}} {
		for key := range config {
			if key == "outTradeNo" || key == "expirationTime" || key == "tryOut" {
				delete(config, key)
			}
		}
		for key, value := range state {
			config[key] = value
		}
		recorder := httptest.NewRecorder()
		route.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/animeGardenList", nil))
		var result struct {
			Data []struct {
				Subjects []struct {
					Score float64 `json:"score"`
				} `json:"subjects"`
			} `json:"data"`
		}
		_ = json.Unmarshal(recorder.Body.Bytes(), &result)
		if len(result.Data) != 1 || result.Data[0].Subjects[0].Score != 8.8 {
			t.Fatalf("公开评分未默认启用：%s", recorder.Body.String())
		}
	}
	if calls != 4 {
		t.Fatalf("公共评分缓存未复用: calls=%d", calls)
	}
}

func TestPublicMikanMappingAddsBgmIDScoreAndSubscriptionState(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/Home/Search":
			_, _ = w.Write([]byte(`<ul class="an-ul"><li><span data-src="/cover.jpg"></span><a href="/Home/Bangumi/123">节目</a></li></ul>`))
		case r.URL.Path == "/Home/Bangumi/123":
			_, _ = w.Write([]byte(`<div class="bangumi-info">Bangumi番组计划链接：<a href="https://bgm.tv/subject/999">link</a></div>`))
		case r.URL.Path == "/v0/subjects/999":
			_, _ = w.Write([]byte(`{"id":999,"rating":{"score":8.8}}`))
		default:
			t.Errorf("意外请求 %s", r.URL.String())
		}
	}))
	defer upstream.Close()
	store := &memoryStore{config: map[string]any{"mikanHost": upstream.URL, "bgmApi": upstream.URL, "cacheBaseURL": upstream.URL}, subs: []map[string]any{{"id": "show", "url": "https://mikanani.me/RSS?bangumiId=123", "bgmUrl": "https://bgm.tv/subject/999"}}}
	client := discovery.New(store, upstream.Client(), entitlementStub(false))
	var route server.Route
	for _, candidate := range client.Routes() {
		if candidate.Path == "/api/mikan" {
			route = candidate
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mikan?text=节目", strings.NewReader(`{}`))
	recorder := httptest.NewRecorder()
	route.Handler.ServeHTTP(recorder, req)
	var response struct {
		Data struct {
			Weeks []struct {
				Items []struct {
					BgmID  string  `json:"bgmId"`
					Exists bool    `json:"exists"`
					Score  float64 `json:"score"`
				} `json:"items"`
			} `json:"weeks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Weeks) != 1 || len(response.Data.Weeks[0].Items) != 1 {
		t.Fatalf("响应结构：%s", recorder.Body.String())
	}
	item := response.Data.Weeks[0].Items[0]
	if item.BgmID != "999" || !item.Exists || item.Score != 8.8 {
		t.Fatalf("映射 enrichment 不符：%+v", item)
	}
}

func TestMetadataReturnsRuleTemplateValuesFromProvidedAndFixtureSources(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/3/tv/123/season/2":
			_, _ = w.Write([]byte(`{"episodes":[{"episode_number":1,"name":"TMDB 集标题"}]}`))
		case r.URL.Path == "/v0/episodes":
			_, _ = w.Write([]byte(`{"data":[{"id":"e1","ep":1,"sort":1,"type":0,"name":"BGM JP","name_cn":"BGM 中文"}]}`))
		default:
			t.Errorf("意外请求 %s", r.URL.String())
		}
	}))
	defer upstream.Close()
	client := discovery.New(&memoryStore{config: map[string]any{"bgmApi": upstream.URL, "tmdbApi": upstream.URL}}, upstream.Client(), nil)
	metadata, err := client.Metadata(context.Background(), map[string]any{"bgmUrl": "https://bgm.tv/subject/123", "season": 2, "tmdb": map[string]any{"id": "123"}, "jpTitle": "日本語名", "themoviedbName": "外部剧名"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"tmdbid": "123", "bgmId": "123", "jpTitle": "日本語名", "themoviedbName": "外部剧名", "episodeTitle": "TMDB 集标题", "bgmEpisodeTitle": "BGM 中文", "bgmJpEpisodeTitle": "BGM JP"}
	for key, value := range want {
		if metadata[key] != value {
			t.Fatalf("metadata[%s]=%q，期望 %q；全部值=%v", key, metadata[key], value, metadata)
		}
	}
}
