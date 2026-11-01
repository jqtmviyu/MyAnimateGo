// Package rules 提供流水线共用的命名与集数规则。
package rules

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Item 保存规则处理所需的 RSS 条目字段。
type Item struct {
	Title    string
	Subgroup string
	Name     string
	Episode  *float64
	Master   bool
}

// RenameInput 保存解析集数和生成文件名所需的订阅设置。
type RenameInput struct {
	Title              string
	Season             int
	Offset             int
	OVA                bool
	CustomEpisode      bool
	CustomEpisodeRegex *string
	CustomEpisodeGroup *int
	SkipHalfEpisodes   bool
	Template           string
	// Metadata 的键可使用 tmdbid 等占位符名，也可直接使用 ${tmdbid}。
	Metadata          map[string]string
	RenameDelYear     *bool
	RenameDelTmdbId   *bool
	MaxFileNameLength int
}

var defaultEpisodePattern = regexp.MustCompile(`(.*|\[.*])((?: - |Vol |[Ee][Pp]?)\d+(\.5)?( ?\(\d+\))?|【\d+(\.5)?】|\[\d+(\.5)?( ?\(\d+\))?( ?[vV]\d)?( ?END)?( ?完)?( ?FIN)?]|第\d+(\.5)?[话話集]( - END)?|^\[TOC].* \d+|^六四位元字幕组.*★\d+(\.5)?★)`)
var numberPattern = regexp.MustCompile(`\d+(\.5)?`)
var trailingHashPattern = regexp.MustCompile(`\[([A-Z]|\d){8}]$`)
var tmdbIDPattern = regexp.MustCompile(` ?\[tmdbid=\d+]`)
var yearPattern = regexp.MustCompile(` ?\((19|20)\d{2}\)`)
var resolutionPattern = regexp.MustCompile(`(720|1080|2160)[Pp]`)

var metadataTemplatePlaceholders = []string{
	"${tmdbid}", "${episodeTitle}", "${bgmEpisodeTitle}", "${bgmJpEpisodeTitle}",
	"${bgmId}", "${jpTitle}", "${themoviedbName}",
}

// Rename 按固定的 Java 规则处理条目。元数据占位符由调用方提供，缺少所需值时返回错误。
func Rename(input RenameInput, item Item) (Item, bool, error) {
	if input.OVA {
		item.Name = renameDel(input.Title, input)
		return item, true, nil
	}
	title := strings.TrimSpace(strings.ReplaceAll(item.Title, "+NCOPED", ""))
	title = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(title, "\n", " "), "\t", " "))
	title = strings.TrimSpace(trailingHashPattern.ReplaceAllString(title, ""))
	group := 2
	pattern := defaultEpisodePattern
	if input.CustomEpisode {
		if input.CustomEpisodeRegex == nil || input.CustomEpisodeGroup == nil {
			return item, false, fmt.Errorf("自定义集数规则需要正则表达式和捕获组编号")
		}
		compiled, err := regexp.Compile(*input.CustomEpisodeRegex)
		if err != nil {
			return item, false, fmt.Errorf("自定义集数正则表达式不受 Go regexp 支持：%w", err)
		}
		pattern = compiled
		group = *input.CustomEpisodeGroup
		if group < 0 || group > pattern.NumSubexp() {
			return item, false, fmt.Errorf("自定义集数捕获组编号 %d 超出有效范围 0..%d", group, pattern.NumSubexp())
		}
	}
	match := pattern.FindStringSubmatch(title)
	if match == nil || group >= len(match) || strings.TrimSpace(match[group]) == "" {
		return item, false, nil
	}
	episodeText := numberPattern.FindString(match[group])
	if episodeText == "" {
		return item, false, nil
	}
	episode, err := strconv.ParseFloat(episodeText, 64)
	if err != nil {
		return item, false, fmt.Errorf("解析集数 %q 失败：%w", episodeText, err)
	}
	episode += float64(input.Offset)
	item.Episode = &episode
	integer := int(episode)
	isHalf := episode != float64(integer)
	if input.SkipHalfEpisodes && isHalf {
		return item, false, nil
	}
	episodeSuffix := strconv.Itoa(integer)
	if isHalf {
		episodeSuffix += ".5"
	}
	episodeFormat := fmt.Sprintf("%02d", integer)
	if isHalf {
		episodeFormat += ".5"
	}
	subgroup := strings.TrimSpace(item.Subgroup)
	if subgroup == "" {
		subgroup = "未知字幕组"
	}
	cleanItemTitle := SanitizeFilename(title)
	resolutionTitle := strings.NewReplacer("1920x1080", "1080p", "3840x2160", "2160p", "1280x720", "720p").Replace(cleanItemTitle)
	resolution := "none"
	if found := resolutionPattern.FindString(resolutionTitle); found != "" {
		resolution = strings.ToLower(found)
	}
	template := input.Template
	if strings.TrimSpace(template) == "" {
		template = "${title} S${seasonFormat}E${episodeFormat}"
	}
	metadataReplacements := make(map[string]string, len(metadataTemplatePlaceholders))
	for _, placeholder := range metadataTemplatePlaceholders {
		if strings.Contains(template, placeholder) {
			key := strings.TrimSuffix(strings.TrimPrefix(placeholder, "${"), "}")
			value, ok := input.Metadata[key]
			if !ok {
				value, ok = input.Metadata[placeholder]
			}
			if !ok {
				return item, false, fmt.Errorf("模板占位符 %s 缺少调用方提供的元数据", placeholder)
			}
			metadataReplacements[placeholder] = value
		}
	}
	replacements := map[string]string{
		"${seasonFormat}": fmt.Sprintf("%02d", input.Season), "${episodeFormat}": episodeFormat,
		"${season}": strconv.Itoa(input.Season), "${episode}": episodeSuffix,
		"${subgroup}": subgroup, "${itemTitle}": cleanItemTitle,
		"${resolution}": resolution, "${title}": input.Title,
	}
	for key, value := range metadataReplacements {
		replacements[key] = value
	}
	rename := SanitizeFilename(renameDel(replaceTemplate(template, replacements), input))
	if input.MaxFileNameLength > 0 {
		rename = truncateRunes(rename, input.MaxFileNameLength)
	}
	item.Name = rename
	return item, true, nil
}

func replaceTemplate(template string, replacements map[string]string) string {
	keys := []string{"${seasonFormat}", "${episodeFormat}", "${season}", "${episode}", "${subgroup}", "${itemTitle}", "${resolution}", "${tmdbid}", "${title}", "${episodeTitle}", "${bgmEpisodeTitle}", "${bgmJpEpisodeTitle}", "${bgmId}", "${jpTitle}", "${themoviedbName}"}
	for _, key := range keys {
		template = strings.ReplaceAll(template, key, replacements[key])
	}
	return template
}

// SanitizeFilename 按 Java 规则转换文件名中的特殊字符并压缩连续空格。
func SanitizeFilename(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "1/2", "½")
	value = strings.NewReplacer("/", " ", `\`, " ", ":", "：", "?", "？", "|", "｜", "*", " ", "<", " ", ">", " ", `"`, " ").Replace(value)
	for strings.Contains(value, "  ") {
		value = strings.ReplaceAll(value, "  ", " ")
	}
	return strings.TrimSpace(value)
}

func renameDel(title string, input RenameInput) string {
	removeID, removeYear := true, true
	if input.RenameDelTmdbId != nil {
		removeID = *input.RenameDelTmdbId
	}
	if input.RenameDelYear != nil {
		removeYear = *input.RenameDelYear
	}
	if removeID {
		title = strings.TrimSpace(tmdbIDPattern.ReplaceAllString(title, ""))
	}
	if removeYear {
		title = strings.TrimSpace(yearPattern.ReplaceAllString(title, ""))
	}
	return title
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max])
	}
	return value
}

// ProgressOptions 控制进度按最大集数或可用条目数计算。
type ProgressOptions struct {
	Episodes    []float64
	DownloadNew bool
	StandbyRSS  bool
	Coexist     bool
	MasterOnly  bool
	MasterFlags []bool
}

// CurrentEpisodeNumber 排除半集并计算当前进度。
func CurrentEpisodeNumber(options ProgressOptions) int {
	count, max := 0, 0
	for i, episode := range options.Episodes {
		if options.StandbyRSS && options.Coexist && i < len(options.MasterFlags) && !options.MasterFlags[i] {
			continue
		}
		if options.MasterOnly && i < len(options.MasterFlags) && !options.MasterFlags[i] {
			continue
		}
		integer := int(episode)
		if episode != float64(integer) {
			continue
		}
		count++
		if integer > max {
			max = integer
		}
	}
	if options.DownloadNew {
		return max
	}
	return count
}

// MissingOptions 配置缺集扫描。
type MissingOptions struct {
	Episodes            []float64
	Enabled             bool
	SubscriptionEnabled bool
	OVA                 bool
}

// MissingEpisodes 返回已知最小和最大集数之间缺失的整数集数。
func MissingEpisodes(options MissingOptions) []int {
	if !options.Enabled || !options.SubscriptionEnabled || options.OVA || len(options.Episodes) == 0 {
		return nil
	}
	seen, min, max := map[int]bool{}, math.MaxInt, math.MinInt
	for _, episode := range options.Episodes {
		n := int(episode)
		seen[n] = true
		if n < min {
			min = n
		}
		if n > max {
			max = n
		}
	}
	if min == max {
		return nil
	}
	missing := make([]int, 0)
	for episode := min; episode <= max; episode++ {
		if !seen[episode] {
			missing = append(missing, episode)
			if len(missing) > 50 {
				return missing
			}
		}
	}
	return missing
}

// MergeSources 按来源顺序合并条目，并保留每个 Java 去重键的首条记录。
func MergeSources(sources [][]Item, coexist bool) []Item {
	seen := make(map[string]bool)
	merged := make([]Item, 0)
	for _, source := range sources {
		for _, item := range source {
			key := ""
			if coexist {
				key = item.Name
			} else if item.Episode != nil {
				key = strconv.FormatFloat(*item.Episode, 'f', -1, 64)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, item)
		}
	}
	return merged
}
