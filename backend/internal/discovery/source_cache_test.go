package discovery

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSourceCacheWarmExpiryForceAndIsolation(t *testing.T) {
	c := New(nil, nil, nil)
	var calls int
	load := func(context.Context) ([]byte, error) { calls++; return []byte{byte(calls)}, nil }
	a, err := c.sourceBytes(context.Background(), "source", time.Minute, load)
	if err != nil {
		t.Fatal(err)
	}
	a[0] = 99
	b, _ := c.sourceBytes(context.Background(), "source", time.Minute, load)
	if calls != 1 || b[0] != 1 {
		t.Fatal("热读取未复用，或返回内容污染了缓存")
	}
	c.mu.Lock()
	entry := c.documents["source"]
	entry.expires = time.Now().Add(-time.Second)
	c.documents["source"] = entry
	c.mu.Unlock()
	c.sourceBytes(context.Background(), "source", time.Minute, load)
	ctx := context.WithValue(context.Background(), sourceRefreshKey{}, true)
	c.sourceBytes(ctx, "source", time.Minute, load)
	if calls != 3 {
		t.Fatalf("过期和显式刷新应各读取一次：%d", calls)
	}
}
func TestSourceCacheCanceledReaderDoesNotCancelOtherReaders(t *testing.T) {
	c := New(nil, nil, nil)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	load := func(ctx context.Context) ([]byte, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return []byte("fresh"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := c.sourceBytes(ctx, "shared", time.Minute, load); first <- err }()
	<-started
	second := make(chan error, 1)
	go func() { _, err := c.sourceBytes(context.Background(), "shared", time.Minute, load); second <- err }()
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("相同来源重复读取：%d", calls.Load())
	}
}
func TestDirectoryForcedRefreshReadsNewSource(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, "<html><ul></ul></html>")
	}))
	defer upstream.Close()
	c := New(&proxyStore{config: map[string]any{"mikanHost": upstream.URL}}, nil, nil)
	for _, suffix := range []string{"", "", "&refresh=true", ""} {
		_, _, err := c.mikanRoute(httptest.NewRequest("POST", "/api/mikan?metadata=false"+suffix, nil))
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("两次普通打开加一次手动刷新应请求两次：%d", calls.Load())
	}
}
func TestSourceTransportReusesConnections(t *testing.T) {
	var connections atomic.Int32
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	upstream.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	upstream.Start()
	defer upstream.Close()
	c := New(nil, nil, nil)
	for range 2 {
		if _, _, err := c.requestText(context.Background(), map[string]any{}, "GET", upstream.URL, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if connections.Load() != 1 {
		t.Fatalf("连接没有复用：%d", connections.Load())
	}
}
