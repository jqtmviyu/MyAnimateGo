package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/transmission"
)

const completionTestHash = "abcdef0123456789abcdef0123456789abcdef01"

func TestCompletionUsesAniPathAndPersistsPerFileMove(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := t.TempDir()
	config := map[string]any{"downloadPathTemplate": filepath.Join(root, "${title}"), "completedPathTemplate": filepath.Join(root, "global", "${title}"), "automaticTasksPaused": false, "completed": true, "autoDisabled": true, "delete": true}
	if err := store.SaveConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	sub := map[string]any{"id": "ani-1", "title": "Show", "season": 1, "subgroup": "Group", "completed": true, "customCompleted": true, "customCompletedPathTemplate": filepath.Join(root, "ani-specific", "${title}"), "enable": false, "ova": false, "totalEpisodeNumber": 12, "currentEpisodeNumber": 12}
	if err := store.PutSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := engine.DownloadPath(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	source := valueString(paths["downloadPath"])
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "episode.mkv"), []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	globalPath, err := completedPath(engine, ctx, config, sub)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(globalPath)) != "ani-specific" {
		t.Fatalf("customCompletedPathTemplate ignored: %s", globalPath)
	}
	runtime, err := New(store, Options{Engine: engine})
	if err != nil {
		t.Fatal(err)
	}
	torrents := []transmission.Torrent{{Hash: completionTestHash, Name: "Show S01E12", DownloadDir: source, Status: 6, IsFinished: true, Labels: []string{"ani-rss"}}}
	if err := runtime.enqueueCompletionJobs(ctx, config, sub, source, torrents); err != nil {
		t.Fatal(err)
	}
	key := "move-media:" + completionTestHash + ":" + filepath.Join(source, "episode.mkv")
	job, err := store.EnqueueJob(ctx, JobMoveMedia, key, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var move MediaMove
	if err := json.Unmarshal(job.Payload, &move); err != nil {
		t.Fatal(err)
	}
	if move.SourcePath != filepath.Join(source, "episode.mkv") || move.TargetPath != filepath.Join(globalPath, "episode.mkv") || move.DownloadDir != globalPath {
		t.Fatalf("unexpected durable file move: %#v", move)
	}
	// 若进程在改完 Transmission 路径后崩溃，下一次扫描仍须补齐旧目录文件意图。
	torrents[0].DownloadDir = globalPath
	if err := runtime.enqueueCompletionJobs(ctx, config, sub, source, torrents); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.EnqueueJob(ctx, JobMoveMedia, key, json.RawMessage("{}"))
	if err != nil || recovered.ID != job.ID || recovered.Payload == nil {
		t.Fatalf("file move intent was not recovered: %#v, %v", recovered, err)
	}
}

func TestMediaMoveWithoutServiceIsAnExplicitFailure(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := New(store, Options{})
	if err != nil {
		t.Fatal(err)
	}
	handler := mediaMoveHandler{runtime: runtime}
	job := jobs.Job{ID: "move-1", Payload: json.RawMessage(`{"sourcePath":"/old/a.mkv","targetPath":"/new/a.mkv"}`)}
	if _, err := handler.Reconcile(context.Background(), job); err == nil {
		t.Fatal("missing media reconciliation service was accepted")
	}
	if err := handler.Execute(context.Background(), job); err == nil {
		t.Fatal("missing media service was treated as a successful move")
	}
}

func TestDeletionRejectsStandbyBranchWithoutPolicy(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(context.Background(), map[string]any{"delete": true, "deleteStandbyRSSOnly": false}); err != nil {
		t.Fatal(err)
	}
	runtime, err := New(store, Options{})
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := runtime.deletionAllowed(context.Background(), deletePayload{StandbyReplacement: true}, transmission.Torrent{Hash: completionTestHash, Labels: []string{"ani-rss", "RENAME"}, Status: 6, IsFinished: true})
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("standby replacement deletion bypassed its explicit policy")
	}
}

func TestStandbyReplacementQueuesOnlySameEpisodeWithCompletedMain(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := New(store, Options{})
	if err != nil {
		t.Fatal(err)
	}
	subscription := map[string]any{"id": "ani-1", "subgroup": "主字幕组"}
	main := transmission.Torrent{Hash: completionTestHash, Name: "Show S01E12", DownloadDir: "/downloads/show", Status: 6, IsFinished: true, Labels: []string{"ani-rss"}}
	standby := transmission.Torrent{Hash: "bbbbbb0123456789abcdef0123456789abcdef01", Name: "Show S01E12", DownloadDir: "/downloads/show", Status: 6, IsFinished: true, Labels: []string{"ani-rss", "备用RSS", "RENAME"}}
	wrongEpisode := transmission.Torrent{Hash: "cccccc0123456789abcdef0123456789abcdef01", Name: "Show S01E13", DownloadDir: "/downloads/show", Status: 6, IsFinished: true, Labels: []string{"ani-rss", "备用RSS", "RENAME"}}
	err = runtime.enqueueStandbyReplacements(ctx, map[string]any{"awaitStalledUP": false}, subscription, main.DownloadDir, []pipeline.Item{{InfoHash: main.Hash}}, []transmission.Torrent{main, standby, wrongEpisode})
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.EnqueueJob(ctx, JobDeleteTorrent, "delete-standby:"+standby.Hash, json.RawMessage("{}"))
	if err != nil {
		t.Fatal(err)
	}
	var payload deletePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.StandbyReplacement || payload.MainTorrentHash != main.Hash || payload.TorrentHash != standby.Hash {
		t.Fatalf("unexpected persisted standby replacement intent: %#v", payload)
	}
}
