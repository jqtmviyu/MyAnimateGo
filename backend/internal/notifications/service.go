package notifications

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type Service struct {
	store      Store
	deliveries DeliveryStore
	options    Options
}

func New(store Store, options Options) (*Service, error) {
	if store == nil {
		return nil, errors.New("通知存储不能为空")
	}
	deliveries, ok := store.(DeliveryStore)
	if !ok {
		return nil, errors.New("通知存储未实现持久投递记录")
	}
	if options.HTTPClient == nil {
		options.HTTPClient = http.DefaultClient
	}
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	return &Service{store: store, deliveries: deliveries, options: options}, nil
}

// Send delivers the configured channels once; unknown remote outcomes stay in an uncertain state.
func (s *Service) Send(ctx context.Context, eventID string, subscription map[string]any, text, status string) error {
	if eventID == "" {
		return errors.New("通知事件 ID 不能为空")
	}
	if subscription == nil {
		return errors.New("通知订阅不能为空")
	}
	if enabled, exists := subscription["message"]; exists && !truth(enabled) {
		return nil
	}
	config, err := s.store.GetConfig(ctx)
	if err != nil {
		return err
	}
	channels, err := eligibleChannels(config, status)
	if err != nil {
		return err
	}
	var failures []error
	uncertain := false
	for index, channel := range channels {
		key := deliveryKey(index, channel)
		states, err := s.deliveries.GetNotificationDeliveryStates(ctx, eventID)
		if err != nil {
			return err
		}
		switch states[key] {
		case "delivered":
			continue
		case "failed":
			failures = append(failures, fmt.Errorf("通知渠道 %s 之前已明确失败", stringValue(channel["notificationType"])))
			continue
		case "sending", "uncertain":
			uncertain = true
			continue
		}
		attempts := intValue(channel["retry"])
		if attempts < 1 {
			attempts = 1
		}
		for attempt := 0; attempt < attempts; attempt++ {
			if err := s.deliveries.SetNotificationDelivery(ctx, eventID, key, "sending"); err != nil {
				return err
			}
			err = s.deliverChannel(ctx, config, channel, subscription, text, status)
			if errors.Is(err, ErrDeliveryUncertain) {
				_ = s.deliveries.SetNotificationDelivery(ctx, eventID, key, "uncertain")
				uncertain = true
				break
			}
			var rejected rejectedDelivery
			if errors.As(err, &rejected) {
				_ = s.deliveries.SetNotificationDelivery(ctx, eventID, key, "failed")
				failures = append(failures, fmt.Errorf("%s：%w", stringValue(channel["notificationType"]), err))
				break
			}
			if err == nil {
				if err := s.deliveries.SetNotificationDelivery(ctx, eventID, key, "delivered"); err != nil {
					return err
				}
				break
			}
			_ = s.deliveries.SetNotificationDelivery(ctx, eventID, key, "failed")
			if attempt+1 == attempts {
				failures = append(failures, fmt.Errorf("%s：%w", stringValue(channel["notificationType"]), err))
				break
			}
			if err := s.options.Sleep(ctx, 1); err != nil {
				return err
			}
		}
	}
	if uncertain {
		return ErrDeliveryUncertain
	}
	return errors.Join(failures...)
}

// Reconcile reports only persisted delivery outcomes; remote services without idempotency are never guessed.
func (s *Service) Reconcile(ctx context.Context, eventID string, subscription map[string]any, text, status string) (bool, error) {
	if enabled, exists := subscription["message"]; exists && !truth(enabled) {
		return true, nil
	}
	config, err := s.store.GetConfig(ctx)
	if err != nil {
		return false, err
	}
	channels, err := eligibleChannels(config, status)
	if err != nil {
		return false, err
	}
	states, err := s.deliveries.GetNotificationDeliveryStates(ctx, eventID)
	if err != nil {
		return false, err
	}
	done := true
	for index, channel := range channels {
		switch states[deliveryKey(index, channel)] {
		case "delivered":
		case "failed":
			done = false
		case "sending", "uncertain":
			done = false
		default:
			done = false
		}
	}
	return done, nil
}

func eligibleChannels(config map[string]any, status string) ([]map[string]any, error) {
	raw, ok := config["notificationConfigList"]
	if !ok || raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var channels []map[string]any
	if err := json.Unmarshal(data, &channels); err != nil {
		return nil, fmt.Errorf("通知配置列表格式错误：%w", err)
	}
	filtered := channels[:0]
	for _, channel := range channels {
		if !truth(channel["enable"]) || !hasStatus(channel["statusList"], status) || !supportedChannel(stringValue(channel["notificationType"])) {
			continue
		}
		filtered = append(filtered, channel)
	}
	sort.SliceStable(filtered, func(i, j int) bool { return intValue(filtered[i]["sort"]) < intValue(filtered[j]["sort"]) })
	return filtered, nil
}

func deliveryKey(index int, channel map[string]any) string {
	data, _ := json.Marshal(channel)
	digest := sha256.Sum256(data)
	return fmt.Sprintf("%03d:%s:%s", index, stringValue(channel["notificationType"]), hex.EncodeToString(digest[:8]))
}

func hasStatus(value any, expected string) bool {
	switch list := value.(type) {
	case []any:
		for _, item := range list {
			if strings.EqualFold(stringValue(item), expected) {
				return true
			}
		}
	case []string:
		for _, item := range list {
			if strings.EqualFold(item, expected) {
				return true
			}
		}
	}
	return false
}

func stringValue(value any) string { text, _ := value.(string); return text }
func truth(value any) bool         { result, _ := value.(bool); return result }
func intValue(value any) int {
	switch number := value.(type) {
	case float64:
		return int(number)
	case int:
		return number
	case int64:
		return int(number)
	}
	return 0
}

func supportedChannel(kind string) bool {
	return strings.EqualFold(kind, "WEB_HOOK") || strings.EqualFold(kind, "EMBY_REFRESH")
}
