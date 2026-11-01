// Package discovery 对外部番剧目录提供可测试的 HTTP 客户端和兼容路由。
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

// Doer 是客户端网络接缝；测试可注入 httptest.Server.Client。
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Entitlement 保留旧构造接口兼容性；公开目录元数据不再依赖捐赠校验。
type Entitlement interface {
	Verify(context.Context, map[string]any) (bool, error)
}

type cacheEntry struct {
	value   any
	expires time.Time
	used    time.Time
}

// Client 通过配置和订阅仓储读取调用所需数据，绝不从进程环境推断账号凭据。
type Client struct {
	store                 server.Store
	doer                  Doer
	subscriptionPreviewer SubscriptionPreviewer
	mu                    sync.Mutex
	cache                 map[string]cacheEntry
	documents             map[string]documentEntry
	documentBytes         int
	cacheTimer            *time.Timer
	cacheClosed           bool
	documentFlights       singleflight.Group
	httpClient            *http.Client
	httpClientKey         string
}

func New(store server.Store, doer Doer, _ Entitlement) *Client {
	return &Client{store: store, doer: doer, cache: map[string]cacheEntry{}, documents: map[string]documentEntry{}}
}

// SetSubscriptionPreviewer 注入 RSS 预览器，让 rssToAni 与 MCP 共用偏移和字幕组推断。
func (c *Client) SetSubscriptionPreviewer(previewer SubscriptionPreviewer) {
	c.subscriptionPreviewer = previewer
}

func (c *Client) config(ctx context.Context) (map[string]any, error) {
	if c.store == nil {
		return nil, errors.New("资源检索尚未配置存储")
	}
	return c.store.GetConfig(ctx)
}

func (c *Client) subscriptions(ctx context.Context) ([]map[string]any, error) {
	if c.store == nil {
		return nil, errors.New("资源检索尚未配置存储")
	}
	return c.store.ListSubscriptions(ctx)
}

func (c *Client) get(ctx context.Context, config map[string]any, key string, fallback string) string {
	if value := strings.TrimSpace(text(config[key])); value != "" {
		return strings.TrimRight(value, "/")
	}
	return fallback
}

func (c *Client) do(ctx context.Context, config map[string]any, method, rawURL string, headers http.Header, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "backend")
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	client := c.sourceClient(config)
	return client.Do(req)
}

func proxyFor(config map[string]any, allowlist []string) func(*http.Request) (*url.URL, error) {
	proxyOn, _ := config["proxy"].(bool)
	if !proxyOn {
		return nil
	}
	host := strings.TrimSpace(text(config["proxyHost"]))
	port := integer(config["proxyPort"])
	if host == "" || port < 1 {
		return nil
	}
	proxyURL := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port))}
	if user := text(config["proxyUsername"]); user != "" {
		proxyURL.User = url.UserPassword(user, text(config["proxyPassword"]))
	}
	return func(req *http.Request) (*url.URL, error) {
		requestHost := strings.ToLower(req.URL.Hostname())
		for _, candidate := range allowlist {
			candidate = strings.ToLower(strings.TrimSpace(candidate))
			if candidate != "" && (requestHost == candidate || strings.HasSuffix(requestHost, "."+candidate)) {
				return proxyURL, nil
			}
		}
		return nil, nil
	}
}

func (c *Client) request(ctx context.Context, config map[string]any, method, rawURL string, headers http.Header, body io.Reader, target any) (*http.Response, error) {
	response, err := c.do(ctx, config, method, rawURL, headers, body)
	if err != nil {
		return nil, fmt.Errorf("请求外部资源失败：%w", err)
	}
	defer response.Body.Close()
	if target == nil {
		return response, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return response, fmt.Errorf("外部服务返回 HTTP %d：%s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(target); err != nil {
		return response, fmt.Errorf("解析外部服务 JSON 失败：%w", err)
	}
	return response, nil
}

func (c *Client) requestText(ctx context.Context, config map[string]any, method, rawURL string, headers http.Header, body io.Reader) (int, []byte, error) {
	response, err := c.do(ctx, config, method, rawURL, headers, body)
	if err != nil {
		return 0, nil, fmt.Errorf("请求外部资源失败：%w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	return response.StatusCode, data, err
}

// Token 仅用于资料请求认证；留空时使用匿名请求。
func (c *Client) bgmHeaders(config map[string]any) http.Header {
	token := strings.TrimSpace(text(config["bgmToken"]))
	if token == "" {
		return nil
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	return headers
}

func (c *Client) cached(key string, ttl time.Duration, fetch func() (any, error)) (any, error) {
	c.mu.Lock()
	entry, ok := c.cache[key]
	if ok && time.Now().Before(entry.expires) {
		entry.used = time.Now()
		c.cache[key] = entry
		c.mu.Unlock()
		return entry.value, nil
	}
	c.mu.Unlock()
	value, err := fetch()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	now := time.Now()
	c.expireCachesLocked(now)
	if _, exists := c.cache[key]; !exists && len(c.cache) >= 128 {
		var oldest string
		var used time.Time
		for key, entry := range c.cache {
			if oldest == "" || entry.used.Before(used) {
				oldest, used = key, entry.used
			}
		}
		delete(c.cache, oldest)
	}
	c.cache[key] = cacheEntry{value: value, expires: now.Add(ttl), used: now}
	c.scheduleCacheExpiryLocked(ttl)
	c.mu.Unlock()
	return value, nil
}

func text(value any) string  { s, _ := value.(string); return s }
func boolean(value any) bool { b, _ := value.(bool); return b }
func integer(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		n, _ := number.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(number)
		return n
	}
	return 0
}
func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}
