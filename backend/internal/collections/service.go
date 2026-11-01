package collections

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/torrent"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/transmission"
)

const JobKind = "transmission-collection"

type Enqueuer interface {
	EnqueueJob(context.Context, string, string, json.RawMessage) (jobs.Job, error)
}

type Options struct {
	Enqueuer     Enqueuer
	Config       func(context.Context) (map[string]any, error)
	DownloadPath func(context.Context, map[string]any) (string, error)
	HTTPClient   *http.Client
	PollInterval time.Duration
	PollTimeout  time.Duration
}

type Service struct{ options Options }

func New(options Options) (*Service, error) {
	if options.Enqueuer == nil || options.Config == nil || options.DownloadPath == nil {
		return nil, errors.New("合集服务需要任务队列、配置读取和下载路径回调")
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 250 * time.Millisecond
	}
	if options.PollTimeout <= 0 {
		options.PollTimeout = 30 * time.Second
	}
	return &Service{options: options}, nil
}

func (s *Service) Routes() []server.Route {
	return []server.Route{
		{Method: http.MethodPost, Path: "/api/previewCollection", Handler: http.HandlerFunc(s.handlePreview)},
		{Method: http.MethodPost, Path: "/api/startCollection", Handler: http.HandlerFunc(s.handleStart)},
		{Method: http.MethodPost, Path: "/api/getCollectionSubgroup", Handler: http.HandlerFunc(s.handleSubgroup)},
	}
}

func (s *Service) handlePreview(w http.ResponseWriter, r *http.Request) {
	var request collectionRequest
	if err := decode(r, &request); err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	config, err := s.options.Config(r.Context())
	if err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	result, err := Preview(request.Torrent, request.Ani, config)
	if err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	server.Respond(w, 200, "success", result.Files)
}

func (s *Service) handleStart(w http.ResponseWriter, r *http.Request) {
	var request collectionRequest
	if err := decode(r, &request); err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	if request.Torrent == "" {
		server.Respond(w, 500, "合集种子不能为空", nil)
		return
	}
	if _, err := s.options.Config(r.Context()); err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	result, err := Preview(request.Torrent, request.Ani, map[string]any{})
	if err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	if len(result.Files) == 0 {
		server.Respond(w, 500, "合集没有可选择的文件", nil)
		return
	}
	payload, err := json.Marshal(request)
	if err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	key := "collection:" + strings.ToLower(result.Hash)
	job, err := s.options.Enqueuer.EnqueueJob(r.Context(), JobKind, key, payload)
	if err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	server.Respond(w, 200, "合集任务已加入队列", map[string]string{"jobId": job.ID})
}

func (s *Service) handleSubgroup(w http.ResponseWriter, r *http.Request) {
	var request collectionRequest
	if err := decode(r, &request); err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	result, err := Preview(request.Torrent, request.Ani, map[string]any{})
	if err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	// 上传检测只看筛选后的文件名，页面预填的未知字幕组不参与判断。
	group := "未知字幕组"
	for _, file := range result.Files {
		if detected := detectedSubgroup(file.Name); detected != "" {
			group = detected
			break
		}
	}
	server.Respond(w, 200, "success", group)
}

type collectionRequest struct {
	Torrent string         `json:"torrent"`
	Ani     map[string]any `json:"ani"`
	BgmInfo map[string]any `json:"bgmInfo,omitempty"`
}

type jobPayload struct {
	Torrent string         `json:"torrent"`
	Ani     map[string]any `json:"ani"`
	BgmInfo map[string]any `json:"bgmInfo,omitempty"`
}

func (s *Service) Handler() jobs.Handler { return collectionHandler{service: s} }

type collectionHandler struct{ service *Service }

func (h collectionHandler) Reconcile(ctx context.Context, job jobs.Job) (bool, error) {
	var payload jobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return false, fmt.Errorf("解析合集任务失败：%w", err)
	}
	metadata, err := decodeMetadata(payload.Torrent)
	if err != nil {
		return false, err
	}
	config, err := h.service.options.Config(ctx)
	if err != nil {
		return false, err
	}
	client := h.service.client(config)
	torrents, err := client.List(ctx, []string{"hashString", "name", "status", "files", "fileStats", "labels"})
	if err != nil {
		return false, fmt.Errorf("查询 Transmission 合集任务失败：%w", err)
	}
	for _, current := range torrents {
		if !strings.EqualFold(current.Hash, metadata.TransmissionHash) {
			continue
		}
		if !ownsCollection(current.Labels, metadata.TransmissionHash) {
			return false, nil
		}
		wanted, names, err := expectedFiles(payload, config)
		if err != nil {
			return false, err
		}
		return collectionComplete(current, wanted, names), nil
	}
	return false, nil
}

func (h collectionHandler) Execute(ctx context.Context, job jobs.Job) error {
	var payload jobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("解析合集任务失败：%w", err)
	}
	metadata, err := decodeMetadata(payload.Torrent)
	if err != nil {
		return err
	}
	config, err := h.service.options.Config(ctx)
	if err != nil {
		return err
	}
	wanted, renameMap, err := expectedFiles(payload, config)
	if err != nil {
		return err
	}
	if len(wanted) == 0 {
		return errors.New("合集没有可选择的文件")
	}
	downloadPath, err := h.service.options.DownloadPath(ctx, payload.Ani)
	if err != nil {
		return err
	}
	client := h.service.client(config)
	labels := []string{"MyAnimeGo合集下载", collectionOwnerLabel(metadata.TransmissionHash)}
	if group := mapString(payload.Ani, "subgroup"); group != "" {
		labels = append(labels, group)
	}
	metainfo, err := base64.StdEncoding.DecodeString(payload.Torrent)
	if err != nil {
		return fmt.Errorf("合集种子不是有效 Base64：%w", err)
	}
	added, err := client.Add(ctx, transmission.AddRequest{Metainfo: metainfo, DownloadDir: downloadPath, Labels: labels, Paused: true})
	if err != nil {
		return fmt.Errorf("添加 Transmission 合集任务失败：%w", err)
	}
	hash := added.Hash
	if hash == "" {
		hash = metadata.TransmissionHash
	}
	if !strings.EqualFold(hash, metadata.TransmissionHash) {
		return fmt.Errorf("Transmission 返回的合集散列与种子不匹配")
	}
	if added.Duplicate {
		current, err := client.List(ctx, []string{"hashString", "labels"})
		if err != nil {
			return fmt.Errorf("核对已存在的 Transmission 合集归属失败：%w", err)
		}
		owned := false
		for _, item := range current {
			if strings.EqualFold(item.Hash, hash) && ownsCollection(item.Labels, metadata.TransmissionHash) {
				owned = true
				break
			}
		}
		if !owned {
			return errors.New("合集种子已存在于 Transmission，但该任务不属于 ani-rss，已停止修改")
		}
	}
	selected := make([]int, 0, len(wanted))
	all := make([]int, 0, len(metadata.Files))
	for id := range wanted {
		selected = append(selected, id)
	}
	sort.Ints(selected)
	for _, file := range metadata.Files {
		all = append(all, file.Index)
	}
	if len(all) > 0 {
		if err := client.SetFilesWanted(ctx, hash, all, false); err != nil {
			return fmt.Errorf("设置合集文件为不下载失败：%w", err)
		}
	}
	if len(selected) > 0 {
		if err := client.SetFilesWanted(ctx, hash, selected, true); err != nil {
			return fmt.Errorf("选择合集文件失败：%w", err)
		}
	}
	renameIDs := make([]int, 0, len(renameMap))
	for id := range renameMap {
		renameIDs = append(renameIDs, id)
	}
	sort.Ints(renameIDs)
	for _, id := range renameIDs {
		newBase := renameMap[id]
		originalBase := path.Base(metadata.Files[id].Name)

		current, err := client.List(ctx, []string{"hashString", "files"})
		if err != nil {
			return fmt.Errorf("重命名前查询 Transmission 文件失败：%w", err)
		}
		alreadyRenamed := false
		oldPath := ""
		// RPC 路径包含种子根目录，按固定文件索引取得当前路径后再改文件名。
		for _, item := range current {
			if !strings.EqualFold(item.Hash, hash) || id >= len(item.Files) {
				continue
			}
			currentPath := item.Files[id].Name
			if path.Base(currentPath) == newBase {
				alreadyRenamed = true
			}
			if path.Base(currentPath) == originalBase {
				oldPath = currentPath
			}
		}

		if alreadyRenamed {
			continue
		}
		if oldPath == "" {
			return fmt.Errorf("Transmission 中合集文件 %s 不存在，无法恢复重命名", metadata.Files[id].Name)
		}
		if err := client.Rename(ctx, hash, oldPath, newBase); err != nil {
			return fmt.Errorf("重命名合集文件 %s 失败：%w", oldPath, err)
		}
	}
	if err := client.Start(ctx, hash); err != nil {
		return fmt.Errorf("启动 Transmission 合集任务失败：%w", err)
	}
	return h.service.waitComplete(ctx, client, hash, wanted, renameMap)
}

func collectionOwnerLabel(hash string) string { return "ani-rss-collection:" + strings.ToLower(hash) }

func ownsCollection(labels []string, hash string) bool {
	owner := collectionOwnerLabel(hash)
	for _, label := range labels {
		if label == owner {
			return true
		}
	}
	return false
}

func (s *Service) client(config map[string]any) *transmission.Client {
	return transmission.New(transmission.Config{URL: mapString(config, "downloadToolHost"), Username: mapString(config, "downloadToolUsername"), Password: mapString(config, "downloadToolPassword"), HTTPClient: s.options.HTTPClient})
}

func (s *Service) waitComplete(ctx context.Context, client *transmission.Client, hash string, wanted map[int]bool, names map[int]string) error {
	deadline := time.NewTimer(s.options.PollTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(s.options.PollInterval)
	defer ticker.Stop()
	for {
		torrents, err := client.List(ctx, []string{"hashString", "status", "files", "fileStats"})
		if err != nil {
			return fmt.Errorf("确认 Transmission 合集状态失败：%w", err)
		}
		for _, current := range torrents {
			if strings.EqualFold(current.Hash, hash) && collectionComplete(current, wanted, names) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Transmission 合集文件状态未在时限内确认")
		case <-ticker.C:
		}
	}
}

func decode(r *http.Request, target any) error {
	if r.Body == nil {
		return errors.New("JSON 请求为空")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 40<<20))
	if err := decoder.Decode(target); err != nil {
		return errors.New("JSON 请求格式错误")
	}
	return nil
}

var subgroupPattern = regexp.MustCompile(`^\[([^\]]+)]`)

func detectedSubgroup(name string) string {
	for _, candidate := range []string{name, path.Base(name)} {
		if match := subgroupPattern.FindStringSubmatch(candidate); len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

func decodeMetadata(encoded string) (*torrent.Metadata, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("合集种子不是有效 Base64：%w", err)
	}
	metadata, err := torrent.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("解析合集种子失败：%w", err)
	}
	return metadata, nil
}

func expectedFiles(payload jobPayload, config map[string]any) (map[int]bool, map[int]string, error) {
	preview, err := Preview(payload.Torrent, payload.Ani, config)
	if err != nil {
		return nil, nil, err
	}
	wanted, names := make(map[int]bool), make(map[int]string)
	for _, file := range preview.Files {
		if file.ReName == "" {
			continue
		}
		wanted[file.ID] = true
		names[file.ID] = path.Base(file.ReName)
	}
	return wanted, names, nil
}

func collectionComplete(current transmission.Torrent, wanted map[int]bool, names map[int]string) bool {
	if current.Status == 0 || len(wanted) == 0 || len(current.Files) == 0 || len(current.FileStats) != len(current.Files) {
		return false
	}
	for id, file := range current.Files {
		shouldWant := wanted[id]
		if id >= len(current.FileStats) || current.FileStats[id].Wanted != shouldWant {
			return false
		}
		if expected, ok := names[id]; ok && path.Base(file.Name) != expected {
			return false
		}
	}
	return true
}
