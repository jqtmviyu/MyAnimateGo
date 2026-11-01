// Package pipeline 按订阅规则读取 RSS、预览资源并将任务交给 Transmission。
package pipeline

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

// Store 提供流水线所需的持久化接口。
type Store interface {
	GetConfig(context.Context) (map[string]any, error)
	ListSubscriptions(context.Context) ([]map[string]any, error)
	PutSubscription(context.Context, map[string]any) error
	DataDir() string
}

// RSSFetcher 允许运行入口替换 RSS HTTP 读取器。
type RSSFetcher func(context.Context, string) ([]byte, error)

// NotificationEnqueuer persists a status snapshot with a caller-provided idempotency key.
type NotificationEnqueuer func(context.Context, string, map[string]any, string, string) error

// Options 设置时间源、HTTP 客户端和种子缓存目录。
type Options struct {
	RSSFetcher             RSSFetcher
	MetadataResolver       func(context.Context, map[string]any, float64) (map[string]string, error)
	EnqueueNotification    NotificationEnqueuer
	HTTPClient             *http.Client
	TransmissionHTTPClient *http.Client
	Now                    func() time.Time
	TorrentCacheDir        string
	MaxRSSBytes            int64
	MaxTorrentBytes        int64
}

// Engine 协调订阅、资源、缓存和 Transmission。
type Engine struct {
	store   Store
	options Options
	mu      sync.Mutex
}

// New 创建下载编排引擎。
func New(store Store, options Options) (*Engine, error) {
	if store == nil {
		return nil, errors.New("流水线存储不能为空")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.HTTPClient == nil {
		options.HTTPClient = directHTTPClient(30 * time.Second)
	}
	if options.MaxRSSBytes <= 0 {
		options.MaxRSSBytes = 8 << 20
	}
	if options.MaxTorrentBytes <= 0 {
		options.MaxTorrentBytes = 32 << 20
	}
	if options.TorrentCacheDir == "" {
		options.TorrentCacheDir = store.DataDir()
	}
	return &Engine{store: store, options: options}, nil
}

// Routes 返回前端使用的 RSS、下载路径与下载器查询路由。
func (e *Engine) Routes() []server.Route {
	return []server.Route{
		{Method: http.MethodPost, Path: "/api/previewAni", Handler: http.HandlerFunc(e.handlePreview)},
		{Method: http.MethodPost, Path: "/api/downloadPath", Handler: http.HandlerFunc(e.handleDownloadPath)},
		{Method: http.MethodPost, Path: "/api/refreshAni", Handler: http.HandlerFunc(e.handleRefreshAni)},
		{Method: http.MethodPost, Path: "/api/refreshAll", Handler: http.HandlerFunc(e.handleRefreshAll)},
		{Method: http.MethodPost, Path: "/api/torrentsInfos", Handler: http.HandlerFunc(e.handleTorrentsInfos)},
		{Method: http.MethodPost, Path: "/api/downloadLoginTest", Handler: http.HandlerFunc(e.handleDownloadLoginTest)},
	}
}

func (e *Engine) config(ctx context.Context) (map[string]any, error) {
	return e.store.GetConfig(ctx)
}

func (e *Engine) now() time.Time { return e.options.Now() }

func (e *Engine) cacheDir() string {
	return filepath.Join(e.options.TorrentCacheDir, "torrents")
}
