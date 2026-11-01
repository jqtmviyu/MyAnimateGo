package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestDevelopmentTaskActivationRunsQueueAndPersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	rssRead := make(chan struct{}, 8)
	var hold atomic.Bool
	entered, canceled := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rss" {
			if hold.Load() {
				close(entered)
				<-r.Context().Done()
				close(canceled)
				return
			}
			rssRead <- struct{}{}
			_, _ = io.WriteString(w, `<rss><channel></channel></rss>`)
			return
		}
		_, _ = io.WriteString(w, `{"result":"success","arguments":{"torrents":[]}}`)
	}))
	defer upstream.Close()
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{"automaticTasksPaused": true, "rss": false, "rename": false, "downloadToolHost": upstream.URL, "bgmApi": upstream.URL}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{"id": "reviewed", "title": "审核作品", "enable": true, "season": 1, "url": upstream.URL + "/rss"}); err != nil {
		t.Fatal(err)
	}
	queue := func(key string) string {
		job, err := store.EnqueueJob(ctx, "rss-refresh", key, json.RawMessage(`{"subscriptionId":"reviewed","automatic":false}`))
		if err != nil {
			t.Fatal(err)
		}
		return job.ID
	}
	job := queue("before-review")
	events, stop := start(t, "serve", "-dev", "-data-dir", dir, "-listen", "127.0.0.1:0")
	defer func() { stop() }()
	address := events["address"].(string)
	token := login(t, address)
	change := func(paused bool) {
		t.Helper()
		got := post(t, address, "/api/setAutomaticTasksPaused", token, map[string]any{"paused": paused, "reviewed": true})
		if got["code"] != float64(200) {
			t.Fatalf("change task state: %v", got)
		}
	}
	checkPaused := func(want bool) {
		t.Helper()
		got := post(t, address, "/api/config", token, nil)["data"].(map[string]any)
		if got["automaticTasksPaused"] != want {
			t.Fatalf("paused=%v, want %v", got["automaticTasksPaused"], want)
		}
	}
	if got := post(t, address, "/api/setAutomaticTasksPaused", "", map[string]any{"paused": false, "reviewed": true}); got["code"] != float64(403) {
		t.Fatal("task activation must share authentication")
	}
	if got := post(t, address, "/api/setAutomaticTasksPaused", token, map[string]any{"paused": false}); got["code"] != float64(500) {
		t.Fatal("task activation must require explicit review")
	}
	checkPaused(true)
	change(false)
	deadline := time.Now().Add(4 * time.Second)
	for {
		state, err := store.GetJob(ctx, job)
		if err != nil {
			t.Fatal(err)
		}
		if state.State == "resolved" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reviewed queue did not execute: %s", state.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-rssRead:
	default:
		t.Fatal("RSS not actually read")
	}
	stop()
	events, stop = start(t, "serve", "-dev", "-data-dir", dir, "-listen", "127.0.0.1:0")
	address = events["address"].(string)
	token = login(t, address)
	checkPaused(false)
	// 旧设置页仍携带暂停状态，保存普通配置不能改变当前运行状态。
	post(t, address, "/api/setConfig", token, map[string]any{"siteTitle": "审核后标题", "automaticTasksPaused": true, "migrationReport": map[string]any{"sourceVersion": "forged"}})
	checkPaused(false)
	// 用实际进行中的 HTTP 请求确认暂停接通取消，而不只改变显示状态。
	hold.Store(true)
	queue("pause-in-flight")
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight RSS request did not start")
	}
	change(true)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("pause did not cancel in-flight RSS request")
	}
	hold.Store(false)
	post(t, address, "/api/setConfig", token, map[string]any{"automaticTasksPaused": false})
	checkPaused(true)
	job = queue("after-pause")
	select {
	case <-rssRead:
		t.Fatal("paused task read RSS")
	case <-time.After(700 * time.Millisecond):
	}
	state, err := store.GetJob(ctx, job)
	if err != nil || state.State != "pending" {
		t.Fatalf("paused job=%+v err=%v", state, err)
	}
	stop()
	events, stop = start(t, "serve", "-dev", "-data-dir", dir, "-listen", "127.0.0.1:0")
	address = events["address"].(string)
	token = login(t, address)
	checkPaused(true)
	config := post(t, address, "/api/config", token, nil)["data"].(map[string]any)
	if config["siteTitle"] != "审核后标题" || config["migrationReport"] != nil {
		t.Fatal("ordinary save lost title or wrote protected report")
	}
}
