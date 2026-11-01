package acceptance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/discovery"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

// 公开目录验收只查询元数据；真实账号、评分和下载操作另行授权。
func TestPublicDiscoveryLive(t *testing.T) {
	if os.Getenv("ANI_RSS_TEST_PUBLIC_SOURCES") != "true" {
		t.Skip("set ANI_RSS_TEST_PUBLIC_SOURCES=true to query public source APIs")
	}
	t.Run("Bangumi", func(t *testing.T) {
		t.Parallel()
		fixture := newLiveDiscovery(t)
		var entries []struct{ ID, Name string }
		fixture.post("/api/searchBgm?name="+url.QueryEscape("カウボーイビバップ"), nil, &entries)
		found := false
		for _, entry := range entries {
			found = found || entry.ID == "253"
		}
		if !found {
			t.Fatal("public search did not contain subject 253")
		}
		metadata, err := fixture.client.Metadata(fixture.ctx, map[string]any{"bgmUrl": "https://bgm.tv/subject/253", "season": 1}, 1)
		if err != nil || metadata["jpTitle"] != "カウボーイビバップ" || metadata["bgmJpEpisodeTitle"] == "第1集" {
			t.Fatalf("subject/episode metadata was not resolved: %v, %v", metadata, err)
		}
		t.Logf("subject 253 found among %d results; public subject and episode titles resolved", len(entries))
	})
	t.Run("AniList", func(t *testing.T) {
		t.Parallel()
		fixture := newLiveDiscovery(t)
		name, err := fixture.client.AniListRomaji(fixture.ctx, "カウボーイビバップ")
		if err != nil || name != "Cowboy Bebop" {
			t.Fatalf("public romaji lookup = %q, %v", name, err)
		}
		t.Log("GraphQL query resolved Cowboy Bebop")
	})
	t.Run("AnimeGarden", func(t *testing.T) {
		t.Parallel()
		fixture := newLiveDiscovery(t)
		var weeks []struct {
			WeekLabel string
			Subjects  []struct{ ID, Name string }
		}
		fixture.post("/api/animeGardenList", nil, &weeks)
		id, total := "", 0
		for _, week := range weeks {
			for _, subject := range week.Subjects {
				if subject.ID == "" || subject.Name == "" {
					t.Fatal("public subject missing ID/name")
				}
				if id == "" {
					id = subject.ID
				}
				total++
			}
		}
		if total == 0 {
			t.Fatal("public subject list is empty")
		}
		var groups []liveResourceGroup
		fixture.post("/api/animeGardenGroup?bgmId="+url.QueryEscape(id), nil, &groups)
		assertLiveGroups(t, groups)
		t.Logf("%d subjects in %d weeks; subject %s has %d resource groups", total, len(weeks), id, len(groups))
	})
	t.Run("AniBT", func(t *testing.T) {
		t.Parallel()
		fixture := newLiveDiscovery(t)
		var result struct {
			ByWeekday []struct {
				Animes []struct {
					BgmID string
					Title struct{ Primary string }
				}
			}
		}
		fixture.post("/api/aniBT", map[string]any{}, &result)
		id, total := "", 0
		for _, week := range result.ByWeekday {
			for _, anime := range week.Animes {
				if anime.BgmID == "" || anime.Title.Primary == "" {
					t.Fatal("public anime missing Bangumi ID/primary title")
				}
				if id == "" {
					id = anime.BgmID
				}
				total++
			}
		}
		if total == 0 {
			t.Fatal("public season list is empty")
		}
		var groups []liveResourceGroup
		fixture.post("/api/aniBTGroup?bgmId="+url.QueryEscape(id), nil, &groups)
		assertLiveGroups(t, groups)
		t.Logf("%d anime; subject %s has %d resource groups", total, id, len(groups))
	})
	t.Run("Mikan", func(t *testing.T) {
		t.Parallel()
		fixture := newLiveDiscovery(t)
		var result struct {
			TotalItem int
			Weeks     []struct {
				Items []struct{ Title, URL string }
			}
		}
		fixture.post("/api/mikan", nil, &result)
		first, total := "", 0
		for _, week := range result.Weeks {
			for _, item := range week.Items {
				if item.Title == "" || item.URL == "" {
					t.Fatal("public Mikan item missing title/URL")
				}
				if first == "" {
					first = item.URL
				}
				total++
			}
		}
		if total == 0 || result.TotalItem != total {
			t.Fatalf("public Mikan item count = %d/%d", result.TotalItem, total)
		}
		var groups []liveResourceGroup
		fixture.post("/api/mikanGroup?url="+url.QueryEscape(first), nil, &groups)
		assertLiveGroups(t, groups)
		t.Logf("%d anime; first title has %d resource groups", total, len(groups))
	})
}

type liveResourceGroup struct {
	RSS   string
	Items []struct{ Title string }
}

func assertLiveGroups(t *testing.T, groups []liveResourceGroup) {
	t.Helper()
	if len(groups) == 0 {
		t.Fatal("public resource groups are empty")
	}
	resources := 0
	for _, group := range groups {
		parsed, err := url.Parse(group.RSS)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			t.Fatal("resource group has no absolute HTTP feed URL")
		}
		for _, item := range group.Items {
			if item.Title == "" {
				t.Fatal("resource title is empty")
			}
			resources++
		}
	}
	if resources == 0 {
		t.Fatal("public resource groups contain no resources")
	}
}

type liveDiscovery struct {
	t      *testing.T
	ctx    context.Context
	client *discovery.Client
	base   string
}

func newLiveDiscovery(t *testing.T) *liveDiscovery {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SaveConfig(ctx, map[string]any{"apiKey": "public-source-acceptance-only", "bgmApi": "https://api.bgm.tv", "automaticTasksPaused": true}); err != nil {
		t.Fatal(err)
	}
	api, err := server.New(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if address := os.Getenv("ANI_RSS_TEST_PUBLIC_PROXY"); address != "" {
		proxy, err := url.Parse(address)
		if err != nil || proxy.Host == "" {
			t.Fatal("ANI_RSS_TEST_PUBLIC_PROXY must contain a valid proxy URL")
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	t.Cleanup(transport.CloseIdleConnections)
	client := discovery.New(store, liveDiscoveryHTTP{t: t, client: &http.Client{Transport: transport, Timeout: 20 * time.Second}}, nil)
	if err := api.Register(client.Routes()...); err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(api)
	t.Cleanup(host.Close)
	return &liveDiscovery{t: t, ctx: ctx, client: client, base: host.URL}
}

type liveDiscoveryHTTP struct {
	t      *testing.T
	client *http.Client
}

func (d liveDiscoveryHTTP) Do(request *http.Request) (*http.Response, error) {
	if request.Header.Get("Authorization") != "" {
		return nil, fmt.Errorf("public acceptance must not send account credentials")
	}
	response, err := d.client.Do(request)
	if err == nil {
		d.t.Logf("%s %s%s HTTP %d", request.Method, request.URL.Host, request.URL.EscapedPath(), response.StatusCode)
	}
	return response, err
}

func (f *liveDiscovery) post(path string, body any, target any) {
	f.t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.base+path, bytes.NewReader(encoded))
	if err != nil {
		f.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("api-key", "public-source-acceptance-only")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope struct {
		Code    int
		Message string
		Data    json.RawMessage
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&envelope); err != nil {
		f.t.Fatal(err)
	}
	if envelope.Code != 200 {
		message := strings.TrimSpace(envelope.Message)
		if len(message) > 300 {
			message = message[:300]
		}
		f.t.Fatalf("source query code=%d: %s", envelope.Code, message)
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		f.t.Fatal(err)
	}
}
