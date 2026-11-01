package app

import (
	"context"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/notifications"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/scheduler"
)

type completionNotifier struct {
	store interface {
		ListSubscriptions(context.Context) ([]map[string]any, error)
	}
	notices *notifications.Service
}

func (n completionNotifier) subscription(ctx context.Context, event scheduler.Notification) (map[string]any, string, error) {
	items, err := n.store.ListSubscriptions(ctx)
	if err != nil {
		return nil, "", err
	}
	text := event.TorrentName + " 下载完成"
	for _, tag := range event.Tags {
		if tag == "备用RSS" {
			text = "(备用RSS) " + text
			break
		}
	}
	for _, item := range items {
		if item["id"] != event.SubscriptionID {
			continue
		}
		if groups, ok := item["standbyRssList"].([]any); ok {
			for _, raw := range groups {
				if group, ok := raw.(map[string]any); ok {
					for _, tag := range event.Tags {
						if tag == group["label"] {
							item["subgroup"] = tag
						}
					}
				}
			}
		}
		return item, text, nil
	}
	return nil, text, nil
}

func (n completionNotifier) reconcile(ctx context.Context, id string, event scheduler.Notification) (bool, error) {
	ani, text, err := n.subscription(ctx, event)
	if err != nil || ani == nil {
		return ani == nil, err
	}
	return n.notices.Reconcile(ctx, id, ani, text, "DOWNLOAD_END")
}

func (n completionNotifier) send(ctx context.Context, id string, event scheduler.Notification) error {
	ani, text, err := n.subscription(ctx, event)
	if err != nil || ani == nil {
		return err
	}
	return n.notices.Send(ctx, id, ani, text, "DOWNLOAD_END")
}
