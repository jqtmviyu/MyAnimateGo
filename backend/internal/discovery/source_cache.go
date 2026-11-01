package discovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var mikanHTMLPattern = regexp.MustCompile(`(?i)<(?:html|ul|div)`)

type documentEntry struct {
	body          []byte
	expires, used time.Time
}
type sourceRefreshKey struct{}

func configFingerprint(config map[string]any, keys ...string) string {
	values := make([]any, 0, len(keys))
	for _, key := range keys {
		values = append(values, config[key])
	}
	raw, _ := json.Marshal(values)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func transportFingerprint(config map[string]any) string {
	return configFingerprint(config, "proxy", "proxyHost", "proxyPort", "proxyUsername", "proxyPassword", "proxyList")
}
func (c *Client) sourceClient(config map[string]any) Doer {
	if c.doer != nil {
		return c.doer
	}
	key := transportFingerprint(config)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.httpClient != nil && c.httpClientKey == key {
		return c.httpClient
	}
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = proxyFor(config, strings.Split(text(config["proxyList"]), "\n"))
	c.httpClient = &http.Client{Transport: tr, Timeout: 20 * time.Second}
	c.httpClientKey = key
	return c.httpClient
}
func sourceKey(config map[string]any, resource string) string {
	return resource + ":" + transportFingerprint(config) + ":" + configFingerprint(config, "bgmApi", "bgmToken", "cacheBaseURL", "tmdbApi", "tmdbApiKey", "tmdbLanguage", "tmdbRomaji")
}
func (c *Client) sourceBytes(ctx context.Context, key string, ttl time.Duration, fetch func(context.Context) ([]byte, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	force, _ := ctx.Value(sourceRefreshKey{}).(bool)
	get := func() ([]byte, bool) {
		c.mu.Lock()
		defer c.mu.Unlock()
		entry, ok := c.documents[key]
		if !ok || !time.Now().Before(entry.expires) {
			return nil, false
		}
		entry.used = time.Now()
		c.documents[key] = entry
		return entry.body, true
	}
	if !force {
		if body, ok := get(); ok {
			return bytes.Clone(body), nil
		}
	}
	result := c.documentFlights.DoChan(key, func() (any, error) {
		if !force {
			if body, ok := get(); ok {
				return body, nil
			}
		}
		// 同一来源的并发读取合并；一个页面取消不会中断其他等待者，外部读取最多 20 秒。
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		body, err := fetch(work)
		if err != nil {
			return nil, err
		}
		now := time.Now()
		c.mu.Lock()
		c.expireCachesLocked(now)
		if old, ok := c.documents[key]; ok {
			delete(c.documents, key)
			c.documentBytes -= len(old.body)
		}
		const limit = 16 << 20
		if len(body) <= limit {
			for len(c.documents) >= 128 || c.documentBytes+len(body) > limit {
				var oldest string
				var used time.Time
				for k, entry := range c.documents {
					if oldest == "" || entry.used.Before(used) {
						oldest, used = k, entry.used
					}
				}
				c.documentBytes -= len(c.documents[oldest].body)
				delete(c.documents, oldest)
			}
			c.documents[key] = documentEntry{body: bytes.Clone(body), expires: now.Add(ttl), used: now}
			c.documentBytes += len(body)
			c.scheduleCacheExpiryLocked(ttl)
		}
		c.mu.Unlock()
		return body, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case out := <-result:
		if out.Err != nil {
			return nil, out.Err
		}
		return bytes.Clone(out.Val.([]byte)), nil
	}
}

// 只缓存提取后的数据；HTML 和 DOM 在本次解析后即可回收。
func (c *Client) mikanDocument(ctx context.Context, config map[string]any, kind, rawURL string, target any, parse func([]byte) (any, error)) error {
	raw, err := c.sourceBytes(ctx, sourceKey(config, "mikan:"+kind+":"+rawURL), 90*time.Second, func(work context.Context) ([]byte, error) {
		status, body, err := c.requestText(work, config, http.MethodGet, rawURL, nil, nil)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("Mikan 返回 HTTP %d", status)
		}
		if !mikanHTMLPattern.Match(body) {
			return nil, fmt.Errorf("Mikan 响应不是 HTML")
		}
		result, err := parse(body)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	})
	if err != nil {
		return err
	}
	// 每个调用方获得独立对象，订阅状态、评分和规则修改不会污染其他请求。
	return json.Unmarshal(raw, target)
}
