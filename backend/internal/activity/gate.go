// Package activity 在备份快照期间协调 HTTP 和后台任务的业务写入。
package activity

import (
	"context"
	"math"

	"golang.org/x/sync/semaphore"
)

type Gate struct{ permits *semaphore.Weighted }

func New() *Gate { return &Gate{permits: semaphore.NewWeighted(math.MaxInt64)} }

// Shared 允许普通业务并发执行，并等待正在进行的快照完成。
func (g *Gate) Shared(ctx context.Context) (func(), error) { return g.acquire(ctx, 1) }

// Freeze 等待在途业务结束，在释放前暂停新的业务写入。
func (g *Gate) Freeze(ctx context.Context) (func(), error) { return g.acquire(ctx, math.MaxInt64) }

func (g *Gate) acquire(ctx context.Context, n int64) (func(), error) {
	if err := g.permits.Acquire(ctx, n); err != nil {
		return nil, err
	}
	return func() { g.permits.Release(n) }, nil
}
