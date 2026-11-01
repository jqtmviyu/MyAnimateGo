// Package filesapi preserves the image, backup, and log HTTP surface.
package filesapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

type Store interface {
	server.Store
	DataDir() string
	SnapshotDB(context.Context, string) error
	RestoreSnapshot(context.Context, string) error
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type HostResolver func(context.Context, string) ([]net.IP, error)

// SnapshotBarrier blocks all application writes to SQLite or backed-up assets until release is called.
type SnapshotBarrier func(context.Context) (release func(), err error)

type LogEntry struct {
	Timestamp int64  `json:"ts"`
	Message   string `json:"message"`
	Level     string `json:"level"`
	Logger    string `json:"loggerName"`
	Thread    string `json:"threadName"`
}
type Options struct {
	Store           Store
	HTTPDoer        HTTPDoer
	ResolveHost     HostResolver
	Now             func() time.Time
	MaxUpload       int64
	LogCapacity     int
	SnapshotBarrier SnapshotBarrier
	OnRestore       func()
}

type Service struct {
	store   Store
	options Options
	mu      sync.Mutex
	logs    []LogEntry
}

func New(options Options) (*Service, error) {
	if options.Store == nil {
		return nil, errors.New("文件服务存储不能为空")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MaxUpload <= 0 {
		options.MaxUpload = 256 << 20
	}
	if options.LogCapacity <= 0 {
		options.LogCapacity = 1000
	}
	if options.ResolveHost == nil {
		options.ResolveHost = func(ctx context.Context, host string) ([]net.IP, error) {
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			ips := make([]net.IP, 0, len(addresses))
			for _, address := range addresses {
				ips = append(ips, address.IP)
			}
			return ips, nil
		}
	}
	return &Service{store: options.Store, options: options, logs: make([]LogEntry, 0, options.LogCapacity)}, nil
}

func (s *Service) Routes() []server.Route {
	return []server.Route{
		{Method: http.MethodPost, Path: "/api/upload", Handler: http.HandlerFunc(s.upload)},
		{Method: http.MethodPost, Path: "/api/uploadAndReadToBase64", Handler: http.HandlerFunc(s.uploadBase64)},
		{Method: http.MethodPost, Path: "/api/uploadAndRead", Handler: http.HandlerFunc(s.uploadText)},
		{Method: http.MethodGet, Path: "/api/file", Handler: http.HandlerFunc(s.file)},
		{Method: http.MethodGet, Path: "/api/proxyImage", Handler: http.HandlerFunc(s.proxyImage)},
		{Method: http.MethodPost, Path: "/api/logs", Handler: http.HandlerFunc(s.listLogs)},
		{Method: http.MethodPost, Path: "/api/clearLogs", Handler: http.HandlerFunc(s.clearLogs)},
		{Method: http.MethodGet, Path: "/api/downloadLogs", Handler: http.HandlerFunc(s.downloadLogs)},
		{Method: http.MethodGet, Path: "/api/exportBackup", Handler: http.HandlerFunc(s.exportBackup)},
		{Method: http.MethodPost, Path: "/api/importBackup", Handler: http.HandlerFunc(s.importBackup)},
		{Method: http.MethodPost, Path: "/api/clearCache", Handler: http.HandlerFunc(s.clearCache)},
		{Method: http.MethodPost, Path: "/api/deleteTorrent", Handler: http.HandlerFunc(s.deleteTorrentCache)},
	}
}

// AppendLog lets the runtime attach its bounded log stream to the compatible API.
func (s *Service) AppendLog(entry LogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.Timestamp == 0 {
		entry.Timestamp = s.options.Now().UnixMilli()
	}
	if len(s.logs) == s.options.LogCapacity {
		copy(s.logs, s.logs[1:])
		s.logs = s.logs[:len(s.logs)-1]
	}
	s.logs = append(s.logs, entry)
}

func (s *Service) doerFor(ctx context.Context, host string) (HTTPDoer, error) {
	if s.options.HTTPDoer != nil {
		if client, ok := s.options.HTTPDoer.(*http.Client); ok {
			copy := *client
			copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			return &copy, nil
		}
		return s.options.HTTPDoer, nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	config, err := s.store.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	proxyEnabled, _ := config["proxy"].(bool)
	proxyHost := strings.TrimSpace(stringValue(config["proxyHost"]))
	proxyPort := integer(config["proxyPort"])
	requestHost := strings.ToLower(host)
	allowed := false
	for _, candidate := range strings.Split(stringValue(config["proxyList"]), "\n") {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate != "" && (requestHost == candidate || strings.HasSuffix(requestHost, "."+candidate)) {
			allowed = true
			break
		}
	}
	if proxyEnabled && proxyHost != "" && proxyPort > 0 && allowed {
		proxyURL := &url.URL{Scheme: "http", Host: net.JoinHostPort(proxyHost, strconv.Itoa(proxyPort))}
		if username := stringValue(config["proxyUsername"]); username != "" {
			proxyURL.User = url.UserPassword(username, stringValue(config["proxyPassword"]))
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

func (s *Service) freezeWrites(ctx context.Context) (func(), error) {
	if s.options.SnapshotBarrier == nil {
		return nil, errors.New("备份与恢复需要应用级业务写屏障")
	}
	release, err := s.options.SnapshotBarrier(ctx)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, errors.New("应用级业务写屏障没有提供释放回调")
	}
	return release, nil
}
