package discovery

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIdleCachesReleaseExpiredValues(t *testing.T) {
	c := New(nil, nil, nil)
	defer c.Close()
	_, _ = c.cached("metadata", 10*time.Millisecond, func() (any, error) { return strings.Repeat("m", 1<<20), nil })
	_, _ = c.sourceBytes(context.Background(), "html", 10*time.Millisecond, func(context.Context) ([]byte, error) { return make([]byte, 1<<20), nil })
	deadline := time.Now().Add(time.Second)
	for {
		c.mu.Lock()
		count, size := len(c.cache)+len(c.documents), c.documentBytes
		c.mu.Unlock()
		if count == 0 && size == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("空闲缓存过期仍持有 %d 条，文档 %d 字节", count, size)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestMetadataCacheHasCapacityLimit(t *testing.T) {
	c := New(nil, nil, nil)
	defer c.Close()
	for i := 0; i < 256; i++ {
		_, err := c.cached(fmt.Sprint(i), time.Minute, func() (any, error) { return strings.Repeat("x", 4096), nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) > 128 {
		t.Fatalf("元数据缓存无限增长：%d 条", len(c.cache))
	}
}

func BenchmarkMikanDirectoryWarm(b *testing.B) {
	html := "<HTML><UL><!--" + strings.Repeat("CONTENT ", 64<<10) + "--></UL></HTML>"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, html) }))
	defer upstream.Close()
	c := New(&proxyStore{config: map[string]any{"mikanHost": upstream.URL}}, nil, nil)
	defer c.Close()
	if _, err := c.searchMikan(context.Background(), "", nil, false); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.searchMikan(context.Background(), "", nil, false); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPublicMetadataCachePreservesDisplayFields(t *testing.T) {
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, `{"id":1,"name":"作品","images":{"small":"https://example.test/cover.jpg"},"rating":{"score":8.5,"count":{"10":100}},"infobox":[{"key":"简介","value":"详细资料"}],"tags":[{"name":"标签"}]}`)
	}))
	defer upstream.Close()
	c := New(nil, nil, nil)
	defer c.Close()
	for range 2 {
		info, err := c.publicSubject(context.Background(), map[string]any{"bgmApi": upstream.URL}, "1")
		if err != nil {
			t.Fatal(err)
		}
		if info.Score != 8.5 || info.Cover != "https://example.test/cover.jpg" {
			t.Fatalf("列表展示字段改变：%+v", info)
		}
	}
	if calls != 1 {
		t.Fatalf("列表元数据未复用：%d", calls)
	}
}
