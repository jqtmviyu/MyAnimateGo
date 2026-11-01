package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/transmission"
)

// enqueueCompletionJobs 将已完成任务拆成可独立恢复的步骤，所有副作用先落队列。
func (r *Runtime) enqueueCompletionJobs(ctx context.Context, config, subscription map[string]any, downloadDir string, torrents []transmission.Torrent) error {
	id := valueString(subscription["id"])
	eligibleForMove := valueBool(config["completed"]) && valueBool(subscription["completed"]) && valueBool(config["autoDisabled"]) && !valueBool(subscription["enable"]) && !valueBool(subscription["ova"]) && valueInt(subscription["totalEpisodeNumber"]) > 0 && valueInt(subscription["currentEpisodeNumber"]) >= valueInt(subscription["totalEpisodeNumber"])
	var targetDir string
	var files []MediaMove
	var deletions []deletePayload
	if eligibleForMove {
		if r.options.Engine == nil {
			return errors.New("完结目录计算缺少 pipeline.Engine")
		}
		var err error
		targetDir, err = completedPath(r.options.Engine, ctx, config, subscription)
		if err != nil {
			return err
		}
		err = filepath.WalkDir(downloadDir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || path == downloadDir {
				return nil
			}
			rel, err := filepath.Rel(downloadDir, path)
			if err != nil {
				return err
			}
			files = append(files, MediaMove{SubscriptionID: id, SourceDir: downloadDir, SourcePath: path, TargetPath: filepath.Join(targetDir, rel), DownloadDir: targetDir})
			return nil
		})
		if errors.Is(err, os.ErrNotExist) {
			files = nil
		} else if err != nil {
			return fmt.Errorf("读取完结目录文件失败：%w", err)
		}
	}
	for _, torrent := range torrents {
		inSource := samePath(torrent.DownloadDir, downloadDir)
		inCompleted := eligibleForMove && samePath(torrent.DownloadDir, targetDir)
		if !ownsTorrent(torrent) || (!inSource && !inCompleted) || !torrentFinished(torrent, false) {
			continue
		}
		base := taskPayload{SubscriptionID: id, TorrentHash: torrent.Hash, DownloadDir: torrent.DownloadDir}
		if !hasTag(torrent, "下载完成") {
			if _, err := r.enqueue(ctx, JobCompleteTag, "complete-tag:"+strings.ToLower(torrent.Hash), base); err != nil {
				return err
			}
		}
		event := Notification{SubscriptionID: id, TorrentHash: torrent.Hash, TorrentName: torrent.Name, Tags: append([]string(nil), torrent.Labels...)}
		if _, err := r.enqueue(ctx, JobNotifyComplete, "notify-complete:"+strings.ToLower(torrent.Hash), event); err != nil {
			return err
		}
		if eligibleForMove && inSource && !samePath(downloadDir, targetDir) {
			move := MediaMove{SubscriptionID: id, TorrentHash: torrent.Hash, SourcePath: downloadDir, TargetPath: targetDir, DownloadDir: targetDir}
			if _, err := r.enqueue(ctx, JobMoveLocation, "move-location:"+strings.ToLower(torrent.Hash), move); err != nil {
				return err
			}
		}
		if valueBool(config["delete"]) && !valueBool(config["deleteStandbyRSSOnly"]) && torrentFinished(torrent, valueBool(config["awaitStalledUP"])) {
			deleteDir := torrent.DownloadDir
			if eligibleForMove && !samePath(downloadDir, targetDir) {
				deleteDir = targetDir
			}
			deletions = append(deletions, deletePayload{SubscriptionID: id, TorrentHash: torrent.Hash, DownloadDir: deleteDir})
		}
	}
	for _, move := range files {
		if samePath(downloadDir, targetDir) {
			break
		}
		move.TorrentHash = firstFinishedHash(torrents, downloadDir, targetDir, eligibleForMove, config)
		if move.TorrentHash == "" {
			break
		}
		key := "move-media:" + strings.ToLower(move.TorrentHash) + ":" + move.SourcePath
		if _, err := r.enqueue(ctx, JobMoveMedia, key, move); err != nil {
			return err
		}
	}
	for _, deletion := range deletions {
		if _, err := r.enqueue(ctx, JobDeleteTorrent, "delete-torrent:"+strings.ToLower(deletion.TorrentHash), deletion); err != nil {
			return err
		}
	}
	if valueBool(config["autoDisabled"]) && !valueBool(subscription["ova"]) {
		total, current := valueInt(subscription["totalEpisodeNumber"]), valueInt(subscription["currentEpisodeNumber"])
		if total > 0 && current >= total {
			if _, err := r.enqueue(ctx, JobDisableSub, "disable-sub:"+id, disablePayload{SubscriptionID: id, CurrentEpisode: current, TotalEpisode: total}); err != nil {
				return err
			}
		}
	}
	return nil
}

func firstFinishedHash(torrents []transmission.Torrent, dir, targetDir string, allowTarget bool, config map[string]any) string {
	for _, torrent := range torrents {
		inSource := samePath(torrent.DownloadDir, dir)
		inTarget := allowTarget && samePath(torrent.DownloadDir, targetDir)
		if ownsTorrent(torrent) && (inSource || inTarget) && torrentFinished(torrent, false) {
			return torrent.Hash
		}
	}
	return ""
}

type completeTagHandler struct{ runtime *Runtime }

func (h completeTagHandler) payload(job jobs.Job) (taskPayload, error) {
	var p taskPayload
	err := json.Unmarshal(job.Payload, &p)
	return p, err
}
func (h completeTagHandler) Reconcile(ctx context.Context, job jobs.Job) (bool, error) {
	p, err := h.payload(job)
	if err != nil {
		return false, err
	}
	config, err := h.runtime.store.GetConfig(ctx)
	if err != nil {
		return false, err
	}
	t, exists, err := getTorrent(ctx, h.runtime.transmission(config), p.TorrentHash)
	if err != nil {
		return false, err
	}
	if !exists {
		return true, nil
	}
	if hasTag(t, "下载完成") {
		return true, nil
	}
	return false, h.runtime.verifyTorrentPath(t, p.DownloadDir)
}
func (h completeTagHandler) Execute(ctx context.Context, job jobs.Job) error {
	p, err := h.payload(job)
	if err != nil {
		return err
	}
	config, err := h.runtime.store.GetConfig(ctx)
	if err != nil {
		return err
	}
	client := h.runtime.transmission(config)
	t, exists, err := getTorrent(ctx, client, p.TorrentHash)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("Transmission 任务已不存在")
	}
	if err := h.runtime.verifyTorrentPath(t, p.DownloadDir); err != nil {
		return err
	}
	labels := append(append([]string(nil), t.Labels...), "下载完成")
	if err := client.SetLabels(ctx, p.TorrentHash, uniqueLabels(labels)); err != nil {
		return err
	}
	_, err = waitForTorrent(ctx, h.runtime, client, p.TorrentHash, func(v transmission.Torrent) bool { return hasTag(v, "下载完成") })
	return err
}

type taskPayload struct {
	SubscriptionID string `json:"subscriptionId"`
	TorrentHash    string `json:"torrentHash"`
	DownloadDir    string `json:"downloadDir"`
}

type moveLocationHandler struct{ runtime *Runtime }

func (h moveLocationHandler) payload(job jobs.Job) (MediaMove, error) {
	var payload MediaMove
	err := json.Unmarshal(job.Payload, &payload)
	return payload, err
}

func (h moveLocationHandler) Reconcile(ctx context.Context, job jobs.Job) (bool, error) {
	payload, err := h.payload(job)
	if err != nil {
		return false, err
	}
	torrents, err := h.runtime.listTorrents(ctx)
	if err != nil {
		return false, err
	}
	torrent, exists := findTorrent(torrents, payload.TorrentHash)
	if !exists {
		return false, errors.New("Transmission 任务在保存路径迁移前已不存在")
	}
	if !ownsTorrent(torrent) {
		return true, nil
	}
	if samePath(torrent.DownloadDir, payload.TargetPath) {
		return true, nil
	}
	if !samePath(torrent.DownloadDir, payload.SourcePath) {
		return false, errors.New("Transmission 保存路径与迁移意图不符，拒绝移动")
	}
	return false, nil
}

func (h moveLocationHandler) Execute(ctx context.Context, job jobs.Job) error {
	payload, err := h.payload(job)
	if err != nil {
		return err
	}
	config, err := h.runtime.store.GetConfig(ctx)
	if err != nil {
		return err
	}
	client := h.runtime.transmission(config)
	torrent, exists, err := getTorrent(ctx, client, payload.TorrentHash)
	if err != nil || !exists {
		return err
	}
	if !ownsTorrent(torrent) || !samePath(torrent.DownloadDir, payload.SourcePath) {
		if samePath(torrent.DownloadDir, payload.TargetPath) {
			return nil
		}
		return errors.New("Transmission 保存路径未确认，拒绝移动")
	}
	// Java 只更新任务保存路径，随后逐文件移动旧目录内容；Transmission 的 move=true 会抢先搬空旧目录。
	if _, err := client.Call(ctx, "torrent-set-location", map[string]any{"ids": []string{payload.TorrentHash}, "location": payload.TargetPath, "move": false}); err != nil {
		return fmt.Errorf("移动 Transmission 任务失败：%w", err)
	}
	_, err = waitForTorrent(ctx, h.runtime, client, payload.TorrentHash, func(current transmission.Torrent) bool {
		return samePath(current.DownloadDir, payload.TargetPath)
	})
	if err != nil {
		return fmt.Errorf("Transmission 移动结果未确认：%w", err)
	}
	return nil
}

type deletePayload struct {
	SubscriptionID     string `json:"subscriptionId"`
	TorrentHash        string `json:"torrentHash"`
	DownloadDir        string `json:"downloadDir"`
	StandbyReplacement bool   `json:"standbyReplacement,omitempty"`
	MainTorrentHash    string `json:"mainTorrentHash,omitempty"`
}

type deleteTorrentHandler struct{ runtime *Runtime }

func (h deleteTorrentHandler) payload(job jobs.Job) (deletePayload, error) {
	var payload deletePayload
	err := json.Unmarshal(job.Payload, &payload)
	return payload, err
}

func (h deleteTorrentHandler) Reconcile(ctx context.Context, job jobs.Job) (bool, error) {
	payload, err := h.payload(job)
	if err != nil {
		return false, err
	}
	torrents, err := h.runtime.listTorrents(ctx)
	if err != nil {
		return false, err
	}
	torrent, exists := findTorrent(torrents, payload.TorrentHash)
	if !exists {
		return true, nil
	}
	allowed, err := h.runtime.deletionAllowed(ctx, payload, torrent)
	if err != nil {
		return false, err
	}
	return !allowed, nil
}

func (h deleteTorrentHandler) Execute(ctx context.Context, job jobs.Job) error {
	payload, err := h.payload(job)
	if err != nil {
		return err
	}
	config, err := h.runtime.store.GetConfig(ctx)
	if err != nil {
		return err
	}
	client := h.runtime.transmission(config)
	torrent, exists, err := getTorrent(ctx, client, payload.TorrentHash)
	if err != nil || !exists {
		return err
	}
	allowed, err := h.runtime.deletionAllowed(ctx, payload, torrent)
	if err != nil {
		return err
	}
	if !allowed {
		return nil
	}
	if err := client.Remove(ctx, payload.TorrentHash, false); err != nil {
		return fmt.Errorf("删除已完成 Transmission 任务失败：%w", err)
	}
	for attempt := 0; attempt < h.runtime.options.RenamePollAttempts; attempt++ {
		if attempt > 0 {
			if err := wait(ctx, h.runtime.options.RenamePollInterval); err != nil {
				return err
			}
		}
		current, err := h.runtime.listTorrents(ctx)
		if err != nil {
			return err
		}
		if _, exists := findTorrent(current, payload.TorrentHash); !exists {
			return nil
		}
	}
	return errors.New("Transmission 删除结果尚未确认")
}

func (r *Runtime) deletionAllowed(ctx context.Context, payload deletePayload, torrent transmission.Torrent) (bool, error) {
	config, err := r.store.GetConfig(ctx)
	if err != nil {
		return false, err
	}
	if !valueBool(config["delete"]) || (!payload.StandbyReplacement && valueBool(config["deleteStandbyRSSOnly"])) || !ownsTorrent(torrent) {
		return false, nil
	}
	if !torrentFinished(torrent, valueBool(config["awaitStalledUP"])) {
		return false, nil
	}
	if (valueBool(config["rename"]) || payload.StandbyReplacement) && !hasTag(torrent, "RENAME") {
		return false, nil
	}
	if payload.StandbyReplacement {
		if !valueBool(config["deleteStandbyRSSOnly"]) || payload.MainTorrentHash == "" {
			return false, nil
		}
		main, exists, err := getTorrent(ctx, r.transmission(config), payload.MainTorrentHash)
		if err != nil {
			return false, err
		}
		if !exists || !ownsTorrent(main) || !torrentFinished(main, valueBool(config["awaitStalledUP"])) || !samePath(main.DownloadDir, payload.DownloadDir) {
			return false, nil
		}
		mainEpisode, candidateEpisode := episodePattern.FindStringSubmatch(main.Name), episodePattern.FindStringSubmatch(torrent.Name)
		if len(mainEpisode) < 3 || len(candidateEpisode) < 3 || !strings.EqualFold(mainEpisode[1], candidateEpisode[1]) || mainEpisode[2] != candidateEpisode[2] {
			return false, nil
		}
	}
	if r.options.Engine == nil {
		return false, errors.New("删除任务缺少 pipeline.Engine")
	}
	subscription, exists, err := r.subscription(ctx, payload.SubscriptionID)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	if payload.StandbyReplacement && !hasTag(torrent, "备用RSS") && hasTag(torrent, valueString(subscription["subgroup"])) {
		return false, nil
	}
	pathMap, err := r.options.Engine.DownloadPath(ctx, subscription)
	if err != nil {
		return false, err
	}
	expected := valueString(pathMap["downloadPath"])
	pathMatches := samePath(expected, payload.DownloadDir)
	if !pathMatches && valueBool(config["completed"]) {
		if valueBool(config["completed"]) {
			completed, pathErr := completedPath(r.options.Engine, ctx, config, subscription)
			if pathErr != nil {
				return false, pathErr
			}
			pathMatches = samePath(completed, payload.DownloadDir)
		}
	}
	if !pathMatches || !samePath(torrent.DownloadDir, payload.DownloadDir) {
		return false, errors.New("删除前未能确认下载路径，保留任务和文件")
	}
	return true, nil
}

type disableSubscriptionHandler struct{ runtime *Runtime }

type disablePayload struct {
	SubscriptionID string `json:"subscriptionId"`
	CurrentEpisode int    `json:"currentEpisode"`
	TotalEpisode   int    `json:"totalEpisode"`
}

func (h disableSubscriptionHandler) payload(job jobs.Job) (disablePayload, error) {
	var payload disablePayload
	err := json.Unmarshal(job.Payload, &payload)
	return payload, err
}

func (h disableSubscriptionHandler) Reconcile(ctx context.Context, job jobs.Job) (bool, error) {
	payload, err := h.payload(job)
	if err != nil {
		return false, err
	}
	subscription, exists, err := h.runtime.subscription(ctx, payload.SubscriptionID)
	if err != nil || !exists {
		return !exists, err
	}
	return !valueBool(subscription["enable"]), nil
}

func (h disableSubscriptionHandler) Execute(ctx context.Context, job jobs.Job) error {
	payload, err := h.payload(job)
	if err != nil {
		return err
	}
	config, err := h.runtime.store.GetConfig(ctx)
	if err != nil {
		return err
	}
	subscription, exists, err := h.runtime.subscription(ctx, payload.SubscriptionID)
	if err != nil || !exists {
		return err
	}
	if !valueBool(config["autoDisabled"]) || valueBool(subscription["ova"]) || valueInt(subscription["totalEpisodeNumber"]) < 1 || valueInt(subscription["currentEpisodeNumber"]) < valueInt(subscription["totalEpisodeNumber"]) {
		return nil
	}
	subscription["enable"] = false
	if store, ok := h.runtime.store.(interface {
		UpdateSubscriptionFields(context.Context, string, map[string]any) error
	}); ok {
		return store.UpdateSubscriptionFields(ctx, payload.SubscriptionID, map[string]any{"enable": false})
	}
	return h.runtime.store.PutSubscription(ctx, subscription)
}

func completedPath(engine interface {
	DownloadPath(context.Context, map[string]any) (map[string]any, error)
}, ctx context.Context, config, subscription map[string]any) (string, error) {
	template := valueString(config["completedPathTemplate"])
	if valueBool(subscription["customCompleted"]) {
		template = valueString(subscription["customCompletedPathTemplate"])
	}
	if strings.TrimSpace(template) == "" {
		return "", errors.New("完结目录模板为空，拒绝移动")
	}
	copySubscription := make(map[string]any, len(subscription))
	for key, value := range subscription {
		copySubscription[key] = value
	}
	copySubscription["customDownloadPath"] = true
	copySubscription["customDownloadPathTemplate"] = template
	pathMap, err := engine.DownloadPath(ctx, copySubscription)
	if err != nil {
		return "", err
	}
	path := valueString(pathMap["downloadPath"])
	if path == "" {
		return "", errors.New("完结目录无法确认")
	}
	return filepath.Clean(path), nil
}
