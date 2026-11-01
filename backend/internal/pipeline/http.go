package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

type torrentInfo struct {
	ID         string   `json:"id,omitempty"`
	Hash       string   `json:"hash"`
	Name       string   `json:"name"`
	State      string   `json:"state"`
	Category   string   `json:"category,omitempty"`
	TagList    []string `json:"tagList"`
	Completed  int64    `json:"completed"`
	Size       int64    `json:"size"`
	Progress   float64  `json:"progress"`
	FormatSize string   `json:"formatSize"`
	SavePath   string   `json:"savePath"`
}

func (e *Engine) handlePreview(w http.ResponseWriter, r *http.Request) {
	var subscription map[string]any
	if err := decodeJSON(r, &subscription); err != nil {
		respondError(w, err)
		return
	}
	result, err := e.Preview(r.Context(), subscription)
	respond(w, result, err)
}

func (e *Engine) handleDownloadPath(w http.ResponseWriter, r *http.Request) {
	var subscription map[string]any
	if err := decodeJSON(r, &subscription); err != nil {
		respondError(w, err)
		return
	}
	result, err := e.DownloadPath(r.Context(), subscription)
	respond(w, result, err)
}

func (e *Engine) handleRefreshAni(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &request); err != nil {
		respondError(w, err)
		return
	}
	if strings.TrimSpace(request.ID) == "" {
		respondError(w, errors.New("订阅 ID 不能为空"))
		return
	}
	if err := e.RefreshSubscription(r.Context(), request.ID); err != nil {
		respondError(w, err)
		return
	}
	server.Respond(w, 200, "已开始刷新RSS", nil)
}

func (e *Engine) handleRefreshAll(w http.ResponseWriter, r *http.Request) {
	if err := e.RefreshAll(r.Context()); err != nil {
		respondError(w, err)
		return
	}
	server.Respond(w, 200, "已开始刷新RSS", nil)
}

func (e *Engine) handleTorrentsInfos(w http.ResponseWriter, r *http.Request) {
	config, err := e.config(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	torrents, err := e.transmission(config).List(r.Context(), []string{"id", "hashString", "name", "status", "downloadDir", "labels", "isFinished", "totalSize", "haveValid", "files"})
	if err != nil {
		respondError(w, fmt.Errorf("读取 Transmission 下载列表失败：%w", err))
		return
	}
	result := make([]torrentInfo, 0, len(torrents))
	for _, torrent := range torrents {
		// 合集使用专用归属标签，列表按 hash 核对后一起展示。
		if !hasMyAnimeGoLabel(torrent.Labels, torrent.Hash) {
			continue
		}
		total := torrent.TotalSize
		if total < 1 {
			total = 1
		}
		progress := math.Round(float64(torrent.HaveValid)*100/float64(total)*100) / 100
		state := "downloading"
		if torrent.Status == 6 {
			state = "stalledUP"
		} else if torrent.IsFinished {
			state = "stoppedUP"
		}
		labels := torrent.Labels
		if labels == nil {
			labels = []string{}
		}
		result = append(result, torrentInfo{
			Hash: torrent.Hash, Name: torrent.Name, State: state,
			TagList: labels, Completed: torrent.HaveValid, Size: torrent.TotalSize,
			Progress: progress, FormatSize: formatSize(torrent.TotalSize),
			SavePath: absolutePath(torrent.DownloadDir),
		})
	}
	server.Respond(w, 200, "success", result)
}

func (e *Engine) handleDownloadLoginTest(w http.ResponseWriter, r *http.Request) {
	var config map[string]any
	if err := decodeJSON(r, &config); err != nil {
		respondError(w, err)
		return
	}
	if valueString(config["downloadToolHost"]) == "" || valueString(config["downloadToolUsername"]) == "" || valueString(config["downloadToolPassword"]) == "" {
		server.Respond(w, 500, "登录失败", nil)
		return
	}
	_, err := e.transmission(config).Call(r.Context(), "session-get", map[string]any{"fields": []string{"version", "rpc-version", "rpc-version-minimum"}})
	if err != nil {
		server.Respond(w, 500, "登录失败", nil)
		return
	}
	server.Respond(w, 200, "登录成功", nil)
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	if err := decoder.Decode(target); err != nil {
		return errors.New("JSON 请求格式错误")
	}
	return nil
}

func respond(w http.ResponseWriter, data any, err error) {
	if err != nil {
		respondError(w, err)
		return
	}
	server.Respond(w, 200, "success", data)
}

func respondError(w http.ResponseWriter, err error) {
	server.Respond(w, 500, err.Error(), nil)
}

func absolutePath(path string) string {
	if path == "" {
		return ""
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolute)
}
