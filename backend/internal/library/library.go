// Package library 管理订阅封面和已确认媒体目录中的文件整理。
package library

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/discovery"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

// Store 定义读取业务数据和附件根目录所需的持久化边界。
type Store interface {
	server.Store
	DataDir() string
}

type DownloadPathResolver func(context.Context, map[string]any) (map[string]any, error)
type ImageFetcher func(context.Context, string) ([]byte, error)

// TaskManager 在更改订阅/媒体目录前对下载器任务执行同一迁移。
type TaskManager interface {
	MoveSubscription(context.Context, map[string]any, string) error
	DeleteSubscription(context.Context, map[string]any, bool) error
}

// Options 允许 HTTP 层注入 pipeline 路径和订阅封面下载器。
type Options struct {
	DownloadPath           DownloadPathResolver
	ImageFetcher           ImageFetcher
	CoverImage             ImageFetcher
	Tasks                  TaskManager
	HTTPDoer               discovery.Doer
	TransmissionHTTPClient *http.Client
	Entitlement            discovery.Entitlement
	Now                    func() time.Time
	MaxImageSize           int64
}

type Service struct {
	store   Store
	options Options
}

// New 将服务接到当前 SQLite、pipeline 路径规则和 discovery HTTP 客户端。
func New(store Store, options Options) (*Service, error) {
	if store == nil {
		return nil, errors.New("媒体服务存储不能为空")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MaxImageSize <= 0 {
		options.MaxImageSize = 25 << 20
	}
	provider := discovery.New(store, options.HTTPDoer, options.Entitlement)
	if options.CoverImage == nil {
		options.CoverImage = options.ImageFetcher
		if options.CoverImage == nil {
			options.CoverImage = provider.FetchImageURL
		}
	}
	if options.DownloadPath == nil {
		engine, err := pipeline.New(store, pipeline.Options{Now: options.Now})
		if err != nil {
			return nil, err
		}
		options.DownloadPath = engine.DownloadPath
	}
	if options.Tasks == nil {
		options.Tasks = &transmissionTaskManager{store: store, downloadPath: options.DownloadPath, httpClient: options.TransmissionHTTPClient}
	}
	return &Service{store: store, options: options}, nil
}

func (s *Service) config(ctx context.Context) (map[string]any, error) { return s.store.GetConfig(ctx) }

func (s *Service) resolvedPath(ctx context.Context, subscription map[string]any) (string, error) {
	resolved, err := s.options.DownloadPath(ctx, subscription)
	if err != nil {
		return "", err
	}
	raw, _ := resolved["downloadPath"].(string)
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("订阅下载路径为空")
	}
	path, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("解析下载路径失败：%w", err)
	}
	path = filepath.Clean(path)
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("订阅下载路径不能是符号链接")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	path, err = canonicalPath(path)
	if err != nil {
		return "", fmt.Errorf("订阅下载路径无效：%w", err)
	}
	return path, nil
}
