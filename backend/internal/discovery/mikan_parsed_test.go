package discovery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const parsedDetailFixture = `<html><div class="content"><img src="/cover.jpg"></div><div class="bangumi-title">作品</div><div class="bangumi-info">Bangumi番组计划链接：<a href="https://bgm.tv/subject/123">link</a></div><div class="leftbar-item"><a class="subgroup-name" data-anchor="#g1">字幕组</a><span class="date">今日</span></div><div id="g1"><a class="mikan-rss" href="/RSS?subgroupid=8"></a></div><table><tbody><tr><td><a>[字幕组] 作品 01 1080p</a><a data-clipboard-text="magnet:?xt=urn:btih:aa">magnet</a><a href="/seed.torrent">torrent</a></td><td></td><td>1 GB</td><td>2025-01-02</td></tr></tbody></table></html>`

func TestMikanCacheRetainsParsedDataInsteadOfHTML(t *testing.T) {
	html := "<html><ul><!--" + strings.Repeat("unused page layout ", 32<<10) + "--></ul></html>"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, html) }))
	defer upstream.Close()
	c := New(&proxyStore{config: map[string]any{"mikanHost": upstream.URL}}, nil, nil)
	defer c.Close()
	if _, err := c.searchMikan(context.Background(), "", nil, false); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.documentBytes >= len(html)/10 {
		t.Fatalf("仍保留大页 HTML：缓存 %d，原文 %d 字节", c.documentBytes, len(html))
	}
	for _, entry := range c.documents {
		if !json.Valid(entry.body) {
			t.Fatal("缓存没有保存解析结果")
		}
	}
}

func TestMikanDetailCacheIsolationAndRefresh(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); io.WriteString(w, parsedDetailFixture) }))
	defer upstream.Close()
	c := New(nil, nil, nil)
	defer c.Close()
	config := map[string]any{"mikanHost": upstream.URL}
	a, err := c.mikanInfo(context.Background(), config, "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Groups) != 1 || len(a.Groups[0].Items) != 1 {
		t.Fatalf("详情缺少资源：%+v", a)
	}
	a.Groups[0].Items[0].Title = "已修改"
	a.Groups[0].Items[0].CreatedAt.Time = a.Groups[0].Items[0].CreatedAt.Time.AddDate(1, 0, 0)
	a.Groups[0].GroupRegex.Tags = append(a.Groups[0].GroupRegex.Tags, "已修改")
	b, err := c.mikanInfoFromURL(context.Background(), config, upstream.URL+"/Home/Bangumi/1")
	if err != nil {
		t.Fatal(err)
	}
	if b.Groups[0].Items[0].Title == "已修改" || b.Groups[0].Items[0].CreatedAt.Time.Year() != 2025 {
		t.Fatal("返回对象污染了字幕组缓存")
	}
	for _, tag := range b.Groups[0].GroupRegex.Tags {
		if tag == "已修改" {
			t.Fatal("规则标签污染了缓存")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("同一详情重复请求：%d", calls.Load())
	}
	ctx := context.WithValue(context.Background(), sourceRefreshKey{}, true)
	if _, err := c.mikanInfo(ctx, config, "1"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("强制刷新没有读取上游")
	}
}

func BenchmarkMikanDetailWarm(b *testing.B) {
	html := parsedDetailFixture + "<!--" + strings.Repeat("PAGE ", 100<<10) + "-->"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, html) }))
	defer upstream.Close()
	c := New(nil, nil, nil)
	defer c.Close()
	config := map[string]any{"mikanHost": upstream.URL}
	if _, err := c.mikanInfo(context.Background(), config, "1"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.mikanInfo(context.Background(), config, "1"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestConcurrentMikanReadersShareParsing(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "<html><ul></ul></html>") }))
	defer upstream.Close()
	c := New(nil, nil, nil)
	defer c.Close()
	var parses atomic.Int32
	start, release := make(chan struct{}), make(chan struct{})
	var readers sync.WaitGroup
	errors := make(chan error, 8)
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			var result mikan
			errors <- c.mikanDocument(context.Background(), nil, "directory", upstream.URL, &result, func(html []byte) (any, error) {
				if parses.Add(1) == 1 {
					close(start)
				}
				<-release
				return parseMikanDirectory(html, upstream.URL)
			})
		}()
	}
	<-start
	close(release)
	readers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if parses.Load() != 1 {
		t.Fatalf("并发读取重复解析 %d 次", parses.Load())
	}
}
