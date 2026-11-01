package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const defaultAnimeGardenHost = "https://api.animes.garden"

func (c *Client) searchAnimeGarden(ctx context.Context, bgmURL string) ([]animeGardenWeek, error) {
	config, err := c.config(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(bgmURL) != "" {
		id := subjectIDFromURL(bgmURL)
		if id == "" {
			return nil, fmt.Errorf("无法从 bgmUrl 解析 subject ID")
		}
		subject, err := c.bgmSubject(ctx, config, id, false)
		if err != nil {
			return nil, err
		}
		return []animeGardenWeek{{WeekLabel: "搜索", Subjects: []animeGardenSubject{{ID: id, Name: bgmFinalName(subject, config), Cover: publicCover(subject), Score: publicScore(subject), Exists: true}}}}, nil
	}
	base := c.get(ctx, config, "animeGardenBaseURL", defaultAnimeGardenHost)
	var envelope struct {
		Subjects []animeGardenSubject `json:"subjects"`
	}
	_, err = c.request(ctx, config, http.MethodGet, base+"/subjects", nil, nil, &envelope)
	if err != nil {
		return nil, err
	}
	if envelope.Subjects == nil {
		return nil, fmt.Errorf("AnimeGarden 响应缺少 subjects")
	}
	subs, err := c.subscriptions(ctx)
	if err != nil {
		return nil, err
	}
	c.enrichAnimeGarden(ctx, config, envelope.Subjects)
	ids := subscribedBgmIDs(subs)
	byWeek := map[string][]animeGardenSubject{}
	for _, subject := range envelope.Subjects {
		subject.Exists = ids[subject.ID]
		if subject.ActivedAt != nil {
			subject.WeekLabel = weekLabel(dayIndex(subject.ActivedAt.Time))
		} else {
			subject.WeekLabel = ""
		}
		byWeek[subject.WeekLabel] = append(byWeek[subject.WeekLabel], subject)
	}
	weeks := make([]animeGardenWeek, 0, 7)
	for _, label := range []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"} {
		items := byWeek[label]
		if len(items) == 0 {
			continue
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].Score > items[j].Score })
		weeks = append(weeks, animeGardenWeek{WeekLabel: label, Subjects: items})
	}
	sortWeeks(weeks, func(w animeGardenWeek) string { return w.WeekLabel })
	return weeks, nil
}

func (c *Client) animeGardenGroups(ctx context.Context, bgmID string) ([]animeGardenGroup, error) {
	config, err := c.config(ctx)
	if err != nil {
		return nil, err
	}
	base := c.get(ctx, config, "animeGardenBaseURL", defaultAnimeGardenHost)
	query := url.Values{}
	query.Set("subject", bgmID)
	query.Set("pageSize", "200")
	query.Set("duplicate", "false")
	var envelope struct {
		Resources []animeGardenItem `json:"resources"`
	}
	_, err = c.request(ctx, config, http.MethodGet, queryURL(base+"/resources", query), nil, nil, &envelope)
	if err != nil {
		return nil, err
	}
	if envelope.Resources == nil {
		return nil, fmt.Errorf("AnimeGarden 响应缺少 resources")
	}
	itemsByFansub := map[string][]animeGardenItem{}
	candidates := make([]animeGardenGroup, 0)
	for i := range envelope.Resources {
		item := envelope.Resources[i]
		if item.Fansub == nil {
			continue
		}
		item.FormatSize = formatSize(item.Size)
		itemsByFansub[item.Fansub.ID] = append(itemsByFansub[item.Fansub.ID], item)
		query := url.Values{}
		query.Set("subject", bgmID)
		query.Set("fansub", item.Fansub.Name)
		candidates = append(candidates, animeGardenGroup{ID: item.Fansub.ID, Name: item.Fansub.Name, LastUpdatedAt: item.CreatedAt, RSS: queryURL(base+"/feed.xml", query), BgmID: bgmID})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return dateValue(candidates[i].LastUpdatedAt) > dateValue(candidates[j].LastUpdatedAt)
	})
	groups := make([]animeGardenGroup, 0, len(itemsByFansub))
	seen := map[string]bool{}
	for _, group := range candidates {
		if seen[group.ID] {
			continue
		}
		seen[group.ID] = true
		group.Items = itemsByFansub[group.ID]
		titles := make([]string, 0, len(group.Items))
		for _, item := range group.Items {
			titles = append(titles, item.Title)
		}
		group.GroupRegex = deriveGroupRegex(titles)
		groups = append(groups, group)
	}
	return groups, nil
}

func dateValue(value *javaDate) int64 {
	if value == nil {
		return 0
	}
	return value.Unix()
}

func formatSize(size int64) string {
	if size < 0 {
		size = 0
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := float64(size)
	index := 0
	for value >= 1024 && index < len(units)-1 {
		value /= 1024
		index++
	}
	return fmt.Sprintf("%.2f %s", value, units[index])
}

func parseObject(data json.RawMessage) map[string]any {
	result := map[string]any{}
	_ = json.Unmarshal(data, &result)
	return result
}
