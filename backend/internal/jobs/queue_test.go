package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/activity"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

type remoteHandler struct {
	applied bool
	calls   int
}

type fixedHandler struct {
	fail   bool
	writes int
}

func TestSnapshotFreezeStopsTaskClaimUntilBusinessWritesCanResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.EnqueueJob(ctx, "rename", "blocked-by-snapshot", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	gate := activity.New()
	release, err := gate.Freeze(ctx)
	if err != nil {
		t.Fatal(err)
	}
	requested := make(chan struct{}, 1)
	handler := &fixedHandler{}
	manager := jobs.New(store, map[string]jobs.Handler{"rename": handler}, jobs.Options{Guard: func(ctx context.Context, _ string) (func(), error) {
		select {
		case requested <- struct{}{}:
		default:
		}
		return gate.Shared(ctx)
	}})
	done := make(chan error, 1)
	go func() { _, err := manager.RunOnce(ctx); done <- err }()
	select {
	case <-requested:
	case <-ctx.Done():
		release()
		t.Fatal(ctx.Err())
	}
	current, err := store.GetJob(ctx, job.ID)
	if err != nil {
		release()
		t.Fatal(err)
	}
	if current.State != "pending" {
		release()
		t.Fatalf("快照期间任务已被领取：%v", current)
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	current, err = store.GetJob(ctx, job.ID)
	if err != nil || current.State != "resolved" || handler.writes != 1 {
		t.Fatalf("快照后未恢复业务：%v %v", current, err)
	}
}

func (h *fixedHandler) Reconcile(context.Context, jobs.Job) (bool, error) { return false, nil }
func (h *fixedHandler) Execute(context.Context, jobs.Job) error {
	if h.fail {
		return errors.New("配置待修复")
	}
	h.writes++
	return nil
}

func TestFailedTaskCanBeRetriedAfterConfigurationFix(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job, err := store.EnqueueJob(ctx, "rename", "file-1", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	handler := &fixedHandler{fail: true}
	manager := jobs.New(store, map[string]jobs.Handler{"rename": handler}, jobs.Options{MaxAttempts: 1})
	if _, err := manager.RunOnce(ctx); err == nil {
		t.Fatal("配置错误应记录失败")
	}
	failed, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != "failed" {
		t.Fatal(failed.State)
	}
	handler.fail = false
	if err := store.RequeueJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if ran, err := manager.RunOnce(ctx); !ran || err != nil {
		t.Fatalf("修复后应恢复：%v", err)
	}
	done, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.State != "resolved" || handler.writes != 1 {
		t.Fatal("恢复后应执行一次并完成")
	}
}

func (h *remoteHandler) Reconcile(context.Context, jobs.Job) (bool, error) { return h.applied, nil }
func (h *remoteHandler) Execute(context.Context, jobs.Job) error {
	h.calls++
	h.applied = true
	return errors.New("模拟请求成功后响应丢失")
}

func TestLostResponseIsReconciledAfterDatabaseReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.EnqueueJob(ctx, "add", "hash-1", json.RawMessage(`{"subscription":"sample"}`))
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := store.EnqueueJob(ctx, "add", "hash-1", json.RawMessage(`{"subscription":"changed"}`))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != duplicate.ID {
		t.Fatal("同一业务键应复用原任务")
	}
	handler := &remoteHandler{}
	now := time.Now()
	manager := jobs.New(store, map[string]jobs.Handler{"add": handler}, jobs.Options{Owner: "first", Now: func() time.Time { return now }, RetryDelay: time.Second})
	if ran, err := manager.RunOnce(ctx); !ran || err == nil {
		t.Fatal("首轮应记录响应丢失")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now = now.Add(2 * time.Second)
	manager = jobs.New(store, map[string]jobs.Handler{"add": handler}, jobs.Options{Owner: "restarted", Now: func() time.Time { return now }})
	if ran, err := manager.RunOnce(ctx); !ran || err != nil {
		t.Fatalf("重启后应对账完成：%v", err)
	}
	job, err := store.GetJob(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != "resolved" || handler.calls != 1 {
		t.Fatalf("任务状态=%s，写请求次数=%d", job.State, handler.calls)
	}
	if string(job.Payload) != string(first.Payload) {
		t.Fatal("重复入队不应覆盖原任务内容")
	}
}

func TestExpiredClaimIsReconciledBeforeExecution(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	queued, err := store.EnqueueJob(ctx, "add", "existing-remote", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimJob(ctx, "dead-process", time.Now(), time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("领取任务：%v", err)
	}
	handler := &remoteHandler{applied: true}
	manager := jobs.New(store, map[string]jobs.Handler{"add": handler}, jobs.Options{Owner: "new-process", Now: func() time.Time { return time.Now().Add(2 * time.Minute) }})
	if ran, err := manager.RunOnce(ctx); !ran || err != nil {
		t.Fatalf("恢复任务：%v", err)
	}
	job, err := store.GetJob(ctx, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != "resolved" || handler.calls != 0 {
		t.Fatal("远端已完成时不能再次执行写操作")
	}
}

func TestExpiredOwnerCannotCompleteAReclaimedJob(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	queued, err := store.EnqueueJob(ctx, "rename", "episode-1", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	oldClaim, err := store.ClaimJob(ctx, "same-worker", now, time.Minute)
	if err != nil || oldClaim == nil {
		t.Fatalf("首次领取：%v", err)
	}
	newClaim, err := store.ClaimJob(ctx, "same-worker", now.Add(2*time.Minute), time.Minute)
	if err != nil || newClaim == nil {
		t.Fatalf("租约到期后重新领取：%v", err)
	}
	if oldClaim.Owner == newClaim.Owner {
		t.Fatal("每次领取都必须使用新的 fencing owner")
	}
	if err := store.ResolveJob(ctx, queued.ID, oldClaim.Owner); err == nil {
		t.Fatal("过期 owner 不得完成新 owner 的任务")
	}
	current, err := store.GetJob(ctx, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != "running" || current.Owner != newClaim.Owner {
		t.Fatalf("旧 owner 覆盖了新领取状态：%#v", current)
	}
	if err := store.ResolveJob(ctx, queued.ID, newClaim.Owner); err != nil {
		t.Fatalf("新 owner 应能完成任务：%v", err)
	}
}
