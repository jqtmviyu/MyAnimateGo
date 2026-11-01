package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/rules"
	"github.com/mozillazg/go-pinyin"
)

var bgmSubjectID = regexp.MustCompile(`/subject/(\d+)`)
var polyphonicNames = map[string][]string{
	"重庆": {"chong", "qing"}, "重启": {"chong", "qi"}, "重回": {"chong", "hui"},
	"重生": {"chong", "sheng"}, "重来": {"chong", "lai"}, "调教": {"tiao", "jiao"},
}

// DownloadPath 展开下载路径模板并报告路径是否发生变化。
func (e *Engine) DownloadPath(ctx context.Context, subscription map[string]any) (map[string]any, error) {
	config, err := e.config(ctx)
	if err != nil {
		return nil, err
	}
	downloadPath, err := e.downloadPath(ctx, config, subscription)
	if err != nil {
		return nil, err
	}
	change := false
	if id := valueString(subscription["id"]); id != "" {
		items, err := e.store.ListSubscriptions(ctx)
		if err != nil {
			return nil, err
		}
		for _, old := range items {
			if valueString(old["id"]) != id {
				continue
			}
			old["season"] = subscription["season"]
			oldPath, err := e.downloadPath(ctx, config, old)
			if err != nil {
				return nil, err
			}
			change = downloadPath != oldPath
			break
		}
	}
	return map[string]any{"change": change, "downloadPath": downloadPath}, nil
}

// Preview 返回下载路径、RSS 资源和缺集列表，不向 Transmission 写入任务。
func (e *Engine) Preview(ctx context.Context, subscription map[string]any) (map[string]any, error) {
	config, err := e.config(ctx)
	if err != nil {
		return nil, err
	}
	items, err := e.items(ctx, config, subscription)
	if err != nil {
		return nil, err
	}
	downloadPath, err := e.downloadPath(ctx, config, subscription)
	if err != nil {
		return nil, err
	}
	for i := range items {
		cached, err := findCachedTorrent(e.cacheDir(), items[i].InfoHash)
		if err != nil {
			return nil, err
		}
		items[i].HasDownloaded = cached != ""
		if cached != "" {
			items[i].TorrentPath = cached
			continue
		}
		if e.localEpisodeExists(config, subscription, items[i], downloadPath) {
			items[i].HasDownloaded = true
		}
	}
	return map[string]any{
		"downloadPath": downloadPath,
		"items":        items,
		"omitList":     omitList(config, subscription, items),
	}, nil
}

func (e *Engine) downloadPath(ctx context.Context, config, subscription map[string]any) (string, error) {
	template := valueString(config["downloadPathTemplate"])
	if valueBool(subscription["ova"]) && valueString(config["ovaDownloadPathTemplate"]) != "" {
		template = valueString(config["ovaDownloadPathTemplate"])
	}
	if valueBool(subscription["customDownloadPath"]) {
		if custom := firstLine(valueString(subscription["customDownloadPathTemplate"])); custom != "" {
			template = custom
		}
	}
	if strings.TrimSpace(template) == "" {
		return "", errors.New("下载路径模板不能为空")
	}
	jpTitle := valueString(subscription["jpTitle"])
	if strings.Contains(template, "${jpTitle}") && jpTitle == "" && e.options.MetadataResolver != nil {
		metadata, err := e.options.MetadataResolver(ctx, subscription, 0)
		if err != nil {
			return "", fmt.Errorf("读取下载路径元数据失败：%w", err)
		}
		jpTitle = metadata["jpTitle"]
	}
	date := parseSubscriptionDate(subscription, e.now())
	tmdbDate := date
	if tmdb, ok := subscription["tmdb"].(map[string]any); ok {
		if parsed, ok := parseDateValue(tmdb["date"]); ok {
			tmdbDate = parsed
		}
	}
	year, month := date.Year(), int(date.Month())
	quarter, quarterName := 0, ""
	if strings.Contains(template, "${quarter}") || strings.Contains(template, "${quarterFormat}") || strings.Contains(template, "${quarterName}") {
		switch month {
		case 12, 1, 2:
			quarter, quarterName = 1, "冬"
			if month == 12 {
				year++
			}
		case 3, 4, 5:
			quarter, quarterName = 4, "春"
		case 6, 7, 8:
			quarter, quarterName = 7, "夏"
		default:
			quarter, quarterName = 10, "秋"
		}
	}
	season := valueInt(subscription["season"])
	title := strings.TrimSpace(valueString(subscription["title"]))
	fullPinyin, _ := pinyinTitle(title)
	bgmID := ""
	if matches := bgmSubjectID.FindStringSubmatch(valueString(subscription["bgmUrl"])); len(matches) > 1 {
		bgmID = matches[1]
	}
	tmdbID := ""
	if tmdb, ok := subscription["tmdb"].(map[string]any); ok {
		tmdbID = valueString(tmdb["id"])
	}
	// 1.x 的季度按自然季度合并，与新版冬季跨年的规则不同。
	legacyQuarter := ((month-1)/3)*3 + 1
	template = strings.ReplaceAll(template, "${legacyQuarterFormat}", fmt.Sprintf("%02d", legacyQuarter))

	replacements := map[string]string{
		"${quarterFormat}": fmt.Sprintf("%02d", quarter), "${quarterName}": quarterName,
		"${quarter}": strconv.Itoa(quarter), "${tmdbYear}": strconv.Itoa(tmdbDate.Year()),
		"${monthFormat}": fmt.Sprintf("%02d", month), "${year}": strconv.Itoa(year),
		"${month}": strconv.Itoa(month), "${seasonFormat}": fmt.Sprintf("%02d", season),
		"${season}": strconv.Itoa(season), "${letter}": firstLetter(fullPinyin),
		"${bgmId}": bgmID, "${title}": title, "${themoviedbName}": valueString(subscription["themoviedbName"]),
		"${subgroup}": valueString(subscription["subgroup"]), "${tmdbid}": tmdbID,
		"${jpTitle}": jpTitle,
	}
	for _, key := range []string{"${quarterFormat}", "${quarterName}", "${quarter}", "${tmdbYear}", "${monthFormat}", "${year}", "${month}", "${seasonFormat}", "${season}", "${letter}", "${bgmId}", "${title}", "${themoviedbName}", "${subgroup}", "${tmdbid}", "${jpTitle}"} {
		template = strings.ReplaceAll(template, key, replacements[key])
	}
	absolute, err := filepath.Abs(template)
	if err != nil {
		return "", fmt.Errorf("解析下载路径失败：%w", err)
	}
	return filepath.Clean(absolute), nil
}

func renameInput(config, subscription map[string]any, offset int) rules.RenameInput {
	template := valueString(config["renameTemplate"])
	if valueBool(subscription["customRenameTemplateEnable"]) && valueString(subscription["customRenameTemplate"]) != "" {
		template = valueString(subscription["customRenameTemplate"])
	}
	input := rules.RenameInput{
		Title: valueString(subscription["title"]), Season: valueInt(subscription["season"]), Offset: offset,
		OVA: valueBool(subscription["ova"]), CustomEpisode: valueBool(subscription["customEpisode"]),
		SkipHalfEpisodes: valueBool(config["skip5"]), Template: template,
		Metadata:          map[string]string{"tmdbid": "", "themoviedbName": valueString(subscription["themoviedbName"])},
		MaxFileNameLength: valueInt(config["maxFileNameLength"]),
	}
	if value, exists := config["renameDelYear"]; exists {
		flag := valueBool(value)
		input.RenameDelYear = &flag
	}
	if value, exists := config["renameDelTmdbId"]; exists {
		flag := valueBool(value)
		input.RenameDelTmdbId = &flag
	}
	if custom := valueString(subscription["customEpisodeStr"]); custom != "" {
		input.CustomEpisodeRegex = &custom
	}
	if group, ok := subscription["customEpisodeGroupIndex"]; ok {
		value := valueInt(group)
		input.CustomEpisodeGroup = &value
	}
	if tmdb, ok := subscription["tmdb"].(map[string]any); ok {
		input.Metadata["tmdbid"] = valueString(tmdb["id"])
	}
	if id := bgmSubjectID.FindStringSubmatch(valueString(subscription["bgmUrl"])); len(id) > 1 {
		input.Metadata["bgmId"] = id[1]
	}
	if jp := valueString(subscription["jpTitle"]); jp != "" {
		input.Metadata["jpTitle"] = jp
	}
	return input
}

func parseSubscriptionDate(subscription map[string]any, now time.Time) time.Time {
	if parsed, ok := parseDateValue(subscription["releaseDate"]); ok {
		return parsed
	}
	year, month := valueInt(subscription["year"]), valueInt(subscription["month"])
	if year > 0 && month > 0 && month <= 12 {
		day := valueInt(subscription["date"])
		if day < 1 || day > 31 {
			day = 1
		}
		return time.Date(year, time.Month(month), day, 0, 0, 0, 0, now.Location())
	}
	return now
}

func parseDateValue(value any) (time.Time, bool) {
	text := valueString(value)
	if text == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", time.RFC1123Z, time.RFC1123, "2006-01-02", "2006-01", "2006"} {
		if parsed, err := time.ParseInLocation(layout, text, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func pinyinTitle(title string) (string, string) {
	args := pinyin.NewArgs()
	args.Fallback = func(r rune, _ pinyin.Args) []string { return []string{string(r)} }
	var full, initials strings.Builder
	runes := []rune(title)
	for i := 0; i < len(runes); i++ {
		var words []string
		if i+1 < len(runes) {
			words = polyphonicNames[string(runes[i:i+2])]
			if len(words) > 0 {
				i++
			}
		}
		if len(words) == 0 {
			words = pinyin.SinglePinyin(runes[i], args)
		}
		if len(words) == 0 || words[0] == "" {
			continue
		}
		for _, value := range words {
			word := strings.ToLower(value)
			full.WriteString(word)
			initials.WriteRune([]rune(word)[0])
		}
	}
	return full.String(), initials.String()
}

func firstLetter(value string) string {
	if value == "" {
		return "0"
	}
	first := []rune(value)[0]
	if first >= 'a' && first <= 'z' {
		return strings.ToUpper(string(first))
	}
	if first >= 'A' && first <= 'Z' {
		return string(first)
	}
	if first >= '0' && first <= '9' {
		return "0"
	}
	return "#"
}

func firstLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func (e *Engine) localEpisodeExists(config, subscription map[string]any, item Item, downloadPath string) bool {
	if !valueBool(config["rename"]) || !valueBool(config["fileExist"]) || valueString(config["downloadPathTemplate"]) == "" {
		return false
	}
	entries, err := os.ReadDir(downloadPath)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || !videoFile(entry.Name()) {
			continue
		}
		if valueBool(subscription["ova"]) || fileMatchesEpisode(entry.Name(), valueInt(subscription["season"]), item.Episode) {
			return true
		}
	}
	return false
}

func fileMatchesEpisode(name string, season int, episode float64) bool {
	match := seasonEpisodePattern.FindStringSubmatch(name)
	if len(match) != 3 {
		return false
	}
	parsedSeason, err1 := strconv.Atoi(match[1])
	parsedEpisode, err2 := strconv.ParseFloat(match[2], 64)
	return err1 == nil && err2 == nil && parsedSeason == season && parsedEpisode == episode
}

func videoFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mkv", ".mp4", ".avi", ".wmv":
		return true
	default:
		return false
	}
}

func omitList(config, subscription map[string]any, items []Item) []int {
	if !valueBool(config["omit"]) || !valueBool(subscription["omit"]) || valueBool(subscription["ova"]) || len(items) == 0 {
		return []int{}
	}
	seen, min, max := map[int]bool{}, int(^uint(0)>>1), -int(^uint(0)>>1)-1
	for _, item := range items {
		number := int(item.Episode)
		seen[number] = true
		if number < min {
			min = number
		}
		if number > max {
			max = number
		}
	}
	missing := []int{}
	for episode := min; episode <= max; episode++ {
		if !seen[episode] {
			missing = append(missing, episode)
			if len(missing) > 50 {
				break
			}
		}
	}
	return missing
}
