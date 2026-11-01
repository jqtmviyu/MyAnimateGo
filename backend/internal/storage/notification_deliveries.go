package storage

import (
	"context"
	"fmt"
	"time"
)

// SetNotificationDelivery persists channel-level progress before retry can resend it.
func (s *Store) SetNotificationDelivery(ctx context.Context, eventID, key, state string) error {
	if eventID == "" || key == "" {
		return fmt.Errorf("通知事件 ID 和渠道键不能为空")
	}
	switch state {
	case "sending", "delivered", "failed", "uncertain":
	default:
		return fmt.Errorf("无效的通知投递状态 %q", state)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO notification_deliveries(event_id, delivery_key, state, updated_at)
VALUES(?, ?, ?, ?) ON CONFLICT(event_id, delivery_key) DO UPDATE SET state = excluded.state, updated_at = excluded.updated_at`, eventID, key, state, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("保存通知投递状态：%w", err)
	}
	return nil
}

func (s *Store) GetNotificationDeliveryStates(ctx context.Context, eventID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT delivery_key, state FROM notification_deliveries WHERE event_id = ? ORDER BY delivery_key`, eventID)
	if err != nil {
		return nil, fmt.Errorf("读取通知投递状态：%w", err)
	}
	defer rows.Close()
	deliveries := map[string]string{}
	for rows.Next() {
		var key, state string
		if err := rows.Scan(&key, &state); err != nil {
			return nil, err
		}
		deliveries[key] = state
	}
	return deliveries, rows.Err()
}
