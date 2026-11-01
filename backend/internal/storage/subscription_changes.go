package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// ReplaceSubscriptionsAndScheduleRefresh 在同一事务中保存订阅和创建后的刷新意图。
func (s *Store) ReplaceSubscriptionsAndScheduleRefresh(ctx context.Context, subscriptions []map[string]any, subscriptionID, eventKey string) error {
	payload, err := json.Marshal(map[string]any{"subscriptionId": subscriptionID, "automatic": false})
	if err != nil {
		return err
	}
	kind := "rss-refresh"
	for _, item := range subscriptions {
		if item["id"] == subscriptionID && item["enable"] == false {
			kind = "subscription-progress"
			break
		}
	}
	return s.ReplaceSubscriptionsAndEnqueue(ctx, subscriptions, kind, eventKey, payload)
}

// ReplaceSubscriptionsAndEnqueue 让订阅变更和其外部操作意图一起提交。
func (s *Store) ReplaceSubscriptionsAndEnqueue(ctx context.Context, subscriptions []map[string]any, kind, eventKey string, payload json.RawMessage) error {
	if kind == "" || eventKey == "" || !json.Valid(payload) {
		return errors.New("订阅变更任务缺少类型、业务键或有效内容")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var intent struct {
		SubscriptionID string         `json:"subscriptionId"`
		Action         string         `json:"action"`
		New            map[string]any `json:"new"`
	}
	if err := json.Unmarshal(payload, &intent); err != nil {
		return err
	}
	targetID := intent.SubscriptionID
	if intent.New != nil {
		targetID, _ = intent.New["id"].(string)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,value FROM subscriptions`)
	if err != nil {
		return err
	}
	current := map[string]map[string]any{}
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			rows.Close()
			return err
		}
		current[id] = item
	}
	rowErr := rows.Err()
	if err := rows.Close(); err != nil {
		return err
	}
	if rowErr != nil {
		return rowErr
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscriptions`); err != nil {
		return err
	}
	for _, item := range subscriptions {
		id, _ := item["id"].(string)
		if id == "" {
			return errors.New("subscription id is required")
		}
		// 没有被本次请求编辑的订阅使用事务内现值，保留工作者刚更新的状态。
		if old, exists := current[id]; exists {
			if id != targetID {
				item = old
			} else if intent.Action == "move" {
				item["currentEpisodeNumber"], item["lastDownloadTime"] = old["currentEpisodeNumber"], old["lastDownloadTime"]
			}
		}
		raw, err := json.Marshal(runtimeSubscription(item))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO subscriptions(id,value) VALUES(?,?)`, id, raw); err != nil {
			return err
		}
	}
	hash := sha256.Sum256([]byte(kind + "\x00" + eventKey))
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs(id,kind,job_key,payload,state,available_at,created_at) VALUES(?,?,?,?,'pending',?,?)`, hex.EncodeToString(hash[:]), kind, eventKey, payload, now, now); err != nil {
		return err
	}
	return tx.Commit()
}
