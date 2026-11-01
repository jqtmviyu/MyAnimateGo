package storage_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestSQLiteSnapshotRestoresConfigSubscriptionsAndQueuedJobs(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{"siteTitle": "snapshot"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{"id": "show-1", "title": "保留作品"}); err != nil {
		t.Fatal(err)
	}
	job, err := store.EnqueueJob(ctx, "backup-test", "backup-1", json.RawMessage(`{"saved":true}`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(dir, "snapshot.db")
	if err := store.SnapshotDB(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(ctx, map[string]any{"siteTitle": "changed"}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSubscriptions(ctx, []string{"show-1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	config, err := store.GetConfig(ctx)
	if err != nil || config["siteTitle"] != "snapshot" {
		t.Fatalf("snapshot config was not restored: %#v, %v", config, err)
	}
	subscriptions, err := store.ListSubscriptions(ctx)
	if err != nil || len(subscriptions) != 1 || subscriptions[0]["title"] != "保留作品" {
		t.Fatalf("snapshot subscriptions were not restored: %#v, %v", subscriptions, err)
	}
	restoredJob, err := store.GetJob(ctx, job.ID)
	if err != nil || string(restoredJob.Payload) != `{"saved":true}` {
		t.Fatalf("snapshot queued task was not restored: %#v, %v", restoredJob, err)
	}
}
