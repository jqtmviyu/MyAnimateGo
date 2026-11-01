// Package notifications sends configured notifications and integrates media services.
package notifications

import (
	"context"
	"errors"
	"net/http"
)

var ErrDeliveryUncertain = errors.New("通知是否送达无法确认；需要人工确认后才能重试")

type DeliveryStore interface {
	SetNotificationDelivery(context.Context, string, string, string) error
	GetNotificationDeliveryStates(context.Context, string) (map[string]string, error)
}

type Store interface {
	GetConfig(context.Context) (map[string]any, error)
	SaveConfig(context.Context, map[string]any) error
	ListSubscriptions(context.Context) ([]map[string]any, error)
}

type DownloadPath func(context.Context, map[string]any) (string, error)

type EpisodeTitleResolver func(context.Context, map[string]any, int) (map[string]string, error)
type SubscriptionMetadata func(context.Context, map[string]any) (map[string]string, error)

type Options struct {
	HTTPClient    *http.Client
	DownloadPath  DownloadPath
	EpisodeTitles EpisodeTitleResolver
	Metadata      SubscriptionMetadata
	Sleep         func(context.Context, int64) error
}

func IsDeliveryUncertain(err error) bool { return errors.Is(err, ErrDeliveryUncertain) }
