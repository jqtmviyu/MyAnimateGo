package migration_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/migration"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestPreviewConvertsAllHistoricalFixtureVersions(t *testing.T) {
	for _, version := range []string{"v1.1.50", "v1.2.82", "v3.2.52"} {
		t.Run(version, func(t *testing.T) {
			source := fixtureDirectory(t, version)
			configBefore, err := os.ReadFile(filepath.Join(source, map[bool]string{true: "config.v2.json", false: "config.json"}[version == "v3.2.52"]))
			if err != nil {
				t.Fatal(err)
			}
			report, err := migration.Preview(source)
			if err != nil {
				t.Fatal(err)
			}
			if report.SourceVersion != version || report.SubscriptionCount != 1 {
				t.Fatalf("unexpected preview: %#v", report)
			}
			store, err := storage.Open(filepath.Join(t.TempDir(), "db.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if _, err := migration.Import(context.Background(), store, source); err != nil {
				t.Fatal(err)
			}
			saved, err := store.GetConfig(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			storedReport, err := json.Marshal(saved["migrationReport"])
			expectedReport, _ := json.Marshal(report)
			var stored, expected any
			_ = json.Unmarshal(storedReport, &stored)
			_ = json.Unmarshal(expectedReport, &expected)
			if err != nil || !reflect.DeepEqual(stored, expected) {
				t.Fatalf("persistent migration report differs from preview: %s", storedReport)
			}
			aniList, err := store.ListSubscriptions(context.Background())
			if err != nil || len(aniList) != 1 {
				t.Fatalf("imported subscriptions = %#v, %v", aniList, err)
			}
			ani := aniList[0]
			if ani["id"] != "00000000-0000-4000-8000-000000000001" || ani["currentEpisodeNumber"] != float64(2) {
				t.Fatalf("lost subscription data: %#v", ani)
			}
			if version == "v1.1.50" {
				config, err := store.GetConfig(context.Background())
				if err != nil || config["renameSleepSeconds"] != float64(60) || ani["releaseDatePrecision"] != "month" {
					t.Fatalf("minute interval or month precision lost: %#v %#v (%v)", config, ani, err)
				}
				standby := ani["standbyRssList"].([]any)[0].(map[string]any)
				if standby["url"] != "https://example.invalid/rss/standby.xml" || standby["label"] != "未知字幕组" || standby["offset"] != float64(0) {
					t.Fatalf("legacy standby source was not preserved: %#v", standby)
				}
			}
			if version == "v1.2.82" {
				standby := ani["standbyRssList"].([]any)[0].(map[string]any)
				if standby["label"] != "备用字幕组" || standby["url"] != "https://example.invalid/rss/standby.xml" || standby["offset"] != float64(0) {
					t.Fatalf("nested standby source fields changed: %#v", standby)
				}
			}
			if version == "v1.2.82" || version == "v3.2.52" {
				if ani["releaseDate"] != "2025-04-06" || ani["releaseDatePrecision"] != "day" {
					t.Fatalf("day precision lost: %#v", ani)
				}
			}
			configAfter, err := os.ReadFile(filepath.Join(source, map[bool]string{true: "config.v2.json", false: "config.json"}[version == "v3.2.52"]))
			if err != nil || string(configAfter) != string(configBefore) {
				t.Fatalf("source config changed during preview/import: %v", err)
			}
		})
	}
}

func TestImportDirectoryAndZipIsRepeatableAndPausesAutomaticTasks(t *testing.T) {
	ctx := context.Background()
	for _, asZip := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "zip"}[asZip], func(t *testing.T) {
			source := fixtureDirectory(t, "v3.2.52")
			if asZip {
				source = zipDirectory(t, source)
			}
			db := filepath.Join(t.TempDir(), "db.sqlite")
			store, err := storage.Open(db)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			for i := 0; i < 2; i++ {
				if _, err := migration.Import(ctx, store, source); err != nil {
					t.Fatal(err)
				}
			}
			config, err := store.GetConfig(ctx)
			if err != nil || config["automaticTasksPaused"] != true {
				t.Fatalf("automatic tasks must be paused: %#v, %v", config, err)
			}
			subs, err := store.ListSubscriptions(ctx)
			if err != nil || len(subs) != 1 {
				t.Fatalf("repeat import duplicated records: %#v, %v", subs, err)
			}
		})
	}
}

func TestPreviewReportsUnknownFieldsWithoutValuesAndImportRetainsThem(t *testing.T) {
	source := fixtureDirectory(t, "v3.2.52")
	configPath := filepath.Join(source, "config.v2.json")
	var configInput map[string]any
	readJSON(t, configPath, &configInput)
	configInput["customConfig"] = "private-value"
	writeJSON(t, configPath, configInput)
	subPath := filepath.Join(source, "ani.v2.json")
	var subInput []map[string]any
	readJSON(t, subPath, &subInput)
	subInput[0]["customField"] = "private-value"
	writeJSON(t, subPath, subInput)

	report, err := migration.Preview(source)
	if err != nil {
		t.Fatal(err)
	}
	encodedReport, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.UnknownFields) != 2 || bytes.Contains(encodedReport, []byte("private-value")) {
		t.Fatalf("report must name unknown fields without values: %#v", report)
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := migration.Import(context.Background(), store, source); err != nil {
		t.Fatal(err)
	}
	config, err := store.GetConfig(context.Background())
	if err != nil || config["customConfig"] != "private-value" {
		t.Fatalf("unknown config field was lost: %#v, %v", config, err)
	}
	subs, err := store.ListSubscriptions(context.Background())
	if err != nil || subs[0]["customField"] != "private-value" {
		t.Fatalf("unknown subscription field was lost: %#v, %v", subs, err)
	}
}

func TestImportCopiesDirectoryAttachmentsAndZipSupportedPaths(t *testing.T) {
	for _, asZip := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "zip"}[asZip], func(t *testing.T) {
			source := fixtureDirectory(t, "v3.2.52")
			assetFiles := map[string]string{
				"files/cover.jpg":             "cover-data",
				"torrents/ab/episode.torrent": "torrent-data",
				"cache/rename-cache.json":     "cache-data",
				"webui/webui.json":            "webui-data",
			}
			for name, content := range assetFiles {
				path := filepath.Join(source, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if asZip {
				source = zipDirectory(t, source)
			}
			dataDir := filepath.Join(t.TempDir(), "data")
			if err := os.MkdirAll(dataDir, 0700); err != nil {
				t.Fatal(err)
			}
			store, err := storage.Open(filepath.Join(dataDir, "ani-rss.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			report, err := migration.Import(context.Background(), store, source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := migration.Import(context.Background(), store, source); err != nil {
				t.Fatalf("identical repeat import failed: %v", err)
			}
			if asZip {
				assertHas(t, report.IncludedPaths, "files", "torrents")
				assertHas(t, report.OmittedPaths, "cache", "webui")
			} else {
				assertHas(t, report.IncludedPaths, "files", "torrents", "cache")
				assertHas(t, report.OmittedPaths, "webui")
			}
			for name, content := range assetFiles {
				if strings.HasPrefix(name, "webui/") || asZip && strings.HasPrefix(name, "cache/") {
					continue
				}
				data, err := os.ReadFile(filepath.Join(store.DataDir(), filepath.FromSlash(name)))
				if err != nil || string(data) != content {
					t.Fatalf("imported asset %s = %q, %v", name, data, err)
				}
			}
		})
	}
}

func TestImportConflictLeavesDatabaseAndFilesUntouched(t *testing.T) {
	source := fixtureDirectory(t, "v3.2.52")
	assetPath := filepath.Join(source, "files", "cover.jpg")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("incoming"), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	store, err := storage.Open(filepath.Join(dataDir, "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(context.Background(), map[string]any{"marker": "before"}); err != nil {
		t.Fatal(err)
	}
	conflictPath := filepath.Join(dataDir, "files", "cover.jpg")
	if err := os.MkdirAll(filepath.Dir(conflictPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflictPath, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = migration.Import(context.Background(), store, source)
	if err == nil {
		t.Fatal("import accepted an existing path with different content")
	}
	config, readErr := store.GetConfig(context.Background())
	if readErr != nil || config["marker"] != "before" {
		t.Fatalf("conflict changed database: %#v, %v", config, readErr)
	}
	data, readErr := os.ReadFile(conflictPath)
	if readErr != nil || string(data) != "existing" {
		t.Fatalf("conflict changed existing file: %q, %v", data, readErr)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestImportRejectsSymlinkedDestinationDirectory(t *testing.T) {
	source := fixtureDirectory(t, "v3.2.52")
	assetPath := filepath.Join(source, "files", "cover.jpg")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("incoming"), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dataDir, "files")); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(dataDir, "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := migration.Import(context.Background(), store, source); err == nil {
		t.Fatal("import followed a symbolic link in the destination")
	}
	if _, err := os.Stat(filepath.Join(outside, "cover.jpg")); !os.IsNotExist(err) {
		t.Fatalf("import wrote through destination symlink: %v", err)
	}
}

func TestImportRollsBackNewFilesWhenDatabaseWriteFails(t *testing.T) {
	source := fixtureDirectory(t, "v3.2.52")
	assetPath := filepath.Join(source, "files", "cover.jpg")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("incoming"), 0600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "ani-rss.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(context.Background(), map[string]any{"marker": "before"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = migration.Import(context.Background(), store, source)
	if err == nil {
		t.Fatal("import succeeded with a closed database")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dbPath), "files", "cover.jpg")); !os.IsNotExist(err) {
		t.Fatalf("new file remained after database failure: %v", err)
	}
	store, err = storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config, err := store.GetConfig(context.Background())
	if err != nil || config["marker"] != "before" {
		t.Fatalf("database changed after failed import: %#v, %v", config, err)
	}
}

func TestImportRejectsAssetSymlinksAndZipTraversal(t *testing.T) {
	source := fixtureDirectory(t, "v3.2.52")
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "files"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "files", "linked.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := migration.Preview(source); err == nil {
		t.Fatal("preview accepted a symbolic link in the attachment tree")
	}

	zipPath := filepath.Join(t.TempDir(), "unsafe.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(file)
	for name, content := range map[string]string{
		"config.v2.json":      "{}",
		"ani.v2.json":         "[]",
		"../files/escape.txt": "escape",
	} {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := migration.Preview(zipPath); err == nil {
		t.Fatal("preview accepted a ZIP path traversal entry")
	}
}

func assertHas(t *testing.T, got []string, want ...string) {
	t.Helper()
	for _, item := range want {
		found := false
		for _, actual := range got {
			if actual == item {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%q not found in %v", item, got)
		}
	}
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func fixtureDirectory(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	for _, suffix := range []string{"config.json", "subscriptions.json"} {
		name := version + "-" + suffix
		if version == "v3.2.52" {
			name = version + "-" + map[string]string{"config.json": "config.json", "subscriptions.json": "subscriptions.json"}[suffix]
		}
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		target := map[string]string{"config.json": "config.json", "subscriptions.json": "ani.json"}[suffix]
		if version == "v3.2.52" {
			target = map[string]string{"config.json": "config.v2.json", "subscriptions.json": "ani.v2.json"}[suffix]
		}
		if err := os.WriteFile(filepath.Join(dir, target), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func zipDirectory(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(file)
	err = filepath.WalkDir(dir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, filePath)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		writer, err := w.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		_, err = io.Copy(writer, bytes.NewReader(data))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
