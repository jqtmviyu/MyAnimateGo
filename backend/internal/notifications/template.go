package notifications

import (
	"context"

	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var statusWords = map[string][2]string{
	"DOWNLOAD_START": {"🎈", "开始下载"}, "DOWNLOAD_END": {"🎉", "下载完成"},
	"OMIT": {"⛔", "缺少集数"}, "ERROR": {"❌", "发生错误"},
	"COMPLETED": {"🎊", "订阅完结"},
}
var episodePattern = regexp.MustCompile(`(?i)S\d+E(\d+(?:\.5)?)`)

func (s *Service) render(ctx context.Context, global, channel, subscription map[string]any, text, status string) (string, error) {
	template := stringValue(channel["notificationTemplate"])
	if template == "" {
		template = "${notification}"
	}
	comment := stringValue(channel["comment"])
	if comment == "" {
		comment = "无备注"
	}
	template = strings.ReplaceAll(template, "${comment}", comment)
	result, err := s.renderFields(ctx, global, subscription, template, text, status)
	if err != nil {
		return "", err
	}
	if strings.Contains(result, "${notification}") {
		outer := stringValue(global["notificationTemplate"])
		outer, err = s.renderFields(ctx, global, subscription, outer, text, status)
		if err != nil {
			return "", err
		}
		result = strings.ReplaceAll(result, "${notification}", outer)
	}
	return strings.TrimSpace(result), nil
}

func (s *Service) renderFields(ctx context.Context, global, subscription map[string]any, template, text, status string) (string, error) {
	season := intValue(subscription["season"])
	episode := 1.0
	if match := episodePattern.FindStringSubmatch(text); len(match) > 1 {
		episode, _ = strconv.ParseFloat(match[1], 64)
	}
	episodeFormat := fmt.Sprintf("%02d", int(episode))
	if episode-float64(int(episode)) != 0 {
		episodeFormat += ".5"
	}
	releaseDate := parseReleaseDate(subscription["releaseDate"])
	tmdb, _ := subscription["tmdb"].(map[string]any)
	tmdbID := stringValue(tmdb["id"])
	tmdbURL := ""
	if tmdbID != "" {
		kind := "tv"
		if truth(subscription["ova"]) {
			kind = "movie"
		}
		tmdbURL = "https://www.themoviedb.org/" + kind + "/" + tmdbID
	}
	downloadPath := ""
	if strings.Contains(template, "${downloadPath}") && s.options.DownloadPath != nil {
		var err error
		downloadPath, err = s.options.DownloadPath(ctx, subscription)
		if err != nil {
			return "", err
		}
	}
	jpTitle := stringValue(subscription["jpTitle"])
	if strings.Contains(template, "${jpTitle}") && jpTitle == "" && s.options.Metadata != nil {
		metadata, err := s.options.Metadata(ctx, subscription)
		if err != nil {
			return "", err
		}
		jpTitle = metadata["jpTitle"]
	}
	words := statusWords[strings.ToUpper(status)]
	values := map[string]string{
		"${text}": text, "${title}": stringValue(subscription["title"]), "${subgroup}": stringValue(subscription["subgroup"]),
		"${score}": fmt.Sprint(subscription["score"]), "${season}": strconv.Itoa(season), "${seasonFormat}": fmt.Sprintf("%02d", season),
		"${currentEpisodeNumber}": fmt.Sprint(subscription["currentEpisodeNumber"]), "${totalEpisodeNumber}": fmt.Sprint(subscription["totalEpisodeNumber"]),
		"${themoviedbName}": stringValue(subscription["themoviedbName"]), "${bgmUrl}": stringValue(subscription["bgmUrl"]),
		"${episode}": strconv.FormatFloat(episode, 'f', -1, 64), "${episodeFormat}": episodeFormat,
		"${year}": strconv.Itoa(releaseDate.Year()), "${month}": strconv.Itoa(int(releaseDate.Month())), "${date}": strconv.Itoa(releaseDate.Day()),
		"${tmdbid}": tmdbID, "${tmdburl}": tmdbURL, "${emoji}": words[0], "${action}": words[1], "${downloadPath}": downloadPath,
		"${jpTitle}": jpTitle,
	}
	for _, field := range []string{"score", "currentEpisodeNumber", "totalEpisodeNumber"} {
		if value, exists := subscription[field]; exists && value != nil {
			values["${"+field+"}"] = fmt.Sprint(value)
		} else {
			delete(values, "${"+field+"}")
		}
	}
	episodeTitleFields := []string{"episodeTitle", "bgmEpisodeTitle", "bgmJpEpisodeTitle"}
	needsEpisodeTitles := false
	for _, field := range episodeTitleFields {
		if strings.Contains(template, "${"+field+"}") {
			needsEpisodeTitles = true
		}
	}
	if needsEpisodeTitles {
		defaultTitle := "第" + strconv.FormatFloat(episode, 'f', -1, 64) + "集"
		values["${episodeTitle}"] = defaultTitle
		values["${bgmEpisodeTitle}"] = defaultTitle
		values["${bgmJpEpisodeTitle}"] = defaultTitle
		if episode == float64(int(episode)) && s.options.EpisodeTitles != nil {
			titles, err := s.options.EpisodeTitles(ctx, subscription, int(episode))
			if err != nil {
				return "", err
			}
			for field, title := range titles {
				values["${"+field+"}"] = title
			}
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		template = strings.ReplaceAll(template, key, values[key])
	}
	return template, nil
}

func parseReleaseDate(value any) time.Time {
	switch date := value.(type) {
	case string:
		for _, layout := range []string{time.RFC3339, "2006-01-02", "2006/01/02"} {
			if parsed, err := time.Parse(layout, date); err == nil {
				return parsed
			}
		}
	case float64:
		return time.UnixMilli(int64(date))
	}
	return time.Time{}
}
