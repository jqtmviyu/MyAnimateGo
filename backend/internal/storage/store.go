package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store 保存应用配置和订阅业务记录。
type Store struct {
	db      *sql.DB
	dataDir string
}

// Open 打开 SQLite 数据库，并确保基础表已创建。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	// 备份和只读检查可能持有第二个连接，等待短事务释放 SQLite 文件锁。
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		db.Close()
		return nil, fmt.Errorf("读取数据库格式版本：%w", err)
	}
	if version > 2 {
		db.Close()
		return nil, fmt.Errorf("数据库格式版本 %d 高于当前程序支持的版本 2", version)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;
CREATE TABLE IF NOT EXISTS config (
	name TEXT PRIMARY KEY,
	value BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS subscriptions (
	id TEXT PRIMARY KEY,
	value BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS jobs (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	job_key TEXT NOT NULL,
	payload BLOB NOT NULL,
	state TEXT NOT NULL,
	attempts INTEGER NOT NULL DEFAULT 0,
	available_at INTEGER NOT NULL,
	lease_until INTEGER NOT NULL DEFAULT 0,
	owner TEXT NOT NULL DEFAULT '',
	last_error TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	UNIQUE(kind,job_key)
);
CREATE TABLE IF NOT EXISTS notification_deliveries (
	event_id TEXT NOT NULL,
	delivery_key TEXT NOT NULL,
	state TEXT NOT NULL,
	updated_at INTEGER NOT NULL,
	PRIMARY KEY(event_id, delivery_key)
);
PRAGMA user_version = 2;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize SQLite: %w", err)
	}
	if _, err := db.Exec(retireRemovedJobsSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("清理已移除功能的任务：%w", err)
	}
	dataDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(dataDir); err == nil {
		dataDir = resolved
	}
	return &Store{db: db, dataDir: dataDir}, nil
}

// DataDir 返回 SQLite 数据库所在目录，附件以此目录为根保存。
func (s *Store) DataDir() string { return s.dataDir }

// Close 关闭数据库连接。
func (s *Store) Close() error { return s.db.Close() }

// GetConfig 返回已保存配置；尚未配置时返回空对象。
func (s *Store) GetConfig(ctx context.Context) (map[string]any, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM config WHERE name = 'current'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if config == nil {
		config = map[string]any{}
	}
	return runtimeConfig(config), nil
}

// SaveConfig 原子替换运行配置，保留停用字段之外的未识别设置。
func (s *Store) SaveConfig(ctx context.Context, config map[string]any) error {
	raw, err := json.Marshal(runtimeConfig(config))
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO config(name, value) VALUES('current', ?)
ON CONFLICT(name) DO UPDATE SET value = excluded.value`, raw)
	if err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

// ListSubscriptions 按插入顺序返回订阅，保持旧前端依赖的稳定顺序。
func (s *Store) ListSubscriptions(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT value FROM subscriptions ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("decode subscription: %w", err)
		}
		result = append(result, runtimeSubscription(item))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// PutSubscription 新增或更新一个带稳定 ID 的订阅。
func (s *Store) PutSubscription(ctx context.Context, subscription map[string]any) error {
	id, ok := subscription["id"].(string)
	if !ok || id == "" {
		return errors.New("subscription id is required")
	}
	raw, err := json.Marshal(runtimeSubscription(subscription))
	if err != nil {
		return fmt.Errorf("encode subscription: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO subscriptions(id, value) VALUES(?, ?)
ON CONFLICT(id) DO UPDATE SET value = excluded.value`, id, raw)
	if err != nil {
		return fmt.Errorf("save subscription: %w", err)
	}
	return nil
}

// ReplaceSubscriptions 在一个事务中全量替换订阅集合。
func (s *Store) ReplaceSubscriptions(ctx context.Context, subscriptions []map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscriptions`); err != nil {
		return fmt.Errorf("clear subscriptions: %w", err)
	}
	for _, subscription := range subscriptions {
		id, ok := subscription["id"].(string)
		if !ok || id == "" {
			return errors.New("subscription id is required")
		}
		raw, err := json.Marshal(runtimeSubscription(subscription))
		if err != nil {
			return fmt.Errorf("encode subscription: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO subscriptions(id, value) VALUES(?, ?)`, id, raw); err != nil {
			return fmt.Errorf("replace subscription: %w", err)
		}
	}
	return tx.Commit()
}

// DeleteSubscriptions 删除指定 ID 的订阅。
func (s *Store) DeleteSubscriptions(ctx context.Context, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM subscriptions WHERE id = ?`, id); err != nil {
			return fmt.Errorf("delete subscription: %w", err)
		}
	}
	return tx.Commit()
}

// ImportSnapshot 在一个事务中合并配置和订阅，重复导入保持幂等。
func (s *Store) ImportSnapshot(ctx context.Context, config map[string]any, subscriptions []map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	configJSON, err := json.Marshal(runtimeConfig(config))
	if err != nil {
		return fmt.Errorf("encode imported config: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO config(name, value) VALUES('current', ?)
ON CONFLICT(name) DO UPDATE SET value = excluded.value`, configJSON); err != nil {
		return fmt.Errorf("import config: %w", err)
	}
	for _, subscription := range subscriptions {
		id, ok := subscription["id"].(string)
		if !ok || id == "" {
			return errors.New("imported subscription id is required")
		}
		raw, err := json.Marshal(runtimeSubscription(subscription))
		if err != nil {
			return fmt.Errorf("encode imported subscription: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO subscriptions(id, value) VALUES(?, ?)
ON CONFLICT(id) DO UPDATE SET value = excluded.value`, id, raw); err != nil {
			return fmt.Errorf("import subscription: %w", err)
		}
	}
	return tx.Commit()
}

// 历史配置读写时清理已停用字段，保留 Transmission 的连接信息。
func runtimeConfig(config map[string]any) map[string]any {
	return cleanNotificationStatuses(withoutFields(config, "customJs", "customCss", "downloadToolType", "legacyDownloader", "qbUseDownloadPath", "qbContentLayout", "qbRenameTitle", "ratioLimit", "seedingTimeLimit", "inactiveSeedingTimeLimit", "priorityKeywordsEnable", "priorityKeywords", "autoTrackersUpdate", "trackersUpdateUrls", "scrape", "followDay", "bangumiIniEnabled", "tmdbImage", "procrastinating", "procrastinatingDay", "procrastinatingMasterOnly", "bgmTokenType", "bgmAppID", "bgmAppSecret", "bgmRefreshToken", "bgmRedirectUri", "bgmOAuthBaseURL", "githubToken", "autoUpdate", "autoStart", "version", "tmdbIdPlexMode"))
}

func withoutFields(value map[string]any, fields ...string) map[string]any {
	var result map[string]any
	for _, field := range fields {
		if _, ok := value[field]; !ok {
			continue
		}
		if result == nil {
			result = make(map[string]any, len(value))
			for key, item := range value {
				result[key] = item
			}
		}
		delete(result, field)
	}
	if result == nil {
		return value
	}
	return result
}

func runtimeSubscription(subscription map[string]any) map[string]any {
	return withoutFields(subscription, "customPriorityKeywordsEnable", "customPriorityKeywords", "procrastinating")
}
