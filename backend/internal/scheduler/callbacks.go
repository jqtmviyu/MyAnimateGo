package scheduler

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
)

type metadataHandler struct{ runtime *Runtime }

func (h metadataHandler) Reconcile(ctx context.Context, job jobs.Job) (bool, error) {
	if h.runtime.options.Callbacks.ReconcileMetadata == nil {
		return false, errors.New("元数据任务对账回调未配置")
	}
	return h.runtime.options.Callbacks.ReconcileMetadata(ctx, job.ID)
}

func (h metadataHandler) Execute(ctx context.Context, job jobs.Job) error {
	if h.runtime.options.Callbacks.RefreshMetadata == nil {
		return errors.New("元数据刷新回调未配置")
	}
	return h.runtime.options.Callbacks.RefreshMetadata(ctx, job.ID)
}

type backupHandler struct{ runtime *Runtime }

func (h backupHandler) Reconcile(ctx context.Context, job jobs.Job) (bool, error) {
	if h.runtime.options.Callbacks.ReconcileBackup == nil {
		return false, errors.New("备份对账 callback 未配置")
	}
	return h.runtime.options.Callbacks.ReconcileBackup(ctx, job.ID)
}

func (h backupHandler) Execute(ctx context.Context, job jobs.Job) error {
	if h.runtime.options.Callbacks.Backup == nil {
		return errors.New("备份回调未配置")
	}
	return h.runtime.options.Callbacks.Backup(ctx, job.ID)
}

type notificationHandler struct{ runtime *Runtime }

func (h notificationHandler) payload(job jobs.Job) (Notification, error) {
	var event Notification
	if err := json.Unmarshal(job.Payload, &event); err != nil {
		return event, err
	}
	event.ID = job.ID
	return event, nil
}

func (h notificationHandler) Reconcile(ctx context.Context, job jobs.Job) (bool, error) {
	if h.runtime.options.Callbacks.ReconcileNotification == nil {
		return false, errors.New("完成通知对账回调未配置")
	}
	event, err := h.payload(job)
	if err != nil {
		return false, err
	}
	return h.runtime.options.Callbacks.ReconcileNotification(ctx, job.ID, event)
}

func (h notificationHandler) Execute(ctx context.Context, job jobs.Job) error {
	if h.runtime.options.Callbacks.NotifyCompletion == nil {
		return errors.New("完成通知回调未配置")
	}
	event, err := h.payload(job)
	if err != nil {
		return err
	}
	// 通知有独立持久任务和对账回调，不能以易失远端标签作为发送前提。
	return h.runtime.options.Callbacks.NotifyCompletion(ctx, job.ID, event)
}

type mediaMoveHandler struct{ runtime *Runtime }

func (h mediaMoveHandler) payload(job jobs.Job) (MediaMove, error) {
	var move MediaMove
	if err := json.Unmarshal(job.Payload, &move); err != nil {
		return move, err
	}
	return move, nil
}

func (h mediaMoveHandler) Reconcile(ctx context.Context, job jobs.Job) (bool, error) {
	if h.runtime.options.Callbacks.ReconcileMediaMove == nil {
		return false, errors.New("媒体迁移对账回调未配置")
	}
	move, err := h.payload(job)
	if err != nil {
		return false, err
	}
	if err := h.runtime.confirmLocation(ctx, move); err != nil {
		return false, err
	}
	return h.runtime.options.Callbacks.ReconcileMediaMove(ctx, job.ID, move)
}

func (h mediaMoveHandler) Execute(ctx context.Context, job jobs.Job) error {
	if h.runtime.options.Callbacks.MoveCompletedMedia == nil {
		return errors.New("媒体迁移回调未配置")
	}
	move, err := h.payload(job)
	if err != nil {
		return err
	}
	if err := h.runtime.confirmLocation(ctx, move); err != nil {
		return err
	}
	return h.runtime.options.Callbacks.MoveCompletedMedia(ctx, job.ID, move)
}
