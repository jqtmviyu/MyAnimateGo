package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// UpdateSubscriptionFields 在事务内合并当前记录，后台进度不会覆盖用户的新设置。
func (s *Store) UpdateSubscriptionFields(ctx context.Context, id string, fields map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := patchSubscriptionFields(ctx, tx, id, fields); err != nil {
		return err
	}
	return tx.Commit()
}

func patchSubscriptionFields(ctx context.Context, tx *sql.Tx, id string, fields map[string]any) error {
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT value FROM subscriptions WHERE id=?`, id).Scan(&raw); err != nil {
		return fmt.Errorf("读取待更新订阅：%w", err)
	}
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil {
		return err
	}
	for key, value := range fields {
		if key != "id" {
			item[key] = value
		}
	}
	raw, err := json.Marshal(runtimeSubscription(item))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET value=? WHERE id=?`, raw, id); err != nil {
		return err
	}
	return nil
}

// UpdateSubscriptionFieldsBatch 原子更新选中的订阅字段，保留其他当前字段。
func (s *Store) UpdateSubscriptionFieldsBatch(ctx context.Context, ids []string, fields map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		var raw []byte
		if err := tx.QueryRowContext(ctx, `SELECT value FROM subscriptions WHERE id=?`, id).Scan(&raw); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return fmt.Errorf("读取待批量更新订阅：%w", err)
		}
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			return err
		}
		for key, value := range fields {
			if key != "id" && key != "currentEpisodeNumber" && key != "lastDownloadTime" {
				item[key] = value
			}
		}
		raw, err = json.Marshal(runtimeSubscription(item))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET value=? WHERE id=?`, raw, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReplaceSubscriptionsPreservingCurrent 在当前记录上提交导入，保留并发更新的进度。
func (s *Store) ReplaceSubscriptionsPreservingCurrent(ctx context.Context, subscriptions []map[string]any, editedIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,value FROM subscriptions`)
	if err != nil {
		return err
	}
	current := make(map[string]map[string]any)
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			_ = rows.Close()
			return err
		}
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			_ = rows.Close()
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
	edited := make(map[string]bool, len(editedIDs))
	for _, id := range editedIDs {
		edited[id] = true
	}
	for _, item := range subscriptions {
		id, _ := item["id"].(string)
		if id == "" {
			return errors.New("subscription id is required")
		}
		if old, exists := current[id]; exists {
			if !edited[id] {
				item = old
			} else {
				for _, key := range []string{"currentEpisodeNumber", "lastDownloadTime"} {
					if value, exists := old[key]; exists {
						item[key] = value
					} else {
						delete(item, key)
					}
				}
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
	return tx.Commit()
}
