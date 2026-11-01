package pipeline_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestSameSourceUsesNewestReplacementForEpisode(t *testing.T) {
	feed := `<rss><channel><item><title>Example - 01 v2</title><guid>2222222222222222222222222222222222222222</guid><enclosure url="magnet:?xt=urn:btih:2222222222222222222222222222222222222222"/></item><item><title>Example - 01 v1</title><guid>1111111111111111111111111111111111111111</guid><enclosure url="magnet:?xt=urn:btih:1111111111111111111111111111111111111111"/></item></channel></rss>`
	engine, sub := metadataPreview(t, feed, pipeline.Options{}, "${title} S${seasonFormat}E${episodeFormat}")
	preview, err := engine.Preview(context.Background(), sub)
	if err != nil {
		t.Fatal(err)
	}
	items := preview["items"].([]pipeline.Item)
	if len(items) != 1 || items[0].InfoHash != "2222222222222222222222222222222222222222" {
		t.Fatalf("同源新版应覆盖旧版：%#v", items)
	}
}

func TestDownloadPathResolvesMissingJapaneseTitle(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(context.Background(), map[string]any{"downloadPathTemplate": filepath.Join(dir, "${jpTitle}", "Season ${season}")}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{MetadataResolver: func(context.Context, map[string]any, float64) (map[string]string, error) {
		return map[string]string{"jpTitle": "日本語名"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.DownloadPath(context.Background(), map[string]any{"id": "path", "title": "中文名", "season": 2})
	if err != nil {
		t.Fatal(err)
	}
	if result["downloadPath"] != filepath.Join(dir, "日本語名", "Season 2") {
		t.Fatalf("路径应包含解析后的日文标题：%v", result)
	}
}

func TestPreviewResolvesEpisodeMetadataAfterApplyingOffset(t *testing.T) {
	feed := `<rss><channel><item><title>Example - 04</title><enclosure url="magnet:?xt=urn:btih:1111111111111111111111111111111111111111"/></item></channel></rss>`
	calls := 0
	options := pipeline.Options{MetadataResolver: func(ctx context.Context, sub map[string]any, episode float64) (map[string]string, error) {
		calls++
		if episode != 2 {
			t.Fatalf("元数据应使用偏移后的集数：%v", episode)
		}
		return map[string]string{"episodeTitle": "第二集标题"}, nil
	}}
	engine, sub := metadataPreview(t, feed, options, "${title} ${bgmId} E${episodeFormat} ${episodeTitle}")
	sub["offset"] = -2
	preview, err := engine.Preview(context.Background(), sub)
	if err != nil {
		t.Fatal(err)
	}
	items := preview["items"].([]pipeline.Item)
	if len(items) != 1 || items[0].ReName != "Example 123 E02 第二集标题" || calls != 1 {
		t.Fatalf("元数据模板未接通：%#v，调用=%d", items, calls)
	}
	plain, sub := metadataPreview(t, feed, options, "${title} ${bgmId} E${episodeFormat}")
	if _, err := plain.Preview(context.Background(), sub); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("本地已有字段足够时应直接命名")
	}
}

func metadataPreview(t *testing.T, feed string, options pipeline.Options, template string) (*pipeline.Engine, map[string]any) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SaveConfig(context.Background(), map[string]any{"downloadPathTemplate": filepath.Join(dir, "media", "${title}"), "renameTemplate": template}); err != nil {
		t.Fatal(err)
	}
	options.RSSFetcher = func(context.Context, string) ([]byte, error) { return []byte(feed), nil }
	engine, err := pipeline.New(store, options)
	if err != nil {
		t.Fatal(err)
	}
	return engine, map[string]any{"id": "metadata", "title": "Example", "season": 1, "url": "https://example.invalid/rss", "subgroup": "Fansub", "bgmUrl": "https://bgm.tv/subject/123"}
}
