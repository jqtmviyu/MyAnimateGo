package discovery

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAniBTCatalogKeepsScheduleAndUpcomingWorks(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, `{"ok":true,"data":{"total":2,"limit":200,"truncated":true,"scope":{"kind":"season"},"availableSeasons":["2026秋"],"byWeekday":[{"weekday":1,"weekdayLabel":"周一","animes":[{"bgmId":123,"episodes":12,"airingAt":1792980000,"rssReleaseCount":0,"title":{"primary":"未发布作品","japanese":"原名"}},{"bgmId":456,"rssReleaseCount":10,"title":{"primary":"已有资源"}}]}]}}`)
	}))
	defer upstream.Close()
	c := New(&proxyStore{config: map[string]any{"aniBTBaseURL": upstream.URL}}, nil, nil)
	defer c.Close()
	for range 2 {
		result, err := c.searchAniBT(context.Background(), "2026秋", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(result.ByWeekday) != 1 || len(result.ByWeekday[0].Animes) != 2 {
			t.Fatal("时间表丢失暂无资源作品")
		}
		if !result.Truncated || result.Total != 2 || result.ByWeekday[0].Animes[0].AiringAt == 0 || result.ByWeekday[0].Animes[0].Episodes != 12 {
			t.Fatal("时间表或截断字段缺失")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("目录未复用短缓存")
	}
	ctx := context.WithValue(context.Background(), sourceRefreshKey{}, true)
	if _, err := c.searchAniBT(ctx, "2026秋", "", ""); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("显式刷新未读取上游")
	}
}

func TestAniBTGroupsKeepResourceMetadataAndGenerateRSS(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":true,"data":{"groups":[{"slug":"fansub-a","name":"字幕组","totalReleases":40,"publishedEpisodes":[1,2.5],"items":[{"releaseId":"rel_1","title":"[字幕组] 作品 [01][1080p]","size":1048576,"format":"MKV","customTags":["HEVC"],"publishedAt":1786863045049}]}]}}`)
	}))
	defer upstream.Close()
	c := New(&proxyStore{config: map[string]any{"aniBTBaseURL": upstream.URL}}, nil, nil)
	defer c.Close()
	groups, err := c.aniBTGroups(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	g := groups[0]
	if g.TotalReleases != 40 || len(g.PublishedEpisodes) != 2 || len(g.Items) != 1 || g.Items[0].Format != "MKV" {
		t.Fatal("字幕组资源字段缺失")
	}
	if !strings.Contains(g.RSS, "bgmId=123") || !strings.Contains(g.RSS, "groupSlug=fansub-a") || g.Items[0].Torrent != upstream.URL+"/api/torrent/rel_1.torrent" {
		t.Fatalf("订阅或下载地址错误：%+v", g)
	}
	if len(g.GroupRegex.RegexList) == 0 {
		t.Fatal("未生成资源匹配规则")
	}
}
