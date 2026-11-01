package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
)

// 启动和恢复备份时结束已移除功能的任务，避免继续执行或重试。
const retireRemovedJobsSQL = `UPDATE jobs SET state='resolved', owner='', lease_until=0, last_error='本地刮削功能已移除'
WHERE kind='library-scrape' AND state!='resolved';
UPDATE jobs SET state='resolved', owner='', lease_until=0, last_error='摸鱼检测已移除'
WHERE kind='notification-delivery' AND json_extract(CAST(payload AS TEXT), '$.status')='PROCRASTINATING' AND state!='resolved';
UPDATE jobs SET state='resolved', owner='', lease_until=0, last_error='黑名单更新已由节点过滤服务接管' WHERE kind='blocklist-update' AND state!='resolved'`

const jobColumns = `id,kind,job_key,payload,state,attempts,owner,last_error,available_at,lease_until`

func (s *Store) EnqueueJob(ctx context.Context, kind, key string, payload json.RawMessage) (jobs.Job, error) {
	if kind == "" || key == "" || !json.Valid(payload) {
		return jobs.Job{}, errors.New("任务类型、业务键和 JSON 内容不能为空")
	}
	hash := sha256.Sum256([]byte(kind + "\x00" + key))
	id := hex.EncodeToString(hash[:])
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO jobs(id,kind,job_key,payload,state,available_at,created_at) VALUES(?,?,?,?,'pending',?,?) ON CONFLICT(kind,job_key) DO NOTHING`, id, kind, key, []byte(payload), now, now)
	if err != nil {
		return jobs.Job{}, fmt.Errorf("保存任务：%w", err)
	}
	return s.GetJob(ctx, id)
}

func (s *Store) GetJob(ctx context.Context, id string) (jobs.Job, error) {
	job, err := scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id=?`, id))
	if err != nil {
		return jobs.Job{}, fmt.Errorf("读取任务：%w", err)
	}
	return *job, nil
}

func (s *Store) ClaimJob(ctx context.Context, owner string, now time.Time, lease time.Duration) (*jobs.Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	job, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE (state='pending' AND available_at<=?) OR (state='running' AND lease_until<=?) ORDER BY created_at,rowid LIMIT 1`, now.UnixMilli(), now.UnixMilli()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var leaseID [16]byte
	if _, err := rand.Read(leaseID[:]); err != nil {
		return nil, fmt.Errorf("生成任务租约标识：%w", err)
	}
	leaseOwner := owner + "/" + hex.EncodeToString(leaseID[:])
	job.Owner = leaseOwner
	job.State = "running"
	job.Attempts++
	job.LeaseUntil = now.Add(lease)
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET state='running',owner=?,attempts=?,lease_until=? WHERE id=?`, leaseOwner, job.Attempts, job.LeaseUntil.UnixMilli(), job.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *Store) ResolveJob(ctx context.Context, id, owner string) error {
	return s.finishJob(ctx, id, owner, "resolved", "", time.Time{})
}
func (s *Store) RetryJob(ctx context.Context, id, owner, message string, at time.Time) error {
	return s.finishJob(ctx, id, owner, "pending", message, at)
}
func (s *Store) FailJob(ctx context.Context, id, owner, message string) error {
	return s.finishJob(ctx, id, owner, "failed", message, time.Time{})
}

// 修复配置后由操作者显式重试，已完成或仍在执行的任务保持原状。
func (s *Store) RequeueJob(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET state='pending',attempts=0,available_at=?,last_error='',owner='',lease_until=0 WHERE id=? AND state='failed'`, time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("只有失败任务可以重新排队")
	}
	return nil
}

func (s *Store) finishJob(ctx context.Context, id, owner, state, message string, at time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET state=?,last_error=?,available_at=?,owner='',lease_until=0 WHERE id=? AND owner=? AND state='running'`, state, message, at.UnixMilli(), id, owner)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("任务领取状态已变化，结果未覆盖新状态")
	}
	return nil
}

func scanJob(row interface{ Scan(...any) error }) (*jobs.Job, error) {
	job := &jobs.Job{}
	var payload []byte
	var available, lease int64
	if err := row.Scan(&job.ID, &job.Kind, &job.Key, &payload, &job.State, &job.Attempts, &job.Owner, &job.LastError, &available, &lease); err != nil {
		return nil, err
	}
	job.Payload = append(json.RawMessage(nil), payload...)
	job.AvailableAt = time.UnixMilli(available)
	job.LeaseUntil = time.UnixMilli(lease)
	return job, nil
}
