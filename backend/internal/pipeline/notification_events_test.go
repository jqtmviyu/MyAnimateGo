package pipeline_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

type queuedNotification struct {
	key          string
	subscription map[string]any
	text         string
	status       string
}

func TestRefreshEnqueuesOmitSnapshotsWithStableKeys(t *testing.T) {
	torrents := []any{}
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "fixture-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode RPC: %v", err)
			return
		}
		if request.Method != "torrent-get" {
			t.Errorf("unexpected RPC method %q", request.Method)
		}
		writeRPC(w, map[string]any{"torrents": torrents})
	}))
	defer rpc.Close()
	dataDir := t.TempDir()
	store, err := storage.Open(filepath.Join(dataDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(context.Background(), map[string]any{
		"automaticTasksPaused": false, "downloadToolHost": rpc.URL, "downloadToolUsername": "fixture", "downloadToolPassword": "fixture",
		"downloadPathTemplate": filepath.Join(dataDir, "downloads", "${title}"), "renameTemplate": "${title} ${episodeFormat}",
		"omit": true, "standbyRss": false, "coexist": false,
		"notificationConfigList": []any{
			map[string]any{"enable": true, "notificationType": "WEB_HOOK", "statusList": []any{"OMIT"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	feed := `<rss><channel><item><title>Show - 03</title><guid>3333333333333333333333333333333333333333</guid><enclosure url="magnet:?xt=urn:btih:3333333333333333333333333333333333333333"/><pubDate>Tue, 01 Apr 2025 10:00:00 GMT</pubDate></item><item><title>Show - 01</title><guid>1111111111111111111111111111111111111111</guid><enclosure url="magnet:?xt=urn:btih:1111111111111111111111111111111111111111"/><pubDate>Tue, 01 Apr 2025 09:00:00 GMT</pubDate></item></channel></rss>`
	if err := store.PutSubscription(context.Background(), map[string]any{
		"id": "notify-sub", "url": "fixture", "title": "Show", "season": 1, "releaseDate": "2025-04-01",
		"subgroup": "Demo", "enable": true, "ova": false, "omit": true,
		"totalEpisodeNumber": 10, "notDownload": []any{float64(1), float64(3)}, "downloadNew": false,
		"standbyRssList": []any{}, "match": []any{}, "exclude": []any{},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2025, 4, 10, 12, 0, 0, 0, time.UTC)
	var events []queuedNotification
	engine, err := pipeline.New(store, pipeline.Options{
		Now:        func() time.Time { return now },
		RSSFetcher: func(context.Context, string) ([]byte, error) { return []byte(feed), nil },
		EnqueueNotification: func(_ context.Context, key string, subscription map[string]any, text, status string) error {
			events = append(events, queuedNotification{key: key, subscription: subscription, text: text, status: status})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := jobs.WithJobID(context.Background(), "rss-job-17")
	if err := engine.RefreshSubscription(ctx, "notify-sub"); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("queued notification events = %#v", events)
	}
	byStatus := map[string]queuedNotification{}
	for _, event := range events {
		byStatus[event.status] = event
	}
	if got := byStatus["OMIT"]; got.key != "omit:notify-sub:2:2025-04-10" || got.text != "缺少集数 Show S01E02" || got.subscription["id"] != "notify-sub" {
		t.Fatalf("OMIT event = %#v", got)
	}
	events = nil
	if err := engine.RefreshSubscription(ctx, "notify-sub"); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].key != "omit:notify-sub:2:2025-04-10" {
		t.Fatalf("repeated refresh must reuse idempotency keys: %#v", events)
	}
	if err := store.PutSubscription(context.Background(), map[string]any{
		"id": "muted-sub", "url": "fixture", "title": "Show", "season": 1, "releaseDate": "2025-04-01",
		"subgroup": "Demo", "enable": true, "message": false, "ova": false, "omit": true,
		"totalEpisodeNumber": 10, "notDownload": []any{float64(1), float64(3)}, "downloadNew": false,
		"standbyRssList": []any{}, "match": []any{}, "exclude": []any{},
	}); err != nil {
		t.Fatal(err)
	}
	events = nil
	if err := engine.RefreshSubscription(ctx, "muted-sub"); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("message=false subscription queued notifications: %#v", events)
	}
}

func TestRefreshReportsConfiguredNotificationFailureInsteadOfIgnoringIt(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(context.Background(), map[string]any{"omit": true, "notificationConfigList": []any{map[string]any{"enable": true, "notificationType": "WEB_HOOK", "statusList": []any{"OMIT"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(context.Background(), map[string]any{"id": "sub", "title": "Show", "season": 1, "enable": true, "omit": true, "totalEpisodeNumber": 10, "url": "fixture"}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{
		Now: func() time.Time { return time.Date(2025, 4, 10, 12, 0, 0, 0, time.UTC) },
		RSSFetcher: func(context.Context, string) ([]byte, error) {
			return []byte(`<rss><channel><item><title>Show - 03</title><guid>3333333333333333333333333333333333333333</guid><enclosure url="magnet:?xt=urn:btih:3333333333333333333333333333333333333333"/></item><item><title>Show - 01</title><guid>1111111111111111111111111111111111111111</guid><enclosure url="magnet:?xt=urn:btih:1111111111111111111111111111111111111111"/></item></channel></rss>`), nil
		},
		EnqueueNotification: func(context.Context, string, map[string]any, string, string) error {
			return errors.New("notification queue unavailable")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.RefreshSubscription(context.Background(), "sub"); err == nil {
		t.Fatal("configured notification queue failure was reported as success")
	}
}

func TestDownloadRetryQueuesTerminalErrorOnceWithSchedulerJobKey(t *testing.T) {
	addCalls := 0
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "fixture-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Method == "torrent-add" {
			addCalls++
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeRPC(w, map[string]any{"torrents": []any{}})
	}))
	defer rpc.Close()
	dataDir := t.TempDir()
	store, err := storage.Open(filepath.Join(dataDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(context.Background(), map[string]any{
		"automaticTasksPaused": false, "downloadToolHost": rpc.URL, "downloadToolUsername": "fixture", "downloadToolPassword": "fixture",
		"downloadPathTemplate": filepath.Join(dataDir, "downloads", "${title}"), "renameTemplate": "${title} ${episodeFormat}", "downloadRetry": 2,
		"notificationConfigList": []any{map[string]any{"enable": true, "notificationType": "WEB_HOOK", "statusList": []any{"ERROR"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(context.Background(), map[string]any{
		"id": "error-sub", "url": "fixture", "title": "Show", "season": 1, "enable": true, "ova": false,
		"subgroup": "Demo", "notDownload": []any{}, "standbyRssList": []any{}, "match": []any{}, "exclude": []any{},
	}); err != nil {
		t.Fatal(err)
	}
	var events []queuedNotification
	engine, err := pipeline.New(store, pipeline.Options{
		RSSFetcher: func(context.Context, string) ([]byte, error) {
			return []byte(`<rss><channel><item><title>Show - 01</title><guid>` + sampleHash + `</guid><enclosure url="magnet:?xt=urn:btih:` + sampleHash + `"/></item></channel></rss>`), nil
		},
		EnqueueNotification: func(_ context.Context, key string, subscription map[string]any, text, status string) error {
			events = append(events, queuedNotification{key: key, subscription: subscription, text: text, status: status})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := jobs.WithJobID(context.Background(), "rss-job-terminal")
	if err := engine.RefreshSubscription(ctx, "error-sub"); err == nil {
		t.Fatal("failed Transmission add should be returned to the durable RSS job")
	}
	if addCalls != 2 {
		t.Fatalf("internal download retry count = %d", addCalls)
	}
	if len(events) != 1 || events[0].key != "download-error:rss-job-terminal:"+sampleHash || events[0].status != "ERROR" || events[0].text != "Show 01 添加失败，疑似为坏种" {
		t.Fatalf("terminal error event = %#v", events)
	}
}

func TestUnconfirmedTransmissionResultDoesNotQueueDownloadError(t *testing.T) {
	addCalls := 0
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "fixture-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Method == "torrent-add" {
			addCalls++
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if addCalls > 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeRPC(w, map[string]any{"torrents": []any{}})
	}))
	defer rpc.Close()
	dataDir := t.TempDir()
	store, err := storage.Open(filepath.Join(dataDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(context.Background(), map[string]any{
		"automaticTasksPaused": false, "downloadToolHost": rpc.URL, "downloadToolUsername": "fixture", "downloadToolPassword": "fixture",
		"downloadPathTemplate": filepath.Join(dataDir, "downloads", "${title}"), "renameTemplate": "${title} ${episodeFormat}", "downloadRetry": 3,
		"notificationConfigList": []any{map[string]any{"enable": true, "notificationType": "WEB_HOOK", "statusList": []any{"ERROR"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(context.Background(), map[string]any{"id": "unknown-sub", "url": "fixture", "title": "Show", "season": 1, "enable": true, "subgroup": "Demo", "notDownload": []any{}, "standbyRssList": []any{}}); err != nil {
		t.Fatal(err)
	}
	queued := 0
	engine, err := pipeline.New(store, pipeline.Options{
		RSSFetcher: func(context.Context, string) ([]byte, error) {
			return []byte(`<rss><channel><item><title>Show - 01</title><guid>` + sampleHash + `</guid><enclosure url="magnet:?xt=urn:btih:` + sampleHash + `"/></item></channel></rss>`), nil
		},
		EnqueueNotification: func(context.Context, string, map[string]any, string, string) error { queued++; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.RefreshSubscription(context.Background(), "unknown-sub"); err == nil {
		t.Fatal("unknown Transmission result must remain an error")
	} else {
		t.Logf("expected unconfirmed RPC error: %v", err)
	}
	if addCalls != 1 || queued != 0 {
		t.Fatalf("unknown result addCalls=%d queued notifications=%d", addCalls, queued)
	}
	if _, err := findTestPreparedCache(dataDir, sampleHash); err != nil {
		t.Fatalf("unconfirmed operation should preserve its prepared cache: %v", err)
	}
}

func TestCompletedNotificationUsesCurrentDownloadCountWhenConcurrencyLimitDefersAnEpisode(t *testing.T) {
	firstHash := sampleHash
	secondHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	torrents := []any{}
	adds := 0
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "fixture-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		switch request.Method {
		case "torrent-get":
			writeRPC(w, map[string]any{"torrents": torrents})
		case "torrent-add":
			adds++
			hash := firstHash
			if adds == 2 {
				hash = secondHash
			}
			torrents = append(torrents, map[string]any{"id": adds, "hashString": hash, "name": "Show", "status": 4, "downloadDir": "/downloads", "labels": []string{"ani-rss", "Demo"}, "isFinished": false})
			writeRPC(w, map[string]any{"torrent-added": map[string]any{"hashString": hash}})
		default:
			t.Errorf("unexpected RPC method %q", request.Method)
			writeRPC(w, map[string]any{})
		}
	}))
	defer rpc.Close()
	dataDir := t.TempDir()
	store, err := storage.Open(filepath.Join(dataDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(context.Background(), map[string]any{
		"automaticTasksPaused": false, "downloadToolHost": rpc.URL, "downloadToolUsername": "fixture", "downloadToolPassword": "fixture",
		"downloadPathTemplate": "/downloads", "renameTemplate": "${title} ${episodeFormat}", "downloadCount": 1, "downloadRetry": 1,
		"autoDisabled": true, "notificationConfigList": []any{map[string]any{"enable": true, "notificationType": "WEB_HOOK", "statusList": []any{"COMPLETED"}}},
	}); err != nil {
		t.Fatal(err)
	}
	feed := `<rss><channel><item><title>Show - 02</title><guid>` + secondHash + `</guid><enclosure url="magnet:?xt=urn:btih:` + secondHash + `"/></item><item><title>Show - 01</title><guid>` + firstHash + `</guid><enclosure url="magnet:?xt=urn:btih:` + firstHash + `"/></item></channel></rss>`
	if err := store.PutSubscription(context.Background(), map[string]any{
		"id": "limited-sub", "url": "fixture", "title": "Show", "season": 1, "totalEpisodeNumber": 2, "enable": true,
		"subgroup": "Demo", "standbyRssList": []any{}, "notDownload": []any{}, "match": []any{}, "exclude": []any{},
	}); err != nil {
		t.Fatal(err)
	}
	var completed []string
	engine, err := pipeline.New(store, pipeline.Options{
		RSSFetcher: func(context.Context, string) ([]byte, error) { return []byte(feed), nil },
		EnqueueNotification: func(_ context.Context, key string, _ map[string]any, _ string, status string) error {
			if status == "COMPLETED" {
				completed = append(completed, key)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.RefreshSubscription(context.Background(), "limited-sub"); err != nil {
		t.Fatal(err)
	}
	subscriptions, err := store.ListSubscriptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if subscriptions[0]["currentEpisodeNumber"] != float64(1) || adds != 1 {
		t.Fatalf("currentDownloadCount should wait for deferred episode: sub=%#v adds=%d", subscriptions[0], adds)
	}
	if len(completed) != 0 {
		t.Fatalf("deferred episode triggered completed notification: %#v", completed)
	}
}

func findTestPreparedCache(dataDir, hash string) (string, error) {
	paths, err := filepath.Glob(filepath.Join(dataDir, "torrents", "*", "*", hash+".*.pending"))
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", errors.New("prepared cache not found")
	}
	return paths[0], nil
}
