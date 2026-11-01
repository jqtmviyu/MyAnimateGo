package discovery_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/discovery"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestResolveSubscriptionCombinesMikanBGMDefaultsSubgroupAndEpisodeOffsets(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	upstream := testUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		switch r.URL.Path {
		case "/Home/Bangumi/3828":
			_, _ = io.WriteString(w, mikanDetailsHTML)
		case "/v0/subjects/123", "/bgm/subjects/123":
			_, _ = io.WriteString(w, bgmSubjectJSON)
		case "/v0/episodes":
			_, _ = io.WriteString(w, `{"data":[]}`)
		case "/3/search/tv":
			_, _ = io.WriteString(w, `{"results":[]}`)
		case "/main.xml":
			_, _ = io.WriteString(w, rssWithEpisode(3))
		case "/standby.xml":
			_, _ = io.WriteString(w, rssWithEpisode(7))
		default:
			http.NotFound(w, r)
		}
	})
	store := openSubscriptionStore(t, map[string]any{
		"mikanHost": upstream.URL, "bgmApi": upstream.URL, "cacheBaseURL": upstream.URL, "tmdbApi": upstream.URL,
		"downloadPathTemplate": "/tmp/${title}/Season ${season}", "completedPathTemplate": "/tmp/completed",
		"renameTemplate": "[${subgroup}] ${title} S${seasonFormat}E${episodeFormat}", "offset": true,
		"standbyRss": true, "downloadNew": true, "enabledExclude": true, "importExclude": true,
		"exclude": []any{"720[Pp]", "全局过滤"}, "rssTimeout": 5,
	})
	client := discovery.New(store, upstream.Client(), nil)
	previewer := newPreviewer(t, store, upstream.Client())
	resolved, err := client.ResolveSubscription(context.Background(), map[string]any{
		"url":            upstream.URL + "/main.xml?bangumiId=3828&subgroupid=370",
		"standbyRssList": []any{map[string]any{"label": "备用组", "url": upstream.URL + "/standby.xml", "offset": 99}},
	}, previewer)
	if err != nil {
		t.Fatal(err)
	}
	if resolved["type"] != "mikan" || resolved["enable"] != true || resolved["downloadNew"] != true || resolved["globalExclude"] != true {
		t.Fatalf("resolved source/global defaults = %#v", resolved)
	}
	if resolved["mikanTitle"] != "蜜柑作品" || resolved["bgmUrl"] != "https://bgm.tv/subject/123" || resolved["subgroup"] != "字幕组甲" {
		t.Fatalf("resolved Mikan/BGM association = %#v", resolved)
	}
	if resolved["title"] != "作品中文" || fmt.Sprint(resolved["season"]) != "2" || fmt.Sprint(resolved["offset"]) != "-2" {
		t.Fatalf("resolved Bangumi defaults or main RSS offset = %#v", resolved)
	}
	standby := resolved["standbyRssList"].([]any)
	if len(standby) != 1 || standby[0].(map[string]any)["offset"] != -4 {
		t.Fatalf("resolved standby RSS offsets = %#v", standby)
	}
	exclude := resolved["exclude"].([]any)
	wantExclude := []any{"720[Pp]", "全局过滤", `\d-\d`, "合集", "特别篇"}
	if !reflect.DeepEqual(exclude, wantExclude) {
		t.Fatalf("importExclude merge = %#v, want %#v", exclude, wantExclude)
	}
	mu.Lock()
	defer mu.Unlock()
	if !containsRequest(requests, "GET /Home/Bangumi/3828") || !containsRequest(requests, "GET /main.xml") || !containsRequest(requests, "GET /standby.xml") {
		t.Fatalf("Mikan detail and both RSS feeds must be queried: %v", requests)
	}
}

func TestResolveSubscriptionSupportsAniBTAnimeGardenAndOtherSources(t *testing.T) {
	upstream := testUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/subjects/123", "/bgm/subjects/123":
			_, _ = io.WriteString(w, bgmSubjectJSON)
		case "/v0/episodes":
			_, _ = io.WriteString(w, `{"data":[]}`)
		case "/3/search/tv":
			_, _ = io.WriteString(w, `{"results":[]}`)
		default:
			http.NotFound(w, r)
		}
	})
	store := openSubscriptionStore(t, map[string]any{
		"bgmApi": upstream.URL, "cacheBaseURL": upstream.URL, "tmdbApi": upstream.URL,
		"downloadPathTemplate": "/tmp/${title}/Season ${season}", "completedPathTemplate": "/tmp/completed",
		"renameTemplate": "[${subgroup}] ${title} S${seasonFormat}E${episodeFormat}", "offset": false,
	})
	client := discovery.New(store, upstream.Client(), nil)
	for _, tc := range []struct {
		name  string
		input map[string]any
		kind  string
		group string
	}{
		{name: "ani-bt", input: map[string]any{"url": "https://anibt.net/rss?bgmId=123&groupSlug=group-a", "type": "ani-bt"}, kind: "ani-bt", group: "group-a"},
		{name: "anime-garden", input: map[string]any{"url": "https://animes.garden/feed.xml?subject=123&fansub=组乙", "type": "anime-garden"}, kind: "anime-garden", group: "组乙"},
		{name: "other", input: map[string]any{"url": "https://other.example/rss.xml", "type": "other", "bgmUrl": "https://bgm.tv/subject/123", "subgroup": "外部组"}, kind: "other", group: "外部组"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := client.ResolveSubscription(context.Background(), tc.input, nil)
			if err != nil {
				t.Fatal(err)
			}
			if resolved["type"] != tc.kind || resolved["subgroup"] != tc.group || resolved["bgmUrl"] != "https://bgm.tv/subject/123" || resolved["title"] != "作品中文" {
				t.Fatalf("resolved %s subscription = %#v", tc.name, resolved)
			}
		})
	}
}

func TestPublicDiscoveryWrappersUseTheSameFixtureBackedSearchesAndGroups(t *testing.T) {
	upstream := testUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Home/Search":
			_, _ = io.WriteString(w, `<html><ul class="an-ul"><li><span data-src="/cover.jpg"></span><a href="/Home/Bangumi/9">搜索作品</a></li></ul></html>`)
		case "/Home/Bangumi/2":
			_, _ = io.WriteString(w, mikanDetailsHTML)
		case "/api/seasons/anime":
			_, _ = io.WriteString(w, `{"data":{"currentSeason":"2026春","availableSeasons":["2026春"],"byWeekday":[{"weekday":1,"weekdayLabel":"星期一","animes":[{"bgmId":"123","rating":8,"rssReleaseCount":1,"title":{"primary":"AniBT作品"}}]}]}}`)
		case "/api/anime/groups":
			_, _ = io.WriteString(w, `{"data":{"groups":[{"slug":"group-a","name":"字幕组A","items":[]}]}}`)
		case "/subjects":
			_, _ = io.WriteString(w, `{"subjects":[]}`)
		case "/resources":
			_, _ = io.WriteString(w, `{"resources":[]}`)
		case "/bgm/cover":
			_, _ = io.WriteString(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	})
	store := openSubscriptionStore(t, map[string]any{
		"mikanHost": upstream.URL, "aniBTBaseURL": upstream.URL, "animeGardenBaseURL": upstream.URL,
	})
	client := discovery.New(store, upstream.Client(), nil)
	client.SetSubscriptionPreviewer(newPreviewer(t, store, upstream.Client()))
	ctx := context.Background()
	mikanValue, err := client.SearchMikan(ctx, "搜索", nil)
	if err != nil {
		t.Fatal(err)
	}
	mikanJSON := encodeAny(t, mikanValue)
	if mikanJSON["totalItem"] != float64(1) {
		t.Fatalf("Mikan public wrapper result = %#v", mikanJSON)
	}
	groups, err := client.GetMikanGroups(ctx, upstream.URL+"/Home/Bangumi/2")
	if err != nil || reflect.ValueOf(groups).Len() != 1 {
		t.Fatalf("Mikan groups wrapper = %#v, %v", groups, err)
	}
	aniBT, err := client.SearchAniBT(ctx, discovery.AniBTSearchInput{Title: "AniBT作品"})
	if err != nil || encodeAny(t, aniBT)["currentSeason"] != "2026春" {
		t.Fatalf("AniBT search wrapper = %#v, %v", aniBT, err)
	}
	aniBTGroups, err := client.GetAniBTGroups(ctx, "123")
	if err != nil || reflect.ValueOf(aniBTGroups).Len() != 1 {
		t.Fatalf("AniBT group wrapper = %#v, %v", aniBTGroups, err)
	}
	if _, err := client.SearchAnimeGarden(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if groups, err := client.GetAnimeGardenGroups(ctx, "123"); err != nil || reflect.ValueOf(groups).Len() != 0 {
		t.Fatalf("AnimeGarden group wrapper = %#v, %v", groups, err)
	}
}

func TestRSSToAniHTTPRouteUsesSharedResolutionMethod(t *testing.T) {
	upstream := testUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/subjects/123", "/bgm/subjects/123":
			_, _ = io.WriteString(w, bgmSubjectJSON)
		case "/v0/episodes":
			_, _ = io.WriteString(w, `{"data":[]}`)
		case "/3/search/tv":
			_, _ = io.WriteString(w, `{"results":[]}`)
		default:
			http.NotFound(w, r)
		}
	})
	store := openSubscriptionStore(t, map[string]any{
		"bgmApi": upstream.URL, "cacheBaseURL": upstream.URL, "tmdbApi": upstream.URL,
		"downloadPathTemplate": "/tmp/${title}/Season ${season}", "completedPathTemplate": "/tmp/completed",
		"renameTemplate": "[${subgroup}] ${title} S${seasonFormat}E${episodeFormat}",
	})
	application, err := server.New(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	client := discovery.New(store, upstream.Client(), nil)
	if err := application.Register(client.Routes()...); err != nil {
		t.Fatal(err)
	}
	web := httptest.NewServer(application)
	defer web.Close()
	input := map[string]any{"url": "https://other.example/feed.xml", "type": "other", "bgmUrl": "https://bgm.tv/subject/123", "subgroup": "外部组"}
	direct, err := client.ResolveSubscription(context.Background(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	directJSON := encodeAny(t, direct)
	token := loginDiscovery(t, web.URL)
	result := postDiscovery(t, web.URL, "/api/rssToAni", token, input)
	if result["code"] != float64(200) {
		t.Fatalf("rssToAni result = %#v", result)
	}
	data := result["data"].(map[string]any)
	for _, field := range []string{"url", "type", "bgmUrl", "subgroup", "title", "season", "enable", "globalExclude", "downloadNew"} {
		if !reflect.DeepEqual(data[field], directJSON[field]) {
			t.Errorf("HTTP rssToAni %s = %#v, ResolveSubscription = %#v", field, data[field], directJSON[field])
		}
	}
}

const bgmSubjectJSON = `{"id":"123","name":"Series","name_cn":"作品中文","eps":12,"date":"2026-01-02","platform":"TV","images":{"small":""},"tags":[{"name":"Season 2"}],"rating":{"score":8.1}}`

const mikanDetailsHTML = `<html><div class="content"><img src="/cover.jpg"></div><div class="bangumi-title">蜜柑作品</div><div class="bangumi-info">Bangumi番组计划链接：<a href="https://bgm.tv/subject/123">作品</a></div><div class="leftbar-item"><a class="subgroup-name" data-anchor="#370">字幕组甲</a><span class="date">周一</span></div><div id="370"><a class="mikan-rss" href="/main.xml"></a></div><div><table><tbody><tr><td><a>标题</a><a data-clipboard-text="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"></a><a href="/episode.torrent">种子</a><td>size</td><td>1 MiB</td><td>2026-01-02</td></tr></tbody></table></div></html>`

func rssWithEpisode(episode int) string {
	return fmt.Sprintf(`<rss><channel><item><title>[字幕组甲] 作品 - %02d [1080p].mkv</title><enclosure url="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567" length="1234"/></item></channel></rss>`, episode)
}

func testUpstream(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	return upstream
}

func openSubscriptionStore(t *testing.T, config map[string]any) *storage.Store {
	t.Helper()
	store, err := storage.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(context.Background(), config); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newPreviewer(t *testing.T, store *storage.Store, client *http.Client) *pipeline.Engine {
	t.Helper()
	engine, err := pipeline.New(store, pipeline.Options{
		RSSFetcher: func(ctx context.Context, feedURL string) ([]byte, error) {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
			if err != nil {
				return nil, err
			}
			response, err := client.Do(request)
			if err != nil {
				return nil, err
			}
			defer response.Body.Close()
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				return nil, fmt.Errorf("RSS fixture returned HTTP %d", response.StatusCode)
			}
			return io.ReadAll(response.Body)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func containsRequest(requests []string, fragment string) bool {
	for _, request := range requests {
		if strings.Contains(request, fragment) {
			return true
		}
	}
	return false
}

func encodeAny(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func loginDiscovery(t *testing.T, baseURL string) string {
	t.Helper()
	result := postDiscovery(t, baseURL, "/api/login", "", map[string]any{"username": "admin", "password": "21232f297a57a5a743894a0e4a801fc3"})
	token, _ := result["data"].(string)
	if result["code"] != float64(200) || token == "" {
		t.Fatalf("login result = %#v", result)
	}
	return token
}

func postDiscovery(t *testing.T, baseURL, path, token string, body any) map[string]any {
	t.Helper()
	data, _ := json.Marshal(body)
	request, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestResolveSubscriptionDoesNotWaitForSlowMetadataFallback(t *testing.T) {
	upstream := testUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/subjects/123":
			_, _ = io.WriteString(w, bgmSubjectJSON)
		case "/bgm/subjects/123":
			<-r.Context().Done()
		case "/v0/episodes":
			_, _ = io.WriteString(w, `{"data":[]}`)
		case "/3/search/tv":
			_, _ = io.WriteString(w, `{"results":[]}`)
		default:
			http.NotFound(w, r)
		}
	})
	store := openSubscriptionStore(t, map[string]any{"bgmApi": upstream.URL, "cacheBaseURL": upstream.URL, "tmdbApi": upstream.URL})
	client := discovery.New(store, upstream.Client(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resolved, err := client.ResolveSubscription(ctx, map[string]any{"url": "https://mikanani.me/RSS/Bangumi?bangumiId=1&subgroupid=2", "bgmUrl": "https://bgm.tv/subject/123", "subgroup": "字幕组"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("官方资料已返回，解析仍等待备用缓存直到超时")
	}
	if resolved["title"] != "作品中文" {
		t.Fatalf("official subject metadata lost: %v", resolved["title"])
	}
}
