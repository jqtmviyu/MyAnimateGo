// Package collections provides Transmission-backed multi-file torrent selection.
package collections

import (
	"encoding/base64"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/rules"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/torrent"
)

type File struct {
	ID         int      `json:"-"`
	Name       string   `json:"title"`
	Length     int64    `json:"length"`
	FormatSize string   `json:"formatSize"`
	Episode    *float64 `json:"episode,omitempty"`
	ReName     string   `json:"reName,omitempty"`
	Subgroup   string   `json:"subgroup"`
}

type PreviewResult struct {
	Hash  string `json:"hash"`
	Name  string `json:"name"`
	Files []File `json:"files"`
}

// Preview parses metainfo and applies the subscription's episode and filename rules.
func Preview(encoded string, subscription, config map[string]any) (PreviewResult, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return PreviewResult{}, fmt.Errorf("合集种子不是有效 Base64：%w", err)
	}
	metadata, err := torrent.Parse(data)
	if err != nil {
		return PreviewResult{}, fmt.Errorf("解析合集种子失败：%w", err)
	}
	ani := mapString(subscription, "title")
	group := mapString(subscription, "subgroup")
	input := rules.RenameInput{
		Title: ani, Season: mapInt(subscription, "season"), Offset: mapInt(subscription, "offset"),
		OVA: mapBool(subscription, "ova"), CustomEpisode: mapBool(subscription, "customEpisode"),
		SkipHalfEpisodes: mapBool(config, "skip5"), Template: mapString(config, "renameTemplate"),
		Metadata:          map[string]string{"tmdbid": "", "themoviedbName": mapString(subscription, "themoviedbName")},
		MaxFileNameLength: mapInt(config, "maxFileNameLength"),
	}
	if custom, ok := subscription["customEpisodeStr"].(string); ok {
		input.CustomEpisodeRegex = &custom
	}
	if subscription["customEpisodeGroupIndex"] != nil {
		groupIndex := mapInt(subscription, "customEpisodeGroupIndex")
		input.CustomEpisodeGroup = &groupIndex
	}
	if mapBool(subscription, "customRenameTemplateEnable") && mapString(subscription, "customRenameTemplate") != "" {
		input.Template = mapString(subscription, "customRenameTemplate")
	}
	match, err := expressions(subscription["match"], group)
	if err != nil {
		return PreviewResult{}, err
	}
	exclude, err := expressions(subscription["exclude"], group)
	if err != nil {
		return PreviewResult{}, err
	}
	globalExclude, err := expressions(config["exclude"], group)
	if err != nil {
		return PreviewResult{}, err
	}
	files := make([]File, 0, len(metadata.Files))
	for _, source := range metadata.Files {
		name := strings.TrimSuffix(strings.ReplaceAll(source.Name, `\`, "/"), "/")
		if strings.HasPrefix(name, "_____padding_file_") && strings.Contains(name, "BitComet") {
			continue
		}
		if matchesAny(exclude, name) || !matchesAll(match, name) || (mapBool(subscription, "globalExclude") && matchesAny(globalExclude, name)) {
			continue
		}
		file := File{ID: source.Index, Name: name, Length: source.Length, FormatSize: formatSize(source.Length), Subgroup: group}
		renamed, ok, err := rules.Rename(input, rules.Item{Title: path.Base(name), Subgroup: group})
		if err != nil {
			return PreviewResult{}, err
		}
		if !ok {
			continue
		}
		ext := path.Ext(name)
		newName := renamed.Name
		if subtitle := strings.ToLower(ext); subtitle == ".ass" || subtitle == ".srt" || subtitle == ".ssa" || subtitle == ".vtt" {
			stemExt := path.Ext(strings.TrimSuffix(name, ext))
			if stemExt != "" {
				newName += stemExt
			}
		}
		if ext != "" {
			newName += ext
		}
		file.ReName, file.Episode = newName, renamed.Episode
		files = append(files, file)
	}
	sort.SliceStable(files, func(i, j int) bool {
		left, right := 0.0, 0.0
		if files[i].Episode != nil {
			left = *files[i].Episode
		}
		if files[j].Episode != nil {
			right = *files[j].Episode
		}
		return left < right
	})
	return PreviewResult{Hash: metadata.TransmissionHash, Name: metadata.Name, Files: files}, nil
}

func expressions(raw any, subgroup string) ([]*regexp.Regexp, error) {
	var values []string
	switch items := raw.(type) {
	case []string:
		values = items
	case []any:
		for _, item := range items {
			if value, ok := item.(string); ok {
				values = append(values, value)
			}
		}
	}
	result := make([]*regexp.Regexp, 0, len(values))
	for _, expression := range values {
		if strings.TrimSpace(expression) == "" {
			continue
		}
		if found := subgroupExpression.FindStringSubmatch(expression); len(found) > 0 {
			if found[1] != subgroup {
				continue
			}
			expression = found[2]
		}
		compiled, err := regexp.Compile(expression)
		if err != nil {
			return nil, fmt.Errorf("合集筛选表达式 %q 无效：%w", expression, err)
		}
		result = append(result, compiled)
	}
	return result, nil
}

var subgroupExpression = regexp.MustCompile(`^\[([^\]]+)](.*)$`)

func matchesAny(expressions []*regexp.Regexp, text string) bool {
	for _, expression := range expressions {
		if expression.MatchString(text) {
			return true
		}
	}
	return false
}
func matchesAll(expressions []*regexp.Regexp, text string) bool {
	for _, expression := range expressions {
		if !expression.MatchString(text) {
			return false
		}
	}
	return true
}
func formatSize(size int64) string {
	const unit = int64(1024)
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value, suffix := float64(size), "B"
	for _, next := range []string{"KB", "MB", "GB", "TB"} {
		value /= float64(unit)
		suffix = next
		if value < float64(unit) {
			break
		}
	}
	return fmt.Sprintf("%.2f %s", value, suffix)
}
func mapString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}
func mapBool(values map[string]any, key string) bool { value, _ := values[key].(bool); return value }
func mapInt(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}
