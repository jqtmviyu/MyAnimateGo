package discovery_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/discovery"
)

func TestMikanDirectoryReusesSourceAndAllowsExplicitRefresh(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		io.WriteString(w, `<html><ul class="an-ul"></ul></html>`)
	}))
	defer upstream.Close()
	store := openSubscriptionStore(t, map[string]any{"mikanHost": upstream.URL})
	client := discovery.New(store, upstream.Client(), nil)
	for i := 0; i < 2; i++ {
		if _, err := client.SearchMikan(context.Background(), "", nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := requests.Load(); n != 1 {
		t.Fatalf("相同目录打开两次抓取了 %d 次，预期复用一次来源请求", n)
	}
}

func TestRepeatedRSSResolutionReusesMetadataButBuildsFreshDraft(t *testing.T) {
	var subjects, episodes, tmdbRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/subjects/123":
			subjects.Add(1)
			io.WriteString(w, bgmSubjectJSON)
		case "/bgm/subjects/123":
			io.WriteString(w, bgmSubjectJSON)
		case "/v0/episodes":
			episodes.Add(1)
			io.WriteString(w, `{"data":[]}`)
		case "/3/search/tv":
			tmdbRequests.Add(1)
			io.WriteString(w, `{"results":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	store := openSubscriptionStore(t, map[string]any{"bgmApi": upstream.URL, "cacheBaseURL": upstream.URL, "tmdbApi": upstream.URL})
	client := discovery.New(store, upstream.Client(), nil)
	input := map[string]any{"url": "https://mikanani.me/RSS/Bangumi?bangumiId=1&subgroupid=1", "bgmUrl": "https://bgm.tv/subject/123", "subgroup": "甲"}
	first, err := client.ResolveSubscription(context.Background(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	first["title"] = "用户自己的草稿"
	input["subgroup"] = "乙"
	second, err := client.ResolveSubscription(context.Background(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second["title"] == first["title"] || second["subgroup"] != "乙" || second["id"] == first["id"] {
		t.Fatalf("复用元数据时污染了新草稿")
	}
	if subjects.Load() != 1 || episodes.Load() != 1 || tmdbRequests.Load() != 1 {
		t.Fatalf("重复解析仍请求资料：subject=%d episodes=%d", subjects.Load(), episodes.Load())
	}
}

func TestColdRSSResolutionLoadsIndependentMetadataTogether(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	release := make(chan struct{})
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/subjects/123", "/bgm/subjects/123":
			fmt.Fprintf(w, `{"id":"123","name_cn":"作品","eps":12,"platform":"TV","images":{"large":%q}}`, upstream.URL+"/cover.jpg")
			return
		case "/v0/episodes", "/cover.jpg", "/3/search/tv":
			mu.Lock()
			seen[r.URL.Path] = true
			if len(seen) == 3 {
				close(release)
			}
			mu.Unlock()
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			switch r.URL.Path {
			case "/v0/episodes":
				io.WriteString(w, `{"data":[]}`)
			case "/cover.jpg":
				io.WriteString(w, "cover-fixture")
			default:
				io.WriteString(w, `{"results":[]}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	store := openSubscriptionStore(t, map[string]any{"bgmApi": upstream.URL, "cacheBaseURL": upstream.URL, "tmdbApi": upstream.URL, "bgmImageSize": "large"})
	client := discovery.New(store, upstream.Client(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := client.ResolveSubscription(ctx, map[string]any{"url": "https://mikanani.me/RSS/Bangumi?bangumiId=1", "bgmUrl": "https://bgm.tv/subject/123", "subgroup": "甲"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("集数、封面与 TMDB 仍串行等待，未在截止前同时发出")
	}
}

type resolutionPreviewCounter struct{ calls int }

func (p *resolutionPreviewCounter) Preview(context.Context, map[string]any) (map[string]any, error) {
	p.calls++
	return map[string]any{"items": []any{map[string]any{"title": "[字幕组] 作品 - 03", "episode": 3}}}, nil
}
func TestResolutionReadsMainFeedOnceForSubgroupAndOffset(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/subjects/123", "/bgm/subjects/123":
			io.WriteString(w, bgmSubjectJSON)
		case "/v0/episodes":
			io.WriteString(w, `{"data":[]}`)
		default:
			io.WriteString(w, `{"results":[]}`)
		}
	}))
	defer upstream.Close()
	store := openSubscriptionStore(t, map[string]any{"bgmApi": upstream.URL, "cacheBaseURL": upstream.URL, "tmdbApi": upstream.URL, "offset": true})
	c := discovery.New(store, upstream.Client(), nil)
	preview := &resolutionPreviewCounter{}
	result, err := c.ResolveSubscription(context.Background(), map[string]any{"url": "https://example.invalid/rss", "type": "other", "bgmUrl": "https://bgm.tv/subject/123"}, preview)
	if err != nil {
		t.Fatal(err)
	}
	if preview.calls != 1 || result["subgroup"] != "字幕组" || result["offset"] != -2 {
		t.Fatalf("主 RSS 未复用或推断结果改变：calls=%d", preview.calls)
	}
}
func TestCachedMikanDirectoryUsesLatestSubscriptionState(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			io.WriteString(w, `<html><ul class="an-ul"><li><a href="/Home/Bangumi/1">作品</a></li></ul></html>`)
		case "/Home/Bangumi/1":
			io.WriteString(w, mikanDetailsHTML)
		case "/v0/subjects/123", "/bgm/subjects/123":
			io.WriteString(w, bgmSubjectJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	store := openSubscriptionStore(t, map[string]any{"mikanHost": upstream.URL, "bgmApi": upstream.URL, "cacheBaseURL": upstream.URL})
	c := discovery.New(store, upstream.Client(), nil)
	read := func() bool {
		result, err := c.SearchMikan(context.Background(), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(result)
		var out struct {
			Weeks []struct{ Items []struct{ Exists bool } }
		}
		json.Unmarshal(raw, &out)
		return out.Weeks[0].Items[0].Exists
	}
	if read() {
		t.Fatal("初次目录误报已订阅")
	}
	if err := store.PutSubscription(context.Background(), map[string]any{"id": "one", "url": "https://mikanani.me/RSS/Bangumi?bangumiId=1"}); err != nil {
		t.Fatal(err)
	}
	if !read() {
		t.Fatal("缓存目录未使用新订阅快照")
	}
	if err := store.DeleteSubscriptions(context.Background(), []string{"one"}); err != nil {
		t.Fatal(err)
	}
	if read() {
		t.Fatal("删除后缓存目录仍报已订阅")
	}
}
