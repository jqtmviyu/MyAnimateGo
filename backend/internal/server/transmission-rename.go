package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/transmission"
)

type renameItem struct {
	Index   int    `json:"index"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	Target  string `json:"target"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
type renameRequest struct {
	ID                *int         `json:"id"`
	Hash              string       `json:"hash"`
	Directory         string       `json:"directory"`
	Recursive         bool         `json:"recursive"`
	MediaOnly         bool         `json:"mediaOnly"`
	Mode              string       `json:"mode"`
	Pattern           string       `json:"pattern"`
	Replacement       string       `json:"replacement"`
	Title             string       `json:"title"`
	Season            int          `json:"season"`
	SeasonPattern     string       `json:"seasonPattern"`
	IgnoreCase        bool         `json:"ignoreCase"`
	PreserveExtension bool         `json:"preserveExtension"`
	Items             []renameItem `json:"items"`
}
type renamePreview struct {
	ID          int          `json:"id"`
	Hash        string       `json:"hash"`
	Name        string       `json:"name"`
	Directories []string     `json:"directories"`
	Rows        []renameItem `json:"rows"`
}

var renameMedia = map[string]bool{".mkv": true, ".mp4": true, ".avi": true, ".mov": true, ".m4v": true, ".ts": true, ".m2ts": true, ".webm": true, ".wmv": true, ".flv": true, ".rmvb": true, ".srt": true, ".ass": true, ".ssa": true, ".vtt": true, ".sup": true, ".sub": true, ".smi": true}
var renameSubtitle = regexp.MustCompile(`(?i)([._](?:chs|cht|sc|tc|zh|zho|chi|zh[-_](?:hans|hant|cn|tw)|ja|jpn|jp|en|eng|big5)(?:[&_-](?:chs|cht|ja|jpn|en|eng))?)$`)
var renameEpisode = regexp.MustCompile(`^\d+(?:\.\d+)?$`)

// 预览和提交共用规则，提交时从下载器重读路径再生成计划。
func collectionRenamePlan(ctx context.Context, client *transmission.Client, input renameRequest) (renamePreview, error) {
	plan := renamePreview{Rows: []renameItem{}, Directories: []string{}}
	if input.ID == nil || *input.ID < 0 {
		return plan, errors.New("请选择一个下载任务")
	}
	if len(input.Pattern) > 2048 || len(input.Replacement) > 1024 || len(input.Title) > 512 || len(input.SeasonPattern) > 2048 {
		return plan, errors.New("重命名规则过长")
	}
	if input.Mode != "episode" && input.Mode != "replace" {
		return plan, errors.New("请选择剧集命名或正则替换")
	}
	if input.Pattern == "" {
		return plan, errors.New("请输入匹配正则")
	}
	pattern := input.Pattern
	if input.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	matcher, err := regexp.Compile(pattern)
	if err != nil {
		return plan, fmt.Errorf("匹配正则有误：%s", err)
	}
	var seasonMatcher *regexp.Regexp
	if input.Mode == "episode" {
		if strings.TrimSpace(input.Title) == "" || input.Season < 0 || input.Season > 999 {
			return plan, errors.New("请输入番剧名称和 0–999 范围内的季度")
		}
		if matcher.NumSubexp() < 1 {
			return plan, errors.New("集数正则需要用括号捕获集数，例如 E(\\d+)")
		}
		if input.SeasonPattern != "" {
			seasonMatcher, err = regexp.Compile(input.SeasonPattern)
			if err != nil || seasonMatcher.NumSubexp() < 1 {
				return plan, errors.New("目录季度正则需要有效的第一捕获组")
			}
		}
	}
	data, err := client.Call(ctx, "torrent-get", map[string]any{"ids": []int{*input.ID}, "fields": []string{"id", "hashString", "name", "files"}})
	if err != nil {
		return plan, err
	}
	var result struct {
		Torrents []struct {
			ID    int    `json:"id"`
			Hash  string `json:"hashString"`
			Name  string `json:"name"`
			Files []struct {
				Name string `json:"name"`
			} `json:"files"`
		} `json:"torrents"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return plan, err
	}
	if len(result.Torrents) != 1 {
		return plan, errors.New("任务已移除，请刷新任务列表")
	}
	task := result.Torrents[0]
	if len(task.Files) == 0 {
		return plan, errors.New("文件列表尚未就绪，请等待种子元数据获取完成")
	}
	plan.ID = task.ID
	plan.Hash = task.Hash
	plan.Name = task.Name
	occupied := map[string]int{}
	dirs := map[string]bool{}
	for index, file := range task.Files {
		occupied[strings.ToLower(file.Name)] = index
		for dir := path.Dir(file.Name); dir != "." && dir != "/"; dir = path.Dir(dir) {
			dirs[dir] = true
		}
	}
	for dir := range dirs {
		plan.Directories = append(plan.Directories, dir)
		occupied[strings.ToLower(dir)] = -1
	}
	sort.Strings(plan.Directories)
	if len(dirs) == 0 {
		return plan, errors.New("此任务是单文件，目录批量重命名仅用于包含文件夹的任务")
	}
	if input.Directory != "" && !dirs[input.Directory] {
		return plan, errors.New("目录已变化，请重新打开批量重命名")
	}
	destinations := map[string][]int{}
	for index, file := range task.Files {
		dir := path.Dir(file.Name)
		if dir == "." {
			continue
		}
		if input.Directory != "" && dir != input.Directory && (!input.Recursive || !strings.HasPrefix(dir, input.Directory+"/")) {
			continue
		}
		ext := path.Ext(file.Name)
		if input.MediaOnly && !renameMedia[strings.ToLower(ext)] {
			continue
		}
		row := renameItem{Index: index, Path: file.Name, Name: path.Base(file.Name), Status: "unmatched", Message: "未匹配"}
		subject := row.Name
		if input.PreserveExtension || input.Mode == "episode" {
			subject = strings.TrimSuffix(subject, ext)
		}
		if matcher.MatchString(subject) {
			row.Target = matcher.ReplaceAllString(subject, input.Replacement)
			if input.Mode == "episode" {
				match := matcher.FindStringSubmatch(subject)
				episode := match[1]
				if !renameEpisode.MatchString(episode) {
					row.Status = "invalid"
					row.Message = "集数捕获组应为数字，可包含小数"
					plan.Rows = append(plan.Rows, row)
					continue
				}
				parts := strings.SplitN(episode, ".", 2)
				number, e := strconv.ParseUint(parts[0], 10, 32)
				if e != nil {
					row.Status = "invalid"
					row.Message = "集数超出范围"
					plan.Rows = append(plan.Rows, row)
					continue
				}
				episode = fmt.Sprintf("%02d", number)
				if len(parts) == 2 {
					episode += "." + parts[1]
				}
				season := input.Season
				if seasonMatcher != nil {
					if match := seasonMatcher.FindStringSubmatch(dir); len(match) > 1 {
						season, e = strconv.Atoi(match[1])
						if e != nil || season < 0 || season > 999 {
							row.Status = "invalid"
							row.Message = "目录季度应为 0–999"
							plan.Rows = append(plan.Rows, row)
							continue
						}
					}
				}
				row.Target = fmt.Sprintf("%s S%02dE%s", strings.TrimSpace(input.Title), season, episode)
				if strings.ToLower(ext) == ".ass" || strings.ToLower(ext) == ".ssa" || strings.ToLower(ext) == ".srt" || strings.ToLower(ext) == ".vtt" || strings.ToLower(ext) == ".sup" || strings.ToLower(ext) == ".sub" || strings.ToLower(ext) == ".smi" {
					row.Target += renameSubtitle.FindString(subject)
				}
			}
			if input.PreserveExtension || input.Mode == "episode" {
				row.Target += ext
			}
			row.Status = "ready"
			row.Message = "待重命名"
			if row.Target == row.Name {
				row.Status = "unchanged"
				row.Message = "名称相同"
			} else if strings.TrimSpace(row.Target) == "" || row.Target == "." || row.Target == ".." || strings.ContainsAny(row.Target, "/\\\x00\r\n") || len(row.Target) > 255 {
				row.Status = "invalid"
				row.Message = "新名称为空、过长或包含路径字符"
			} else {
				target := strings.ToLower(path.Join(dir, row.Target))
				if other, exists := occupied[target]; exists && other != index {
					row.Status = "conflict"
					row.Message = "同目录已有同名文件或文件夹"
				}
				destinations[target] = append(destinations[target], len(plan.Rows))
			}
		}
		plan.Rows = append(plan.Rows, row)
	}
	for _, indices := range destinations {
		if len(indices) > 1 {
			for _, index := range indices {
				plan.Rows[index].Status = "conflict"
				plan.Rows[index].Message = "多项将生成同一个文件名"
			}
		}
	}
	return plan, nil
}

func applyCollectionRename(ctx context.Context, client *transmission.Client, input renameRequest) ([]renameItem, error) {
	if input.Hash == "" || len(input.Items) == 0 || len(input.Items) > 500 {
		return nil, errors.New("请选择 1–500 个预览条目")
	}
	plan, err := collectionRenamePlan(ctx, client, input)
	if err != nil {
		return nil, err
	}
	if plan.Hash != input.Hash {
		return nil, errors.New("任务已变化，请重新预览")
	}
	current := map[int]renameItem{}
	for _, row := range plan.Rows {
		current[row.Index] = row
	}
	seen := map[int]bool{}
	rows := make([]renameItem, 0, len(input.Items))
	for _, item := range input.Items {
		row, ok := current[item.Index]
		if !ok || seen[item.Index] || row.Status != "ready" || row.Path != item.Path || row.Target != item.Target {
			return nil, errors.New("文件路径或规则结果已变化，请重新预览后执行")
		}
		seen[item.Index] = true
		row.Status = "pending"
		row.Message = "尚未执行"
		rows = append(rows, row)
	}
	// 每个路径只提交一次，失败后停止；已完成条目单独返回，供刷新后继续。
	for index := range rows {
		row := &rows[index]
		_, err = client.Call(ctx, "torrent-rename-path", map[string]any{"ids": []int{plan.ID}, "path": row.Path, "name": row.Target})
		if err != nil {
			row.Status = "failed"
			row.Message = err.Error()
			break
		}
		row.Status = "done"
		row.Message = "已重命名"
	}
	return rows, nil
}
