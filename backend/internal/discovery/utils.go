package discovery

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var groupPatterns = []string{
	`1920[Xx]1080`, `3840[Xx]2160`, `1080[Pp]`, `720[Pp]`, `4[Kk]`, `繁`, `简`, `日`, `内嵌`, `内封`, `外挂`,
	`cht|Cht|CHT`, `chs|Chs|CHS`, `avc|Avc|AVC`, `hevc|Hevc|HEVC`, `h264|H264`, `h265|H265`, `10bit|10Bit|10BIT`, `mp4|MP4`, `mkv|MKV`,
}

func deriveGroupRegex(titles []string) groupRegex {
	uniqueTitles := make([]string, 0, len(titles))
	seenTitles := map[string]bool{}
	for _, title := range titles {
		if !seenTitles[title] {
			seenTitles[title] = true
			uniqueTitles = append(uniqueTitles, title)
		}
	}
	tags := make([]string, 0, 5)
	all := make([][]regexItem, 0)
	for _, title := range uniqueTitles {
		matches := make([]regexItem, 0)
		for _, pattern := range groupPatterns {
			re := regexp.MustCompile(pattern)
			label := re.FindString(title)
			if label == "" {
				continue
			}
			matches = append(matches, regexItem{Regex: pattern, Label: label})
			if len(tags) < 5 && !containsString(tags, label) {
				tags = append(tags, label)
			}
		}
		if len(matches) > 0 {
			all = append(all, matches)
		}
	}
	seen := map[string]bool{}
	distinct := make([][]regexItem, 0, len(all))
	for _, row := range all {
		key := fmt.Sprint(row)
		if !seen[key] {
			seen[key] = true
			distinct = append(distinct, row)
		}
	}
	return groupRegex{RegexList: distinct, Tags: tags}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func subjectIDFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	id := parts[len(parts)-1]
	if _, err := strconv.Atoi(id); err != nil {
		return ""
	}
	return id
}

func parseTimeValue(value any) *javaDate {
	if value == nil {
		return nil
	}
	if text, ok := value.(string); ok {
		return parseDate(text)
	}
	if number, ok := value.(float64); ok {
		if number > 1e12 {
			return &javaDate{Time: time.UnixMilli(int64(number)).In(time.Local)}
		}
		if number > 1e9 {
			return &javaDate{Time: time.Unix(int64(number), 0).In(time.Local)}
		}
	}
	return nil
}

func dayIndex(date time.Time) int { return int(date.Weekday()) }
func weekLabel(index int) string {
	labels := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	if index < 0 || index >= len(labels) {
		return ""
	}
	return labels[index]
}

func sortWeeks[T any](values []T, label func(T) string) {
	order := map[string]int{"星期日": 0, "星期一": 1, "星期二": 2, "星期三": 3, "星期四": 4, "星期五": 5, "星期六": 6, "搜索": 7, "Search": 7}
	sort.SliceStable(values, func(i, j int) bool { return order[label(values[i])] < order[label(values[j])] })
}
