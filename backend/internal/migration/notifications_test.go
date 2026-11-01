package migration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/migration"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/notifications"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestImportedLegacyWebhookPreservesTemplateAndEventSelection(t *testing.T) {
	calls := 0
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["text"] != "Show S01E02 已完成" {
			t.Errorf("notification = %q", body["text"])
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer endpoint.Close()
	source := fixtureDirectory(t, "v1.2.82")
	var config map[string]any
	readJSON(t, filepath.Join(source, "config.json"), &config)
	config["webHook"] = true
	config["webHookMethod"] = "POST"
	config["webHookUrl"] = endpoint.URL
	config["webHookBody"] = `{"text":"${message}"}`
	config["messageTemplate"] = "${title} ${text}"
	config["messageList"] = []string{"DOWNLOAD_END"}
	writeJSON(t, filepath.Join(source, "config.json"), config)
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := migration.Import(context.Background(), store, source); err != nil {
		t.Fatal(err)
	}
	service, err := notifications.New(store, notifications.Options{})
	if err != nil {
		t.Fatal(err)
	}
	sub := map[string]any{"id": "show", "title": "Show", "season": 1}
	if err := service.Send(context.Background(), "start", sub, "S01E02 已开始", "DOWNLOAD_START"); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("event selection changed during import")
	}
	if err := service.Send(context.Background(), "end", sub, "S01E02 已完成", "DOWNLOAD_END"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("imported webhook delivered %d times", calls)
	}
}
