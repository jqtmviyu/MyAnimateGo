package scheduler

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
)

type rssRefreshHandler struct{ runtime *Runtime }

type progressHandler struct{ runtime *Runtime }

func (h progressHandler) Reconcile(ctx context.Context, job jobs.Job) (bool, error) {
	var payload refreshPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return false, err
	}
	_, exists, err := h.runtime.subscription(ctx, payload.SubscriptionID)
	return !exists, err
}

func (h progressHandler) Execute(ctx context.Context, job jobs.Job) error {
	if h.runtime.options.Engine == nil {
		return errors.New("进度任务缺少 pipeline.Engine")
	}
	var payload refreshPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return err
	}
	return h.runtime.options.Engine.UpdateSubscriptionProgress(ctx, payload.SubscriptionID)
}

func (h rssRefreshHandler) Reconcile(ctx context.Context, job jobs.Job) (bool, error) {
	if h.runtime.options.Engine == nil {
		return false, errors.New("RSS 任务缺少 pipeline.Engine")
	}
	var payload refreshPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return false, err
	}
	config, err := h.runtime.store.GetConfig(ctx)
	if err != nil {
		return false, err
	}
	if payload.Automatic && !valueBool(config["rss"]) {
		return true, nil
	}
	if payload.SubscriptionID == "" {
		items, err := h.runtime.store.ListSubscriptions(ctx)
		if err != nil {
			return false, err
		}
		for _, item := range items {
			if valueBool(item["enable"]) {
				return false, nil
			}
		}
		return true, nil
	}
	item, exists, err := h.runtime.subscription(ctx, payload.SubscriptionID)
	if err != nil || !exists {
		return !exists, err
	}
	if !valueBool(item["enable"]) {
		return true, nil
	}
	return false, nil
}

func (h rssRefreshHandler) Execute(ctx context.Context, job jobs.Job) error {
	ctx = jobs.WithJobID(ctx, job.ID)
	if h.runtime.options.Engine == nil {
		return errors.New("RSS 任务缺少 pipeline.Engine")
	}
	var payload refreshPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return err
	}
	if payload.SubscriptionID != "" {
		return h.runtime.options.Engine.RefreshSubscription(ctx, payload.SubscriptionID)
	}
	return h.runtime.options.Engine.RefreshAll(ctx)
}
