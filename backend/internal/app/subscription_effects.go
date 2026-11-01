package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/library"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

type subscriptionEffectsHandler struct{ media *library.Service }

// 媒体移动和删除都按持久快照幂等重做，已完成的文件操作由媒体服务对账。
func (h subscriptionEffectsHandler) Reconcile(_ context.Context, job jobs.Job) (bool, error) {
	var payload server.SubscriptionEffects
	return false, json.Unmarshal(job.Payload, &payload)
}

func (h subscriptionEffectsHandler) Execute(ctx context.Context, job jobs.Job) error {
	var payload server.SubscriptionEffects
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return err
	}
	switch payload.Action {
	case "move":
		return h.media.MoveSubscription(ctx, payload.Old, payload.New)
	case "delete":
		for _, item := range payload.Subscriptions {
			if err := h.media.DeleteSubscription(ctx, item, payload.DeleteFiles); err != nil {
				return err
			}
		}
		return nil
	default:
		return errors.New("未知订阅媒体任务")
	}
}
