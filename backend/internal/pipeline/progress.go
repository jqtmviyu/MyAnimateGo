package pipeline

import (
	"context"
	"errors"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/rules"
)

// UpdateSubscriptionProgress 只从 RSS 计算当前进度，供创建禁用订阅后的任务调用。
func (e *Engine) UpdateSubscriptionProgress(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	subscriptions, err := e.store.ListSubscriptions(ctx)
	if err != nil {
		return err
	}
	config, err := e.config(ctx)
	if err != nil {
		return err
	}
	for _, subscription := range subscriptions {
		if valueString(subscription["id"]) != id {
			continue
		}
		items, err := e.items(ctx, config, subscription)
		if err != nil {
			return err
		}
		episodes, masters := make([]float64, len(items)), make([]bool, len(items))
		for i, item := range items {
			episodes[i], masters[i] = item.Episode, item.Master
		}
		subscription["currentEpisodeNumber"] = float64(rules.CurrentEpisodeNumber(rules.ProgressOptions{
			Episodes: episodes, MasterFlags: masters, DownloadNew: valueBool(subscription["downloadNew"]),
			StandbyRSS: valueBool(config["standbyRss"]), Coexist: valueBool(config["coexist"]),
		}))
		return e.saveSubscriptionFields(ctx, subscription, map[string]any{"currentEpisodeNumber": subscription["currentEpisodeNumber"]})
	}
	return errors.New("订阅不存在")
}

func (e *Engine) saveSubscriptionFields(ctx context.Context, subscription, fields map[string]any) error {
	if store, ok := e.store.(interface {
		UpdateSubscriptionFields(context.Context, string, map[string]any) error
	}); ok {
		return store.UpdateSubscriptionFields(ctx, valueString(subscription["id"]), fields)
	}
	for key, value := range fields {
		subscription[key] = value
	}
	return e.store.PutSubscription(ctx, subscription)
}
