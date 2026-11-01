package notifications_test

import (
	"context"
	"encoding/json"

	"io"

	"net/http"
	"net/http/httptest"

	"path/filepath"

	"sync/atomic"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/notifications"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestWebhookNotificationRendersTemplateAndPersistsAcceptedDelivery(t *testing.T) {
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.Header.Get("X-Notify") != "fixture" {
			t.Errorf("webhook request = %s headers %v", r.Method, r.Header)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode webhook body: %v", err)
		}
		if body["text"] != "作品 S01E02 已更新 🎈" {
			t.Errorf("rendered webhook body = %#v", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	store := openNotificationStore(t, map[string]any{
		"notificationTemplate": "${title} ${text} ${emoji}",
		"notificationConfigList": []any{map[string]any{
			"enable": true, "sort": 1, "retry": 3, "notificationType": "WEB_HOOK",
			"statusList": []any{"DOWNLOAD_START"}, "webHookMethod": "POST", "webHookUrl": upstream.URL,
			"webHookHeader": "X-Notify: fixture", "webHookBody": `{"text":"${notification}"}`,
		}},
	})
	defer store.Close()
	service, err := notifications.New(store, notifications.Options{})
	if err != nil {
		t.Fatal(err)
	}
	subscription := map[string]any{"id": "sub-1", "title": "作品", "season": 1}
	if err := service.Send(context.Background(), "event-1", subscription, "S01E02 已更新", "DOWNLOAD_START"); err != nil {
		t.Fatal(err)
	}
	if done, err := service.Reconcile(context.Background(), "event-1", subscription, "S01E02 已更新", "DOWNLOAD_START"); err != nil || !done {
		t.Fatalf("Reconcile() = %v, %v", done, err)
	}
	if calls != 1 {
		t.Fatalf("accepted webhook was sent %d times", calls)
	}
}

func TestWebhookTransportLossStaysUncertainAndIsNotRetried(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server does not support connection hijacking")
		}
		conn, _, err := hijacker.Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	url := upstream.URL
	store := openNotificationStore(t, map[string]any{
		"notificationConfigList": []any{map[string]any{
			"enable": true, "sort": 1, "retry": 3, "notificationType": "WEB_HOOK",
			"statusList": []any{"DOWNLOAD_START"}, "webHookMethod": "POST", "webHookUrl": url,
		}},
	})
	defer store.Close()
	service, err := notifications.New(store, notifications.Options{})
	if err != nil {
		t.Fatal(err)
	}
	event := map[string]any{"id": "sub-1", "title": "作品", "season": 1}
	if err := service.Send(context.Background(), "event-uncertain", event, "updated", "DOWNLOAD_START"); !notifications.IsDeliveryUncertain(err) {
		t.Fatalf("lost response error = %v, want uncertain delivery", err)
	}
	if done, err := service.Reconcile(context.Background(), "event-uncertain", event, "updated", "DOWNLOAD_START"); done || err != nil {
		t.Fatalf("uncertain delivery reconciliation = %v, %v", done, err)
	}
	if err := service.Send(context.Background(), "event-uncertain", event, "updated", "DOWNLOAD_START"); !notifications.IsDeliveryUncertain(err) {
		t.Fatalf("uncertain event retry error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("uncertain webhook was retried %d times", calls.Load())
	}
	upstream.Close()
}

func TestExplicitChannelRejectionIsRecordedAndNotBlindlyRepeated(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "rejected", http.StatusBadRequest)
	}))
	defer upstream.Close()
	sub := map[string]any{"id": "sub", "title": "Show", "season": 1}
	store := openNotificationStore(t, map[string]any{"notificationConfigList": []any{map[string]any{
		"enable": true, "sort": 1, "retry": 3, "notificationType": "WEB_HOOK", "statusList": []any{"DOWNLOAD_END"}, "webHookMethod": "POST", "webHookUrl": upstream.URL,
	}}})
	defer store.Close()
	service, err := notifications.New(store, notifications.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Send(context.Background(), "event-rejected", sub, "done", "DOWNLOAD_END"); err == nil {
		t.Fatal("HTTP 400 should remain a visible notification failure")
	}
	if done, err := service.Reconcile(context.Background(), "event-rejected", sub, "done", "DOWNLOAD_END"); done || err != nil {
		t.Fatalf("failed channel reconcile = %v, %v", done, err)
	}
	if err := service.Send(context.Background(), "event-rejected", sub, "done", "DOWNLOAD_END"); err == nil {
		t.Fatal("persisted failure must stay visible")
	}
	if calls.Load() != 1 {
		t.Fatalf("explicitly rejected webhook was sent %d times", calls.Load())
	}
}

func TestEmbyRefreshUsesSelectedViewIdsAndConfiguredToken(t *testing.T) {
	refreshed := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Library/MediaFolders" {
			_, _ = io.WriteString(w, `{"Items":[{"Id":"selected","Name":"Shows"},{"Id":"ignored","Name":"Movies"}]}`)
			return
		}
		if r.URL.Path != "/emby/Items/selected/Refresh" || r.Method != http.MethodPost || r.Header.Get("X-Emby-Token") != "emby-fixture" {
			t.Errorf("Emby refresh request = %s %s token=%q", r.Method, r.URL.String(), r.Header.Get("X-Emby-Token"))
		}
		if r.URL.Query().Get("Recursive") != "true" || r.URL.Query().Get("ReplaceAllMetadata") != "false" {
			t.Errorf("Emby refresh query = %v", r.URL.Query())
		}
		refreshed++
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()
	store := openNotificationStore(t, map[string]any{"notificationConfigList": []any{map[string]any{
		"enable": true, "sort": 1, "retry": 1, "notificationType": "EMBY_REFRESH", "statusList": []any{"DOWNLOAD_END"},
		"embyHost": upstream.URL, "embyApiKey": "emby-fixture", "embyRefreshViewIds": []any{"selected"},
	}}})
	defer store.Close()
	service, err := notifications.New(store, notifications.Options{HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Send(context.Background(), "emby-refresh", map[string]any{"id": "sub", "title": "Show", "season": 1}, "done", "DOWNLOAD_END"); err != nil {
		t.Fatal(err)
	}
	if refreshed != 1 {
		t.Fatalf("Emby refresh calls = %d", refreshed)
	}
}

func openNotificationStore(t *testing.T, config map[string]any) *storage.Store {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(context.Background(), config); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
