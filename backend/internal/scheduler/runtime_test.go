package scheduler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/scheduler"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

const jobHash = "abcdef0123456789abcdef0123456789abcdef01"

type restoredExternalHandler struct{ completed bool }

func (h *restoredExternalHandler) Reconcile(context.Context, jobs.Job) (bool, error) {
	return h.completed, nil
}
func (h *restoredExternalHandler) Execute(context.Context, jobs.Job) error {
	h.completed = true
	return nil
}

func TestRuntimeRestoresRegisteredBusinessTask(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{"automaticTasksPaused": false}); err != nil {
		t.Fatal(err)
	}
	job, err := store.EnqueueJob(ctx, "fixture-business-task", "persisted", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	handler := &restoredExternalHandler{}
	runtime, err := scheduler.New(store, scheduler.Options{Handlers: map[string]jobs.Handler{"fixture-business-task": handler}, PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for {
		current, err := store.GetJob(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.State == "resolved" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("业务任务未恢复：%+v", current)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestManualRefreshJobSurvivesRestartWithoutRepeatingAcceptedAdd(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "state.db")
	var rpcMu sync.Mutex
	added := 0
	torrents := []map[string]any{}
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "scheduler-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode rpc request: %v", err)
			return
		}
		rpcMu.Lock()
		defer rpcMu.Unlock()
		switch request.Method {
		case "torrent-get":
			writeRPC(w, map[string]any{"torrents": torrents})
		case "torrent-add":
			added++
			torrents = []map[string]any{{
				"id": 11, "hashString": jobHash, "name": "Show", "status": 4, "downloadDir": "/downloads",
				"labels": []string{"ani-rss", "Demo"}, "isFinished": false, "totalSize": 100, "haveValid": 0,
			}}
			if added == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			writeRPC(w, map[string]any{"torrent-added": map[string]any{"hashString": jobHash}})
		default:
			t.Errorf("unexpected rpc method %q", request.Method)
			writeRPC(w, map[string]any{})
		}
	}))
	defer rpc.Close()

	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	seedRuntimeData(t, store, rpc.URL)
	engine := newEngine(t, store)
	options := scheduler.Options{
		Engine: engine, PollInterval: 5 * time.Millisecond, RetryDelay: 5 * time.Millisecond,
		Callbacks: scheduler.Callbacks{
			ReconcileMetadata: func(context.Context, string) (bool, error) { return false, nil },
			RefreshMetadata:   func(context.Context, string) error { return nil },
		},
	}
	runtime, err := scheduler.New(store, options)
	if err != nil {
		t.Fatal(err)
	}
	job, err := runtime.EnqueueRefresh(ctx, "sub-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, store, job.ID, "resolved")
	closeRuntime(t, runtime)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	engine = newEngine(t, store)
	runtime, err = scheduler.New(store, optionsWithEngine(options, engine))
	if err != nil {
		t.Fatal(err)
	}
	secondJob, err := runtime.EnqueueRefresh(ctx, "sub-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitForState(t, store, secondJob.ID, "resolved")
	closeRuntime(t, runtime)
	rpcMu.Lock()
	defer rpcMu.Unlock()
	if added != 1 {
		t.Fatalf("Transmission accepted %d add requests after recovery", added)
	}
}

func TestPauseStopsEnqueueAndMissingRequiredCallbackFailsJob(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{"automaticTasksPaused": true}); err != nil {
		t.Fatal(err)
	}
	runtime, err := scheduler.New(store, scheduler.Options{MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.EnqueueRefresh(ctx, "sub-1"); !errors.Is(err, scheduler.ErrAutomaticTasksPaused) {
		t.Fatalf("enqueue while paused returned %v", err)
	}
	if err := runtime.SetPaused(ctx, false); err != nil {
		t.Fatal(err)
	}
	job, err := store.EnqueueJob(ctx, scheduler.JobKindBackup, "backup:fixture", json.RawMessage(`{"key":"fixture"}`))
	if err != nil {
		t.Fatal(err)
	}
	if processed, err := runtime.RunOnce(ctx); !processed || err == nil {
		t.Fatalf("missing backup callback result = processed:%v err:%v", processed, err)
	}
	got, err := store.GetJob(ctx, job.ID)
	if err != nil || got.State != "failed" || !strings.Contains(got.LastError, "callback") {
		t.Fatalf("missing callback job = %#v, %v", got, err)
	}
}

func seedRuntimeData(t *testing.T, store *storage.Store, rpcURL string) {
	t.Helper()
	ctx := context.Background()
	if err := store.SaveConfig(ctx, map[string]any{
		"automaticTasksPaused": false, "rss": false, "rename": false,
		"configBackup": false, "downloadToolHost": rpcURL,
		"downloadPathTemplate": filepath.Join(store.DataDir(), "downloads", "${title}"),
		"downloadRetry":        float64(1), "standbyRss": false, "coexist": false,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{
		"id": "sub-1", "url": "fixture", "title": "Show", "season": float64(1), "offset": float64(0),
		"subgroup": "Demo", "enable": true, "ova": false, "globalExclude": false,
		"match": []any{}, "exclude": []any{}, "standbyRssList": []any{}, "notDownload": []any{},
	}); err != nil {
		t.Fatal(err)
	}
}

func newEngine(t *testing.T, store *storage.Store) *pipeline.Engine {
	t.Helper()
	engine, err := pipeline.New(store, pipeline.Options{RSSFetcher: func(context.Context, string) ([]byte, error) {
		return []byte(`<rss><channel><item><title>Show - 01</title><enclosure url="magnet:?xt=urn:btih:` + jobHash + `"/><guid>` + jobHash + `</guid></item></channel></rss>`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func optionsWithEngine(options scheduler.Options, engine *pipeline.Engine) scheduler.Options {
	options.Engine = engine
	return options
}

func waitForState(t *testing.T, store *storage.Store, id, state string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := store.GetJob(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == state {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	job, err := store.GetJob(context.Background(), id)
	t.Fatalf("job %s state=%s lastError=%q (%v), want %s", id, job.State, job.LastError, err, state)
}

func closeRuntime(t *testing.T, runtime *scheduler.Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func writeRPC(w http.ResponseWriter, arguments map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"arguments": arguments, "result": "success"})
}
