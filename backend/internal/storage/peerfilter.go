package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// 节点过滤状态单独存储，管理配置接口不会加载动态名单或订阅令牌。
func (s *Store) LoadPeerFilterState(ctx context.Context) (json.RawMessage, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM config WHERE name='peer-filter'`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return data, err
}
func (s *Store) SavePeerFilterState(ctx context.Context, data json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO config(name,value) VALUES('peer-filter',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value`, []byte(data))
	return err
}
