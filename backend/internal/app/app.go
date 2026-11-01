// Package app 接通 HTTP、存储、数据迁移与单实例运行边界。
package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/activity"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/apidocs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/collections"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/desktop"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/discovery"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/filesapi"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/instance"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/library"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/mcpapi"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/migration"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/notifications"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/peerfilter"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/scheduler"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/settings"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/webui"
)

const defaultListenAddress = "0.0.0.0:7789"

var errRestart = errors.New("服务请求重启")

// Run 根据命令行参数执行服务、迁移预览或数据导入，并在上下文取消后停止服务。
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	command := "serve"
	if len(args) > 0 && (args[0] == "serve" || args[0] == "preview" || args[0] == "import") {
		command, args = args[0], args[1:]
	}
	dataDefault := os.Getenv("ANI_RSS_DATA_DIR")
	if dataDefault == "" {
		dataDefault = "data"
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {
		syntax := "myanimego [serve] [-data-dir 目录] [-listen 地址] [-web-dir 目录] [--gui] [-dev]"
		if command == "preview" {
			syntax = "myanimego preview -source 旧数据目录或ZIP文件"
		} else if command == "import" {
			syntax = "myanimego import -source 旧数据目录或ZIP文件 -data-dir 目标目录"
		}
		fmt.Fprintf(stderr, "用法：%s\n", syntax)
		flags.PrintDefaults()
	}
	dataDir := flags.String("data-dir", dataDefault, "SQLite 数据和导入附件的目录")
	source := flags.String("source", "", "旧版数据目录或 ZIP 文件")
	listen := flags.String("listen", defaultListenAddress, "HTTP 监听地址")
	webDir := flags.String("web-dir", "", "覆盖内嵌前端的外部静态目录，用于本地调试")
	development := flags.Bool("dev", false, "开发模式：提供自动任务暂停与迁移审核入口")
	gui := flags.Bool("gui", false, "显示系统托盘及 WebUI、数据目录和退出菜单")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(stderr)
			flags.Usage()
			return nil
		}
		return errors.New("命令参数错误，请检查参数名称和取值")
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("存在多余参数：%v", flags.Args())
	}
	switch command {
	case "preview":
		if *source == "" {
			return errors.New("preview 命令需要 -source 参数")
		}
		report, err := migration.Preview(*source)
		if err != nil {
			return fmt.Errorf("预览旧数据失败：%w", err)
		}
		if err := writeJSON(stdout, report); err != nil {
			return fmt.Errorf("输出预览报告失败：%w", err)
		}
		return nil
	case "import":
		if *source == "" {
			return errors.New("import 命令需要 -source 参数")
		}
		return importLegacy(ctx, *source, *dataDir, stdout)
	case "serve":
		run := func(ctx context.Context, ready func(string)) error {
			for {
				err := serve(ctx, *dataDir, *listen, *webDir, *development, stdout, ready)
				if !errors.Is(err, errRestart) {
					return err
				}
				if ctx.Err() != nil {
					return nil
				}
			}
		}
		if *gui {
			return desktop.Run(ctx, run, *dataDir, stderr)
		}
		return run(ctx, func(string) {})
	default:
		return fmt.Errorf("未知命令 %q", command)
	}
}

func serve(ctx context.Context, dataDir, address, webDir string, development bool, events io.Writer, ready func(string)) (result error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("创建数据目录失败：%w", err)
	}
	lock, err := instance.Acquire(filepath.Join(dataDir, "instance.lock"))
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, lock.Close()) }()
	persistent, err := storage.Open(filepath.Join(dataDir, "state.db"))
	if err != nil {
		return fmt.Errorf("打开 SQLite 数据库失败：%w", err)
	}
	store := &runtimeStore{Store: persistent, development: development}
	defer func() { result = errors.Join(result, store.Close()) }()
	api, err := server.New(ctx, store)
	if err != nil {
		return fmt.Errorf("初始化 HTTP 服务失败：%w", err)
	}
	stopRequests := make(chan bool, 1)
	var stopOnce sync.Once
	administration := settings.New(settings.Options{Store: store, WhitelistTest: server.WhitelistMatches, Stop: func(restart bool) error {
		accepted := false
		stopOnce.Do(func() { accepted = true; stopRequests <- restart })
		if !accepted {
			return errors.New("服务正在停止或重启")
		}
		return nil
	}})
	catalog := discovery.New(store, nil, administration)
	defer catalog.Close()
	var notices *notifications.Service
	engine, err := pipeline.New(store, pipeline.Options{MetadataResolver: catalog.Metadata, EnqueueNotification: func(ctx context.Context, key string, ani map[string]any, text, status string) error {
		_, err := notices.Enqueue(ctx, key, ani, text, status)
		return err
	}})
	if err != nil {
		return fmt.Errorf("初始化 RSS 编排失败：%w", err)
	}
	catalog.SetSubscriptionPreviewer(engine)
	media, err := library.New(store, library.Options{DownloadPath: engine.DownloadPath, CoverImage: catalog.FetchImageURL, Entitlement: administration})
	if err != nil {
		return fmt.Errorf("初始化媒体服务失败：%w", err)
	}
	api.SetSubscriptionCoverImporter(media.RefreshCover)
	writeGate := activity.New()
	var filter *peerfilter.Service
	resources, err := filesapi.New(filesapi.Options{Store: store, SnapshotBarrier: writeGate.Freeze, OnRestore: func() {
		if filter != nil {
			filter.Reload()
		}
	}})
	if err != nil {
		return fmt.Errorf("初始化文件服务失败：%w", err)
	}
	notices, err = notifications.New(store, notifications.Options{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		DownloadPath: func(ctx context.Context, ani map[string]any) (string, error) {
			result, err := engine.DownloadPath(ctx, ani)
			if err != nil {
				return "", err
			}
			path, _ := result["downloadPath"].(string)
			return path, nil
		},
		Metadata: func(ctx context.Context, ani map[string]any) (map[string]string, error) {
			return catalog.Metadata(ctx, ani, 0)
		},
		EpisodeTitles: func(ctx context.Context, ani map[string]any, episode int) (map[string]string, error) {
			return catalog.Metadata(ctx, ani, float64(episode))
		},
	})
	if err != nil {
		return fmt.Errorf("初始化通知服务失败：%w", err)
	}
	collection, err := collections.New(collections.Options{Enqueuer: store, Config: store.GetConfig, DownloadPath: func(ctx context.Context, ani map[string]any) (string, error) {
		resolved, err := engine.DownloadPath(ctx, ani)
		if err != nil {
			return "", err
		}
		path, _ := resolved["downloadPath"].(string)
		return path, nil
	}})
	if err != nil {
		return fmt.Errorf("初始化合集服务失败：%w", err)
	}
	handlers := map[string]jobs.Handler{}
	handlers[collections.JobKind] = collection.Handler()
	handlers[server.JobSubscriptionEffects] = subscriptionEffectsHandler{media: media}
	handlers[notifications.JobKind] = notices.Handler()
	completion := completionNotifier{store: store, notices: notices}
	for kind, handler := range catalog.MetadataJobHandlers() {
		handlers[kind] = handler
	}
	api.SetSubscriptionPathResolver(func(ctx context.Context, ani map[string]any) (string, error) {
		resolved, err := engine.DownloadPath(ctx, ani)
		if err != nil {
			return "", err
		}
		path, _ := resolved["downloadPath"].(string)
		return path, nil
	})
	tasks, err := scheduler.New(store, scheduler.Options{Engine: engine, Handlers: handlers, OnError: func(err error) {
		resources.AppendLog(filesapi.LogEntry{Level: "ERROR", Logger: "scheduler", Message: err.Error()})
	}, Guard: func(ctx context.Context, kind string) (func(), error) {
		release, err := writeGate.Shared(ctx)
		if err != nil {
			return nil, err
		}
		if kind != "" {
			config, err := store.GetConfig(ctx)
			if err != nil {
				release()
				return nil, err
			}
			if config["automaticTasksPaused"] != false {
				release()
				return nil, scheduler.ErrAutomaticTasksPaused
			}
		}
		if kind == scheduler.JobBackup {
			release()
			return func() {}, nil
		}
		return release, nil
	}, Callbacks: scheduler.Callbacks{
		NotifyCompletion:      completion.send,
		ReconcileNotification: completion.reconcile,
		Backup:                resources.Backup,
		ReconcileBackup:       resources.ReconcileBackup,
		RefreshMetadata:       func(ctx context.Context, _ string) error { return catalog.RefreshAutomaticMetadata(ctx) },
		ReconcileMetadata:     func(context.Context, string) (bool, error) { return false, nil },
		MoveCompletedMedia: func(ctx context.Context, _ string, move scheduler.MediaMove) error {
			return media.MoveCompletedFile(ctx, move.SourceDir, move.DownloadDir, move.SourcePath, move.TargetPath)
		},
		ReconcileMediaMove: func(ctx context.Context, _ string, move scheduler.MediaMove) (bool, error) {
			return media.ReconcileCompletedFile(ctx, move.SourceDir, move.DownloadDir, move.SourcePath, move.TargetPath)
		},
	}})
	if err != nil {
		return fmt.Errorf("初始化持久任务失败：%w", err)
	}
	filter, err = peerfilter.New(ctx, peerfilter.Options{Store: store, Directory: filepath.Join(dataDir, "peerfilter"), Guard: writeGate.Shared, OnError: func(err error) {
		resources.AppendLog(filesapi.LogEntry{Level: "ERROR", Logger: "peerfilter", Message: err.Error()})
	}})
	if err != nil {
		return fmt.Errorf("初始化节点过滤失败：%w", err)
	}
	api.SetConfigHooks(peerfilter.ValidateConfig, filter.ConfigChanged)
	api.SetTaskPauseHandler(func(ctx context.Context, paused bool) error {
		err := tasks.SetPaused(ctx, paused)
		if err == nil {
			filter.Notify()
		}
		return err
	})
	if err := api.Register(append([]server.Route{api.TransmissionRoute()}, filter.Routes()...)...); err != nil {
		return err
	}
	var pipelineRoutes []server.Route
	for _, route := range engine.Routes() {
		if route.Path != "/api/refreshAni" && route.Path != "/api/refreshAll" {
			pipelineRoutes = append(pipelineRoutes, route)
		}
	}
	if err := api.Register(pipelineRoutes...); err != nil {
		return err
	}
	if err := api.Register(tasks.Routes()...); err != nil {
		return err
	}
	if err := api.Register(catalog.Routes()...); err != nil {
		return err
	}
	if err := api.Register(administration.Routes()...); err != nil {
		return err
	}
	if err := api.Register(media.Routes()...); err != nil {
		return err
	}
	if err := api.Register(collection.Routes()...); err != nil {
		return err
	}
	if err := api.Register(resources.Routes()...); err != nil {
		return err
	}
	if err := api.Register(notices.Routes()...); err != nil {
		return err
	}
	var documentation *apidocs.Service
	if enabled, _ := strconv.ParseBool(os.Getenv("SWAGGER_ENABLED")); enabled {
		documentation, err = apidocs.New(api.RouteCatalog())
		if err != nil {
			return fmt.Errorf("生成 API 文档失败：%w", err)
		}
		if err := api.Register(documentation.Routes()...); err != nil {
			return err
		}
	}
	mcpEnabled, _ := strconv.ParseBool(os.Getenv("MCP_ENABLED"))
	mcpHandler := mcpapi.New(mcpapi.Options{Enabled: mcpEnabled, Store: store, Guard: writeGate.Shared, Operations: mcpOperations(api, catalog, engine)}).Handler()
	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(strings.TrimRight(r.URL.Path, "/"), "/api/mcp") {
			mcpHandler.ServeHTTP(w, r)
			return
		}
		path := strings.ToLower(strings.TrimRight(r.URL.Path, "/"))
		if path != "/api/exportbackup" && path != "/api/importbackup" && !(r.Method == http.MethodGet && path == "/api/file") {
			release, err := writeGate.Shared(r.Context())
			if err != nil {
				server.Respond(w, 500, "等待备份快照结束："+err.Error(), nil)
				return
			}
			defer release()
		}
		api.ServeHTTP(w, r)
	})
	fallback, err := webui.Handler(webDir)
	if err != nil {
		return err
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) || (documentation != nil && documentation.IsPath(r.URL.Path)) {
			apiHandler.ServeHTTP(w, r)
			return
		}
		fallback.ServeHTTP(w, r)
	})
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("监听地址 %s 失败：%w", address, err)
	}
	defer listener.Close()
	resources.AppendLog(filesapi.LogEntry{Level: "INFO", Logger: "app", Message: "Go 服务已启动"})
	filterCtx, filterCancel := context.WithCancel(ctx)
	filterDone := make(chan struct{})
	go func() { defer close(filterDone); filter.Run(filterCtx) }()
	defer func() { filterCancel(); <-filterDone }()
	if err := tasks.Start(ctx); err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result = errors.Join(result, tasks.Close(closeCtx))
	}()
	if err := writeJSON(events, map[string]any{"type": "listening", "address": listener.Addr().String()}); err != nil {
		return fmt.Errorf("输出监听事件失败：%w", err)
	}
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(listener) }()
	ready(listener.Addr().String())
	restart := false
	select {
	case <-ctx.Done():
	case restart = <-stopRequests:
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("运行 HTTP 服务失败：%w", err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		_ = httpServer.Close()
		return fmt.Errorf("停止 HTTP 服务失败：%w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	if restart {
		return errRestart
	}
	return nil
}

func importLegacy(ctx context.Context, source, dataDir string, output io.Writer) (result error) {
	if _, err := migration.Preview(source); err != nil {
		return fmt.Errorf("预览旧数据失败：%w", err)
	}
	overlap, err := importPathsOverlap(source, dataDir)
	if err != nil {
		return fmt.Errorf("检查导入路径失败：%w", err)
	}
	if overlap {
		return errors.New("旧数据源必须与目标数据目录分开")
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("创建数据目录失败：%w", err)
	}
	lock, err := instance.Acquire(filepath.Join(dataDir, "instance.lock"))
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, lock.Close()) }()
	store, err := storage.Open(filepath.Join(dataDir, "state.db"))
	if err != nil {
		return fmt.Errorf("打开 SQLite 数据库失败：%w", err)
	}
	defer func() { result = errors.Join(result, store.Close()) }()
	report, err := migration.Import(ctx, store, source)
	if err != nil {
		return fmt.Errorf("导入旧数据失败：%w", err)
	}
	if err := writeJSON(output, report); err != nil {
		return fmt.Errorf("输出导入报告失败：%w", err)
	}
	return nil
}

func isAPIPath(path string) bool {
	path = strings.ToLower(path)
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

func importPathsOverlap(source, dataDir string) (bool, error) {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return false, fmt.Errorf("读取旧数据源信息失败：%w", err)
	}
	sourcePath, err := canonicalPath(source)
	if err != nil {
		return false, fmt.Errorf("解析旧数据源路径失败：%w", err)
	}
	dataPath, err := canonicalPath(dataDir)
	if err != nil {
		return false, fmt.Errorf("解析目标数据目录失败：%w", err)
	}
	if sourceInfo.IsDir() {
		return pathContains(sourcePath, dataPath) || pathContains(dataPath, sourcePath), nil
	}
	return pathContains(dataPath, sourcePath), nil
}

func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("获取绝对路径失败：%w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("解析路径符号链接失败：%w", err)
	}
	parent := filepath.Dir(absolute)
	if parent == absolute {
		return absolute, nil
	}
	resolvedParent, err := canonicalPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(absolute)), nil
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func writeJSON(output io.Writer, value any) error {
	return json.NewEncoder(output).Encode(value)
}
