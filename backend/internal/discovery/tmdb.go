package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func (c *Client) tmdbName(ctx context.Context, id, title string, ova bool) (tmdbVO, error) {
	config, err := c.config(ctx)
	if err != nil {
		return tmdbVO{}, err
	}
	var tmdb map[string]any
	if id != "" {
		tmdb, err = c.tmdbDetails(ctx, config, id, ova)
	} else {
		title = stripTMDBNameMetadata(title)
		tmdb, _, err = c.tmdbSearch(ctx, config, title, ova)
	}
	if err != nil {
		return tmdbVO{}, err
	}
	if tmdb == nil {
		return tmdbVO{}, errors.New("未获取到 TMDB")
	}
	name := tmdbFinalName(tmdb, config)
	return tmdbVO{TMDB: tmdb, ThemoviedbName: name}, nil
}

func stripTMDBNameMetadata(title string) string {
	title = regexp.MustCompile(` ?\[tmdbid=\d+]`).ReplaceAllString(title, "")
	title = regexp.MustCompile(` ?\((19|20)\d{2}\)`).ReplaceAllString(title, "")
	return strings.TrimSpace(title)
}

func (c *Client) tmdbDetails(ctx context.Context, config map[string]any, id string, ova bool) (map[string]any, error) {
	kind := "tv"
	if ova {
		kind = "movie"
	}
	var result map[string]any
	_, err := c.request(ctx, config, http.MethodGet, c.tmdbURL(config, "/3/"+kind+"/"+url.PathEscape(id), nil), nil, nil, &result)
	if err != nil {
		return nil, err
	}
	return normalizeTMDB(result, kind), nil
}

func (c *Client) tmdbSearch(ctx context.Context, config map[string]any, title string, ova bool) (map[string]any, []map[string]any, error) {
	if strings.TrimSpace(title) == "" {
		return nil, nil, errors.New("TMDB 标题不能为空")
	}
	base, err := url.Parse(c.get(ctx, config, "tmdbApi", "https://api.themoviedb.org"))
	if err != nil {
		return nil, nil, err
	}
	// 官方搜索必须携带 Key；自定义代理可以自行提供鉴权。
	if strings.EqualFold(base.Hostname(), "api.themoviedb.org") && strings.TrimSpace(text(config["tmdbApiKey"])) == "" {
		return nil, nil, errors.New("TMDB API Key 未配置")
	}
	kind := "tv"
	if ova {
		kind = "movie"
	}
	query := url.Values{}
	query.Set("query", title)
	query.Set("include_adult", "false")
	var result struct {
		Results []map[string]any `json:"results"`
	}
	_, err = c.request(ctx, config, http.MethodGet, c.tmdbURL(config, "/3/search/"+kind, query), nil, nil, &result)
	if err != nil {
		return nil, nil, err
	}
	for i := range result.Results {
		result.Results[i] = normalizeTMDB(result.Results[i], kind)
	}
	if len(result.Results) == 0 {
		return nil, result.Results, nil
	}
	if boolean(config["tmdbRomaji"]) {
		if romaji, titleErr := c.tmdbRomaji(ctx, config, result.Results[0], kind); titleErr == nil && romaji != "" {
			result.Results[0]["name"] = sanitizeName(romaji)
		}
	}
	return result.Results[0], result.Results, nil
}

func (c *Client) tmdbRomaji(ctx context.Context, config map[string]any, tmdb map[string]any, kind string) (string, error) {
	id := text(tmdb["id"])
	if id == "" {
		return "", nil
	}
	var response struct {
		Titles []struct {
			ISO   string `json:"iso_3166_1"`
			Type  string `json:"type"`
			Title string `json:"title"`
		} `json:"titles"`
		Results []struct {
			ISO   string `json:"iso_3166_1"`
			Type  string `json:"type"`
			Title string `json:"title"`
		} `json:"results"`
	}
	_, err := c.request(ctx, config, http.MethodGet, c.tmdbURL(config, "/3/"+kind+"/"+url.PathEscape(id)+"/alternative_titles", nil), nil, nil, &response)
	if err == nil {
		titles := response.Titles
		if len(titles) == 0 {
			titles = response.Results
		}
		for _, title := range titles {
			if title.ISO == "JP" && (strings.EqualFold(title.Type, "romaji") || strings.EqualFold(title.Type, "romanization")) {
				return title.Title, nil
			}
		}
	}
	return c.aniListRomaji(ctx, config, text(tmdb["name"]))
}

func (c *Client) tmdbGroups(ctx context.Context, tmdb map[string]any) ([]map[string]any, error) {
	config, err := c.config(ctx)
	if err != nil {
		return nil, err
	}
	id := text(tmdb["id"])
	if id == "" {
		return nil, errors.New("tmdb.id 不能为空")
	}
	var result struct {
		Results []map[string]any `json:"results"`
	}
	_, err = c.request(ctx, config, http.MethodGet, c.tmdbURL(config, "/3/tv/"+url.PathEscape(id)+"/episode_groups", nil), nil, nil, &result)
	if err != nil {
		return nil, err
	}
	if result.Results == nil {
		return []map[string]any{}, nil
	}
	return result.Results, nil
}

func (c *Client) tmdbURL(config map[string]any, path string, query url.Values) string {
	base := c.get(context.Background(), config, "tmdbApi", "https://api.themoviedb.org")
	if query == nil {
		query = url.Values{}
	}
	if key := text(config["tmdbApiKey"]); key != "" {
		query.Set("api_key", key)
	}
	if language := text(config["tmdbLanguage"]); language != "" {
		query.Set("language", language)
	}
	return queryURL(base+path, query)
}

func normalizeTMDB(value map[string]any, kind string) map[string]any {
	result := map[string]any{}
	for key, item := range value {
		result[key] = item
	}
	if id := numericID(result["id"]); id != "" {
		result["id"] = id
	}
	if result["originalName"] == nil {
		result["originalName"] = value["original_name"]
	}
	if result["original_name"] == nil {
		result["original_name"] = value["originalName"]
	}
	if result["name"] == nil {
		result["name"] = value["name"]
	}
	if result["tmdbType"] == nil {
		result["tmdbType"] = kind
	}
	if result["date"] == nil {
		if kind == "movie" {
			result["date"] = value["release_date"]
		} else {
			result["date"] = value["first_air_date"]
		}
	}
	if result["tmdbGroupId"] == nil {
		result["tmdbGroupId"] = ""
	}
	delete(result, "original_name")
	delete(result, "first_air_date")
	delete(result, "release_date")
	return result
}

func tmdbFinalName(tmdb map[string]any, config map[string]any) string {
	name := text(tmdb["name"])
	if boolean(config["tmdbOriginalName"]) {
		name = text(tmdb["originalName"])
		if name == "" {
			name = text(tmdb["original_name"])
		}
	}
	if boolean(config["titleYear"]) {
		if date := text(tmdb["date"]); date != "" {
			year := date
			if len(year) > 4 {
				year = year[:4]
			}
			name = strings.TrimSpace(name) + " (" + year + ")"
		}
	}
	if boolean(config["tmdbId"]) {
		id := text(tmdb["id"])
		if id != "" {
			name += fmt.Sprintf(" [tmdbid=%s]", id)
		}
	}
	return sanitizeName(name)
}

func sanitizeName(value string) string {
	value = strings.ReplaceAll(value, "1/2", "½")
	value = strings.NewReplacer("/", " ", `\`, " ", ":", "：", "?", "？", "|", "｜", "*", " ", "<", " ", ">", " ", `"`, " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func (c *Client) aniListRomaji(ctx context.Context, config map[string]any, title string) (string, error) {
	if strings.TrimSpace(title) == "" {
		return "", nil
	}
	const query = `query ($search: String) { Page (page: 1, perPage: 1) { media (search: $search, type: ANIME) { title { romaji native } } } }`
	body, _ := json.Marshal(map[string]any{"query": query, "variables": map[string]string{"search": title}})
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	var result struct {
		Data struct {
			Page struct {
				Media []struct {
					Title struct {
						Romaji string `json:"romaji"`
						Native string `json:"native"`
					} `json:"title"`
				} `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	requestContext := ctx
	response, err := c.request(requestContext, config, http.MethodPost, c.get(ctx, config, "aniListBaseURL", "https://graphql.anilist.co"), headers, bytes.NewReader(body), &result)
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("AniList 返回 HTTP %d", response.StatusCode)
	}
	if len(result.Data.Page.Media) == 0 {
		return "", nil
	}
	return result.Data.Page.Media[0].Title.Romaji, nil
}

func numericID(value any) string {
	switch n := value.(type) {
	case string:
		return n
	case float64:
		return strconv.FormatInt(int64(n), 10)
	}
	return ""
}
