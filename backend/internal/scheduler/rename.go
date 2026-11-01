package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/transmission"
)

type renamePayload struct {
	SubscriptionID string `json:"subscriptionId"`
	TorrentHash    string `json:"torrentHash"`
	DownloadDir    string `json:"downloadDir"`
	OriginalName   string `json:"originalName"`
	TargetName     string `json:"targetName"`
}

type renameScanHandler struct{ runtime *Runtime }

func (h renameScanHandler) Reconcile(context.Context, jobs.Job) (bool, error) {
	return false, nil
}

func (h renameScanHandler) Execute(ctx context.Context, _ jobs.Job) error {
	return h.runtime.scanTorrents(ctx)
}

type renameHandler struct{ runtime *Runtime }

func (h renameHandler) decode(job jobs.Job) (renamePayload, error) {
	var payload renamePayload
	err := json.Unmarshal(job.Payload, &payload)
	return payload, err
}

func (h renameHandler) Reconcile(ctx context.Context, job jobs.Job) (bool, error) {
	payload, err := h.decode(job)
	if err != nil {
		return false, err
	}
	config, err := h.runtime.store.GetConfig(ctx)
	if err != nil {
		return false, err
	}
	if !valueBool(config["rename"]) {
		return true, nil
	}
	torrents, err := h.runtime.transmission(config).List(ctx, taskFields)
	if err != nil {
		return false, err
	}
	torrent, exists := findTorrent(torrents, payload.TorrentHash)
	if !exists || hasTag(torrent, "RENAME") {
		return true, nil
	}
	if err := h.runtime.verifyTorrentPath(torrent, payload.DownloadDir); err != nil {
		return false, err
	}
	if torrent.Name == payload.TargetName || torrent.Name == payload.OriginalName {
		return false, nil
	}
	return false, fmt.Errorf("Transmission 任务名称已变化，拒绝覆盖未确认名称")
}

func (h renameHandler) Execute(ctx context.Context, job jobs.Job) error {
	payload, err := h.decode(job)
	if err != nil {
		return err
	}
	config, err := h.runtime.store.GetConfig(ctx)
	if err != nil {
		return err
	}
	if !valueBool(config["rename"]) {
		return nil
	}
	client := h.runtime.transmission(config)
	torrent, exists, err := getTorrent(ctx, client, payload.TorrentHash)
	if err != nil || !exists {
		return err
	}
	if err := h.runtime.verifyTorrentPath(torrent, payload.DownloadDir); err != nil {
		return err
	}
	if hasTag(torrent, "RENAME") {
		return nil
	}
	if torrent.Name != payload.TargetName {
		if torrent.Name != payload.OriginalName {
			return errors.New("Transmission 任务名称已变化，拒绝覆盖未确认名称")
		}
		if err := client.Rename(ctx, payload.TorrentHash, payload.OriginalName, payload.TargetName); err != nil {
			return fmt.Errorf("Transmission 重命名失败：%w", err)
		}
		torrent, err = waitForTorrent(ctx, h.runtime, client, payload.TorrentHash, func(current transmission.Torrent) bool {
			return current.Name == payload.TargetName
		})
		if err != nil {
			return fmt.Errorf("等待 Transmission 确认重命名失败：%w", err)
		}
	}
	labels := append([]string(nil), torrent.Labels...)
	labels = append(labels, "RENAME")
	if err := client.SetLabels(ctx, payload.TorrentHash, uniqueLabels(labels)); err != nil {
		return fmt.Errorf("Transmission 重命名标签写入失败：%w", err)
	}
	_, err = waitForTorrent(ctx, h.runtime, client, payload.TorrentHash, func(current transmission.Torrent) bool {
		return current.Name == payload.TargetName && hasTag(current, "RENAME")
	})
	if err != nil {
		return fmt.Errorf("等待 Transmission 确认重命名标签失败：%w", err)
	}
	return nil
}

func (r *Runtime) scanTorrents(ctx context.Context) error {
	if r.options.Engine == nil {
		return errors.New("重命名扫描缺少 pipeline.Engine")
	}
	config, err := r.store.GetConfig(ctx)
	if err != nil {
		return err
	}
	torrents, err := r.transmission(config).List(ctx, taskFields)
	if err != nil {
		return fmt.Errorf("读取 Transmission 任务失败：%w", err)
	}
	subscriptions, err := r.store.ListSubscriptions(ctx)
	if err != nil {
		return err
	}
	for _, subscription := range subscriptions {
		downloadResult, err := r.options.Engine.DownloadPath(ctx, subscription)
		if err != nil {
			return fmt.Errorf("计算订阅下载路径失败：%w", err)
		}
		downloadDir := valueString(downloadResult["downloadPath"])
		if valueBool(config["rename"]) || (valueBool(config["delete"]) && valueBool(config["deleteStandbyRSSOnly"])) {
			preview, previewErr := r.options.Engine.Preview(ctx, subscription)
			if previewErr != nil {
				r.report(fmt.Errorf("读取订阅 RSS 预览失败：%w", previewErr))
			} else {
				items, _ := preview["items"].([]pipeline.Item)
				itemsByHash := make(map[string]pipeline.Item, len(items))
				for _, item := range items {
					itemsByHash[strings.ToLower(item.InfoHash)] = item
				}
				for _, torrent := range torrents {
					if !valueBool(config["rename"]) {
						break
					}
					if !ownsTorrent(torrent) || !samePath(torrent.DownloadDir, downloadDir) || hasTag(torrent, "RENAME") {
						continue
					}
					item, found := itemsByHash[strings.ToLower(torrent.Hash)]
					if !found || item.ReName == "" || torrent.Name == "" {
						continue
					}
					if len(torrent.Name) == 40 && strings.EqualFold(torrent.Name, torrent.Hash) {
						continue
					}
					targetName := item.ReName
					if ext := filepath.Ext(torrent.Name); ext != "" {
						targetName += ext
					}
					if torrent.Name == targetName {
						continue
					}
					payload := renamePayload{
						SubscriptionID: valueString(subscription["id"]), TorrentHash: torrent.Hash,
						DownloadDir: downloadDir, OriginalName: torrent.Name, TargetName: targetName,
					}
					key := "rename:" + strings.ToLower(torrent.Hash) + ":" + targetName
					if _, err := r.enqueue(ctx, JobRename, key, payload); err != nil {
						return err
					}
				}
				if valueBool(config["delete"]) && valueBool(config["deleteStandbyRSSOnly"]) {
					if err := r.enqueueStandbyReplacements(ctx, config, subscription, downloadDir, items, torrents); err != nil {
						return err
					}
				}
			}
		}
		if err := r.enqueueCompletionJobs(ctx, config, subscription, downloadDir, torrents); err != nil {
			return err
		}
	}
	return nil
}

var episodePattern = regexp.MustCompile(`(?i)s(\d+)e(\d+(?:\.5)?)`)

// 主 RSS 对应任务已完成后，才为同路径、同季同集的备用任务写入删除意图。
func (r *Runtime) enqueueStandbyReplacements(ctx context.Context, config, subscription map[string]any, dir string, items []pipeline.Item, torrents []transmission.Torrent) error {
	for _, item := range items {
		main, exists := findTorrent(torrents, item.InfoHash)
		if !exists || !ownsTorrent(main) || !samePath(main.DownloadDir, dir) || !torrentFinished(main, valueBool(config["awaitStalledUP"])) {
			continue
		}
		mainEpisode := episodePattern.FindStringSubmatch(main.Name)
		if len(mainEpisode) == 0 {
			mainEpisode = episodePattern.FindStringSubmatch(item.ReName)
		}
		if len(mainEpisode) < 3 {
			continue
		}
		for _, standby := range torrents {
			if strings.EqualFold(standby.Hash, main.Hash) || !ownsTorrent(standby) || !samePath(standby.DownloadDir, dir) {
				continue
			}
			candidate := episodePattern.FindStringSubmatch(standby.Name)
			if len(candidate) < 3 || !strings.EqualFold(candidate[1], mainEpisode[1]) || candidate[2] != mainEpisode[2] {
				continue
			}
			if !hasTag(standby, "备用RSS") && hasTag(standby, valueString(subscription["subgroup"])) {
				continue
			}
			if !hasTag(standby, "RENAME") || !torrentFinished(standby, valueBool(config["awaitStalledUP"])) {
				continue
			}
			payload := deletePayload{SubscriptionID: valueString(subscription["id"]), TorrentHash: standby.Hash, DownloadDir: dir, StandbyReplacement: true, MainTorrentHash: main.Hash}
			if _, err := r.enqueue(ctx, JobDeleteTorrent, "delete-standby:"+strings.ToLower(standby.Hash), payload); err != nil {
				return err
			}
			break
		}
	}
	return nil
}

func getTorrent(ctx context.Context, client *transmission.Client, hash string) (transmission.Torrent, bool, error) {
	torrents, err := client.List(ctx, taskFields)
	if err != nil {
		return transmission.Torrent{}, false, err
	}
	torrent, exists := findTorrent(torrents, hash)
	return torrent, exists, nil
}

func waitForTorrent(ctx context.Context, runtime *Runtime, client *transmission.Client, hash string, confirmed func(transmission.Torrent) bool) (transmission.Torrent, error) {
	var last transmission.Torrent
	for attempt := 0; attempt < runtime.options.RenamePollAttempts; attempt++ {
		if attempt > 0 {
			if err := wait(ctx, runtime.options.RenamePollInterval); err != nil {
				return last, err
			}
		}
		current, exists, err := getTorrent(ctx, client, hash)
		if err != nil {
			return last, err
		}
		if !exists {
			return last, errors.New("Transmission 任务在确认期间消失")
		}
		last = current
		if confirmed(current) {
			return current, nil
		}
	}
	return last, errors.New("Transmission 未确认预期任务状态")
}

func uniqueLabels(labels []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(labels))
	for _, label := range labels {
		if label != "" && !seen[label] {
			seen[label] = true
			result = append(result, label)
		}
	}
	return result
}
