package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/torrent"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/transmission"
)

// ErrAutomaticTasksPaused 表示全局暂停状态阻止了下载写操作。
var ErrAutomaticTasksPaused = errors.New("自动下载任务已暂停")

type terminalAddError struct{ err error }

func (e *terminalAddError) Error() string { return e.err.Error() }
func (e *terminalAddError) Unwrap() error { return e.err }

// RefreshSubscription 刷新一个已保存的订阅。
func (e *Engine) RefreshSubscription(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	ctx = e.notificationContext(ctx, "refresh:"+id)
	items, err := e.store.ListSubscriptions(ctx)
	if err != nil {
		return err
	}
	for _, subscription := range items {
		if valueString(subscription["id"]) == id {
			if !valueBool(subscription["enable"]) {
				return nil
			}
			return e.refreshOne(ctx, subscription)
		}
	}
	return errors.New("订阅不存在")
}

// RefreshAll 按存储顺序刷新已启用的订阅。
func (e *Engine) RefreshAll(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	ctx = e.notificationContext(ctx, "refresh-all")
	items, err := e.store.ListSubscriptions(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, subscription := range items {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		if !valueBool(subscription["enable"]) {
			continue
		}
		if err := e.refreshOne(ctx, subscription); err != nil {
			failures = append(failures, fmt.Errorf("刷新订阅 %s 失败：%w", valueString(subscription["id"]), err))
		}
	}
	return errors.Join(failures...)
}

// Download 根据已完成的订阅决策提交单个资源，并返回 Transmission 的添加结果。
func (e *Engine) Download(ctx context.Context, subscription map[string]any, item Item) (transmission.AddResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ctx = e.notificationContext(ctx, "download:"+valueString(subscription["id"]))
	config, err := e.config(ctx)
	if err != nil {
		return transmission.AddResult{}, err
	}
	paused, configured := config["automaticTasksPaused"]
	if !configured || valueBool(paused) {
		return transmission.AddResult{}, ErrAutomaticTasksPaused
	}
	if item.InfoHash == "" {
		if _, _, err := e.torrentSource(ctx, config, subscription, &item); err != nil {
			return transmission.AddResult{}, err
		}
	}
	downloadPath, err := e.downloadPath(ctx, config, subscription)
	if err != nil {
		return transmission.AddResult{}, err
	}
	client := e.transmission(config)
	fields := []string{"hashString", "labels", "isFinished", "totalSize", "haveValid", "status"}
	torrents, err := client.List(ctx, fields)
	if err != nil {
		return transmission.AddResult{}, fmt.Errorf("查询 Transmission 下载任务失败：%w", err)
	}
	if item.InfoHash != "" && hasHash(torrents, item.InfoHash) {
		pending, err := findPreparedTorrent(e.cacheDir(), item.InfoHash)
		if err != nil {
			return transmission.AddResult{}, err
		}
		if pending != "" {
			if err := e.enqueueDownloadStarted(ctx, config, subscription, item); err != nil {
				return transmission.AddResult{}, err
			}
			if err := commitPreparedCache(pending); err != nil {
				return transmission.AddResult{}, err
			}
		}
		return transmission.AddResult{Hash: item.InfoHash, Duplicate: true}, nil
	}
	result, err := e.submitItem(ctx, client, config, subscription, item, downloadPath)
	if err != nil {
		return result, err
	}
	if err := e.enqueueDownloadStarted(ctx, config, subscription, item); err != nil {
		return transmission.AddResult{}, err
	}
	if item.InfoHash != "" {
		pending, err := findPreparedTorrent(e.cacheDir(), item.InfoHash)
		if err != nil {
			return transmission.AddResult{}, err
		}
		if pending != "" {
			if err := commitPreparedCache(pending); err != nil {
				return transmission.AddResult{}, err
			}
		}
	}
	return result, nil
}

func (e *Engine) refreshOne(ctx context.Context, subscription map[string]any) error {
	config, err := e.config(ctx)
	if err != nil {
		return err
	}
	paused, configured := config["automaticTasksPaused"]
	if !configured || valueBool(paused) {
		return ErrAutomaticTasksPaused
	}
	items, err := e.items(ctx, config, subscription)
	if err != nil {
		return err
	}
	if err := e.enqueueOmissions(ctx, config, subscription, items); err != nil {
		return err
	}
	downloadPath, err := e.downloadPath(ctx, config, subscription)
	if err != nil {
		return err
	}
	client := e.transmission(config)
	fields := []string{"id", "hashString", "name", "status", "downloadDir", "labels", "isFinished", "totalSize", "haveValid", "files"}
	torrents, err := client.List(ctx, fields)
	if err != nil {
		return fmt.Errorf("查询 Transmission 下载任务失败：%w", err)
	}
	byHash := make(map[string]transmission.Torrent, len(torrents))
	active := 0
	for _, torrent := range torrents {
		hash := strings.ToLower(torrent.Hash)
		byHash[hash] = torrent
		if !torrentFinished(torrent) {
			active++
		}
	}
	added := false
	currentDownloadCount := 0
	countEpisode := func(item Item) {
		if item.Master && item.Episode == float64(int(item.Episode)) {
			currentDownloadCount++
		}
	}
	var failures []error
	preparedPaths := make([]string, 0)
	preparedSeen := map[string]bool{}
	rememberPrepared := func(hash string) error {
		pending, err := findPreparedTorrent(e.cacheDir(), hash)
		if err != nil {
			return err
		}
		if pending != "" && !preparedSeen[pending] {
			preparedSeen[pending] = true
			preparedPaths = append(preparedPaths, pending)
		}
		return nil
	}
	for i := range items {
		item := &items[i]
		if item.InfoHash == "" {
			if _, _, err := e.torrentSource(ctx, config, subscription, item); err != nil {
				return err
			}
		}
		hash := strings.ToLower(item.InfoHash)
		if hash != "" {
			if _, exists := byHash[hash]; exists {
				countEpisode(*item)
				pending, err := findPreparedTorrent(e.cacheDir(), item.InfoHash)
				if err != nil {
					return err
				}
				if pending != "" {
					if err := e.enqueueDownloadStarted(ctx, config, subscription, *item); err != nil {
						return err
					}
					if !preparedSeen[pending] {
						preparedSeen[pending] = true
						preparedPaths = append(preparedPaths, pending)
					}
					added = true
				}
				continue
			}
		}
		cached, err := findCachedTorrent(e.cacheDir(), item.InfoHash)
		if err != nil {
			return err
		}
		if cached != "" {
			countEpisode(*item)
			continue
		}
		if containsEpisode(subscription["notDownload"], item.Episode) {
			countEpisode(*item)
			continue
		}
		if valueBool(subscription["downloadNew"]) && !isNewest(items, i) {
			countEpisode(*item)
			continue
		}
		if delayed(config, *item, e.now()) {
			continue
		}
		if e.localEpisodeExists(config, subscription, *item, downloadPath) {
			countEpisode(*item)
			continue
		}
		limit := valueInt(config["downloadCount"])
		if limit > 0 && active >= limit {
			continue
		}
		result, err := e.submitItem(ctx, client, config, subscription, *item, downloadPath)
		if err != nil {
			var terminal *terminalAddError
			if !errors.As(err, &terminal) {
				return err
			}
			countEpisode(*item)
			active++
			failures = append(failures, err)
			added = true
			continue
		}
		if err := e.enqueueDownloadStarted(ctx, config, subscription, *item); err != nil {
			return err
		}
		if err := rememberPrepared(item.InfoHash); err != nil {
			return err
		}
		if result.Hash != "" {
			byHash[strings.ToLower(result.Hash)] = transmission.Torrent{Hash: result.Hash, Name: item.ReName}
		} else if item.InfoHash != "" {
			byHash[hash] = transmission.Torrent{Hash: item.InfoHash, Name: item.ReName}
		}
		active++
		countEpisode(*item)
		added = true
	}
	completed := valueBool(config["autoDisabled"]) && valueBool(subscription["enable"]) && !valueBool(subscription["ova"]) &&
		valueInt(subscription["totalEpisodeNumber"]) > 0 && currentDownloadCount >= valueInt(subscription["totalEpisodeNumber"])
	if added || completed {
		subscription["currentEpisodeNumber"] = currentDownloadCount
		fields := map[string]any{"currentEpisodeNumber": currentDownloadCount}
		if added {
			subscription["lastDownloadTime"] = e.now().UnixMilli()
			fields["lastDownloadTime"] = subscription["lastDownloadTime"]
		}
		if err := e.saveSubscriptionFields(ctx, subscription, fields); err != nil {
			return err
		}
		for _, path := range preparedPaths {
			if err := commitPreparedCache(path); err != nil {
				return fmt.Errorf("订阅进度已保存，但提交种子历史失败：%w", err)
			}
		}
	}
	if err := e.enqueueCompleted(ctx, config, subscription, currentDownloadCount); err != nil {
		return err
	}
	return errors.Join(failures...)
}

func (e *Engine) transmission(config map[string]any) *transmission.Client {
	httpClient := e.options.TransmissionHTTPClient
	if httpClient == nil {
		httpClient = directHTTPClient(30 * time.Second)
	}
	return transmission.New(transmission.Config{
		URL: valueString(config["downloadToolHost"]), Username: valueString(config["downloadToolUsername"]),
		Password: valueString(config["downloadToolPassword"]), HTTPClient: httpClient,
	})
}

func (e *Engine) submitItem(ctx context.Context, client *transmission.Client, config, subscription map[string]any, item Item, downloadPath string) (transmission.AddResult, error) {
	if item.InfoHash == "" {
		item.InfoHash = hashFromMagnet(item.Torrent)
		if item.InfoHash == "" {
			item.InfoHash = hashFromTorrentURL(item.Torrent)
		}
	}
	cachePath, err := findPreparedTorrent(e.cacheDir(), item.InfoHash)
	if err != nil {
		return transmission.AddResult{}, err
	}
	if cachePath == "" {
		cachePath, err = findCachedTorrent(e.cacheDir(), item.InfoHash)
		if err != nil {
			return transmission.AddResult{}, err
		}
	}
	var request transmission.AddRequest
	if cachePath != "" {
		metainfo, magnet, readErr := torrentMetainfoFromCache(cachePath)
		if readErr != nil {
			return transmission.AddResult{}, readErr
		}
		request = transmission.AddRequest{Metainfo: metainfo, Magnet: magnet}
	} else {
		cachePath, request, err = e.torrentSource(ctx, config, subscription, &item)
		if err != nil {
			return transmission.AddResult{}, err
		}
	}
	request.DownloadDir = downloadPath
	request.Labels = torrentLabels(config, subscription, item)
	request.Paused = false
	tries := 1
	if configured, exists := config["downloadRetry"]; exists {
		tries = valueInt(configured)
	}
	if tries < 1 {
		return transmission.AddResult{}, errors.New("downloadRetry 未启用下载尝试")
	}
	if item.InfoHash == "" {
		tries = 1
	}
	for attempt := 0; attempt < tries; attempt++ {
		result, addErr := client.Add(ctx, request)
		if addErr == nil {
			return result, nil
		}
		if item.InfoHash != "" {
			current, listErr := client.List(ctx, []string{"hashString"})
			if listErr != nil {
				return transmission.AddResult{}, errors.Join(addErr, fmt.Errorf("添加结果尚未确认，保留准备缓存等待恢复：%w", listErr))
			} else if hasHash(current, item.InfoHash) {
				return transmission.AddResult{Hash: item.InfoHash, Duplicate: true}, nil
			}
		}
		if attempt+1 < tries && item.InfoHash != "" {
			continue
		}
		failure := fmt.Errorf("添加 Transmission 下载任务失败：%w", addErr)
		if item.InfoHash != "" {
			failureText := fmt.Sprintf("%s 添加失败，疑似为坏种", item.ReName)
			failureKey := fmt.Sprintf("download-error:%s:%s", notificationOperationID(ctx), strings.ToLower(item.InfoHash))
			if notifyErr := e.enqueueNotification(ctx, config, subscription, failureKey, failureText, "ERROR"); notifyErr != nil {
				failure = errors.Join(failure, notifyErr)
			}
		}
		return transmission.AddResult{}, &terminalAddError{err: failure}
	}
	return transmission.AddResult{}, errors.New("Transmission 下载尝试未执行")
}

func (e *Engine) torrentSource(ctx context.Context, config, subscription map[string]any, item *Item) (string, transmission.AddRequest, error) {
	if item.InfoHash == "" {
		item.InfoHash = hashFromTorrentURL(item.Torrent)
	}
	extension := ".torrent"
	var content []byte
	magnet := ""
	if strings.HasPrefix(strings.ToLower(item.Torrent), "magnet:") {
		extension = ".txt"
		magnet = item.Torrent
		content = []byte(magnet)
	} else if strings.HasPrefix(strings.ToLower(item.Torrent), "ed2k:") {
		return "", transmission.AddRequest{}, errors.New("Transmission 不支持直接添加 ed2k 资源")
	} else {
		body, status, err := e.fetchTorrent(ctx, config, item.Torrent)
		if err != nil {
			return "", transmission.AddRequest{}, err
		}
		if status == http.StatusNotFound {
			if item.InfoHash == "" {
				return "", transmission.AddRequest{}, errors.New("种子文件不存在且 RSS 没有 infoHash")
			}
			extension = ".txt"
			magnet = magnetForHash(item.InfoHash, item.ReName)
			content = []byte(magnet)
		} else if strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(body))), "magnet:") {
			extension = ".txt"
			magnet = strings.TrimSpace(string(body))
			content = []byte(magnet)
		} else if len(body) > 0 {
			content = body
		} else {
			return "", transmission.AddRequest{}, errors.New("种子文件为空")
		}
	}
	if item.InfoHash == "" {
		if magnet != "" {
			item.InfoHash = hashFromMagnet(magnet)
		} else {
			metadata, err := torrent.Parse(content)
			if err != nil {
				return "", transmission.AddRequest{}, fmt.Errorf("读取种子实际散列失败：%w", err)
			}
			item.InfoHash = metadata.TransmissionHash
		}
		if item.InfoHash == "" {
			return "", transmission.AddRequest{}, errors.New("资源缺少可用于对账的种子散列")
		}
	}
	request := transmission.AddRequest{Metainfo: content}
	if magnet != "" {
		request = transmission.AddRequest{Magnet: magnet}
	}
	if committed, err := findCachedTorrent(e.cacheDir(), item.InfoHash); err != nil {
		return "", transmission.AddRequest{}, err
	} else if committed != "" {
		return committed, request, nil
	}
	cachePath := cachePath(e.cacheDir(), valueString(subscription["id"]), item.InfoHash, extension) + ".pending"
	if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
		return "", transmission.AddRequest{}, fmt.Errorf("创建种子缓存目录失败：%w", err)
	}
	if err := atomicWrite(cachePath, content); err != nil {
		return "", transmission.AddRequest{}, fmt.Errorf("保存种子缓存失败：%w", err)
	}
	return cachePath, request, nil
}

func (e *Engine) fetchTorrent(ctx context.Context, config map[string]any, source string) ([]byte, int, error) {
	timeout := 30 * time.Second
	if seconds := valueInt(config["rssTimeout"]); seconds > 0 {
		timeout = time.Duration(seconds) * time.Second
	}
	client := e.rssHTTPClientForURL(config, timeout, source)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("创建种子请求失败：%w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("获取种子文件失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, response.StatusCode, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, response.StatusCode, fmt.Errorf("种子文件返回 HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, e.options.MaxTorrentBytes+1))
	if err != nil {
		return nil, response.StatusCode, fmt.Errorf("读取种子文件失败：%w", err)
	}
	if int64(len(body)) > e.options.MaxTorrentBytes {
		return nil, response.StatusCode, errors.New("种子文件超过大小限制")
	}
	if strings.HasPrefix(strings.TrimSpace(string(body)), "magnet:") {
		return []byte(strings.TrimSpace(string(body))), response.StatusCode, nil
	}
	return body, response.StatusCode, nil
}

func cachePath(root, id, hash, extension string) string {
	id = safeComponent(id)
	hash = safeComponent(strings.ToLower(hash))
	if id == "" {
		id = "unassigned"
	}
	if hash == "" {
		hash = "unknown"
	}
	return filepath.Join(root, id[:1], id, hash+extension)
}

func findCachedTorrent(root, hash string) (string, error) {
	return findTorrentCache(root, hash, false)
}

func findPreparedTorrent(root, hash string) (string, error) {
	return findTorrentCache(root, hash, true)
}

func findTorrentCache(root, hash string, prepared bool) (string, error) {
	if hash == "" {
		return "", nil
	}
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	wanted := strings.ToLower(safeComponent(hash))
	found := ""
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		name := strings.ToLower(entry.Name())
		suffix := ""
		if prepared {
			suffix = ".pending"
		}
		if name == wanted+".torrent"+suffix || name == wanted+".txt"+suffix {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return "", err
	}
	return found, nil
}

func commitPreparedCache(path string) error {
	const pendingSuffix = ".pending"
	if !strings.HasSuffix(path, pendingSuffix) {
		return nil
	}
	committed := strings.TrimSuffix(path, pendingSuffix)
	if info, err := os.Lstat(committed); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("已提交种子历史路径不是普通文件：%s", committed)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("清理重复的准备缓存失败：%w", err)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(path, committed); err != nil {
		return fmt.Errorf("提交种子历史缓存失败：%w", err)
	}
	return nil
}

func atomicWrite(path string, content []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".torrent-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func safeComponent(value string) string {
	var result strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func magnetForHash(hash, name string) string {
	return "magnet:?xt=urn:btih:" + url.QueryEscape(hash) + "&dn=" + url.QueryEscape(name)
}

func torrentLabels(config, subscription map[string]any, item Item) []string {
	labels := []string{"ani-rss"}
	subgroup := item.Subgroup
	if subgroup == "" {
		subgroup = "未知字幕组"
	}
	labels = append(labels, subgroup)
	if !item.Master {
		labels = append(labels, "备用RSS")
	}
	if valueBool(subscription["customTagsEnable"]) {
		labels = append(labels, stringSlice(subscription["customTags"])...)
	} else {
		labels = append(labels, stringSlice(config["customTags"])...)
	}
	return uniqueStrings(labels)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func torrentFinished(torrent transmission.Torrent) bool {
	if torrent.TotalSize > 0 && float64(torrent.HaveValid)*100/float64(torrent.TotalSize) < 100 {
		return false
	}
	return torrent.Status == 6 || torrent.IsFinished
}

func hasHash(torrents []transmission.Torrent, hash string) bool {
	for _, torrent := range torrents {
		if strings.EqualFold(torrent.Hash, hash) {
			return true
		}
	}
	return false
}

func hasMyAnimeGoLabel(labels []string, hash string) bool {
	for _, label := range labels {
		if label == "ani-rss" || (hash != "" && label == "ani-rss-collection:"+strings.ToLower(hash)) {
			return true
		}
	}
	return false
}

func containsEpisode(values any, episode float64) bool {
	slice, _ := values.([]any)
	for _, item := range slice {
		if valueFloat(item) == episode {
			return true
		}
	}
	return false
}

func delayed(config map[string]any, item Item, now time.Time) bool {
	if item.pubTime == nil {
		return false
	}
	minutes := valueInt(config["delayedDownload"])
	if minutes <= 0 {
		return false
	}
	return now.Add(-time.Duration(minutes) * time.Minute).Before(*item.pubTime)
}

func isNewest(items []Item, index int) bool {
	if len(items) == 0 || index == len(items)-1 {
		return true
	}
	item, newest := items[index], items[len(items)-1]
	if item.pubTime == nil || newest.pubTime == nil {
		return false
	}
	return item.pubTime.In(time.Local).Format("2006-01-02") == newest.pubTime.In(time.Local).Format("2006-01-02")
}

func torrentMetainfoFromCache(path string) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	dataPath := strings.TrimSuffix(path, ".pending")
	if strings.EqualFold(filepath.Ext(dataPath), ".txt") {
		magnet := strings.TrimSpace(string(data))
		if !strings.HasPrefix(strings.ToLower(magnet), "magnet:") {
			return nil, "", errors.New("种子缓存文本不是 magnet URI")
		}
		return nil, magnet, nil
	}
	if len(data) == 0 {
		hash := strings.TrimSuffix(filepath.Base(dataPath), filepath.Ext(dataPath))
		if hash == "" {
			return nil, "", errors.New("种子缓存文件为空且文件名不含 hash")
		}
		return nil, magnetForHash(hash, ""), nil
	}
	return data, "", nil
}
