package collections_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/collections"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
)

type queue struct{ job jobs.Job }

func (q *queue) EnqueueJob(_ context.Context, kind, key string, payload json.RawMessage) (jobs.Job, error) {
	q.job = jobs.Job{ID: "fixture", Kind: kind, Key: key, Payload: payload}
	return q.job, nil
}

func TestCollectionJobSelectsRenamesAndResumesFixtureTorrent(t *testing.T) {
	metainfo, err := os.ReadFile(filepath.Join("..", "torrent", "testdata", "v1-multifile-utf8.torrent"))
	if err != nil {
		t.Fatal(err)
	}
	hash := "" // filled from the fixed torrent fixture by the service's public preview below
	encoded := base64.StdEncoding.EncodeToString(metainfo)
	ani := map[string]any{"title": "作品", "season": 1, "subgroup": "字幕组", "match": []any{}, "exclude": []any{}}
	preview, err := collections.Preview(encoded, ani, map[string]any{"renameTemplate": "${title} S${seasonFormat}E${episodeFormat}"})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	files := []map[string]any{{"name": path.Join(preview.Name, "集/第01集.mkv"), "length": 2048}, {"name": path.Join(preview.Name, "subs/subtitle.srt"), "length": 78}}
	wanted := []int{1, 1}
	var labels []string
	status := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string                     `json:"method"`
			Args   map[string]json.RawMessage `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		args := map[string]any{}
		switch request.Method {
		case "torrent-get":
			if hash == "" {
				args["torrents"] = []any{}
			} else {
				stats := []map[string]bool{{"wanted": wanted[0] != 0}, {"wanted": wanted[1] != 0}}
				args["torrents"] = []any{map[string]any{"hashString": hash, "name": preview.Name, "status": status, "files": files, "fileStats": stats, "labels": labels}}
			}
		case "torrent-add":
			var encodedMetainfo string
			_ = json.Unmarshal(request.Args["metainfo"], &encodedMetainfo)
			if encodedMetainfo != encoded {
				t.Errorf("torrent-add metainfo differed from fixture")
			}
			if hash == "" {
				hash = preview.Hash
			}
			_ = json.Unmarshal(request.Args["labels"], &labels)
			args["torrent-added"] = map[string]string{"hashString": hash}
		case "torrent-set":
			var ids []int
			if raw := request.Args["files-unwanted"]; len(raw) > 0 {
				_ = json.Unmarshal(raw, &ids)
				for _, id := range ids {
					wanted[id] = 0
				}
			}
			if raw := request.Args["files-wanted"]; len(raw) > 0 {
				_ = json.Unmarshal(raw, &ids)
				for _, id := range ids {
					wanted[id] = 1
				}
			}
		case "torrent-rename-path":
			var oldPath, name string
			_ = json.Unmarshal(request.Args["path"], &oldPath)
			_ = json.Unmarshal(request.Args["name"], &name)
			for _, file := range files {
				if file["name"] == oldPath {
					file["name"] = path.Join(path.Dir(oldPath), name)
				}
			}
		case "torrent-start":
			status = 4
		default:
			t.Errorf("unexpected Transmission method %q", request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"arguments": args, "result": "success"})
	}))
	defer server.Close()
	q := &queue{}
	service, err := collections.New(collections.Options{Enqueuer: q, Config: func(context.Context) (map[string]any, error) {
		return map[string]any{"downloadToolHost": server.URL, "renameTemplate": "${title} S${seasonFormat}E${episodeFormat}"}, nil
	}, DownloadPath: func(context.Context, map[string]any) (string, error) { return "/downloads", nil }, PollInterval: time.Millisecond, PollTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	job := jobs.Job{Kind: collections.JobKind, Payload: mustJSON(t, map[string]any{"torrent": encoded, "ani": ani})}
	done, err := service.Handler().Reconcile(context.Background(), job)
	if err != nil || done {
		t.Fatalf("initial reconcile = %v, %v", done, err)
	}
	if err := service.Handler().Execute(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	done, err = service.Handler().Reconcile(context.Background(), job)
	if err != nil || !done {
		t.Fatalf("final reconcile = %v, %v", done, err)
	}
	if wanted[0] != 1 || wanted[1] != 0 || files[0]["name"] != path.Join(preview.Name, "集/作品 S01E01.mkv") || status != 4 {
		t.Fatalf("Transmission state wanted=%v files=%v status=%d", wanted, files, status)
	}
}

func TestCollectionDoesNotModifyAnUnownedDuplicate(t *testing.T) {
	metainfo, err := os.ReadFile(filepath.Join("..", "torrent", "testdata", "v1-multifile-utf8.torrent"))
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(metainfo)
	preview, err := collections.Preview(encoded, map[string]any{"title": "作品", "season": 1}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var mutationCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		response := map[string]any{"arguments": map[string]any{}, "result": "success"}
		switch request.Method {
		case "torrent-add":
			response["arguments"] = map[string]any{"torrent-duplicate": map[string]string{"hashString": preview.Hash}}
		case "torrent-get":
			response["arguments"] = map[string]any{"torrents": []any{map[string]any{"hashString": preview.Hash, "labels": []string{"user-created"}}}}
		case "torrent-set", "torrent-rename-path", "torrent-start":
			mutationCalls++
		default:
			t.Errorf("unexpected Transmission method %q", request.Method)
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	service, err := collections.New(collections.Options{
		Enqueuer: &queue{},
		Config: func(context.Context) (map[string]any, error) {
			return map[string]any{"downloadToolHost": server.URL}, nil
		},
		DownloadPath: func(context.Context, map[string]any) (string, error) { return "/downloads", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	job := jobs.Job{Kind: collections.JobKind, Payload: mustJSON(t, map[string]any{"torrent": encoded, "ani": map[string]any{"title": "作品", "season": 1}})}
	if err := service.Handler().Execute(context.Background(), job); err == nil {
		t.Fatal("expected ownership conflict for an existing user task")
	}
	if mutationCalls != 0 {
		t.Fatalf("mutated an unowned Transmission task %d times", mutationCalls)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
