package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// MikanSeason 是 Mikan 查询支持的季度筛选条件。
type MikanSeason = mikanSeason

// AniBTSearchInput 保存 AniBT 的公开搜索参数。
type AniBTSearchInput struct {
	Season string
	BGMURL string
	Title  string
}

// SubscriptionPreviewer 读取并筛选 RSS 条目，供订阅组名和偏移推断复用。
type SubscriptionPreviewer interface {
	Preview(context.Context, map[string]any) (map[string]any, error)
}

var subgroupPrefix = regexp.MustCompile(`^\[(.+?)]`)

// SearchMikan 返回 Mikan 搜索结果，供 JSON API 和 MCP 共用。
func (c *Client) SearchMikan(ctx context.Context, text string, season *MikanSeason) (any, error) {
	return c.searchMikan(ctx, text, season)
}

// SearchAniBT 返回 AniBT 搜索结果，供 JSON API 和 MCP 共用。
func (c *Client) SearchAniBT(ctx context.Context, input AniBTSearchInput) (any, error) {
	return c.searchAniBT(ctx, input.Season, input.BGMURL, input.Title)
}

// SearchAnimeGarden 返回 AnimeGarden 搜索结果，供 JSON API 和 MCP 共用。
func (c *Client) SearchAnimeGarden(ctx context.Context, bgmURL string) (any, error) {
	return c.searchAnimeGarden(ctx, bgmURL, "")
}

// GetMikanGroups 返回一个 Mikan 页面上的字幕组 RSS。
func (c *Client) GetMikanGroups(ctx context.Context, pageURL string) (any, error) {
	return c.mikanGroups(ctx, pageURL)
}

// GetAniBTGroups 返回 Bangumi subject 对应的 AniBT 字幕组。
func (c *Client) GetAniBTGroups(ctx context.Context, bgmID string) (any, error) {
	return c.aniBTGroups(ctx, bgmID)
}

// GetAnimeGardenGroups 返回 Bangumi subject 对应的 AnimeGarden 字幕组。
func (c *Client) GetAnimeGardenGroups(ctx context.Context, bgmID string) (any, error) {
	return c.animeGardenGroups(ctx, bgmID)
}

// ResolveSubscription 将 RSS 来源、Bangumi 元数据和全局规则合成一份订阅草稿。
func (c *Client) ResolveSubscription(ctx context.Context, request map[string]any, previewer SubscriptionPreviewer) (map[string]any, error) {
	if request == nil {
		return nil, errors.New("RSS 订阅请求不能为空")
	}
	rssURL := strings.TrimSpace(text(request["url"]))
	if rssURL == "" {
		return nil, errors.New("RSS地址 不能为空")
	}
	sourceType := strings.TrimSpace(text(request["type"]))
	if sourceType == "" {
		sourceType = "mikan"
	}
	enable := true
	if raw, exists := request["enable"]; exists && raw != nil {
		var ok bool
		enable, ok = raw.(bool)
		if !ok {
			return nil, errors.New("enable 必须是布尔值")
		}
	}
	standby, err := standbyList(request["standbyRssList"])
	if err != nil {
		return nil, err
	}
	config, err := c.config(ctx)
	if err != nil {
		return nil, err
	}
	bgmURL, subgroup := text(request["bgmUrl"]), text(request["subgroup"])
	mikanTitle := ""
	switch sourceType {
	case "mikan":
		if strings.TrimSpace(subgroup) == "" && strings.TrimSpace(bgmURL) == "" {
			info, err := c.mikanInfoForRSS(ctx, config, rssURL)
			if err != nil {
				return nil, fmt.Errorf("获取 Mikan 订阅信息失败：%w", err)
			}
			mikanTitle, bgmURL, subgroup = info.Title, info.BgmURL, info.Subgroup
		}
	case "ani-bt":
		params, err := url.Parse(rssURL)
		if err != nil {
			return nil, fmt.Errorf("解析 AniBT RSS 地址失败：%w", err)
		}
		bgmURL = ""
		if params.Query().Get("bgmId") != "" {
			bgmURL = "https://bgm.tv/subject/" + params.Query().Get("bgmId")
		}
		if strings.TrimSpace(subgroup) == "" {
			subgroup = params.Query().Get("groupSlug")
		}
	case "anime-garden":
		params, err := url.Parse(rssURL)
		if err != nil {
			return nil, fmt.Errorf("解析 AnimeGarden RSS 地址失败：%w", err)
		}
		bgmURL = ""
		if params.Query().Get("subject") != "" {
			bgmURL = "https://bgm.tv/subject/" + params.Query().Get("subject")
		}
		if strings.TrimSpace(subgroup) == "" {
			subgroup = params.Query().Get("fansub")
		}
	}
	bgmID := subjectIDLoose(bgmURL)
	if bgmID == "" {
		return nil, errors.New("bgmUrl 不能为空或格式错误")
	}
	ani, err := c.aniBySubjectID(ctx, bgmID)
	if err != nil {
		return nil, fmt.Errorf("获取 Bangumi 作品信息失败：%w", err)
	}
	ani["url"] = rssURL
	ani["type"] = sourceType
	ani["enable"] = enable
	ani["standbyRssList"] = standby
	ani["downloadNew"] = boolean(config["downloadNew"])
	ani["globalExclude"] = boolean(config["enabledExclude"])
	if mikanTitle != "" {
		ani["mikanTitle"] = mikanTitle
	}
	if strings.TrimSpace(subgroup) == "" {
		subgroup = "未知字幕组"
	}
	ani["subgroup"] = subgroup
	if boolean(config["importExclude"]) {
		ani["exclude"] = mergeStrings(stringSlice(config["exclude"]), stringSlice(ani["exclude"]))
	}
	if subgroup == "未知字幕组" || boolean(config["offset"]) && !boolean(ani["ova"]) {
		if previewer == nil {
			return nil, errors.New("RSS 订阅解析需要预览器来推断字幕组或集数偏移")
		}
		var mainItems []previewItem
		mainRead := false
		if subgroup == "未知字幕组" {
			items, err := previewFeed(ctx, previewer, ani, rssURL, "", 0)
			if err != nil {
				return nil, fmt.Errorf("推断字幕组失败：%w", err)
			}
			mainItems, mainRead = items, true
			if inferred := inferSubgroup(items); inferred != "" {
				subgroup = inferred
				ani["subgroup"] = inferred
			}
		}
		if boolean(config["offset"]) && !boolean(ani["ova"]) {
			if !mainRead {
				mainItems, err = previewFeed(ctx, previewer, ani, rssURL, "", 0)
				if err != nil {
					return nil, fmt.Errorf("推断剧集偏移失败：%w", err)
				}
			}
			offset := inferOffset(mainItems)
			ani["offset"] = offset
			for _, raw := range standby {
				entry := raw.(map[string]any)
				feedURL := text(entry["url"])
				items, err := previewFeed(ctx, previewer, ani, feedURL, "", offset)
				if err != nil {
					return nil, fmt.Errorf("推断备用 RSS 剧集偏移失败：%w", err)
				}
				entry["offset"] = inferOffset(items)
			}
		}
	}
	return ani, nil
}

func (c *Client) rssToAniRoute(r *http.Request) (any, string, error) {
	input, err := bodyMap(r)
	if err != nil {
		return nil, "", err
	}
	ani, err := c.ResolveSubscription(r.Context(), input, c.subscriptionPreviewer)
	if err != nil {
		return nil, "", fmt.Errorf("RSS解析失败：%w", err)
	}
	return ani, "success", nil
}

type mikanSubscriptionInfo struct {
	Title    string
	BgmURL   string
	Subgroup string
}

func (c *Client) mikanInfoForRSS(ctx context.Context, config map[string]any, rssURL string) (mikanSubscriptionInfo, error) {
	params, err := url.Parse(rssURL)
	if err != nil {
		return mikanSubscriptionInfo{}, err
	}
	bangumiID := params.Query().Get("bangumiId")
	if bangumiID == "" {
		return mikanSubscriptionInfo{}, nil
	}
	info, err := c.mikanInfo(ctx, config, bangumiID)
	if err != nil {
		return mikanSubscriptionInfo{}, err
	}
	groupID := ""
	for key, values := range params.Query() {
		if strings.EqualFold(key, "subgroupid") && len(values) > 0 {
			groupID = values[0]
			break
		}
	}
	result := mikanSubscriptionInfo{Title: info.Title, BgmURL: info.BgmURL}
	for _, group := range info.Groups {
		if group.SubgroupID == groupID {
			result.Subgroup = group.Label
			break
		}
	}
	return result, nil
}

func previewFeed(ctx context.Context, previewer SubscriptionPreviewer, ani map[string]any, feedURL, subgroup string, offset int) ([]previewItem, error) {
	subscription := copyMap(ani)
	subscription["url"] = feedURL
	subscription["subgroup"] = subgroup
	subscription["offset"] = offset
	subscription["standbyRssList"] = []any{}
	preview, err := previewer.Preview(ctx, subscription)
	if err != nil {
		return nil, err
	}
	raw, ok := preview["items"]
	if !ok {
		return nil, errors.New("RSS 预览没有返回条目列表")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var items []previewItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("解析 RSS 预览条目失败：%w", err)
	}
	return items, nil
}

type previewItem struct {
	Title   string  `json:"title"`
	Episode float64 `json:"episode"`
}

func inferSubgroup(items []previewItem) string {
	for _, item := range items {
		title := item.Title
		if !subgroupPrefix.MatchString(title) {
			title = path.Base(title)
		}
		if match := subgroupPrefix.FindStringSubmatch(title); len(match) == 2 {
			return match[1]
		}
	}
	return ""
}

func inferOffset(items []previewItem) int {
	if len(items) == 0 {
		return 0
	}
	minimum := items[0].Episode
	for _, item := range items[1:] {
		if item.Episode < minimum {
			minimum = item.Episode
		}
	}
	whole := int(minimum)
	if minimum != float64(whole) {
		return -whole
	}
	return -(whole - 1)
}

func standbyList(value any) ([]any, error) {
	if value == nil {
		return []any{}, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, errors.New("standbyRssList 必须是列表")
	}
	result := make([]any, 0, len(list))
	for i, item := range list {
		entry, ok := item.(map[string]any)
		if !ok || strings.TrimSpace(text(entry["url"])) == "" {
			return nil, fmt.Errorf("备用 RSS 第 %d 项缺少有效 URL", i+1)
		}
		result = append(result, copyMap(entry))
	}
	return result, nil
}

func mergeStrings(groups ...[]string) []any {
	seen := map[string]bool{}
	result := make([]any, 0)
	for _, values := range groups {
		for _, value := range values {
			if seen[value] {
				continue
			}
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func stringSlice(value any) []string {
	switch list := value.(type) {
	case []string:
		return append([]string(nil), list...)
	case []any:
		result := make([]string, 0, len(list))
		for _, item := range list {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func copyMap(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
