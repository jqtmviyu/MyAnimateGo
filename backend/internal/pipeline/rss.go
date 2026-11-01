package pipeline

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/rules"
)

// Item 对应原版 RSS 预览使用的资源字段。
type Item struct {
	Title         string  `json:"title"`
	ReName        string  `json:"reName"`
	Torrent       string  `json:"torrent"`
	InfoHash      string  `json:"infoHash"`
	Episode       float64 `json:"episode"`
	FormatSize    string  `json:"formatSize"`
	Length        int64   `json:"length"`
	HasDownloaded bool    `json:"hasDownloaded"`
	TorrentPath   string  `json:"torrentPath,omitempty"`
	Master        bool    `json:"master"`
	Subgroup      string  `json:"subgroup"`
	PubDate       *string `json:"pubDate,omitempty"`
	pubTime       *time.Time
}

type xmlElement struct {
	XMLName  xml.Name
	Attrs    []xml.Attr   `xml:",any,attr"`
	Text     string       `xml:",chardata"`
	Children []xmlElement `xml:",any"`
}

var (
	hashTextPattern      = regexp.MustCompile(`(?i)^([a-f0-9]{40})$`)
	magnetHashPattern    = regexp.MustCompile(`(?i)urn:btih:([a-z0-9]{8,})`)
	seasonEpisodePattern = regexp.MustCompile(`(?i)S(\d+)E(\d+(?:\.5)?)`)
)

func (e *Engine) items(ctx context.Context, config, subscription map[string]any) ([]Item, error) {
	mainGroup := valueString(subscription["subgroup"])
	if mainGroup == "" {
		mainGroup = "未知字幕组"
	}
	items, err := e.feedItems(ctx, config, subscription, valueString(subscription["url"]), mainGroup, valueInt(subscription["offset"]), true)
	if err != nil {
		return nil, err
	}
	if !valueBool(config["standbyRss"]) {
		sortItems(items)
		return items, nil
	}
	standby, _ := subscription["standbyRssList"].([]any)
	for _, raw := range standby {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		group := valueString(entry["label"])
		if group == "" {
			group = "未知字幕组"
		}
		feed, err := e.feedItems(ctx, config, subscription, valueString(entry["url"]), group, valueInt(entry["offset"]), false)
		if err != nil {
			return nil, fmt.Errorf("读取备用 RSS 失败：%w", err)
		}
		items = append(items, feed...)
	}
	coexist := valueBool(config["coexist"])
	seen := make(map[string]bool, len(items))
	merged := make([]Item, 0, len(items))
	for _, item := range items {
		key := strconv.FormatFloat(item.Episode, 'f', -1, 64)
		if coexist {
			key = item.ReName
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, item)
	}
	sortItems(merged)
	return merged, nil
}

func (e *Engine) feedItems(ctx context.Context, config, subscription map[string]any, feedURL, subgroup string, offset int, master bool) ([]Item, error) {
	if strings.TrimSpace(feedURL) == "" {
		return nil, errors.New("RSS URL 不能为空")
	}
	body, err := e.fetchRSS(ctx, config, feedURL)
	if err != nil {
		return nil, err
	}
	var document xmlElement
	if err := xml.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("解析 RSS XML 失败：%w", err)
	}
	channel := child(document, "channel")
	if channel == nil {
		return nil, errors.New("RSS 缺少 channel 节点")
	}
	var sourceItems []xmlElement
	for _, item := range channel.Children {
		if strings.EqualFold(item.XMLName.Local, "item") {
			sourceItems = append(sourceItems, item)
		}
	}
	items := make([]Item, 0, len(sourceItems))
	for i := len(sourceItems) - 1; i >= 0; i-- {
		item, ok, err := e.parseItem(subscription, sourceItems[i], subgroup, offset, master)
		if err != nil {
			return nil, err
		}
		if ok {
			items = append(items, item)
		}
	}
	return e.filterAndRename(ctx, config, subscription, subgroup, offset, items)
}

func (e *Engine) fetchRSS(ctx context.Context, config map[string]any, feedURL string) ([]byte, error) {
	if e.options.RSSFetcher != nil {
		return e.options.RSSFetcher(ctx, feedURL)
	}
	timeout := time.Duration(valueInt(config["rssTimeout"])) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	client := e.rssHTTPClientForURL(config, timeout, feedURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 RSS 请求失败：%w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("请求 RSS 失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("RSS 返回 HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, e.options.MaxRSSBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取 RSS 失败：%w", err)
	}
	if int64(len(body)) > e.options.MaxRSSBytes {
		return nil, errors.New("RSS 内容超过大小限制")
	}
	if len(bytesTrimSpace(body)) == 0 || bytesTrimSpace(body)[0] != '<' {
		return nil, errors.New("RSS 内容不是 XML")
	}
	return body, nil
}

func (e *Engine) rssHTTPClientForURL(config map[string]any, timeout time.Duration, target string) *http.Client {
	client := *e.options.HTTPClient
	client.Timeout = timeout
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	} else {
		transport = transport.Clone()
	}
	if !proxyMatches(config, target) {
		transport.Proxy = nil
		client.Transport = transport
		return &client
	}
	proxyHost := valueString(config["proxyHost"])
	if !strings.Contains(proxyHost, "://") {
		proxyHost = "http://" + proxyHost
	}
	if valueInt(config["proxyPort"]) > 0 {
		parsed, err := url.Parse(proxyHost)
		if err == nil && parsed.Port() == "" {
			parsed.Host = net.JoinHostPort(parsed.Hostname(), strconv.Itoa(valueInt(config["proxyPort"])))
			proxyHost = parsed.String()
		}
	}
	proxyURL, err := url.Parse(proxyHost)
	if err == nil {
		if user, password := valueString(config["proxyUsername"]), valueString(config["proxyPassword"]); user != "" && password != "" {
			proxyURL.User = url.UserPassword(user, password)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
		client.Transport = transport
	}
	return &client
}

func directHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Timeout: timeout, Transport: transport}
}

func proxyMatches(config map[string]any, target string) bool {
	if !valueBool(config["proxy"]) || valueString(config["proxyHost"]) == "" || valueInt(config["proxyPort"]) <= 0 {
		return false
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, entry := range strings.Split(valueString(config["proxyList"]), "\n") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if host == entry || strings.HasSuffix(host, "."+entry) {
			return true
		}
	}
	return false
}

func (e *Engine) parseItem(subscription map[string]any, node xmlElement, subgroup string, offset int, master bool) (Item, bool, error) {
	title := strings.TrimSpace(textChild(node, "title"))
	torrent, length, size, infoHash := "", int64(0), "0MiB", ""
	var pubDate *time.Time
	for _, item := range node.Children {
		switch strings.ToLower(item.XMLName.Local) {
		case "enclosure":
			torrent = attr(item, "url")
			if parsed, err := strconv.ParseInt(attr(item, "length"), 10, 64); err == nil {
				length = parsed
			} else if torrent != "" {
				length = 1
			}
			if hash := hashFromMagnet(torrent); hash != "" {
				infoHash = hash
			}
		case "guid":
			guid := strings.TrimSpace(item.Text)
			if hashTextPattern.MatchString(guid) {
				infoHash = guid
			}
		case "infohash":
			infoHash = strings.TrimSpace(item.Text)
		case "size":
			size = strings.TrimSpace(item.Text)
		case "pubdate":
			if parsed, err := parseDate(item.Text); err == nil {
				pubDate = &parsed
			}
		case "torrent":
			if nested := child(item, "infohash"); nested != nil {
				infoHash = strings.TrimSpace(nested.Text)
			}
			if nested := child(item, "pubDate"); nested != nil && pubDate == nil {
				if parsed, err := parseDate(nested.Text); err == nil {
					pubDate = &parsed
				}
			}
			if nested := child(item, "contentLength"); nested != nil && length == 0 {
				length, _ = strconv.ParseInt(strings.TrimSpace(nested.Text), 10, 64)
			}
			if nested := child(item, "magneturi"); nested != nil && torrent == "" {
				torrent = strings.TrimSpace(nested.Text)
			}
		case "link":
			link := strings.TrimSpace(item.Text)
			if strings.HasSuffix(link, ".torrent") {
				torrent = link
			}
		}
	}
	if torrent == "" {
		return Item{}, false, nil
	}
	if infoHash == "" {
		infoHash = hashFromTorrentURL(torrent)
	}
	if decoded, err := url.QueryUnescape(infoHash); err == nil {
		infoHash = decoded
	}
	infoHash = strings.ToLower(infoHash)
	if size == "0MiB" {
		size = formatSize(length)
	}
	item := Item{Title: title, Torrent: torrent, InfoHash: infoHash, Length: length, FormatSize: size, Subgroup: subgroup, Master: master}
	if pubDate != nil {
		formatted := pubDate.In(time.Local).Format("2006-01-02 15:04:05")
		item.PubDate = &formatted
		item.pubTime = pubDate
	}
	return item, true, nil
}

func (e *Engine) filterAndRename(ctx context.Context, config, subscription map[string]any, subgroup string, offset int, items []Item) ([]Item, error) {
	filters := stringSlice(subscription["exclude"])
	matches := stringSlice(subscription["match"])
	globalFilters := stringSlice(config["exclude"])
	filtered := make([]Item, 0, len(items))
	positions := make(map[string]int)
	for _, item := range items {
		skip := false
		for _, pattern := range filters {
			pattern, applies := scopedPattern(pattern, subgroup)
			if !applies || pattern == "" {
				continue
			}
			matched, err := titleMatches(pattern, item.Title)
			if err != nil {
				return nil, err
			}
			if matched {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		for _, pattern := range matches {
			pattern, applies := scopedPattern(pattern, subgroup)
			if !applies || pattern == "" {
				continue
			}
			matched, err := titleMatches(pattern, item.Title)
			if err != nil {
				return nil, err
			}
			if !matched {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if valueBool(subscription["globalExclude"]) {
			for _, pattern := range globalFilters {
				pattern, applies := scopedPattern(pattern, subgroup)
				if !applies || pattern == "" {
					continue
				}
				matched, err := titleMatches(pattern, item.Title)
				if err != nil {
					return nil, err
				}
				if matched {
					skip = true
					break
				}
			}
		}
		if skip {
			continue
		}
		input := renameInput(config, subscription, offset)
		initialEpisode := 1.0
		ruleItem := rules.Item{Title: item.Title, Subgroup: item.Subgroup, Master: item.Master, Episode: &initialEpisode}
		if !input.OVA && e.options.MetadataResolver != nil && needsMetadata(input) {
			probe := input
			probe.Template = "${title}"
			parsed, accepted, err := rules.Rename(probe, ruleItem)
			if err != nil {
				return nil, err
			}
			if !accepted || parsed.Episode == nil {
				continue
			}
			metadata, err := e.options.MetadataResolver(ctx, subscription, *parsed.Episode)
			if err != nil {
				return nil, fmt.Errorf("读取命名元数据失败：%w", err)
			}
			for key, value := range metadata {
				input.Metadata[key] = value
			}
		}
		ruleItem, accepted, err := rules.Rename(input, ruleItem)
		if err != nil {
			return nil, err
		}
		if !accepted || ruleItem.Episode == nil {
			continue
		}
		item.ReName = ruleItem.Name
		item.Episode = *ruleItem.Episode
		key := strconv.FormatFloat(item.Episode, 'f', -1, 64)
		if position, exists := positions[key]; exists {
			filtered[position] = item
			continue
		}
		positions[key] = len(filtered)
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func needsMetadata(input rules.RenameInput) bool {
	for _, key := range []string{"episodeTitle", "bgmEpisodeTitle", "bgmJpEpisodeTitle", "jpTitle", "bgmId"} {
		if strings.Contains(input.Template, "${"+key+"}") && input.Metadata[key] == "" {
			return true
		}
	}
	return false
}

func sortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Episode < items[j].Episode })
}

func scopedPattern(pattern, subgroup string) (string, bool) {
	if !strings.HasPrefix(pattern, "{{") {
		return pattern, true
	}
	separator := strings.Index(pattern, "}}:")
	if separator < 2 {
		return pattern, true
	}
	if pattern[2:separator] != subgroup {
		return "", false
	}
	return pattern[separator+3:], true
}

func titleMatches(pattern, title string) (bool, error) {
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return false, fmt.Errorf("匹配规则 %q 无法解析：%w", pattern, err)
	}
	return compiled.MatchString(title), nil
}

func child(node xmlElement, name string) *xmlElement {
	for i := range node.Children {
		if strings.EqualFold(node.Children[i].XMLName.Local, name) {
			return &node.Children[i]
		}
	}
	return nil
}

func textChild(node xmlElement, name string) string {
	if found := child(node, name); found != nil {
		return strings.TrimSpace(found.Text)
	}
	return ""
}

func attr(node xmlElement, name string) string {
	for _, item := range node.Attrs {
		if strings.EqualFold(item.Name.Local, name) {
			return item.Value
		}
	}
	return ""
}

func parseDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, time.RFC3339, "Mon, 2 Jan 2006 15:04:05 MST", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("日期格式不支持")
}

func hashFromMagnet(value string) string {
	if match := magnetHashPattern.FindStringSubmatch(value); len(match) > 1 {
		return match[1]
	}
	return ""
}

func hashFromTorrentURL(value string) string {
	if hash := hashFromMagnet(value); hash != "" {
		return hash
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	name := strings.TrimSuffix(path.Base(parsed.Path), ".torrent")
	if len(name) == 40 {
		if _, err := hex.DecodeString(name); err == nil {
			return strings.ToLower(name)
		}
	}
	return ""
}

func formatSize(size int64) string {
	if size < 0 {
		size = 0
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value, unit := float64(size), 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return fmt.Sprintf("%.2f %s", value, units[unit])
}

func bytesTrimSpace(value []byte) []byte { return bytes.TrimSpace(value) }
