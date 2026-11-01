package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/transmission"
)

var torrentListFields = []string{"id", "hashString", "name", "status", "percentDone", "recheckProgress", "totalSize", "sizeWhenDone", "leftUntilDone", "rateDownload", "rateUpload", "eta", "uploadRatio", "uploadedEver", "downloadedEver", "error", "errorString", "peersConnected", "peersSendingToUs", "peersGettingFromUs", "addedDate", "doneDate", "queuePosition", "labels", "downloadDir", "isFinished", "isStalled", "haveValid", "metadataPercentComplete", "bandwidthPriority", "group", "activityDate", "secondsSeeding", "isPrivate", "file-count", "editDate", "pieceCount", "seedRatioLimit", "seedRatioMode"}
var torrentDetailFields = []string{"comment", "creator", "dateCreated", "activityDate", "pieceCount", "pieceSize", "downloadLimit", "downloadLimited", "uploadLimit", "uploadLimited", "honorsSessionLimits", "bandwidthPriority", "seedRatioMode", "seedRatioLimit", "seedIdleMode", "seedIdleLimit", "peer-limit", "group", "sequential_download", "haveUnchecked", "corruptEver", "desiredAvailable", "secondsDownloading", "secondsSeeding", "isPrivate", "magnetLink", "metadataPercentComplete", "peersFrom", "webseedsSendingToUs"}
var transmissionSessionFields = []string{"version", "rpc-version", "speed-limit-down", "speed-limit-down-enabled", "speed-limit-up", "speed-limit-up-enabled", "alt-speed-enabled", "alt-speed-down", "alt-speed-up", "download-dir", "download-queue-enabled", "download-queue-size", "seed-queue-enabled", "seed-queue-size", "units"}

// TransmissionRoute 复用连接和会话挑战结果；调用方只能操作当前配置的下载器。
func (s *Server) TransmissionRoute() Route {
	var mu sync.Mutex
	var cached *transmission.Client
	var previous [3]string
	return Route{Method: http.MethodPost, Path: "/api/transmissionRPC", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Method     string         `json:"method"`
			Arguments  map[string]any `json:"arguments"`
			Details    bool           `json:"details"`
			FreshFiles bool           `json:"freshFiles"`
			Trackers   bool           `json:"trackers"`
			Columns    []string       `json:"columns"`
			View       string         `json:"view"`
			Rename     *renameRequest `json:"rename"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			Respond(w, 400, "下载管理请求格式错误", nil)
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			Respond(w, 400, "请求只接受一个 JSON 对象", nil)
			return
		}
		batchRename := input.Method == "torrent-rename-preview" || input.Method == "torrent-rename-batch"
		var args map[string]any
		var err error
		if batchRename {
			if input.Rename == nil {
				Respond(w, 400, "缺少重命名规则", nil)
				return
			}
		} else {
			args, err = managedTransmissionArguments(input.Method, input.Arguments, input.Details, input.View, input.FreshFiles, input.Trackers, input.Columns)
		}
		if err != nil {
			Respond(w, 400, err.Error(), nil)
			return
		}
		timeout := 20 * time.Second
		if input.Method == "torrent-rename-batch" {
			timeout = 5 * time.Minute
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		config, err := s.store.GetConfig(ctx)
		if err != nil {
			Respond(w, 500, "读取 Transmission 配置失败", nil)
			return
		}
		connection := [3]string{strings.TrimSpace(text(config["downloadToolHost"])), text(config["downloadToolUsername"]), text(config["downloadToolPassword"])}
		if connection[0] == "" {
			Respond(w, 400, "请先在设置中配置 Transmission 连接", nil)
			return
		}
		mu.Lock()
		if cached == nil || connection != previous {
			cached = transmission.New(transmission.Config{URL: connection[0], Username: connection[1], Password: connection[2]})
			previous = connection
		}
		client := cached
		mu.Unlock()
		if batchRename {
			var result any
			if input.Method == "torrent-rename-preview" {
				result, err = collectionRenamePlan(ctx, client, *input.Rename)
			} else {
				result, err = applyCollectionRename(ctx, client, *input.Rename)
			}
			if err != nil {
				Respond(w, 400, err.Error(), nil)
				return
			}
			Respond(w, 200, "success", result)
			return
		}
		data, err := client.Call(ctx, input.Method, args)
		if err != nil {
			Respond(w, 502, "Transmission 请求失败："+err.Error(), nil)
			return
		}
		if (input.Method == "session-get" && input.View == "settings") || (input.Method == "torrent-get" && input.Details && input.View == "options") {
			var result map[string]any
			if json.Unmarshal(data, &result) == nil {
				group := "session"
				if input.Method == "torrent-get" {
					group = "torrent"
				}
				result["_fields"] = transmissionFields[group]
				data, _ = json.Marshal(result)
			}
		}
		if input.Method == "torrent-get" && !input.Details && input.Trackers {
			data = compactTrackerSummaries(data)
		}
		Respond(w, 200, "success", data)
	})}
}

func managedTransmissionArguments(method string, args map[string]any, details bool, view string, freshFiles, trackers bool, columns []string) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	allowed := map[string]string{}
	needsIDs := false
	switch method {
	case "torrent-get":
		allowed["ids"] = "readids"
		if details {
			needsIDs = true
		}
	case "session-get", "session-stats", "port-test", "blocklist-update", "group-get":
	case "free-space":
		allowed["path"] = "nonempty"
	case "group-set":
		allowed["name"] = "nonempty"
		allowed["honorsSessionLimits"] = "bool"
		for _, key := range []string{"speed-limit-down", "speed-limit-up"} {
			allowed[key] = "int"
		}
		for _, key := range []string{"speed-limit-down-enabled", "speed-limit-up-enabled"} {
			allowed[key] = "bool"
		}
	case "torrent-set-location":
		needsIDs = true
		allowed = map[string]string{"ids": "ids", "location": "nonempty", "move": "bool"}
		if _, ok := args["move"].(bool); !ok {
			return nil, errors.New("请选择移动文件或仅更改位置")
		}
	case "torrent-rename-path":
		needsIDs = true
		allowed = map[string]string{"ids": "ids", "path": "relativepath", "name": "basename"}
		if ids, ok := args["ids"].([]any); !ok || len(ids) != 1 {
			return nil, errors.New("每次重命名一个任务内的文件或目录")
		}

	case "torrent-start", "torrent-start-now", "torrent-stop", "torrent-verify", "torrent-reannounce", "queue-move-top", "queue-move-bottom", "queue-move-up", "queue-move-down":
		needsIDs = true
		allowed["ids"] = "ids"
	case "torrent-remove":
		needsIDs = true
		allowed["ids"] = "ids"
		allowed["delete-local-data"] = "bool"
	case "torrent-set":
		needsIDs = true
		allowed["ids"] = "ids"
		for _, field := range transmissionFields["torrent"] {
			allowed[field.Key] = "torrentfield"
		}
		allowed["labels"] = "labels"
		allowed["trackerList"] = "text"

		for _, key := range []string{"files-wanted", "files-unwanted", "priority-high", "priority-normal", "priority-low"} {
			allowed[key] = "indexes"
		}
	case "session-set":
		for _, field := range transmissionFields["session"] {
			allowed[field.Key] = "sessionfield"
		}
	case "torrent-add":
		allowed = map[string]string{"filename": "url", "metainfo": "string", "paused": "bool", "download-dir": "nonempty", "labels": "labels", "bandwidthPriority": "priority", "peer-limit": "int", "sequential_download": "bool", "files-unwanted": "indexes", "files-wanted": "indexes"}
		_, url := args["filename"]
		_, data := args["metainfo"]
		if url == data {
			return nil, errors.New("请选择一个种子文件或输入一个磁力/HTTP 地址")
		}
	default:
		return nil, errors.New("不支持此下载管理操作")
	}
	required := map[string][]string{"free-space": {"path"}, "torrent-set-location": {"location", "move"}, "torrent-rename-path": {"path", "name"}, "group-set": {"name"}}
	for _, key := range required[method] {
		if _, ok := args[key]; !ok {
			return nil, errors.New("缺少下载参数：" + key)
		}
	}
	if needsIDs {
		ids, ok := args["ids"].([]any)
		if !ok || len(ids) == 0 || len(ids) > 200 {
			return nil, errors.New("请选择 1–200 个任务")
		}
		if details && len(ids) != 1 {
			return nil, errors.New("详情每次只读取一个任务")
		}
	}
	for key, value := range args {
		kind, ok := allowed[key]
		if !ok {
			return nil, errors.New("不支持的下载参数：" + key)
		}
		valid := false
		switch kind {
		case "torrentfield", "sessionfield":
			group := "torrent"
			if kind == "sessionfield" {
				group = "session"
			}
			for _, field := range transmissionFields[group] {
				if field.Key == key {
					valid = transmissionFieldValid(value, field)
					break
				}
			}
		case "labels":
			values, ok := value.([]any)
			valid = ok && len(values) <= 100
			for _, item := range values {
				label, ok := item.(string)
				if !ok || strings.TrimSpace(label) == "" || len(label) > 256 {
					valid = false
					break
				}
			}
		case "text", "nonempty", "basename", "relativepath":
			text, ok := value.(string)
			valid = ok && !strings.ContainsRune(text, 0)
			if kind != "text" {
				valid = valid && strings.TrimSpace(text) != ""
			}
			if kind == "basename" {
				valid = valid && text != "." && text != ".." && !strings.ContainsAny(text, "/\\")
			}
			if kind == "relativepath" {
				valid = valid && !strings.HasPrefix(text, "/")
				for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == '/' || r == '\\' }) {
					if part == ".." {
						valid = false
					}
				}
			}
		case "bool":
			_, valid = value.(bool)
		case "int", "priority":
			number, ok := value.(float64)
			valid = ok && !math.IsNaN(number) && math.Trunc(number) == number && number >= 0 && number <= math.MaxInt32
			if kind == "priority" {
				valid = ok && (number == -1 || number == 0 || number == 1)
			}
		case "ids", "indexes", "readids":
			if text, ok := value.(string); kind == "readids" && ok && text == "recently-active" {
				valid = true
				break
			}
			values, ok := value.([]any)
			valid = ok && len(values) > 0 && len(values) <= 10000
			if (kind == "ids" || kind == "readids") && len(values) > 200 {
				valid = false
			}
			for _, item := range values {
				number, ok := item.(float64)
				if !ok || math.Trunc(number) != number || number < 0 || number > math.MaxInt32 {
					valid = false
					break
				}
			}
		case "string", "url":
			value, ok := value.(string)
			valid = ok && strings.TrimSpace(value) != ""
			if kind == "url" {
				valid = valid && (strings.HasPrefix(value, "magnet:?") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://"))
			}
		}
		if !valid {
			return nil, errors.New("下载参数格式错误：" + key)
		}
	}
	if method == "torrent-get" {
		fields := append([]string{}, torrentListFields...)
		if !details && view != "magnets" && columns != nil {
			fields = []string{"id", "hashString", "name", "status", "error", "errorString", "downloadDir", "labels", "rateDownload", "rateUpload", "percentDone", "recheckProgress", "sizeWhenDone", "leftUntilDone", "haveValid", "metadataPercentComplete", "queuePosition", "editDate", "file-count", "peersConnected"}
			allowedFields := map[string]bool{}
			for _, field := range torrentListFields {
				allowedFields[field] = true
			}
			seen := map[string]bool{}
			for _, field := range fields {
				seen[field] = true
			}
			dependencies := map[string]string{"trackerSeeds": "peersSendingToUs", "trackerPeers": "peersGettingFromUs"}
			for _, field := range columns {
				if dependency := dependencies[field]; dependency != "" {
					field = dependency
				}
				if allowedFields[field] && !seen[field] {
					fields = append(fields, field)
					seen[field] = true
				}
			}
		}
		if details {
			fields = append(fields, torrentDetailFields...)
			switch view {
			case "", "overview", "options":
			case "files":
				fields = append(fields, "fileStats")
				if freshFiles {
					fields = append(fields, "files")
				}
			case "trackers":
				fields = append(fields, "trackerStats", "trackerList")
			case "pieces":
				fields = append(fields, "pieces", "availability")
			case "peers":
				fields = append(fields, "peers")
			default:
				return nil, errors.New("不支持的任务详情分类")
			}
		}
		if view == "magnets" {
			fields = []string{"id", "name", "hashString", "magnetLink"}
		}
		if trackers {
			fields = append(fields, "trackerStats")
		}
		args["fields"] = fields
	}
	if method == "session-get" {
		if view == "settings" {
			args["fields"] = append(transmissionFieldNames("session"), "version", "rpc-version", "units", "blocklist-size")
		} else {
			args["fields"] = transmissionSessionFields
		}
	}
	return args, nil
}

// 列表只保留状态与分类需要的 Tracker 字段，完整记录在详情中读取。
func compactTrackerSummaries(data json.RawMessage) json.RawMessage {
	var result map[string]json.RawMessage
	if json.Unmarshal(data, &result) != nil {
		return data
	}
	var torrents []map[string]json.RawMessage
	if json.Unmarshal(result["torrents"], &torrents) != nil {
		return data
	}
	fields := []string{"id", "host", "announce", "isBackup", "hasAnnounced", "lastAnnounceSucceeded", "lastAnnounceResult", "announceState", "nextAnnounceTime", "seederCount", "leecherCount", "downloadCount"}
	for _, torrent := range torrents {
		var trackers []map[string]json.RawMessage
		if json.Unmarshal(torrent["trackerStats"], &trackers) != nil {
			continue
		}
		for index, tracker := range trackers {
			summary := make(map[string]json.RawMessage, len(fields))
			for _, field := range fields {
				if value, ok := tracker[field]; ok {
					summary[field] = value
				}
			}
			trackers[index] = summary
		}
		torrent["trackerStats"], _ = json.Marshal(trackers)
	}
	result["torrents"], _ = json.Marshal(torrents)
	encoded, err := json.Marshal(result)
	if err != nil {
		return data
	}
	return encoded
}
