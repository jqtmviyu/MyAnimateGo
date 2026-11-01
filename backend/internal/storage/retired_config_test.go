package storage_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestConfigWritesAndImportRetainActiveSettings(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	original := map[string]any{"customJs": "old script", "customCss": "old style", "customEpisode": true, "title": "active"}
	for _, write := range []func() error{
		func() error { return store.SaveConfig(ctx, original) },
		func() error { return store.UpdateConfigFields(ctx, original) },
		func() error { return store.ImportSnapshot(ctx, original, nil) },
	} {
		if err := write(); err != nil {
			t.Fatal(err)
		}
		config, err := store.GetConfig(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if config["customEpisode"] != true || config["title"] != "active" {
			t.Fatal("正常自定义集数或其他设置被丢弃")
		}
		if _, ok := config["customJs"]; ok {
			t.Fatal("旧脚本仍被保存")
		}
		if _, ok := config["customCss"]; ok {
			t.Fatal("旧样式仍被保存")
		}
	}
	if len(original) != 4 {
		t.Fatal("写入修改了调用方对象")
	}
}
