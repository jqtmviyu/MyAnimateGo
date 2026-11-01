package library

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/transmission"
)

type transmissionTaskManager struct {
	store        storageReader
	downloadPath DownloadPathResolver
	httpClient   *http.Client
}
type storageReader interface {
	GetConfig(context.Context) (map[string]any, error)
}

func (m *transmissionTaskManager) client(ctx context.Context) (*transmission.Client, error) {
	config, err := m.store.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	host := valueString(config["downloadToolHost"])
	if host == "" {
		return nil, errors.New("Transmission 地址未配置，不能更改关联下载任务")
	}
	return transmission.New(transmission.Config{URL: host, Username: valueString(config["downloadToolUsername"]), Password: valueString(config["downloadToolPassword"]), HTTPClient: m.httpClient}), nil
}

func (m *transmissionTaskManager) matching(ctx context.Context, subscription map[string]any) (*transmission.Client, []transmission.Torrent, error) {
	resolved, err := m.downloadPath(ctx, subscription)
	if err != nil {
		return nil, nil, err
	}
	target := valueString(resolved["downloadPath"])
	if target == "" {
		return nil, nil, errors.New("订阅下载路径为空")
	}
	target, err = canonicalPath(target)
	if err != nil {
		return nil, nil, err
	}
	target = filepath.Clean(target)
	client, err := m.client(ctx)
	if err != nil {
		return nil, nil, err
	}
	torrents, err := client.List(ctx, []string{"name", "hashString", "downloadDir", "labels"})
	if err != nil {
		return nil, nil, err
	}
	matched := make([]transmission.Torrent, 0)
	for _, torrent := range torrents {
		if strings.TrimSpace(torrent.DownloadDir) == "" {
			continue
		}
		dir, err := canonicalPath(torrent.DownloadDir)
		if err != nil || filepath.Clean(dir) != target {
			continue
		}
		if !containsTorrentLabel(torrent.Labels, "ani-rss") {
			return nil, nil, errors.New("订阅目录含有非 ani-rss 任务，拒绝移动或删除共享媒体")
		}
		matched = append(matched, torrent)
	}
	return client, matched, nil
}

func (m *transmissionTaskManager) MoveSubscription(ctx context.Context, subscription map[string]any, target string) error {
	client, torrents, err := m.matching(ctx, subscription)
	if err != nil {
		return err
	}
	for _, torrent := range torrents {
		if err := client.SetLocation(ctx, torrent.Hash, target); err != nil {
			return err
		}
	}
	return nil
}

func (m *transmissionTaskManager) DeleteSubscription(ctx context.Context, subscription map[string]any, deleteFiles bool) error {
	if !deleteFiles {
		return nil
	}
	client, torrents, err := m.matching(ctx, subscription)
	if err != nil {
		return err
	}
	for _, torrent := range torrents {
		if err := client.Remove(ctx, torrent.Hash, true); err != nil {
			return err
		}
	}
	return nil
}

func containsTorrentLabel(labels []string, target string) bool {
	for _, label := range labels {
		if label == target {
			return true
		}
	}
	return false
}
