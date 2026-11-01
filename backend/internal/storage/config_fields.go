package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// UpdateConfigFields 合并当前配置，外部验证或令牌刷新只回写自己负责的字段。
func (s *Store) UpdateConfigFields(ctx context.Context, fields map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT value FROM config WHERE name='current'`).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	config := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return err
		}
	}
	for key, value := range fields {
		config[key] = value
	}
	raw, err = json.Marshal(runtimeConfig(config))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO config(name,value) VALUES('current',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value`, raw); err != nil {
		return err
	}
	return tx.Commit()
}
