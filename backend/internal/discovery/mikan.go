package discovery

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var mikanDirectoryHTMLPattern = regexp.MustCompile(`(?i)<(?:html|ul)`)

var mikanSearchIDPattern = regexp.MustCompile(`^id: (\d+)$`)

var mikanIDPattern = regexp.MustCompile(`\d+/?$`)

func (c *Client) searchMikan(ctx context.Context, queryText string, season *mikanSeason, metadata ...bool) (*mikan, error) {
	includeMetadata := len(metadata) == 0 || metadata[0]
	config, err := c.config(ctx)
	if err != nil {
		return nil, err
	}
	subs, err := c.subscriptions(ctx)
	if err != nil {
		return nil, err
	}
	base := c.get(ctx, config, "mikanHost", "https://mikanani.me")
	result := &mikan{Seasons: []mikanSeason{}, Weeks: []mikanWeek{}}
	if idMatch := mikanSearchIDPattern.FindStringSubmatch(queryText); len(idMatch) == 2 {
		info, err := c.mikanInfo(ctx, config, idMatch[1])
		if err != nil {
			return nil, err
		}
		for _, sub := range subs {
			if mikanRSSID(text(sub["url"])) == idMatch[1] {
				info.Exists = true
				break
			}
		}
		result.Weeks = []mikanWeek{{WeekLabel: "Search", Items: []mikanInfo{*info}}}
		result.TotalItem = 1
		if includeMetadata {
			c.enrichMikan(ctx, config, result, subscribedBgmIDs(subs))
		}
		return result, nil
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(queryText) != "" {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/Home/Search"
		query := parsed.Query()
		query.Set("searchstr", strings.ReplaceAll(queryText, " ", "%20"))
		parsed.RawQuery = query.Encode()
		// Query.Encode 转义百分号，因此恢复 URLUtil.encodeBlank 的空格编码行为。
		parsed.RawQuery = strings.ReplaceAll(parsed.RawQuery, "%2520", "%20")
	} else if season != nil && season.Year != 0 && season.Season != "" {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/Home/BangumiCoverFlowByDayOfWeek"
		query := parsed.Query()
		query.Set("year", strconv.Itoa(season.Year))
		query.Set("seasonStr", season.Season)
		parsed.RawQuery = query.Encode()
	}
	err = c.mikanDocument(ctx, config, "directory", parsed.String(), result, func(html []byte) (any, error) {
		return parseMikanDirectory(html, base)
	})
	if err != nil {
		return nil, err
	}
	bangumiIDs := map[string]bool{}
	bgmIDs := subscribedBgmIDs(subs)
	for _, sub := range subs {
		if id := mikanRSSID(text(sub["url"])); id != "" {
			bangumiIDs[id] = true
		}
	}
	for wi := range result.Weeks {
		for ii := range result.Weeks[wi].Items {
			item := &result.Weeks[wi].Items[ii]
			item.Exists = bangumiIDs[strings.TrimRight(mikanIDPattern.FindString(item.URL), "/")]
		}
	}

	// 海报目录可直接展示 Mikan 数据，评分补全留给需要元数据的调用方。
	if includeMetadata {
		c.enrichMikan(ctx, config, result, bgmIDs)
	}

	for i := range result.Weeks {
		sortMikanInfo(result.Weeks[i].Items)
	}
	return result, nil
}

func parseMikanDirectory(html []byte, base string) (*mikan, error) {
	if !mikanDirectoryHTMLPattern.Match(html) {
		return nil, errors.New("Mikan 响应不是 HTML")
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return nil, err
	}
	result := &mikan{Seasons: parseMikanSeasons(doc), Weeks: []mikanWeek{}}
	if doc.Find(".sk-bangumi").Length() == 0 {
		result.Weeks = []mikanWeek{{WeekLabel: "Search", Items: parseMikanList(doc.Find(".an-ul"), base, nil)}}
	} else {
		doc.Find(".sk-bangumi").Each(func(_ int, section *goquery.Selection) {
			items := parseMikanList(section, base, nil)
			if len(items) == 0 {
				return
			}
			result.Weeks = append(result.Weeks, mikanWeek{WeekLabel: strings.TrimSpace(section.Children().First().Text()), Items: items})
		})
	}
	for _, week := range result.Weeks {
		result.TotalItem += len(week.Items)
	}
	return result, nil
}

func parseMikanSeasons(doc *goquery.Document) []mikanSeason {
	seasons := make([]mikanSeason, 0)
	dateSelect := doc.Find(".date-select").First()
	if dateSelect.Length() == 0 {
		return seasons
	}
	dateText := strings.TrimSpace(dateSelect.Find(".date-text").First().Text())
	dateSelect.Find(".dropdown-menu").Children().Each(func(_ int, child *goquery.Selection) {
		child.Find("li").Each(func(index int, li *goquery.Selection) {
			if index == 0 {
				return
			}
			a := li.Find("a").First()
			yearText, _ := a.Attr("data-year")
			season, _ := a.Attr("data-season")
			year, err := strconv.Atoi(yearText)
			if err != nil {
				return
			}
			label := strings.TrimSpace(yearText + " " + season)
			seasons = append(seasons, mikanSeason{Year: year, Season: season, SeasonLabel: label, Select: strings.HasPrefix(dateText, label)})
		})
	})
	return seasons
}

func parseMikanList(root *goquery.Selection, base string, existing map[string]bool) []mikanInfo {
	items := make([]mikanInfo, 0)
	zeroScore := 0.0
	root.Find("li").Each(func(_ int, li *goquery.Selection) {
		span := li.Find("span").First()
		cover, _ := span.Attr("data-src")
		anchor := li.Find("a").First()
		href, ok := anchor.Attr("href")
		if !ok {
			return
		}
		items = append(items, mikanInfo{Cover: resolveURL(base, cover), URL: resolveURL(base, href), Title: strings.TrimSpace(anchor.Text()), UpdateDate: strings.TrimSpace(li.Find(".an-info .date-text").First().Text()), Exists: existing[strings.TrimRight(mikanIDPattern.FindString(resolveURL(base, href)), "/")], Score: &zeroScore})
	})
	return items
}

func (c *Client) mikanInfo(ctx context.Context, config map[string]any, id string) (*mikanInfo, error) {
	host := c.get(ctx, config, "mikanHost", "https://mikanani.me")
	base, err := url.Parse(host)
	if err != nil {
		return nil, err
	}
	base.Path = "/Home/Bangumi/" + url.PathEscape(id)
	return c.mikanInfoFromURL(ctx, config, base.String())
}

func (c *Client) mikanGroups(ctx context.Context, rawURL string) ([]mikanGroup, error) {
	config, err := c.config(ctx)
	if err != nil {
		return nil, err
	}
	configured, err := url.Parse(c.get(ctx, config, "mikanHost", "https://mikanani.me"))
	if err != nil {
		return nil, err
	}
	requested, err := url.Parse(rawURL)
	if err != nil || requested.Host == "" || !allowedMikanHost(requested, configured) {
		return nil, errors.New("Mikan URL 必须与已配置站点同源")
	}
	info, err := c.mikanInfoFromURL(ctx, config, rawURL)
	if err != nil {
		return nil, err
	}
	return info.Groups, nil
}

func allowedMikanHost(requested, configured *url.URL) bool {
	if strings.EqualFold(requested.Host, configured.Host) && strings.EqualFold(requested.Scheme, configured.Scheme) {
		return true
	}
	if !strings.EqualFold(requested.Scheme, "https") {
		return false
	}
	host := strings.ToLower(requested.Hostname())
	for _, domain := range []string{"mikanani.me", "mikanime.tv"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func (c *Client) mikanInfoFromURL(ctx context.Context, config map[string]any, rawURL string) (*mikanInfo, error) {
	var info mikanInfo
	err := c.mikanDocument(ctx, config, "detail", rawURL, &info, func(html []byte) (any, error) {
		return parseMikanDetail(html, rawURL)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func parseMikanDetail(html []byte, rawURL string) (*mikanInfo, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return nil, err
	}
	info := &mikanInfo{URL: rawURL, Groups: []mikanGroup{}}
	if cover, ok := doc.Find(".content > img").First().Attr("src"); ok {
		info.Cover = resolveURL(origin(rawURL), cover)
	}
	info.Title = strings.TrimSpace(doc.Find(".bangumi-title").First().Text())
	doc.Find(".bangumi-info").Each(func(_ int, row *goquery.Selection) {
		if strings.TrimSpace(row.Contents().First().Text()) == "Bangumi番组计划链接：" {
			info.BgmURL, _ = row.Find("a").First().Attr("href")
		}
	})
	base := origin(rawURL)
	doc.Find(".leftbar-item").Each(func(_ int, subgroup *goquery.Selection) {
		anchor := subgroup.Find("a.subgroup-name").First()
		sectionID, _ := anchor.Attr("data-anchor")
		section := doc.Find(sectionID).First()
		rssPath, _ := section.Find(".mikan-rss").First().Attr("href")
		group := mikanGroup{SubgroupID: strings.TrimPrefix(sectionID, "#"), Label: strings.TrimSpace(anchor.Text()), RSS: resolveURL(base, rssPath), BgmURL: info.BgmURL, UpdateDay: strings.TrimSpace(subgroup.Find(".date").First().Text()), Items: []mikanItem{}}
		section.Next().Find("tbody tr").Each(func(_ int, row *goquery.Selection) {
			anchors, cells := row.Find("a"), row.Find("td")
			if anchors.Length() < 3 || cells.Length() < 4 {
				return
			}
			magnet, _ := anchors.Eq(1).Attr("data-clipboard-text")
			torrent, _ := anchors.Eq(2).Attr("href")
			group.Items = append(group.Items, mikanItem{Title: strings.TrimSpace(anchors.Eq(0).Contents().First().Text()), Magnet: magnet, FormatSize: strings.TrimSpace(cells.Eq(2).Text()), CreatedAt: parseDate(strings.TrimSpace(cells.Eq(3).Text())), Torrent: resolveURL(base, torrent)})
		})
		group.GroupRegex = deriveGroupRegex(itemTitlesMikan(group.Items))
		info.Groups = append(info.Groups, group)
	})
	return info, nil
}

func mikanRSSID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Query().Get("bangumiId")
}

func resolveURL(base, reference string) string {
	if reference == "" {
		return ""
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return reference
	}
	referenceURL, err := url.Parse(reference)
	if err != nil {
		return reference
	}
	return baseURL.ResolveReference(referenceURL).String()
}

func origin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

func parseDate(value string) *javaDate {
	for _, layout := range []string{"2006-01-02", "2006/01/02", "2006-1-2", "2006/1/2", "Jan 2, 2006"} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return &javaDate{Time: parsed}
		}
	}
	return nil
}

func itemTitlesMikan(items []mikanItem) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Title)
	}
	return result
}

func sortMikanInfo(items []mikanInfo) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score == nil {
			return false
		}
		if items[j].Score == nil {
			return true
		}
		return *items[i].Score > *items[j].Score
	})
}
