package library_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/library"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func openStore(t *testing.T, dir string, config map[string]any) *storage.Store {
	t.Helper()
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SaveConfig(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestFileOperationsStayWithinResolvedTempDirectories(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"Show S01E01.mkv": "video", "Show S01E01.ass": "subtitle", "poster.jpg": "art", "random.txt": "other"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	store := openStore(t, root, nil)
	svc, err := library.New(store, library.Options{DownloadPath: func(_ context.Context, ani map[string]any) (map[string]any, error) {
		if ani["customDownloadPathTemplate"] == target {
			return map[string]any{"downloadPath": target}, nil
		}
		return map[string]any{"downloadPath": src}, nil
	},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MoveFiles(context.Background(), map[string]any{"season": 1, "ova": false}, target, library.MoveOptions{CopyMode: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "Show S01E01.mkv")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "poster.jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("系列移动不能复制无关文件：%v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "Show S01E01.mkv")); err != nil {
		t.Fatalf("复制模式应保留来源：%v", err)
	}
	if err := svc.DeleteFiles(context.Background(), map[string]any{"season": 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("删除必须限定到已解析目录：%v", err)
	}
}

func TestMoveFilesRecoversInterruptedCopyAndReplacesSameSizeDifferentContent(t *testing.T) {
	for _, test := range []struct{ name, old, new string }{{"identical copy before source unlink", "video", "video"}, {"stale target", "fresh", "stale"}} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
			if err := os.MkdirAll(source, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(target, 0755); err != nil {
				t.Fatal(err)
			}
			sourceFile, targetFile := filepath.Join(source, "Show S01E01.mkv"), filepath.Join(target, "Show S01E01.mkv")
			if err := os.WriteFile(sourceFile, []byte(test.old), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(targetFile, []byte(test.new), 0600); err != nil {
				t.Fatal(err)
			}
			store := openStore(t, root, nil)
			svc, err := library.New(store, library.Options{DownloadPath: func(context.Context, map[string]any) (map[string]any, error) {
				return map[string]any{"downloadPath": source}, nil
			},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.MoveFiles(context.Background(), map[string]any{"season": 1}, target, library.MoveOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(sourceFile); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("source remains after recovery: %v", err)
			}
			data, err := os.ReadFile(targetFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != test.old {
				t.Fatalf("target content=%q want %q", data, test.old)
			}
		})
	}
}

func TestFileOperationsRejectOverlappingAndSymlinkPaths(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	nested := filepath.Join(source, "nested")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	store := openStore(t, dataDir, nil)
	svc, err := library.New(store, library.Options{DownloadPath: func(_ context.Context, ani map[string]any) (map[string]any, error) {
		return map[string]any{"downloadPath": ani["path"]}, nil
	},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MoveFiles(context.Background(), map[string]any{"path": source, "ova": true}, nested, library.MoveOptions{}); err == nil {
		t.Fatal("nested destination must be rejected")
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(dataDir, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := svc.DeleteFiles(context.Background(), map[string]any{"path": link}); err == nil {
		t.Fatal("symlink resolution must still enforce application data boundary")
	}
}

func TestMoveCompletedFileReconcilesCopyBeforeSourceRemoval(t *testing.T) {
	root := t.TempDir()
	sourceRoot, targetRoot := filepath.Join(root, "source"), filepath.Join(root, "archive")
	if err := os.MkdirAll(sourceRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetRoot, 0755); err != nil {
		t.Fatal(err)
	}
	source, target := filepath.Join(sourceRoot, "Show S01E01.mkv"), filepath.Join(targetRoot, "Show S01E01.mkv")
	if err := os.WriteFile(source, []byte("episode"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("episode"), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	store := openStore(t, dataDir, nil)
	svc, err := library.New(store, library.Options{DownloadPath: func(context.Context, map[string]any) (map[string]any, error) {
		return map[string]any{"downloadPath": sourceRoot}, nil
	},
	})
	if err != nil {
		t.Fatal(err)
	}
	done, err := svc.ReconcileCompletedFile(context.Background(), sourceRoot, targetRoot, source, target)
	if err != nil || done {
		t.Fatalf("copy-before-unlink should be recoverable: done=%v err=%v", done, err)
	}
	if err := svc.MoveCompletedFile(context.Background(), sourceRoot, targetRoot, source, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source remains after retry: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "episode" {
		t.Fatalf("target content=%q err=%v", data, err)
	}
	done, err = svc.ReconcileCompletedFile(context.Background(), sourceRoot, targetRoot, source, target)
	if err != nil || !done {
		t.Fatalf("completed file should reconcile: done=%v err=%v", done, err)
	}
	if err := svc.MoveCompletedFile(context.Background(), sourceRoot, targetRoot, filepath.Join(root, "outside.mkv"), target); err == nil {
		t.Fatal("source outside confirmed root must fail")
	}
}

type fakeTaskManager struct{ moved, deleted int }

func (m *fakeTaskManager) MoveSubscription(context.Context, map[string]any, string) error {
	m.moved++
	return nil
}
func (m *fakeTaskManager) DeleteSubscription(context.Context, map[string]any, bool) error {
	m.deleted++
	return nil
}

func TestSubscriptionMoveAndDeleteCoordinateTaskOwnership(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "old")
	newPath := filepath.Join(root, "new")
	_ = os.MkdirAll(oldPath, 0755)
	_ = os.WriteFile(filepath.Join(oldPath, "Show S01E01.mkv"), []byte("video"), 0644)
	store := openStore(t, root, nil)
	tasks := &fakeTaskManager{}
	svc, err := library.New(store, library.Options{DownloadPath: func(_ context.Context, ani map[string]any) (map[string]any, error) {
		if ani["path"] == "new" {
			return map[string]any{"downloadPath": newPath}, nil
		}
		return map[string]any{"downloadPath": oldPath}, nil
	},

		Tasks: tasks})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MoveSubscription(context.Background(), map[string]any{"id": "a", "path": "old"}, map[string]any{"id": "a", "path": "new"}); err != nil {
		t.Fatal(err)
	}
	if tasks.moved != 1 {
		t.Fatalf("下载器任务移动调用=%d", tasks.moved)
	}
	if _, err := os.Stat(filepath.Join(newPath, "Show S01E01.mkv")); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteSubscription(context.Background(), map[string]any{"id": "a", "path": "new"}, true); err != nil {
		t.Fatal(err)
	}
	if tasks.deleted != 1 {
		t.Fatalf("下载器任务删除调用=%d", tasks.deleted)
	}
}

func TestDeletingSubscriptionWithoutMediaOptionOnlyClearsItsTorrentCache(t *testing.T) {
	root := t.TempDir()
	store := openStore(t, root, nil)
	otherID := "b-sub"
	targetID := "a-sub"
	for _, id := range []string{targetID, otherID} {
		dir := filepath.Join(root, "torrents", id[:1], id)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "hash.torrent"), []byte(id), 0644); err != nil {
			t.Fatal(err)
		}
	}
	svc, err := library.New(store, library.Options{DownloadPath: func(context.Context, map[string]any) (map[string]any, error) {
		return map[string]any{"downloadPath": filepath.Join(root, "media")}, nil
	},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteSubscription(context.Background(), map[string]any{"id": targetID}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "torrents", targetID[:1], targetID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("目标订阅缓存未清理：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "torrents", otherID[:1], otherID, "hash.torrent")); err != nil {
		t.Fatalf("其他订阅缓存受影响：%v", err)
	}
}

func TestDefaultTaskManagerMovesAndDeletesOnlyMyAnimeGoTasksForResolvedPath(t *testing.T) {
	root := t.TempDir()
	oldPath, newPath := filepath.Join(root, "old"), filepath.Join(root, "new")
	if err := os.MkdirAll(oldPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldPath, "Show S01E01.mkv"), []byte("v"), 0644); err != nil {
		t.Fatal(err)
	}
	moved, removed := []string{}, []string{}
	taskPath := oldPath
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.NewDecoder(r.Body).Decode(&call)
		w.Header().Set("Content-Type", "application/json")
		switch call.Method {
		case "torrent-get":
			_, _ = w.Write([]byte(fmt.Sprintf(`{"result":"success","arguments":{"torrents":[{"hashString":"owned","downloadDir":%q,"labels":["ani-rss"]}]}}`, taskPath)))
		case "torrent-set-location":
			ids, _ := call.Arguments["ids"].([]any)
			taskPath = fmt.Sprint(call.Arguments["location"])
			moved = append(moved, fmt.Sprint(ids[0])+":"+taskPath)
			_, _ = w.Write([]byte(`{"result":"success","arguments":{}}`))
		case "torrent-remove":
			ids, _ := call.Arguments["ids"].([]any)
			removed = append(removed, fmt.Sprint(ids[0])+":"+fmt.Sprint(call.Arguments["delete-local-data"]))
			_, _ = w.Write([]byte(`{"result":"success","arguments":{}}`))
		default:
			t.Errorf("unexpected RPC %s", call.Method)
			http.Error(w, "bad", 400)
		}
	}))
	defer rpc.Close()
	store := openStore(t, root, map[string]any{"downloadToolHost": rpc.URL, "downloadToolUsername": "fixture-user", "downloadToolPassword": "fixture-pass"})
	pathResolver := func(_ context.Context, ani map[string]any) (map[string]any, error) {
		if ani["location"] == "new" {
			return map[string]any{"downloadPath": newPath}, nil
		}
		return map[string]any{"downloadPath": oldPath}, nil
	}
	svc, err := library.New(store, library.Options{DownloadPath: pathResolver,

		TransmissionHTTPClient: rpc.Client()})
	if err != nil {
		t.Fatal(err)
	}
	oldAni, newAni := map[string]any{"id": "show", "location": "old"}, map[string]any{"id": "show", "location": "new"}
	if err := svc.MoveSubscription(context.Background(), oldAni, newAni); err != nil {
		t.Fatal(err)
	}
	resolvedNewPath, _ := filepath.EvalSymlinks(newPath)
	if len(moved) != 1 || moved[0] != "owned:"+resolvedNewPath {
		t.Fatalf("只有 MyAnimeGo 任务应移动，记录=%v", moved)
	}
	if _, err := os.Stat(filepath.Join(newPath, "Show S01E01.mkv")); err != nil {
		t.Fatal(err)
	}
	newAni["location"] = "new"
	if err := svc.DeleteSubscription(context.Background(), newAni, true); err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "owned:true" {
		t.Fatalf("只删除 MyAnimeGo 任务并清理本地数据，记录=%v", removed)
	}
	if _, err := os.Stat(newPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("目标目录应被显式删除：%v", err)
	}
}

func TestTaskManagerRefusesSharedDirectoryWithUnownedTorrent(t *testing.T) {
	root := t.TempDir()
	oldPath, newPath := filepath.Join(root, "old"), filepath.Join(root, "new")
	if err := os.MkdirAll(oldPath, 0755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(oldPath, "Show S01E01.mkv")
	if err := os.WriteFile(media, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	mutated := false
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&call)
		if call.Method != "torrent-get" {
			mutated = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"result":"success","arguments":{"torrents":[{"hashString":"owned","downloadDir":%q,"labels":["ani-rss"]},{"hashString":"foreign","downloadDir":%q,"labels":["user"]}]}}`, oldPath, oldPath)))
	}))
	defer rpc.Close()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	store := openStore(t, dataDir, map[string]any{"downloadToolHost": rpc.URL})
	resolver := func(_ context.Context, sub map[string]any) (map[string]any, error) {
		if sub["path"] == "new" {
			return map[string]any{"downloadPath": newPath}, nil
		}
		return map[string]any{"downloadPath": oldPath}, nil
	}
	svc, err := library.New(store, library.Options{DownloadPath: resolver, TransmissionHTTPClient: rpc.Client()})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.MoveSubscription(context.Background(), map[string]any{"id": "show", "path": "old"}, map[string]any{"id": "show", "path": "new"})
	if err == nil {
		t.Fatal("shared media directory with foreign task should be refused")
	}
	if mutated {
		t.Fatal("foreign task directory refusal must happen before Transmission writes")
	}
	if _, err := os.Stat(media); err != nil {
		t.Fatalf("refused move changed source media: %v", err)
	}
}

func TestCompletedMigrationRequiresCompletionAndMovesTasksBeforeFiles(t *testing.T) {
	root := t.TempDir()
	oldPath, archivePath := filepath.Join(root, "current"), filepath.Join(root, "archive")
	if err := os.MkdirAll(oldPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldPath, "Show S01E01.mkv"), []byte("complete"), 0644); err != nil {
		t.Fatal(err)
	}
	store := openStore(t, root, map[string]any{"autoDisabled": true, "completed": true, "completedPathTemplate": "archive"})
	tasks := &fakeTaskManager{}
	svc, err := library.New(store, library.Options{
		DownloadPath: func(_ context.Context, ani map[string]any) (map[string]any, error) {
			if ani["customDownloadPathTemplate"] == "archive" {
				return map[string]any{"downloadPath": archivePath}, nil
			}
			return map[string]any{"downloadPath": oldPath}, nil
		},

		Tasks: tasks,
	})
	if err != nil {
		t.Fatal(err)
	}
	ani := map[string]any{"id": "show", "completed": true, "currentEpisodeNumber": 4, "totalEpisodeNumber": 4, "enable": false, "ova": false}
	if err := svc.MoveCompleted(context.Background(), ani); err != nil {
		t.Fatal(err)
	}
	if tasks.moved != 1 {
		t.Fatalf("完结迁移任务移动次数=%d", tasks.moved)
	}
	if _, err := os.Stat(filepath.Join(archivePath, "Show S01E01.mkv")); err != nil {
		t.Fatalf("完结文件未移动：%v", err)
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("来源目录应清理：%v", err)
	}
	ani["enable"] = true
	if err := svc.MoveCompleted(context.Background(), ani); err != nil {
		t.Fatal(err)
	}
	if tasks.moved != 1 {
		t.Fatal("启用订阅不应执行完结迁移")
	}
}

func TestClearDirectoryAndCoverCleanupPreserveReferencedMedia(t *testing.T) {
	root := t.TempDir()
	store := openStore(t, root, nil)
	files := filepath.Join(root, "files", "a")
	if err := os.MkdirAll(files, 0755); err != nil {
		t.Fatal(err)
	}
	used, orphan := filepath.Join(files, "used.jpg"), filepath.Join(files, "orphan.jpg")
	if err := os.WriteFile(used, []byte("cover"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("orphan"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(context.Background(), map[string]any{"id": "a", "cover": "a/used.jpg"}); err != nil {
		t.Fatal(err)
	}
	svc, err := library.New(store, library.Options{DownloadPath: func(context.Context, map[string]any) (map[string]any, error) {
		return map[string]any{"downloadPath": root}, nil
	},
	})
	if err != nil {
		t.Fatal(err)
	}
	removed, err := svc.ClearCover(context.Background())
	if err != nil || removed != int64(len("orphan")) {
		t.Fatalf("清理大小=%d err=%v", removed, err)
	}
	if _, err := os.Stat(used); err != nil {
		t.Fatalf("引用中的封面被删除：%v", err)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("孤立封面未清理：%v", err)
	}
	empty := filepath.Join(root, "empty", "Season 1")
	if err := os.MkdirAll(empty, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(empty, "poster.jpg"), []byte("art"), 0644); err != nil {
		t.Fatal(err)
	}
	svc.ClearDir(empty, true, true, 2)
	if _, err := os.Stat(empty); err != nil {
		t.Fatalf("包含 artwork 的目录应保留：%v", err)
	}
}

func TestRenameEpisodeFilesPreservesSubtitleSuffixAndIgnoresUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	for name, contents := range map[string]string{"Show S01E01.mkv": "video", "Show S01E01.zh-Hans.ass": "subtitle", "notes.txt": "notes", "Show S01E02.mkv": "other episode"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	store := openStore(t, dir, nil)
	svc, err := library.New(store, library.Options{DownloadPath: func(context.Context, map[string]any) (map[string]any, error) {
		return map[string]any{"downloadPath": dir}, nil
	},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.RenameEpisodeFiles(context.Background(), map[string]any{"season": 1}, 1, "新名 S01E01")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"新名 S01E01.mkv", "新名 S01E01.zh-Hans.ass", "notes.txt", "Show S01E02.mkv"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("文件 %s 状态错误：%v", name, err)
		}
	}
	if len(result.Renamed) != 2 {
		t.Fatalf("重命名清单=%v", result.Renamed)
	}
}
