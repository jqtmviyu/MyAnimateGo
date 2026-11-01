package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"modernc.org/sqlite"
)

type sqliteBackuper interface {
	NewBackup(string) (*sqlite.Backup, error)
}

// SnapshotDB writes a transactionally consistent SQLite copy without closing the live store.
func (s *Store) SnapshotDB(ctx context.Context, target string) (err error) {
	if target == "" {
		return errors.New("数据库快照目标不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	if _, err := os.Lstat(target); err == nil {
		return errors.New("数据库快照目标已存在")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	err = conn.Raw(func(driverConn any) error {
		backuper, ok := driverConn.(sqliteBackuper)
		if !ok {
			return errors.New("SQLite 驱动不支持在线备份")
		}
		backup, err := backuper.NewBackup(target)
		if err != nil {
			return err
		}
		finished := false
		defer func() {
			if !finished {
				_ = backup.Finish()
			}
		}()
		for more := true; more; {
			if err := ctx.Err(); err != nil {
				return err
			}
			more, err = backup.Step(256)
			if err != nil {
				return err
			}
		}
		err = backup.Finish()
		finished = true
		return err
	})
	if err != nil {
		_ = os.Remove(target)
		return fmt.Errorf("创建 SQLite 一致快照失败：%w", err)
	}
	file, err := os.OpenFile(target, os.O_RDONLY, 0)
	if err == nil {
		err = file.Sync()
		_ = file.Close()
	}
	return err
}

// RestoreSnapshot atomically replaces business rows from a validated SQLite snapshot.
func (s *Store) RestoreSnapshot(ctx context.Context, source string) error {
	if source == "" {
		return errors.New("恢复数据库路径不能为空")
	}
	var targetVersion int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&targetVersion); err != nil {
		return err
	}
	if err := validateSnapshot(source, targetVersion); err != nil {
		return err
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	uri := (&url.URL{Scheme: "file", Path: absolute}).String()
	if _, err := s.db.ExecContext(ctx, `ATTACH DATABASE ? AS restore_snapshot`, uri); err != nil {
		return fmt.Errorf("打开数据库快照失败：%w", err)
	}
	defer s.db.ExecContext(context.Background(), `DETACH DATABASE restore_snapshot`)
	currentTables, err := snapshotTables(ctx, s.db, "sqlite_master")
	if err != nil {
		return err
	}
	sourceTables, err := snapshotTables(ctx, s.db, "restore_snapshot.sqlite_master")
	if err != nil {
		return err
	}
	if strings.Join(currentTables, "\x00") != strings.Join(sourceTables, "\x00") {
		return errors.New("数据库快照的业务表结构与当前程序不一致")
	}
	currentSchema, err := snapshotSchema(ctx, s.db, "sqlite_master")
	if err != nil {
		return err
	}
	sourceSchema, err := snapshotSchema(ctx, s.db, "restore_snapshot.sqlite_master")
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(currentSchema, sourceSchema) {
		return errors.New("数据库快照的表、索引或触发器定义与当前数据库不一致")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	for i := len(currentTables) - 1; i >= 0; i-- {
		table := currentTables[i]
		quoted := quoteIdentifier(table)
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+quoted); err != nil {
			return fmt.Errorf("清空现有 %s 失败：%w", table, err)
		}
	}
	for _, table := range currentTables {
		quoted := quoteIdentifier(table)
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+quoted+` SELECT * FROM restore_snapshot.`+quoted); err != nil {
			return fmt.Errorf("恢复 %s 失败：%w", table, err)
		}
	}
	if _, err := tx.ExecContext(ctx, retireRemovedJobsSQL); err != nil {
		return fmt.Errorf("清理恢复的废弃任务：%w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交数据库恢复失败：%w", err)
	}
	return nil
}

type schemaObject struct {
	Type  string
	Name  string
	Table string
	SQL   string
}

func snapshotSchema(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, catalog string) ([]schemaObject, error) {
	rows, err := db.QueryContext(ctx, `SELECT type,name,tbl_name,COALESCE(sql,'') FROM `+catalog+` WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	objects := make([]schemaObject, 0)
	for rows.Next() {
		var object schemaObject
		if err := rows.Scan(&object.Type, &object.Name, &object.Table, &object.SQL); err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return objects, nil
}

func validateSnapshot(path string, expectedVersion int) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var check string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&check); err != nil || check != "ok" {
		return fmt.Errorf("数据库快照校验失败：%v %s", err, check)
	}
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version != expectedVersion {
		return fmt.Errorf("数据库快照版本 %d 与当前数据库版本 %d 不匹配", version, expectedVersion)
	}
	for _, table := range []string{"config", "subscriptions", "jobs"} {
		var name string
		if err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			return fmt.Errorf("数据库快照缺少 %s 表：%w", table, err)
		}
	}
	return nil
}

func snapshotTables(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, catalog string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM `+catalog+` WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(tables)
	return tables, nil
}

func quoteIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
