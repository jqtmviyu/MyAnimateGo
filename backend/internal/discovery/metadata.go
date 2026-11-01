package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Metadata 为 rules.RenameInput.Metadata 提供可用模板值；外部值只由此客户端显式获取。
func (c *Client) Metadata(ctx context.Context, ani map[string]any, episode float64) (map[string]string, error) {
	metadata := map[string]string{}
	if tmdb, ok := ani["tmdb"].(map[string]any); ok {
		metadata["tmdbid"] = text(tmdb["id"])
	} else {
		metadata["tmdbid"] = ""
	}
	metadata["themoviedbName"] = text(ani["themoviedbName"])
	metadata["bgmId"] = subjectIDLoose(text(ani["bgmUrl"]))
	if metadata["bgmId"] == "" && text(ani["type"]) == "mikan" {
		if id, err := c.subjectIDForAni(ctx, ani); err == nil {
			metadata["bgmId"] = id
		}
	}
	if jpTitle := text(ani["jpTitle"]); jpTitle != "" {
		metadata["jpTitle"] = jpTitle
	}
	integerEpisode := int(episode)
	wholeEpisode := episode == float64(integerEpisode)
	defaultTitle := "第" + strconv.Itoa(integerEpisode) + "集"
	if episode != float64(integerEpisode) {
		defaultTitle = fmt.Sprintf("第%s集", strconv.FormatFloat(episode, 'f', -1, 64))
	}
	metadata["episodeTitle"] = defaultTitle
	metadata["bgmEpisodeTitle"] = defaultTitle
	metadata["bgmJpEpisodeTitle"] = defaultTitle

	config, err := c.config(ctx)
	if err != nil {
		return nil, err
	}
	if wholeEpisode {
		if tmdb, ok := ani["tmdb"].(map[string]any); ok && text(tmdb["id"]) != "" && integer(ani["season"]) > 0 {
			if name, err := c.tmdbEpisodeTitle(ctx, config, text(tmdb["id"]), integer(ani["season"]), integerEpisode); err == nil && name != "" {
				metadata["episodeTitle"] = name
			}
		}
	}
	if metadata["bgmId"] != "" {
		if wholeEpisode {
			if episodes, err := c.bgmEpisodeTitleMap(ctx, metadata["bgmId"]); err == nil {
				if episodeInfo, ok := episodes[integerEpisode]; ok {
					jp := episodeInfo.Name
					cn := episodeInfo.NameCN
					if cn == "" {
						cn = jp
					}
					if jp != "" {
						metadata["bgmJpEpisodeTitle"] = sanitizeName(jp)
					}
					if cn != "" {
						metadata["bgmEpisodeTitle"] = sanitizeName(cn)
					}
				}
			}
		}
		if metadata["jpTitle"] == "" {
			if info, err := c.bgmSubject(ctx, config, metadata["bgmId"], false); err == nil && info.Name != "" {
				metadata["jpTitle"] = info.Name
			}
		}
	}
	return metadata, nil
}

// AniListRomaji 暴露纯文本解析边界，TMDB 回退和独立 fixture 共用它。
func (c *Client) AniListRomaji(ctx context.Context, title string) (string, error) {
	config, err := c.config(ctx)
	if err != nil {
		return "", err
	}
	return c.aniListRomaji(ctx, config, title)
}

func subjectIDLoose(raw string) string {
	if raw == "" {
		return ""
	}
	return subjectIDFromURL(raw)
}

func (c *Client) tmdbEpisodeTitle(ctx context.Context, config map[string]any, id string, season, episode int) (string, error) {
	query := url.Values{}
	var response struct {
		Episodes []struct {
			EpisodeNumber int    `json:"episode_number"`
			Name          string `json:"name"`
		} `json:"episodes"`
	}
	_, err := c.request(ctx, config, http.MethodGet, c.tmdbURL(config, "/3/tv/"+url.PathEscape(id)+"/season/"+strconv.Itoa(season), query), nil, nil, &response)
	if err != nil {
		return "", err
	}
	for _, item := range response.Episodes {
		if item.EpisodeNumber == episode {
			return sanitizeName(item.Name), nil
		}
	}
	return "", nil
}

func parseRawJSON(data json.RawMessage) map[string]any {
	result := map[string]any{}
	_ = json.Unmarshal(data, &result)
	return result
}

func metadataValue(metadata map[string]string, key string) string {
	return strings.TrimSpace(metadata[key])
}
