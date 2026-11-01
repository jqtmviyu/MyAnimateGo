package torrent_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/torrent"
)

func TestParseV1SingleFileUsesKnownRawInfoHash(t *testing.T) {
	metadata := parseFixture(t, "v1-single.torrent")
	const expectedHash = "bcf2e8247f61dd5871b7de2f04236d84b32706f0"
	if metadata.HashV1 != expectedHash || metadata.HashV2 != "" || metadata.TransmissionHash != expectedHash {
		t.Fatalf("v1 hashes = v1:%q v2:%q transmission:%q", metadata.HashV1, metadata.HashV2, metadata.TransmissionHash)
	}
	if metadata.MagnetURI != "magnet:?xt=urn:btih:"+expectedHash {
		t.Fatalf("v1 magnet = %q", metadata.MagnetURI)
	}
	if metadata.Name != "test.mkv" {
		t.Fatalf("v1 name = %q", metadata.Name)
	}
	if len(metadata.Files) != 1 || metadata.Files[0] != (torrent.File{Index: 0, Name: "test.mkv", Length: 5}) {
		t.Fatalf("v1 single file = %#v", metadata.Files)
	}
}

func TestParseV1MultiFilePrefersUtf8PathAndKeepsListOrder(t *testing.T) {
	metadata := parseFixture(t, "v1-multifile-utf8.torrent")
	if metadata.Name != "作品" {
		t.Fatalf("v1 UTF-8 name = %q", metadata.Name)
	}
	want := []torrent.File{
		{Index: 0, Name: "集/第01集.mkv", Length: 2048},
		{Index: 1, Name: "subs/subtitle.srt", Length: 78},
	}
	if len(metadata.Files) != len(want) {
		t.Fatalf("files = %#v", metadata.Files)
	}
	for i := range want {
		if metadata.Files[i] != want[i] {
			t.Fatalf("file %d = %#v, want %#v", i, metadata.Files[i], want[i])
		}
	}
}

func TestParsePureV2UsesFullSHA256AndTransmissionShortHash(t *testing.T) {
	metadata := parseFixture(t, "v2-file-tree.torrent")
	const expectedHash = "b6194132e61989c06c2bc0e999f6e607bdbbedad5e94d89a57a8f932b638c027"
	if metadata.HashV1 != "" || metadata.HashV2 != expectedHash || metadata.TransmissionHash != expectedHash[:40] {
		t.Fatalf("v2 hashes = v1:%q v2:%q transmission:%q", metadata.HashV1, metadata.HashV2, metadata.TransmissionHash)
	}
	if metadata.MagnetURI != "magnet:?xt=urn:btmh:1220"+metadata.HashV2 {
		t.Fatalf("v2 magnet = %q", metadata.MagnetURI)
	}
	if metadata.Name != "双语作品" {
		t.Fatalf("v2 UTF-8 name = %q", metadata.Name)
	}
	want := []torrent.File{{Index: 0, Name: "盘/字幕.srt", Length: 64}, {Index: 1, Name: "盘/电影.mkv", Length: 12345}}
	if len(metadata.Files) != len(want) {
		t.Fatalf("v2 file tree = %#v", metadata.Files)
	}
	for i := range want {
		if metadata.Files[i] != want[i] {
			t.Fatalf("v2 file %d = %#v, want %#v", i, metadata.Files[i], want[i])
		}
	}
}

func TestParseHybridExposesBothHashesButRecommendsV1(t *testing.T) {
	metadata := parseFixture(t, "hybrid.torrent")
	const expectedV1 = "59a30abae941065060737c35ec5fd4961aa9e829"
	const expectedV2 = "4566609c968a6814d2210ed8937cd0ac24a4cc89dc4514280ed5bdf538b405c5"
	if metadata.HashV1 != expectedV1 || metadata.HashV2 != expectedV2 || metadata.TransmissionHash != metadata.HashV1 {
		t.Fatalf("hybrid hashes = v1:%q v2:%q transmission:%q", metadata.HashV1, metadata.HashV2, metadata.TransmissionHash)
	}
	if !strings.Contains(metadata.MagnetURI, "xt=urn:btih:"+metadata.HashV1) || !strings.Contains(metadata.MagnetURI, "xt=urn:btmh:1220"+metadata.HashV2) {
		t.Fatalf("hybrid magnet = %q", metadata.MagnetURI)
	}
	if metadata.Name != "混合种子" {
		t.Fatalf("hybrid UTF-8 name = %q", metadata.Name)
	}
	if len(metadata.Files) != 1 || metadata.Files[0].Name != "hybrid/第1集.mkv" || metadata.Files[0].Length != 5 {
		t.Fatalf("hybrid v1 file index = %#v", metadata.Files)
	}
}

func TestParseRejectsMalformedTorrentAndMissingInfo(t *testing.T) {
	for name, data := range map[string][]byte{
		"truncated":      []byte("d4:info d4:name"),
		"missing info":   []byte("d4:name4:teste"),
		"non-dictionary": []byte("d4:infoi1ee"),
		"trailing data":  []byte("d4:info d4:name4:testeeextra"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := torrent.Parse(data); err == nil {
				t.Fatal("invalid torrent should return a parse error")
			}
		})
	}
}

func parseFixture(t *testing.T, name string) *torrent.Metadata {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := torrent.Parse(data)
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return metadata
}
