package scheduler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/scheduler"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestAutomaticRenameScanRenamesTransmissionTask(t *testing.T) {
	ctx := context.Background()
	hash := strings.Repeat("c", 40)
	downloadDir := t.TempDir()
	originalName := "Show - 01.mkv"
	targetName := "Show S01E01.mkv"
	oldPath, newPath := filepath.Join(downloadDir, originalName), filepath.Join(downloadDir, targetName)
	if err := os.WriteFile(oldPath, []byte("completed media"), 0600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	torrent := map[string]any{
		"id": 1, "hashString": hash, "name": originalName, "downloadDir": downloadDir,
		"labels": []string{"ani-rss", "Demo"}, "status": 6, "isFinished": true,
		"totalSize": int64(100), "haveValid": int64(100),
		"files": []any{map[string]any{"name": originalName, "length": int64(100), "bytesCompleted": int64(100)}},
	}
	renameCalls := make(chan struct{}, 1)
	labelCalls := make(chan struct{}, 1)
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "rename-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode RPC request: %v", err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch request.Method {
		case "torrent-get":
			writeRPC(w, map[string]any{"torrents": []any{torrent}})
		case "torrent-rename-path":
			if request.Arguments["path"] != originalName || request.Arguments["name"] != targetName {
				t.Errorf("rename path/name = %#v/%#v, want %q/%q", request.Arguments["path"], request.Arguments["name"], originalName, targetName)
			}
			if err := os.Rename(oldPath, newPath); err != nil {
				t.Errorf("rename fixture media: %v", err)
			}
			torrent["name"] = targetName
			renameCalls <- struct{}{}
			writeRPC(w, map[string]any{})
		case "torrent-set":
			labels, _ := request.Arguments["labels"].([]any)
			torrent["labels"] = labels
			labelCalls <- struct{}{}
			writeRPC(w, map[string]any{})
		default:
			t.Errorf("unexpected RPC method %q", request.Method)
			writeRPC(w, map[string]any{})
		}
	}))
	defer rpc.Close()

	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{
		"automaticTasksPaused": false, "rss": false, "rename": true,
		"renameSleepSeconds": float64(3600), "downloadToolHost": rpc.URL,
		"downloadPathTemplate": downloadDir, "renameTemplate": "${title} S${seasonFormat}E${episodeFormat}",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{
		"id": "rename-sub", "url": "fixture://rss", "title": "Show", "season": float64(1),
		"subgroup": "Demo", "enable": true, "ova": false, "globalExclude": false,
		"match": []any{}, "exclude": []any{}, "standbyRssList": []any{},
	}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{RSSFetcher: func(context.Context, string) ([]byte, error) {
		return []byte(`<rss><channel><item><title>Show - 01</title><enclosure url="magnet:?xt=urn:btih:` + hash + `"/><guid>` + hash + `</guid></item></channel></rss>`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := scheduler.New(store, scheduler.Options{
		Engine: engine, PollInterval: time.Millisecond, RetryDelay: time.Millisecond,
		Callbacks: scheduler.Callbacks{
			ReconcileMetadata: func(context.Context, string) (bool, error) { return false, nil },
			RefreshMetadata:   func(context.Context, string) error { return nil },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer closeRuntime(t, runtime)

	for name, result := range map[string]<-chan struct{}{"torrent-rename-path": renameCalls, "RENAME label": labelCalls} {
		select {
		case <-result:
		case <-time.After(3 * time.Second):
			t.Fatalf("automatic rename did not reach %s", name)
		}
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("original media path still exists: %v", err)
	}
	if data, err := os.ReadFile(newPath); err != nil || string(data) != "completed media" {
		t.Fatalf("renamed media = %q, %v", data, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if torrent["name"] != targetName {
		t.Fatalf("Transmission task name = %#v, want %q", torrent["name"], targetName)
	}
	labels, _ := torrent["labels"].([]any)
	if !containsLabel(labels, "RENAME") {
		t.Fatalf("Transmission task labels = %#v, want RENAME", labels)
	}
}

func TestPendingRenameIsNotStarvedByExpiredAutomaticScans(t *testing.T) {
	ctx := context.Background()
	hash := strings.Repeat("d", 40)
	downloadDir := t.TempDir()
	originalName, targetName := "Show - 01.mkv", "Show S01E01.mkv"
	oldPath, newPath := filepath.Join(downloadDir, originalName), filepath.Join(downloadDir, targetName)
	if err := os.WriteFile(oldPath, []byte("completed media"), 0600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	torrent := map[string]any{
		"id": 1, "hashString": hash, "name": originalName, "downloadDir": downloadDir,
		"labels": []string{"ani-rss", "Demo"}, "status": 6, "isFinished": true,
		"totalSize": int64(100), "haveValid": int64(100),
		"files": []any{map[string]any{"name": originalName, "length": int64(100), "bytesCompleted": int64(100)}},
	}
	renameCalls := make(chan struct{}, 1)
	labelCalls := make(chan struct{}, 1)
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "rename-queue-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode RPC request: %v", err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch request.Method {
		case "torrent-get":
			writeRPC(w, map[string]any{"torrents": []any{torrent}})
		case "torrent-rename-path":
			if request.Arguments["path"] != originalName || request.Arguments["name"] != targetName {
				t.Errorf("rename path/name = %#v/%#v, want %q/%q", request.Arguments["path"], request.Arguments["name"], originalName, targetName)
			}
			if err := os.Rename(oldPath, newPath); err != nil {
				t.Errorf("rename fixture media: %v", err)
			}
			torrent["name"] = targetName
			renameCalls <- struct{}{}
			writeRPC(w, map[string]any{})
		case "torrent-set":
			labels, _ := request.Arguments["labels"].([]any)
			torrent["labels"] = labels
			labelCalls <- struct{}{}
			writeRPC(w, map[string]any{})
		default:
			t.Errorf("unexpected RPC method %q", request.Method)
			writeRPC(w, map[string]any{})
		}
	}))
	defer rpc.Close()

	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{
		"automaticTasksPaused": false, "rss": false, "rename": true,
		"renameSleepSeconds": float64(3600), "downloadToolHost": rpc.URL,
		"downloadPathTemplate": downloadDir, "renameTemplate": "${title} S${seasonFormat}E${episodeFormat}",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{
		"id": "rename-queue-sub", "url": "fixture://rss", "title": "Show", "season": float64(1),
		"subgroup": "Demo", "enable": true, "ova": false, "globalExclude": false,
		"match": []any{}, "exclude": []any{}, "standbyRssList": []any{},
	}); err != nil {
		t.Fatal(err)
	}
	rss := []byte(`<rss><channel><item><title>Show - 01</title><enclosure url="magnet:?xt=urn:btih:` + hash + `"/><guid>` + hash + `</guid></item></channel></rss>`)
	engine, err := pipeline.New(store, pipeline.Options{RSSFetcher: func(ctx context.Context, _ string) ([]byte, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return rss, nil
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	stalePayload, err := json.Marshal(map[string]any{"scheduledAt": time.Now().Add(-3 * time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 18 {
		if _, err := store.EnqueueJob(ctx, scheduler.JobRenameScan, fmt.Sprintf("stale-scan:%02d", i), stalePayload); err != nil {
			t.Fatal(err)
		}
	}
	renamePayload, err := json.Marshal(map[string]any{
		"subscriptionId": "rename-queue-sub", "torrentHash": hash, "downloadDir": downloadDir,
		"originalName": originalName, "targetName": targetName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnqueueJob(ctx, scheduler.JobRename, "rename:"+hash+":"+targetName, renamePayload); err != nil {
		t.Fatal(err)
	}
	runtime, err := scheduler.New(store, scheduler.Options{
		Engine: engine, PollInterval: time.Millisecond, RetryDelay: time.Millisecond,
		Callbacks: scheduler.Callbacks{
			ReconcileMetadata: func(context.Context, string) (bool, error) { return false, nil },
			RefreshMetadata:   func(context.Context, string) error { return nil },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer closeRuntime(t, runtime)

	select {
	case <-renameCalls:
	case <-time.After(time.Second):
		t.Fatal("pending rename job was starved by obsolete rename scans")
	}
	select {
	case <-labelCalls:
	case <-time.After(time.Second):
		t.Fatal("rename job did not write the RENAME label")
	}
	if data, err := os.ReadFile(newPath); err != nil || string(data) != "completed media" {
		t.Fatalf("renamed media = %q, %v", data, err)
	}
}

func containsLabel(labels []any, wanted string) bool {
	for _, label := range labels {
		if label == wanted {
			return true
		}
	}
	return false
}
