package migration_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/migration"
)

func TestPreviewReportsUnconvertedSourceEntriesAndAbsentZipAttachments(t *testing.T) {
	source := fixtureDirectory(t, "v3.2.52")
	if err := os.WriteFile(filepath.Join(source, "database.db"), []byte("private legacy cache content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "other-state"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "other-state", "private.txt"), []byte("private nested content"), 0600); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	for _, name := range []string{"config.v2.json", "ani.v2.json", "database.db", "other-state/private.txt"} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "legacy.zip")
	if err := os.WriteFile(zipPath, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ name, path string }{{"directory", source}, {"zip", zipPath}} {
		t.Run(item.name, func(t *testing.T) {
			before, err := os.ReadFile(filepath.Join(source, "database.db"))
			if err != nil {
				t.Fatal(err)
			}
			report, err := migration.Preview(item.path)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Unconverted []string `json:"unconvertedPaths"`
				Omitted     []string `json:"omittedPaths"`
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Unconverted, []string{"database.db", "other-state"}) {
				t.Errorf("unconverted source entries=%v", got.Unconverted)
			}
			if !reflect.DeepEqual(got.Omitted, []string{"cache", "files", "torrents", "webui"}) {
				t.Errorf("absent attachments=%v", got.Omitted)
			}
			if bytes.Contains(raw, []byte("private legacy cache content")) || bytes.Contains(raw, []byte("private nested content")) {
				t.Fatal("report contains private file contents")
			}
			after, err := os.ReadFile(filepath.Join(source, "database.db"))
			if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
				t.Fatal("source changed during preview")
			}
		})
	}
}
