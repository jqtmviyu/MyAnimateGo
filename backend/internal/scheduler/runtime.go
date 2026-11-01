// Package scheduler 把手动和周期性工作写入 SQLite 队列，并在服务生命周期内恢复执行。
package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/transmission"
)

const (
	JobRSSRefresh     = "rss-refresh"
	JobProgress       = "subscription-progress"
	JobRenameScan     = "rename-scan"
	JobRename         = "rename"
	JobCompleteTag    = "complete-tag"
	JobNotifyComplete = "notify-complete"
	JobMoveLocation   = "move-location"
	JobMoveMedia      = "move-media"
	JobDeleteTorrent  = "delete-torrent"
	JobDisableSub     = "disable-subscription"
	JobMetadata       = "metadata-refresh"
	JobBackup         = "backup"
)

// JobKindBackup exposes the persisted kind for operational tooling and tests.
const JobKindBackup = JobBackup

var ErrAutomaticTasksPaused = errors.New("自动任务已暂停")

type Store interface {
	jobs.Store
	EnqueueJob(context.Context, string, string, json.RawMessage) (jobs.Job, error)
	GetConfig(context.Context) (map[string]any, error)
	SaveConfig(context.Context, map[string]any) error
	UpdateConfigFields(context.Context, map[string]any) error
	ListSubscriptions(context.Context) ([]map[string]any, error)
	PutSubscription(context.Context, map[string]any) error
}

type Notification struct {
	ID             string   `json:"id,omitempty"`
	SubscriptionID string   `json:"subscriptionId"`
	TorrentHash    string   `json:"torrentHash"`
	TorrentName    string   `json:"torrentName"`
	Tags           []string `json:"tags"`
}

type MediaMove struct {
	SubscriptionID string `json:"subscriptionId"`
	TorrentHash    string `json:"torrentHash"`
	SourceDir      string `json:"sourceDir"`
	SourcePath     string `json:"sourcePath"`
	TargetPath     string `json:"targetPath"`
	DownloadDir    string `json:"downloadDir,omitempty"`
}

// Callbacks 接收必须由上层实现的元数据、通知、媒体迁移和备份操作。
type Callbacks struct {
	RefreshMetadata       func(context.Context, string) error
	ReconcileMetadata     func(context.Context, string) (bool, error)
	NotifyCompletion      func(context.Context, string, Notification) error
	ReconcileNotification func(context.Context, string, Notification) (bool, error)
	MoveCompletedMedia    func(context.Context, string, MediaMove) error
	ReconcileMediaMove    func(context.Context, string, MediaMove) (bool, error)
	Backup                func(context.Context, string) error
	ReconcileBackup       func(context.Context, string) (bool, error)
}

type Options struct {
	Guard                  func(context.Context, string) (func(), error)
	Engine                 *pipeline.Engine
	Handlers               map[string]jobs.Handler
	Callbacks              Callbacks
	Now                    func() time.Time
	PollInterval           time.Duration
	Lease                  time.Duration
	RetryDelay             time.Duration
	MaxAttempts            int
	RenamePollInterval     time.Duration
	RenamePollAttempts     int
	TransmissionHTTPClient *http.Client
	OnError                func(error)
}

type Runtime struct {
	store   Store
	manager *jobs.Manager
	options Options

	mu         sync.Mutex
	started    bool
	cancel     context.CancelFunc
	workCancel context.CancelFunc
	done       chan struct{}
	lastError  error
}

type refreshPayload struct {
	SubscriptionID string `json:"subscriptionId,omitempty"`
	Automatic      bool   `json:"automatic"`
}

// New 创建调度器和全部持久任务处理器。
func New(store Store, options Options) (*Runtime, error) {
	if store == nil {
		return nil, errors.New("任务存储不能为空")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	if options.Lease <= 0 {
		options.Lease = time.Minute
	}
	if options.RetryDelay <= 0 {
		options.RetryDelay = 5 * time.Second
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = 3
	}
	if options.RenamePollInterval <= 0 {
		options.RenamePollInterval = time.Second
	}
	if options.RenamePollAttempts <= 0 {
		options.RenamePollAttempts = 10
	}
	runtime := &Runtime{store: store, options: options, done: make(chan struct{})}
	handlers := map[string]jobs.Handler{
		JobRSSRefresh:     rssRefreshHandler{runtime},
		JobProgress:       progressHandler{runtime},
		JobRenameScan:     renameScanHandler{runtime},
		JobRename:         renameHandler{runtime},
		JobCompleteTag:    completeTagHandler{runtime},
		JobNotifyComplete: notificationHandler{runtime},
		JobMoveLocation:   moveLocationHandler{runtime},
		JobMoveMedia:      mediaMoveHandler{runtime},
		JobDeleteTorrent:  deleteTorrentHandler{runtime},
		JobDisableSub:     disableSubscriptionHandler{runtime},
		JobMetadata:       metadataHandler{runtime},
		JobBackup:         backupHandler{runtime},
	}
	for kind, handler := range options.Handlers {
		if _, exists := handlers[kind]; exists || kind == "" || handler == nil {
			return nil, fmt.Errorf("业务任务处理器注册冲突或为空：%s", kind)
		}
		handlers[kind] = handler
	}
	runtime.manager = jobs.New(store, handlers, jobs.Options{Guard: options.Guard, Now: options.Now, Lease: options.Lease, RetryDelay: options.RetryDelay, MaxAttempts: options.MaxAttempts})
	return runtime, nil
}

// Start 启动单工作者循环；循环在传入上下文取消或 Close 时停止。
func (r *Runtime) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return errors.New("调度器已经启动")
	}
	r.started = true
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.done = make(chan struct{})
	go r.run(runCtx)
	return nil
}

// Close 取消当前任务并等待队列恢复点写入后退出。
func (r *Runtime) Close(ctx context.Context) error {
	r.mu.Lock()
	cancel, workCancel, done, started := r.cancel, r.workCancel, r.done, r.started
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if workCancel != nil {
		workCancel()
	}
	if !started {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Paused 返回全局任务暂停状态；字段缺失时按暂停处理。
func (r *Runtime) Paused(ctx context.Context) (bool, error) {
	config, err := r.store.GetConfig(ctx)
	if err != nil {
		return true, err
	}
	paused, exists := config["automaticTasksPaused"]
	return !exists || valueBool(paused), nil
}

// SetPaused 持久化暂停状态，暂停时取消当前任务的可取消上下文。
func (r *Runtime) SetPaused(ctx context.Context, paused bool) error {
	if err := r.store.UpdateConfigFields(ctx, map[string]any{"automaticTasksPaused": paused}); err != nil {
		return err
	}
	if paused {
		r.mu.Lock()
		cancel := r.workCancel
		r.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	return nil
}

func (r *Runtime) enqueueDue(ctx context.Context) error {
	if r.options.Guard != nil {
		release, err := r.options.Guard(ctx, "")
		if err != nil {
			return err
		}
		defer release()
	}
	config, err := r.store.GetConfig(ctx)
	if err != nil {
		return err
	}
	paused, exists := config["automaticTasksPaused"]
	if !exists || valueBool(paused) {
		return nil
	}
	now := r.options.Now()
	if valueBool(config["rss"]) {
		interval := time.Duration(valueInt(config["rssSleepMinutes"])) * time.Minute
		if interval <= 0 {
			interval = 15 * time.Minute
		}
		slot := now.Unix() / int64(interval.Seconds())
		subscriptions, err := r.store.ListSubscriptions(ctx)
		if err != nil {
			return err
		}
		for _, item := range subscriptions {
			if !valueBool(item["enable"]) {
				continue
			}
			id := valueString(item["id"])
			if id == "" {
				continue
			}
			if _, err := r.enqueue(ctx, JobRSSRefresh, fmt.Sprintf("auto:rss:%s:%d", id, slot), refreshPayload{SubscriptionID: id, Automatic: true}); err != nil {
				return err
			}
		}
	}
	// 下载完成检测独立于重命名和清理开关，保持下载完成通知正常触发。
	interval := time.Duration(valueInt(config["renameSleepSeconds"])) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	slot := now.Unix() / int64(interval.Seconds())
	if _, err := r.enqueue(ctx, JobRenameScan, fmt.Sprintf("auto:rename:%d", slot), map[string]any{"scheduledAt": now.Unix()}); err != nil {
		return err
	}
	metadataSlot := now.Unix() / int64((12 * time.Hour).Seconds())
	if _, err := r.enqueue(ctx, JobMetadata, fmt.Sprintf("auto:metadata:%d", metadataSlot), map[string]any{"scheduledAt": now.Unix()}); err != nil {
		return err
	}
	if valueBool(config["configBackup"]) {
		if _, err := r.enqueue(ctx, JobBackup, "auto:backup:"+now.Format("2006-01-02"), map[string]any{"scheduledAt": now.Unix()}); err != nil {
			return err
		}
	}
	return nil
}

// LastError 返回最近一次调度或后台任务错误，错误同时保存在任务记录中。
func (r *Runtime) LastError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastError
}

// RunOnce 手动处理一个可领取任务，便于测试和受控运行入口复用。
func (r *Runtime) RunOnce(ctx context.Context) (bool, error) {
	paused, err := r.Paused(ctx)
	if err != nil || paused {
		return false, err
	}
	return r.manager.RunOnce(ctx)
}

// EnqueueRefresh 将单订阅刷新请求持久化后交给工作者。
func (r *Runtime) EnqueueRefresh(ctx context.Context, subscriptionID string) (jobs.Job, error) {
	paused, err := r.Paused(ctx)
	if err != nil {
		return jobs.Job{}, err
	}
	if paused {
		return jobs.Job{}, ErrAutomaticTasksPaused
	}
	if subscriptionID == "" {
		return jobs.Job{}, errors.New("订阅 ID 不能为空")
	}
	if _, exists, err := r.subscription(ctx, subscriptionID); err != nil {
		return jobs.Job{}, err
	} else if !exists {
		return jobs.Job{}, errors.New("订阅不存在")
	}
	key, err := uniqueKey("manual:" + subscriptionID)
	if err != nil {
		return jobs.Job{}, err
	}
	return r.enqueue(ctx, JobRSSRefresh, key, refreshPayload{SubscriptionID: subscriptionID})
}

// EnqueueRefreshAll 将全部订阅刷新请求持久化后交给工作者。
func (r *Runtime) EnqueueRefreshAll(ctx context.Context) (jobs.Job, error) {
	paused, err := r.Paused(ctx)
	if err != nil {
		return jobs.Job{}, err
	}
	if paused {
		return jobs.Job{}, ErrAutomaticTasksPaused
	}
	key, err := uniqueKey("manual:all")
	if err != nil {
		return jobs.Job{}, err
	}
	return r.enqueue(ctx, JobRSSRefresh, key, refreshPayload{})
}

// Routes 提供原版前端使用的持久化刷新入口；应用接线时应以此替换引擎中的同步刷新路由。
func (r *Runtime) Routes() []server.Route {
	return []server.Route{
		{Method: http.MethodPost, Path: "/api/refreshAni", Handler: http.HandlerFunc(r.handleRefreshAni)},
		{Method: http.MethodPost, Path: "/api/refreshAll", Handler: http.HandlerFunc(r.handleRefreshAll)},
	}
}

func (r *Runtime) handleRefreshAni(w http.ResponseWriter, request *http.Request) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decodeRequest(request, &input); err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	if _, err := r.EnqueueRefresh(request.Context(), input.ID); err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	server.Respond(w, 200, "已开始刷新RSS", nil)
}

func (r *Runtime) handleRefreshAll(w http.ResponseWriter, request *http.Request) {
	if _, err := r.EnqueueRefreshAll(request.Context()); err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	server.Respond(w, 200, "已开始刷新RSS", nil)
}

func (r *Runtime) enqueue(ctx context.Context, kind, key string, payload any) (jobs.Job, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return jobs.Job{}, err
	}
	return r.store.EnqueueJob(ctx, kind, key, data)
}

func uniqueKey(prefix string) (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return prefix + ":" + hex.EncodeToString(data[:]), nil
}

func decodeRequest(r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := decoder.Decode(target); err != nil {
		return errors.New("JSON 请求格式错误")
	}
	return nil
}

func (r *Runtime) subscription(ctx context.Context, id string) (map[string]any, bool, error) {
	items, err := r.store.ListSubscriptions(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, item := range items {
		if valueString(item["id"]) == id {
			return item, true, nil
		}
	}
	return nil, false, nil
}

func (r *Runtime) transmission(config map[string]any) *transmission.Client {
	httpClient := r.options.TransmissionHTTPClient
	if httpClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		httpClient = &http.Client{Timeout: 30 * time.Second, Transport: transport}
	}
	return transmission.New(transmission.Config{
		URL:      valueString(config["downloadToolHost"]),
		Username: valueString(config["downloadToolUsername"]), Password: valueString(config["downloadToolPassword"]),
		HTTPClient: httpClient,
	})
}

func (r *Runtime) run(ctx context.Context) {
	defer close(r.done)
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if err := r.enqueueDue(ctx); err != nil {
			r.report(err)
		}
		paused, err := r.Paused(ctx)
		if err != nil {
			r.report(err)
		} else if !paused {
			workCtx, cancel := context.WithCancel(ctx)
			r.mu.Lock()
			r.workCancel = cancel
			r.mu.Unlock()
			processed, runErr := r.manager.RunOnce(workCtx)
			cancel()
			r.mu.Lock()
			r.workCancel = nil
			r.mu.Unlock()
			if runErr != nil {
				r.report(runErr)
			}
			if processed {
				timer.Reset(0)
				continue
			}
		}
		timer.Reset(r.options.PollInterval)
	}
}

func (r *Runtime) report(err error) {
	if err == nil {
		return
	}
	r.mu.Lock()
	r.lastError = err
	r.mu.Unlock()
	if r.options.OnError != nil {
		r.options.OnError(err)
	}
}
