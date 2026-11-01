package notifications_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/notifications"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestPersistedNotificationJobReconcilesAfterAcceptedWebhookAndIsIdempotent(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusNoContent) }))
	defer upstream.Close()
	database := filepath.Join(t.TempDir(), "state.db")
	store, err := storage.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(context.Background(), map[string]any{"notificationConfigList": []any{map[string]any{
		"enable": true, "sort": 1, "retry": 1, "notificationType": "WEB_HOOK", "statusList": []any{"DOWNLOAD_END"}, "webHookMethod": "POST", "webHookUrl": upstream.URL,
	}}}); err != nil {
		t.Fatal(err)
	}
	service, err := notifications.New(store, notifications.Options{HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	subscription := map[string]any{"id": "sub", "title": "作品", "season": 1, "message": true}
	job, err := service.Enqueue(context.Background(), "torrent:abc", subscription, "下载完成", "DOWNLOAD_END")
	if err != nil {
		t.Fatal(err)
	}
	manager := jobs.New(store, map[string]jobs.Handler{notifications.JobKind: service.Handler()}, jobs.Options{MaxAttempts: 3})
	if ran, err := manager.RunOnce(context.Background()); !ran || err != nil {
		t.Fatalf("RunOnce() = %v, %v", ran, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err = notifications.New(store, notifications.Options{HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	done, err := service.Reconcile(context.Background(), job.ID, subscription, "下载完成", "DOWNLOAD_END")
	if err != nil || !done {
		t.Fatalf("recovered Reconcile() = %v, %v", done, err)
	}
	duplicate, err := service.Enqueue(context.Background(), "torrent:abc", subscription, "下载完成", "DOWNLOAD_END")
	if err != nil || duplicate.ID != job.ID {
		t.Fatalf("duplicate event job=%s want %s err=%v", duplicate.ID, job.ID, err)
	}
	if calls != 1 {
		t.Fatalf("accepted webhook calls = %d", calls)
	}
}

func TestNotificationDeliveryStateSurvivesStoreReopen(t *testing.T) {
	database := filepath.Join(t.TempDir(), "state.db")
	store, err := storage.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetNotificationDelivery(context.Background(), "event", "webhook:1", "delivered"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	states, err := store.GetNotificationDeliveryStates(context.Background(), "event")
	if err != nil || states["webhook:1"] != "delivered" {
		t.Fatalf("persisted deliveries=%#v err=%v", states, err)
	}
}

func TestOpeningExistingSchemaV1AddsDeliveryTableAndPreservesBusinessData(t *testing.T) {
	database := filepath.Join(t.TempDir(), "state.db")
	store, err := storage.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.SaveConfig(ctx, map[string]any{"extension": "kept"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{"id": "kept-id", "title": "Show", "season": 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE notification_deliveries; PRAGMA user_version = 1;`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config, err := store.GetConfig(ctx)
	if err != nil || config["extension"] != "kept" {
		t.Fatalf("migrated config=%#v err=%v", config, err)
	}
	subscriptions, err := store.ListSubscriptions(ctx)
	if err != nil || len(subscriptions) != 1 || subscriptions[0]["id"] != "kept-id" {
		t.Fatalf("migrated subscriptions=%#v err=%v", subscriptions, err)
	}
	if err := store.SetNotificationDelivery(ctx, "event", "hook:1", "delivered"); err != nil {
		t.Fatal(err)
	}
}
