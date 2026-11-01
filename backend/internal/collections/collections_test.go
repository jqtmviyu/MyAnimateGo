package collections_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/collections"
)

func TestPreviewIndexesFilesAndAppliesEpisodeNaming(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "torrent", "testdata", "v1-multifile-utf8.torrent"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := collections.Preview(base64.StdEncoding.EncodeToString(data), map[string]any{
		"title": "作品", "season": 1, "subgroup": "字幕组", "match": []any{}, "exclude": []any{},
	}, map[string]any{"renameTemplate": "${title} S${seasonFormat}E${episodeFormat}"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 {
		t.Fatalf("files = %#v", got.Files)
	}
	if got.Files[0].ID != 0 || got.Files[0].Name != "集/第01集.mkv" || got.Files[0].ReName != "作品 S01E01.mkv" {
		t.Fatalf("first file = %#v", got.Files[0])
	}
}
