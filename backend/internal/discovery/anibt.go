package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const defaultAniBTHost = "https://anibt.net"

func (c *Client) searchAniBT(ctx context.Context, season, bgmURL, title string) (*aniBT, error) {
	config, err := c.config(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(title) != "" {
		season, bgmURL = "", ""
	}
	bgmID := subjectIDFromURL(bgmURL)
	query := url.Values{}
	query.Set("season", season)
	query.Set("bgmId", bgmID)
	query.Set("query", title)
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	base := c.get(ctx, config, "aniBTBaseURL", defaultAniBTHost)
	_, err = c.aniBTJSON(ctx, config, queryURL(base+"/api/seasons/anime", query), &envelope)
	if err != nil {
		return nil, err
	}
	var result aniBT
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, errors.New("AniBT 响应缺少 data")
	}
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		return nil, err
	}
	subs, err := c.subscriptions(ctx)
	if err != nil {
		return nil, err
	}
	ids := subscribedBgmIDs(subs)
	weeks := make([]aniBTWeek, 0, len(result.ByWeekday))
	for _, week := range result.ByWeekday {
		animes := make([]aniBTAnime, 0, len(week.Animes))
		for _, anime := range week.Animes {
			anime.Exists = ids[anime.BgmID]
			animes = append(animes, anime)
		}
		sort.SliceStable(animes, func(i, j int) bool { return animes[i].Rating > animes[j].Rating })
		if len(animes) == 0 {
			continue
		}
		week.Animes = animes
		weeks = append(weeks, week)
	}
	sortWeeks(weeks, func(value aniBTWeek) string { return value.WeekdayLabel })
	result.ByWeekday = weeks
	if result.AvailableSeasons == nil {
		result.AvailableSeasons = []string{}
	}
	return &result, nil
}

func (c *Client) aniBTGroups(ctx context.Context, bgmID string) ([]aniBTGroup, error) {
	config, err := c.config(ctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("bgmId", bgmID)
	var envelope struct {
		Data struct {
			Groups []aniBTGroup `json:"groups"`
		} `json:"data"`
	}
	base := c.get(ctx, config, "aniBTBaseURL", defaultAniBTHost)
	_, err = c.aniBTJSON(ctx, config, queryURL(base+"/api/anime/groups", query), &envelope)
	if err != nil {
		return nil, err
	}
	groups := envelope.Data.Groups
	if groups == nil {
		return nil, errors.New("AniBT 响应缺少 data.groups")
	}
	for i := range groups {
		group := &groups[i]
		group.BgmID = bgmID
		rss := url.Values{}
		rss.Set("bgmId", bgmID)
		rss.Set("groupSlug", group.Slug)
		group.RSS = queryURL(base+"/rss/anime.xml", rss)
		titles := make([]string, 0, len(group.Items))
		for i := range group.Items {
			item := &group.Items[i]
			titles = append(titles, item.Title)
			item.FormatSize = formatSize(item.Size)
			if item.ReleaseID != "" {
				item.Torrent = base + "/api/torrent/" + url.PathEscape(item.ReleaseID) + ".torrent"
			}
		}
		group.GroupRegex = deriveGroupRegex(titles)
	}
	return groups, nil
}

// 查询结果短暂复用；本地订阅状态仍在每次返回前重新合并。
func (c *Client) aniBTJSON(ctx context.Context, config map[string]any, endpoint string, target any) (int, error) {
	raw, err := c.sourceBytes(ctx, sourceKey(config, "anibt:"+endpoint), 90*time.Second, func(work context.Context) ([]byte, error) {
		status, body, err := c.requestText(work, config, http.MethodGet, endpoint, nil, nil)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("AniBT 返回 HTTP %d", status)
		}
		var envelope struct {
			OK   *bool           `json:"ok"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, err
		}
		if envelope.OK != nil && !*envelope.OK {
			return nil, errors.New("AniBT 查询失败")
		}
		if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
			return nil, errors.New("AniBT 响应缺少 data")
		}
		return body, nil
	})
	if err != nil {
		return 0, err
	}
	return http.StatusOK, json.Unmarshal(raw, target)
}
