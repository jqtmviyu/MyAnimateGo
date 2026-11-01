package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/transmission"
)

var taskFields = []string{"id", "hashString", "name", "status", "downloadDir", "labels", "isFinished", "totalSize", "haveValid", "files"}

func (r *Runtime) listTorrents(ctx context.Context) ([]transmission.Torrent, error) {
	config, err := r.store.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	return r.transmission(config).List(ctx, taskFields)
}

func findTorrent(torrents []transmission.Torrent, hash string) (transmission.Torrent, bool) {
	for _, torrent := range torrents {
		if strings.EqualFold(torrent.Hash, hash) {
			return torrent, true
		}
	}
	return transmission.Torrent{}, false
}

func ownsTorrent(torrent transmission.Torrent) bool {
	for _, label := range torrent.Labels {
		if label == "ani-rss" {
			return true
		}
	}
	return false
}

func hasTag(torrent transmission.Torrent, wanted string) bool {
	for _, label := range torrent.Labels {
		if label == wanted {
			return true
		}
	}
	return false
}

func torrentFinished(torrent transmission.Torrent, awaitStopped bool) bool {
	if torrent.TotalSize > 0 && float64(torrent.HaveValid)*100/float64(torrent.TotalSize) < 100 {
		return false
	}
	if awaitStopped {
		return torrent.IsFinished && torrent.Status != 6
	}
	return torrent.Status == 6 || torrent.IsFinished
}

func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftPath, errLeft := filepath.Abs(left)
	rightPath, errRight := filepath.Abs(right)
	if errLeft != nil || errRight != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}
	return filepath.Clean(leftPath) == filepath.Clean(rightPath)
}

func (r *Runtime) completionTagPresent(ctx context.Context, hash string) (bool, error) {
	torrents, err := r.listTorrents(ctx)
	if err != nil {
		return false, err
	}
	torrent, exists := findTorrent(torrents, hash)
	return exists && hasTag(torrent, "下载完成"), nil
}

func (r *Runtime) confirmLocation(ctx context.Context, move MediaMove) error {
	torrents, err := r.listTorrents(ctx)
	if err != nil {
		return err
	}
	torrent, exists := findTorrent(torrents, move.TorrentHash)
	if !exists {
		return errors.New("媒体迁移前下载任务已不存在")
	}
	targetDir := move.DownloadDir
	if targetDir == "" {
		targetDir = move.TargetPath
	}
	if !ownsTorrent(torrent) || !samePath(torrent.DownloadDir, targetDir) {
		return errors.New("Transmission 下载路径尚未确认到目标目录")
	}
	return nil
}

func (r *Runtime) verifyTorrentPath(torrent transmission.Torrent, expected string) error {
	if !ownsTorrent(torrent) {
		return errors.New("任务不属于 ani-rss，拒绝执行")
	}
	if !samePath(torrent.DownloadDir, expected) {
		return errors.New("任务路径与持久化意图不符，拒绝操作")
	}
	return nil
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
