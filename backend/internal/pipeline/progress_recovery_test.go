package pipeline_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

type failProgressStore struct {
	*storage.Store
	failProgress bool
}

func (s *failProgressStore) PutSubscription(ctx context.Context, sub map[string]any) error {
	if s.failProgress && isEpisodeOne(sub["currentEpisodeNumber"]) {
		return errors.New("fixture subscription progress write failed")
	}
	return s.Store.PutSubscription(ctx, sub)
}

func (s *failProgressStore) UpdateSubscriptionFields(ctx context.Context, id string, fields map[string]any) error {
	if s.failProgress && isEpisodeOne(fields["currentEpisodeNumber"]) {
		return errors.New("fixture subscription progress write failed")
	}
	if updater, ok := any(s.Store).(interface {
		UpdateSubscriptionFields(context.Context, string, map[string]any) error
	}); ok {
		return updater.UpdateSubscriptionFields(ctx, id, fields)
	}
	subs, err := s.Store.ListSubscriptions(ctx)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		if sub["id"] != id {
			continue
		}
		for key, value := range fields {
			sub[key] = value
		}
		return s.Store.PutSubscription(ctx, sub)
	}
	return errors.New("fixture subscription missing")
}

func isEpisodeOne(value any) bool {
	switch number := value.(type) {
	case int:
		return number == 1
	case float64:
		return number == 1
	}
	return false
}

func TestRefreshKeepsPreparedCacheUntilProgressCanBeReconciled(t *testing.T) {
	ctx := context.Background()
	torrents := []map[string]any{}
	adds := 0
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "fixture-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode RPC: %v", err)
			return
		}
		switch request.Method {
		case "torrent-get":
			writeRPC(w, map[string]any{"torrents": torrents})
		case "torrent-add":
			adds++
			torrents = []map[string]any{{"id": 9, "hashString": sampleHash, "name": "Show", "status": 4, "downloadDir": request.Arguments["download-dir"], "labels": request.Arguments["labels"], "isFinished": false}}
			writeRPC(w, map[string]any{"torrent-added": map[string]any{"hashString": sampleHash}})
		default:
			t.Errorf("unexpected RPC method %q", request.Method)
			writeRPC(w, map[string]any{})
		}
	}))
	defer rpc.Close()
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "state.db")
	baseStore, err := storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := baseStore.SaveConfig(ctx, map[string]any{
		"automaticTasksPaused": false, "downloadToolHost": rpc.URL, "downloadToolUsername": "fixture", "downloadToolPassword": "fixture",
		"downloadPathTemplate": filepath.Join(dataDir, "downloads", "${title}"), "renameTemplate": "${title} ${episodeFormat}", "downloadRetry": 1, "standbyRss": false,
		"autoDisabled":           true,
		"notificationConfigList": []any{map[string]any{"enable": true, "notificationType": "WEB_HOOK", "statusList": []any{"DOWNLOAD_START", "COMPLETED"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := baseStore.PutSubscription(ctx, map[string]any{
		"id": "progress-sub", "url": "fixture", "title": "Show", "season": 1, "releaseDate": "2024-01-01", "totalEpisodeNumber": 1,
		"subgroup": "Demo", "offset": 0, "enable": true, "ova": false, "downloadNew": false,
		"standbyRssList": []any{}, "notDownload": []any{}, "match": []any{}, "exclude": []any{},
	}); err != nil {
		t.Fatal(err)
	}
	store := &failProgressStore{Store: baseStore, failProgress: true}
	queued := map[string]queuedNotification{}
	queueCalls := 0
	options := pipeline.Options{
		RSSFetcher: func(context.Context, string) ([]byte, error) {
			return []byte(`<rss><channel><item><title>Show - 01</title><guid>` + sampleHash + `</guid><enclosure url="magnet:?xt=urn:btih:` + sampleHash + `"/></item></channel></rss>`), nil
		},
		EnqueueNotification: func(_ context.Context, key string, sub map[string]any, text, status string) error {
			queueCalls++
			queued[key] = queuedNotification{key: key, subscription: sub, text: text, status: status}
			return nil
		},
	}
	engine, err := pipeline.New(store, options)
	if err != nil {
		t.Fatal(err)
	}
	jobContext := jobs.WithJobID(ctx, "rss-job-progress")
	if err := engine.RefreshSubscription(jobContext, "progress-sub"); err == nil {
		t.Fatal("fixture must fail progress persistence after Transmission accepted the task")
	} else {
		t.Logf("expected progress persistence failure: %v", err)
	}
	pending := filepath.Join(dataDir, "torrents", "p", "progress-sub", sampleHash+".txt.pending")
	committed := filepath.Join(dataDir, "torrents", "p", "progress-sub", sampleHash+".txt")
	if _, err := os.Stat(pending); err != nil {
		_, finalErr := os.Stat(committed)
		t.Fatalf("prepared cache must remain while progress is uncommitted: pending=%v committed=%v", err, finalErr)
	}
	if _, err := os.Stat(committed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final cache must wait for progress: %v", err)
	}
	if adds != 1 {
		t.Fatalf("first refresh add calls = %d", adds)
	}
	if queueCalls != 1 || queued["download-start:"+sampleHash].status != "DOWNLOAD_START" || queued["download-start:"+sampleHash].text != "Show 01 已更新" {
		t.Fatalf("accepted task notification was not stably queued: calls=%d queue=%#v", queueCalls, queued)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recoveryStore := &failProgressStore{Store: reopened}
	engine, err = pipeline.New(recoveryStore, options)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.RefreshSubscription(jobContext, "progress-sub"); err != nil {
		t.Fatal(err)
	}
	if adds != 1 {
		t.Fatalf("recovery must reconcile the accepted task without adding again, adds=%d", adds)
	}
	if queueCalls != 3 || len(queued) != 2 {
		t.Fatalf("recovery must reuse the idempotent notification key: calls=%d events=%#v", queueCalls, queued)
	}
	completedFound := false
	for key, event := range queued {
		if strings.HasPrefix(key, "completed:progress-sub:1:") && event.status == "COMPLETED" && event.text == "Show 订阅已完结" {
			completedFound = true
		}
	}
	if !completedFound {
		t.Fatalf("completed subscription event missing: %#v", queued)
	}
	if _, err := os.Stat(committed); err != nil {
		t.Fatalf("final cache missing after recovered progress: %v", err)
	}
	if _, err := os.Stat(pending); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending cache remains after recovered progress: %v", err)
	}
	subscriptions, err := reopened.ListSubscriptions(ctx)
	if err != nil || len(subscriptions) != 1 || subscriptions[0]["currentEpisodeNumber"] != float64(1) {
		t.Fatalf("recovered subscription progress=%#v err=%v", subscriptions, err)
	}
}
