package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type Job struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Key         string          `json:"key"`
	Payload     json.RawMessage `json:"payload"`
	State       string          `json:"state"`
	Attempts    int             `json:"attempts"`
	Owner       string          `json:"-"`
	LastError   string          `json:"lastError,omitempty"`
	AvailableAt time.Time       `json:"availableAt"`
	LeaseUntil  time.Time       `json:"leaseUntil"`
}

// EnqueueRequest 描述随业务状态变更一起保存的持久任务。
type EnqueueRequest struct {
	Kind    string
	Key     string
	Payload json.RawMessage
}

type Store interface {
	// ClaimJob returns a fresh Job.Owner fencing token for each successful claim.
	ClaimJob(context.Context, string, time.Time, time.Duration) (*Job, error)
	ResolveJob(context.Context, string, string) error
	RetryJob(context.Context, string, string, string, time.Time) error
	FailJob(context.Context, string, string, string) error
}

// 对账必须只检查现状；确认尚未完成后才执行可能修改远端或文件的操作。
type Handler interface {
	Reconcile(context.Context, Job) (bool, error)
	Execute(context.Context, Job) error
}

type Options struct {
	Guard       func(context.Context, string) (func(), error)
	Owner       string
	Now         func() time.Time
	Lease       time.Duration
	RetryDelay  time.Duration
	MaxAttempts int
}

type Manager struct {
	store    Store
	handlers map[string]Handler
	options  Options
	mu       sync.Mutex
}

func New(store Store, handlers map[string]Handler, options Options) *Manager {
	if options.Owner == "" {
		var data [16]byte
		_, _ = rand.Read(data[:])
		options.Owner = hex.EncodeToString(data[:])
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Lease <= 0 {
		options.Lease = time.Minute
	}
	if options.RetryDelay <= 0 {
		options.RetryDelay = time.Second
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = 3
	}
	return &Manager{store: store, handlers: handlers, options: options}
}

// 单实例内串行执行，崩溃遗留领取在租约到期后恢复，并先对账再重试。
func (m *Manager) RunOnce(ctx context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.guard(ctx, "")
	if err != nil {
		return false, err
	}
	defer func() {
		if release != nil {
			release()
		}
	}()
	job, err := m.store.ClaimJob(ctx, m.options.Owner, m.options.Now(), m.options.Lease)
	release()
	release = nil
	if err != nil || job == nil {
		return false, err
	}
	handler, exists := m.handlers[job.Kind]
	release, err = m.guard(ctx, job.Kind)
	if err != nil {
		// 尚未执行副作用的领取也要保存恢复状态。
	} else if !exists {
		err = fmt.Errorf("任务处理器未注册：%s", job.Kind)
	} else {
		var done bool
		done, err = handler.Reconcile(ctx, *job)
		if err == nil && !done {
			err = handler.Execute(ctx, *job)
		}
	}
	if release != nil {
		release()
		release = nil
	}
	// 请求取消后也持久化恢复点，避免把已发出的远端操作当作没有发生。
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var guardErr error
	release, guardErr = m.guard(persistCtx, "")
	if guardErr != nil {
		return true, fmt.Errorf("等待保存任务恢复点：%w", guardErr)
	}
	if err == nil {
		return true, m.store.ResolveJob(persistCtx, job.ID, job.Owner)
	}
	var saveErr error
	if !exists || job.Attempts >= m.options.MaxAttempts {
		saveErr = m.store.FailJob(persistCtx, job.ID, job.Owner, err.Error())
	} else {
		saveErr = m.store.RetryJob(persistCtx, job.ID, job.Owner, err.Error(), m.options.Now().Add(m.options.RetryDelay))
	}
	if saveErr != nil {
		return true, fmt.Errorf("任务失败且恢复点保存失败：%v；%w", err, saveErr)
	}
	return true, err
}

func (m *Manager) guard(ctx context.Context, kind string) (func(), error) {
	if m.options.Guard == nil {
		return func() {}, nil
	}
	return m.options.Guard(ctx, kind)
}
