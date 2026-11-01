package discovery

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mozillazg/go-pinyin"
)

//go:embed testdata/ani-defaults.json
var aniDefaults []byte

//go:embed testdata/cover.png
var defaultCover []byte

func bgmBase(config map[string]any) string { return strings.TrimRight(text(config["bgmApi"]), "/") }

func (c *Client) searchBgm(ctx context.Context, name string) ([]bgmInfo, error) {
	if strings.TrimSpace(name) == "" {
		return []bgmInfo{}, nil
	}
	config, err := c.config(ctx)
	if err != nil {
		return nil, err
	}
	name = strings.ReplaceAll(name, "1/2", "½")
	base := bgmBase(config)
	if base == "" {
		return nil, errors.New("Bangumi API 地址未配置")
	}
	query := url.Values{}
	query.Set("type", "2")
	query.Set("max_results", "25")
	query.Set("responseGroup", "small")
	endpoint := base + "/search/subject/" + url.PathEscape(name) + "?" + query.Encode()
	status, data, err := c.requestText(ctx, config, http.MethodGet, endpoint, c.bgmHeaders(config), nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return []bgmInfo{}, nil
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("Bangumi 搜索返回 HTTP %d：%s", status, strings.TrimSpace(string(data)))
	}
	var wrapper struct {
		Code *int      `json:"code"`
		List []bgmInfo `json:"list"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("解析 Bangumi 搜索结果失败：%w", err)
	}
	if wrapper.Code != nil && *wrapper.Code == 404 {
		return []bgmInfo{}, nil
	}
	for i := range wrapper.List {
		wrapper.List[i].Season = bgmSeason(wrapper.List[i])
	}
	if wrapper.List == nil {
		wrapper.List = []bgmInfo{}
	}
	return wrapper.List, nil
}

func (c *Client) bgmSubject(ctx context.Context, config map[string]any, id string, useCache bool) (bgmInfo, error) {
	if !useCache {
		return c.fetchBgmSubject(ctx, config, id, false)
	}
	raw, err := c.sourceBytes(ctx, sourceKey(config, "subject:"+id), 5*time.Minute, func(work context.Context) ([]byte, error) {
		info, err := c.fetchBgmSubject(work, config, id, true)
		if err != nil {
			return nil, err
		}
		return json.Marshal(info)
	})
	if err != nil {
		return bgmInfo{}, err
	}
	var info bgmInfo
	err = json.Unmarshal(raw, &info)
	return info, err
}

func (c *Client) fetchBgmSubject(ctx context.Context, config map[string]any, id string, useCache bool) (bgmInfo, error) {
	if id == "" {
		return bgmInfo{}, errors.New("Bangumi subject ID 不能为空")
	}
	fetchDirect := func() (bgmInfo, error) {
		base := bgmBase(config)
		if base == "" {
			return bgmInfo{}, errors.New("Bangumi API 地址未配置")
		}
		var result bgmInfo
		_, err := c.request(ctx, config, http.MethodGet, base+"/v0/subjects/"+url.PathEscape(id), c.bgmHeaders(config), nil, &result)
		if err == nil {
			return normalizeBgmInfo(result), nil
		}
		return bgmInfo{}, err
	}
	if !useCache {
		return fetchDirect()
	}
	type outcome struct {
		info   bgmInfo
		err    error
		cached bool
	}
	results := make(chan outcome, 2)
	go func() { info, err := fetchDirect(); results <- outcome{info: info, err: err} }()
	go func() {
		var info bgmInfo
		cacheURL := c.get(ctx, config, "cacheBaseURL", "https://cache.wushuo.top") + "/bgm/subjects/" + url.PathEscape(id)
		_, err := c.request(ctx, config, http.MethodGet, cacheURL, nil, nil, &info)
		if err == nil {
			info = normalizeBgmInfo(info)
		}
		results <- outcome{info: info, err: err, cached: true}
	}()
	var direct, cached outcome
	for range 2 {
		result := <-results
		if result.cached {
			cached = result
		} else {
			direct = result
			// 官方资料成功即继续解析，取消尚未完成的备用请求。
			if direct.err == nil {
				return direct.info, nil
			}
		}
	}
	if cached.err == nil {
		return cached.info, nil
	}
	return bgmInfo{}, direct.err
}

func normalizeBgmInfo(info bgmInfo) bgmInfo {
	if info.Date == nil {
		now := time.Now()
		info.Date = &javaDate{Time: now}
	}
	if info.Season < 1 {
		info.Season = bgmSeason(info)
	}
	return info
}

func bgmSeason(info bgmInfo) int {
	for _, tag := range info.Tags {
		if season := seasonFromName(tag.Name); season > 1 {
			return season
		}
	}
	if season := seasonFromName(info.NameCN); season > 1 {
		return season
	}
	if season := seasonFromName(info.Name); season > 1 {
		return season
	}
	for _, box := range info.Infobox {
		if text(box["key"]) != "别名" {
			continue
		}
		values, _ := box["value"].([]any)
		for _, value := range values {
			if item, ok := value.(map[string]any); ok {
				if season := seasonFromName(text(item["v"])); season > 1 {
					return season
				}
			}
		}
	}
	return 1
}

func seasonFromName(name string) int {
	patterns := []*regexp.Regexp{regexp.MustCompile(`第\s*([一二三四五六七八九十百千]+)\s*[季期]`), regexp.MustCompile(`(?i)season\s*(\d+)`), regexp.MustCompile(`(?i)(\d+)(?:st|nd|rd|th)\s*season`), regexp.MustCompile(`(?i)s(\d+)$`)}
	season := 1
	for _, pattern := range patterns {
		match := pattern.FindStringSubmatch(name)
		if len(match) < 2 {
			continue
		}
		if n, err := strconv.Atoi(match[1]); err == nil {
			season = n
			continue
		}
		if n := chineseNumber(match[1]); n > 0 {
			season = n
		}
	}
	return season
}

func chineseNumber(value string) int {
	digits := map[rune]int{'一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9, '零': 0, '十': 10, '百': 100, '千': 1000}
	if len([]rune(value)) == 1 {
		return digits[[]rune(value)[0]]
	}
	result, last := 0, 0
	for _, r := range value {
		n, ok := digits[r]
		if !ok {
			continue
		}
		if n >= 10 {
			if last == 0 {
				last = 1
			}
			result += last * n
			last = 0
		} else {
			last = n
		}
	}
	return result + last
}

func (c *Client) bgmEpisodes(ctx context.Context, id string, kind *int) ([]bgmEpisode, error) {
	config, err := c.config(ctx)
	if err != nil {
		return nil, err
	}
	return c.bgmEpisodesWithConfig(ctx, config, id, kind)
}

func (c *Client) bgmEpisodesWithConfig(ctx context.Context, config map[string]any, id string, kind *int) ([]bgmEpisode, error) {
	query := url.Values{}
	query.Set("subject_id", id)
	query.Set("type", "0")
	query.Set("limit", "1000")
	query.Set("offset", "0")
	var response struct {
		Data []bgmEpisode `json:"data"`
	}
	_, err := c.request(ctx, config, http.MethodGet, bgmBase(config)+"/v0/episodes?"+query.Encode(), c.bgmHeaders(config), nil, &response)
	if err != nil {
		return nil, err
	}
	episodes := response.Data
	if episodes == nil {
		episodes = []bgmEpisode{}
	}
	if kind == nil {
		return episodes, nil
	}
	filtered := make([]bgmEpisode, 0)
	for _, item := range episodes {
		if item.Type == *kind {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (c *Client) bgmTitle(ctx context.Context, ani map[string]any) (string, error) {
	id, err := c.subjectIDForAni(ctx, ani)
	if err != nil {
		return "", err
	}
	info, err := c.bgmSubjectForAni(ctx, ani, id)
	if err != nil {
		return "", err
	}
	config, err := c.config(ctx)
	if err != nil {
		return "", err
	}
	title := bgmFinalName(info, config)
	if tmdb, ok := ani["tmdb"].(map[string]any); ok && text(tmdb["id"]) != "" {
		if boolean(config["titleYear"]) {
			if date := text(tmdb["date"]); len(date) >= 4 {
				title = regexp.MustCompile(`\s*\((19|20)\d{2}\)`).ReplaceAllString(title, "")
				title = strings.TrimSpace(title) + " (" + date[:4] + ")"
			}
		}
		if boolean(config["tmdbId"]) {
			title += " [tmdbid=" + text(tmdb["id"]) + "]"
		}
		return sanitizeName(title), nil
	}
	if boolean(config["titleYear"]) && info.Date != nil {
		title = fmt.Sprintf("%s (%d)", title, info.Date.Year())
	}
	return sanitizeName(title), nil
}

func bgmFinalName(info bgmInfo, config map[string]any) string {
	title := info.NameCN
	if title == "" || boolean(config["bgmJpName"]) {
		title = info.Name
	}
	if strings.TrimSpace(title) == "" {
		title = "无标题"
	}
	return strings.TrimSpace(title)
}

func bgmTitleWithTMDB(info bgmInfo, tmdb map[string]any, config map[string]any) string {
	title := bgmFinalName(info, config)
	if tmdb == nil {
		return title
	}
	if boolean(config["titleYear"]) {
		if date := text(tmdb["date"]); len(date) >= 4 {
			title = regexp.MustCompile(`\s*\((19|20)\d{2}\)`).ReplaceAllString(title, "")
			title = strings.TrimSpace(title) + " (" + date[:4] + ")"
		}
	}
	if boolean(config["tmdbId"]) {
		id := text(tmdb["id"])
		if id != "" {
			title += fmt.Sprintf(" [tmdbid=%s]", id)
		}
	}
	return sanitizeName(title)
}

func (c *Client) bgmSubjectForAni(ctx context.Context, ani map[string]any, id string) (bgmInfo, error) {
	return c.bgmSubject(ctx, mustConfig(c, ctx), id, false)
}
func mustConfig(c *Client, ctx context.Context) map[string]any {
	config, _ := c.config(ctx)
	return config
}

func (c *Client) draftEpisodes(ctx context.Context, config map[string]any, id string) ([]bgmEpisode, error) {
	raw, err := c.sourceBytes(ctx, sourceKey(config, "episodes:"+id), 5*time.Minute, func(work context.Context) ([]byte, error) {
		episodes, err := c.bgmEpisodesWithConfig(work, config, id, intPtr(0))
		if err != nil {
			return nil, err
		}
		return json.Marshal(episodes)
	})
	if err != nil {
		return nil, err
	}
	var episodes []bgmEpisode
	err = json.Unmarshal(raw, &episodes)
	return episodes, err
}

func (c *Client) draftTMDB(ctx context.Context, config map[string]any, title string, ova bool) (map[string]any, error) {
	raw, err := c.sourceBytes(ctx, sourceKey(config, fmt.Sprintf("tmdb:%t:%s", ova, title)), 5*time.Minute, func(work context.Context) ([]byte, error) {
		info, _, err := c.tmdbSearch(work, config, title, ova)
		if err != nil {
			return nil, err
		}
		return json.Marshal(info)
	})
	if err != nil {
		return nil, err
	}
	var info map[string]any
	err = json.Unmarshal(raw, &info)
	return info, err
}

func (c *Client) aniBySubjectID(ctx context.Context, id string) (map[string]any, error) {
	config, err := c.config(ctx)
	if err != nil {
		return nil, err
	}
	info, err := c.bgmSubject(ctx, config, id, true)
	if err != nil {
		return nil, err
	}
	defaults := map[string]any{}
	if err := json.Unmarshal(aniDefaults, &defaults); err != nil {
		return nil, err
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	defaults["id"] = hex.EncodeToString(idBytes)

	ova := strings.EqualFold(info.Platform, "OVA") || info.Platform == "剧场版"
	image := selectBgmImage(info.Images, text(config["bgmImageSize"]))
	bgmURL := "https://bgm.tv/subject/" + id
	title := bgmFinalName(info, config)
	var cover string
	var tmdb map[string]any
	var tmdbErr error
	episodeCount := info.Eps
	var reads sync.WaitGroup
	reads.Add(3)
	go func() {
		defer reads.Done()
		if info.Eps > 0 {
			if episodes, err := c.draftEpisodes(ctx, config, id); err == nil && len(episodes) > 0 {
				episodeCount = len(episodes)
			}
		}
	}()
	go func() {
		defer reads.Done()
		cover, _ = c.saveCover(ctx, config, image)
		if cover == "" {
			cover, _ = c.saveCover(ctx, config, "")
		}
		if cover == "" {
			cover = "cover.png"
		}
	}()
	go func() { defer reads.Done(); tmdb, tmdbErr = c.draftTMDB(ctx, config, title, ova) }()
	reads.Wait()
	info.Eps = episodeCount

	tmdbData := map[string]any{"id": "", "name": "", "originalName": "", "otherMap": map[string]any{}}
	if tmdbID := text(defaults["tmdbId"]); tmdbID != "" {
		tmdbData["id"] = tmdbID
	}
	defaults["bgmUrl"] = bgmURL
	defaults["title"] = title
	defaults["jpTitle"] = info.Name
	defaults["season"] = info.Season
	defaults["totalEpisodeNumber"] = info.Eps
	defaults["ova"] = ova
	defaults["score"] = 0.0
	defaults["releaseDate"] = ""
	if info.Date != nil {
		defaults["releaseDate"] = info.Date.Format("2006-01-02")
	}
	defaults["image"] = image
	defaults["customDownloadPath"] = true
	defaults["customDownloadPathTemplate"] = ""
	if info.Rating != nil {
		defaults["score"] = info.Rating.Score
	}
	defaults["tmdb"] = tmdbData
	defaults["cover"] = cover
	tmdbName := ""
	if tmdbErr == nil && tmdb != nil {
		tmdbName = tmdbFinalName(tmdb, config)
		defaults["tmdb"] = tmdb
		if boolean(config["tmdb"]) && tmdbName != "" {
			defaults["title"] = tmdbName
		} else {
			defaults["title"] = bgmTitleWithTMDB(info, tmdb, config)
		}
	} else if boolean(config["titleYear"]) && info.Date != nil {
		defaults["title"] = fmt.Sprintf("%s (%d)", title, info.Date.Year())
	}
	if tmdbName != "" {
		defaults["themoviedbName"] = tmdbName
	}
	defaults["customDownloadPathTemplate"] = c.downloadPathTemplate(defaults, config, info, tmdb)
	defaults["customCompletedPathTemplate"] = text(config["completedPathTemplate"])
	return defaults, nil
}

func (c *Client) saveCover(ctx context.Context, config map[string]any, image string) (string, error) {
	directory, ok := c.store.(interface{ DataDir() string })
	if !ok {
		return "", nil
	}
	if image == "" {
		target := filepath.Join(directory.DataDir(), "files", "cover.png")
		if _, err := os.Stat(target); err == nil {
			return "cover.png", nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, defaultCover, 0644); err != nil {
			return "", err
		}
		return "cover.png", nil
	}
	hash := md5.Sum([]byte(image))
	name := hex.EncodeToString(hash[:])
	extension := strings.ToLower(filepath.Ext(mustURLPath(image)))
	if extension == "" {
		extension = ".img"
	}
	filename := name + extension
	relative := name[:1] + "/" + filename
	target := filepath.Join(directory.DataDir(), "files", name[:1], filename)
	if _, err := os.Stat(target); err == nil {
		return relative, nil
	}
	status, data, err := c.requestText(ctx, config, http.MethodGet, image, nil, nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("封面服务返回 HTTP %d", status)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, data, 0644); err != nil {
		return "", err
	}
	return relative, nil
}

func (c *Client) downloadPathTemplate(ani, config map[string]any, info bgmInfo, tmdb map[string]any) string {
	template := text(config["downloadPathTemplate"])
	if boolean(ani["ova"]) && text(config["ovaDownloadPathTemplate"]) != "" {
		template = text(config["ovaDownloadPathTemplate"])
	}
	if template == "" {
		return ""
	}
	title := text(ani["title"])
	month, year, tmdbYear := 0, 0, 0
	date := time.Now()
	if info.Date != nil {
		date = info.Date.Time
	}
	year = date.Year()
	month = int(date.Month())
	tmdbDate := date
	if tmdb != nil {
		if raw := text(tmdb["date"]); raw != "" {
			if parsed, err := time.Parse("2006-01-02", raw); err == nil {
				tmdbDate = parsed
			}
		}
	}
	tmdbYear = tmdbDate.Year()
	letter := pinyinInitialLetter(title)
	replacements := map[string]string{"${letter}": letter, "${year}": strconv.Itoa(year), "${tmdbYear}": strconv.Itoa(tmdbYear), "${month}": strconv.Itoa(month), "${monthFormat}": fmt.Sprintf("%02d", month), "${season}": strconv.Itoa(integer(ani["season"])), "${seasonFormat}": fmt.Sprintf("%02d", integer(ani["season"])), "${title}": title, "${themoviedbName}": text(ani["themoviedbName"]), "${subgroup}": text(ani["subgroup"]), "${bgmId}": info.ID, "${tmdbid}": ""}
	if tmdb != nil {
		replacements["${tmdbid}"] = text(tmdb["id"])
	}
	quarter, quarterName := 10, "秋"
	if month == 12 || month <= 2 {
		quarter, quarterName = 1, "冬"
	} else if month <= 5 {
		quarter, quarterName = 4, "春"
	} else if month <= 8 {
		quarter, quarterName = 7, "夏"
	}
	replacements["${quarter}"] = strconv.Itoa(quarter)
	replacements["${quarterFormat}"] = fmt.Sprintf("%02d", quarter)
	replacements["${quarterName}"] = quarterName
	if jp := text(ani["jpTitle"]); jp != "" {
		replacements["${jpTitle}"] = jp
	}
	if date.Month() == 12 && (strings.Contains(template, "${quarter}") || strings.Contains(template, "${quarterFormat}") || strings.Contains(template, "${quarterName}")) {
		year++
	}
	replacements["${year}"] = strconv.Itoa(year)
	for _, key := range []string{"${letter}", "${quarter}", "${quarterFormat}", "${quarterName}", "${tmdbYear}", "${year}", "${month}", "${monthFormat}", "${season}", "${seasonFormat}", "${bgmId}", "${title}", "${themoviedbName}", "${subgroup}", "${tmdbid}", "${jpTitle}"} {
		if value, ok := replacements[key]; ok {
			template = strings.ReplaceAll(template, key, value)
		}
	}
	absolute, err := filepath.Abs(filepath.Clean(template))
	if err == nil {
		return path.Clean(absolute)
	}
	return template
}

func pinyinInitialLetter(title string) string {
	parts := pinyin.Pinyin(strings.TrimSpace(title), pinyin.NewArgs())
	if len(parts) == 0 || len(parts[0]) == 0 {
		return "#"
	}
	first := strings.ToUpper(parts[0][0])
	if first == "" {
		return "#"
	}
	r := []rune(first)[0]
	if r >= '0' && r <= '9' {
		return "0"
	}
	if r >= 'A' && r <= 'Z' {
		return string(r)
	}
	return "#"
}

func mustURLPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}
func selectBgmImage(images bgmImages, size string) string {
	switch size {
	case "small":
		return images.Small
	case "grid":
		return images.Grid
	case "large":
		return images.Large
	case "medium":
		return images.Medium
	case "common":
		return images.Common
	}
	return images.Common
}
func intPtr(value int) *int { return &value }

func (c *Client) subjectIDForAni(ctx context.Context, ani map[string]any) (string, error) {
	if id := subjectIDLoose(text(ani["bgmUrl"])); id != "" {
		return id, nil
	}
	if text(ani["type"]) == "mikan" {
		mikanID := mikanRSSID(text(ani["url"]))
		if mikanID == "" {
			return "", errors.New("无法取得 bangumiId")
		}
		config, err := c.config(ctx)
		if err != nil {
			return "", err
		}
		info, err := c.mikanInfo(ctx, config, mikanID)
		if err != nil {
			return "", err
		}
		if id := subjectIDLoose(info.BgmURL); id != "" {
			return id, nil
		}
	}
	return "", errors.New("bgmUrl 不能为空或格式错误")
}

func (c *Client) bgmEpisodeTitleMap(ctx context.Context, id string) (map[int]bgmEpisode, error) {
	key := "bgm-episode-titles:" + id
	value, err := c.cached(key, 5*time.Minute, func() (any, error) {
		episodes, err := c.bgmEpisodes(ctx, id, intPtr(0))
		if err != nil {
			return nil, err
		}
		result := map[int]bgmEpisode{}
		for _, episode := range episodes {
			result[int(episode.Ep)] = episode
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	result, _ := value.(map[int]bgmEpisode)
	return result, nil
}
