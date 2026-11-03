package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestStorePersistsConfigAndSubscriptionsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "ani-rss.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(ctx, map[string]any{"rssSleepMinutes": float64(15), "extension": "kept"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{"id": "stable-id", "title": "番剧", "custom": true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config, err := store.GetConfig(ctx)
	if err != nil || config["extension"] != "kept" {
		t.Fatalf("GetConfig() = %#v, %v", config, err)
	}
	subs, err := store.ListSubscriptions(ctx)
	if err != nil || len(subs) != 1 || subs[0]["custom"] != true {
		t.Fatalf("ListSubscriptions() = %#v, %v", subs, err)
	}
}

func TestCanceledConfigReadAllowsDatabaseReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(context.Background(), map[string]any{"probe": true}); err != nil {
		t.Fatal(err)
	}
	canceled := false
	for attempt := 0; attempt < 100 && !canceled; attempt++ {
		ctx, cancel := context.WithCancel(context.Background())
		timer := time.AfterFunc(time.Nanosecond, cancel)
		_, queryErr := store.GetConfig(ctx)
		timer.Stop()
		cancel()
		if errors.Is(queryErr, context.Canceled) {
			canceled = true
		} else if queryErr != nil {
			t.Fatalf("GetConfig() error = %v", queryErr)
		}
	}
	if !canceled {
		t.Fatal("GetConfig() did not observe a canceled context after 100 attempts")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen after canceled config read: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestListSubscriptionsPreservesInsertAndReplaceOrderAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "ani-rss.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"z-first", "a-second"} {
		if err := store.PutSubscription(ctx, map[string]any{"id": id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	assertSubscriptionOrder(t, store, "z-first", "a-second")
	if err := store.ReplaceSubscriptions(ctx, []map[string]any{{"id": "y-replaced-first"}, {"id": "b-replaced-second"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assertSubscriptionOrder(t, store, "y-replaced-first", "b-replaced-second")
}

func assertSubscriptionOrder(t *testing.T, store *storage.Store, want ...string) {
	t.Helper()
	subscriptions, err := store.ListSubscriptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(subscriptions) != len(want) {
		t.Fatalf("subscriptions = %#v, want IDs %v", subscriptions, want)
	}
	for i, id := range want {
		if subscriptions[i]["id"] != id {
			t.Fatalf("subscription order = %#v, want IDs %v", subscriptions, want)
		}
	}
}

func TestImportSnapshotRollsBackConfigAndRecordsOnInvalidSubscription(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{"marker": "before"}); err != nil {
		t.Fatal(err)
	}
	err = store.ImportSnapshot(ctx, map[string]any{"marker": "after"}, []map[string]any{
		{"id": "valid", "title": "would be inserted"},
		{"title": "missing id"},
	})
	if err == nil {
		t.Fatal("ImportSnapshot accepted a record without an ID")
	}
	config, err := store.GetConfig(ctx)
	if err != nil || config["marker"] != "before" {
		t.Fatalf("config changed despite rollback: %#v, %v", config, err)
	}
	subs, err := store.ListSubscriptions(ctx)
	if err != nil || len(subs) != 0 {
		t.Fatalf("records changed despite rollback: %#v, %v", subs, err)
	}
}

func TestReplaceSubscriptionsIsAtomic(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.PutSubscription(ctx, map[string]any{"id": "old"}); err != nil {
		t.Fatal(err)
	}
	err = store.ReplaceSubscriptions(ctx, []map[string]any{
		{"id": "new"},
		{"id": "new"},
	})
	if err == nil {
		t.Fatal("ReplaceSubscriptions accepted duplicate IDs")
	}
	subs, err := store.ListSubscriptions(ctx)
	if err != nil || len(subs) != 1 || subs[0]["id"] != "old" {
		t.Fatalf("failed replacement changed existing records: %#v, %v", subs, err)
	}
}

func TestUpdateSubscriptionFieldsAndEnqueueRollsBackTogether(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.PutSubscription(ctx, map[string]any{"id": "sub", "url": "https://old.example/rss"}); err != nil {
		t.Fatal(err)
	}
	requests := []jobs.EnqueueRequest{
		{Kind: "rss-refresh", Key: "first", Payload: json.RawMessage(`{"subscriptionId":"sub"}`)},
		{Kind: "", Key: "invalid", Payload: json.RawMessage(`{}`)},
	}
	if err := store.UpdateSubscriptionFieldsAndEnqueue(ctx, "sub", map[string]any{"url": "https://new.example/rss"}, requests); err == nil {
		t.Fatal("invalid later task should roll back the subscription edit and earlier task")
	}
	subscriptions, err := store.ListSubscriptions(ctx)
	if err != nil || len(subscriptions) != 1 || subscriptions[0]["url"] != "https://old.example/rss" {
		t.Fatalf("failed transaction changed subscription: %#v, %v", subscriptions, err)
	}
	job, err := store.ClaimJob(ctx, "test", time.Now().Add(time.Second), time.Minute)
	if err != nil || job != nil {
		t.Fatalf("failed transaction retained task: %#v, %v", job, err)
	}
}

func TestUpdateSubscriptionFieldsBatchKeepsWorkerFieldsAndUnknownData(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, item := range []map[string]any{
		{"id": "first", "enable": true, "score": 4, "currentEpisodeNumber": 6, "lastDownloadTime": 99, "custom": "kept"},
		{"id": "second", "enable": true, "currentEpisodeNumber": 8, "lastDownloadTime": 101},
		{"id": "untouched", "enable": true},
	} {
		if err := store.PutSubscription(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpdateSubscriptionFieldsBatch(ctx, []string{"first", "second", "missing", "first"}, map[string]any{"enable": false, "id": "rewritten", "currentEpisodeNumber": 100}); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]map[string]any, len(items))
	for _, item := range items {
		byID[item["id"].(string)] = item
	}
	for _, id := range []string{"first", "second"} {
		if byID[id]["enable"] != false {
			t.Fatalf("subscription %s enable not changed: %#v", id, byID[id])
		}
	}
	if byID["first"]["score"] != float64(4) || byID["first"]["currentEpisodeNumber"] != float64(6) || byID["first"]["lastDownloadTime"] != float64(99) || byID["first"]["custom"] != "kept" {
		t.Fatalf("batch update changed worker or unknown fields: %#v", byID["first"])
	}
	if byID["untouched"]["enable"] != true {
		t.Fatalf("batch update changed an unselected subscription: %#v", byID["untouched"])
	}
}
