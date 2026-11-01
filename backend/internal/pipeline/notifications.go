package pipeline

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
)

type notificationOperationContextKey struct{}

func (e *Engine) notificationContext(ctx context.Context, kind string) context.Context {
	if jobs.JobID(ctx) != "" {
		return ctx
	}
	if ctx.Value(notificationOperationContextKey{}) != nil {
		return ctx
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return context.WithValue(ctx, notificationOperationContextKey{}, fmt.Sprintf("%s:%d", kind, e.now().UnixNano()))
	}
	return context.WithValue(ctx, notificationOperationContextKey{}, kind+":"+hex.EncodeToString(token[:]))
}

func notificationOperationID(ctx context.Context) string {
	if id := jobs.JobID(ctx); id != "" {
		return id
	}
	id, _ := ctx.Value(notificationOperationContextKey{}).(string)
	return id
}

func (e *Engine) enqueueNotification(ctx context.Context, config, subscription map[string]any, key, text, status string) error {
	if enabled, exists := subscription["message"].(bool); exists && !enabled {
		return nil
	}
	if !notificationStatusEnabled(config, status) {
		return nil
	}
	if e.options.EnqueueNotification == nil {
		return fmt.Errorf("%s 通知已启用，但持久通知队列未接入", status)
	}
	if key == "" {
		return errors.New("通知幂等键不能为空")
	}
	snapshot := make(map[string]any, len(subscription))
	for field, value := range subscription {
		snapshot[field] = value
	}
	if err := e.options.EnqueueNotification(ctx, key, snapshot, text, status); err != nil {
		return fmt.Errorf("保存 %s 通知任务失败：%w", status, err)
	}
	return nil
}

func (e *Engine) enqueueDownloadStarted(ctx context.Context, config, subscription map[string]any, item Item) error {
	hash := strings.ToLower(item.InfoHash)
	if hash == "" {
		return errors.New("Transmission 已接受任务，但通知缺少种子 hash")
	}
	snapshot := make(map[string]any, len(subscription)+1)
	for key, value := range subscription {
		snapshot[key] = value
	}
	snapshot["subgroup"] = item.Subgroup
	text := fmt.Sprintf("%s 已更新", item.ReName)
	if !item.Master {
		text = "(备用RSS) " + text
	}
	return e.enqueueNotification(ctx, config, snapshot, "download-start:"+hash, text, "DOWNLOAD_START")
}

func notificationStatusEnabled(config map[string]any, status string) bool {
	var channels []map[string]any
	switch value := config["notificationConfigList"].(type) {
	case []any:
		for _, item := range value {
			if channel, ok := item.(map[string]any); ok {
				channels = append(channels, channel)
			}
		}
	case []map[string]any:
		channels = value
	}
	for _, channel := range channels {
		if !valueBool(channel["enable"]) {
			continue
		}
		var statuses []string
		switch value := channel["statusList"].(type) {
		case []any:
			for _, item := range value {
				if text, ok := item.(string); ok {
					statuses = append(statuses, text)
				}
			}
		case []string:
			statuses = value
		}
		for _, item := range statuses {
			if strings.EqualFold(item, status) {
				return true
			}
		}
	}
	return false
}

func (e *Engine) enqueueOmissions(ctx context.Context, config, subscription map[string]any, items []Item) error {
	missing := omitList(config, subscription, items)
	if len(missing) == 0 || len(missing) > 10 {
		return nil
	}
	date := e.now().Format("2006-01-02")
	id, title, season := valueString(subscription["id"]), valueString(subscription["title"]), valueInt(subscription["season"])
	for _, episode := range missing {
		text := fmt.Sprintf("缺少集数 %s S%02dE%02d", title, season, episode)
		key := fmt.Sprintf("omit:%s:%d:%s", id, episode, date)
		if err := e.enqueueNotification(ctx, config, subscription, key, text, "OMIT"); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) enqueueCompleted(ctx context.Context, config, subscription map[string]any, currentDownloadCount int) error {
	if !valueBool(config["autoDisabled"]) || !valueBool(subscription["enable"]) || valueBool(subscription["ova"]) {
		return nil
	}
	total := valueInt(subscription["totalEpisodeNumber"])
	if total < 1 || currentDownloadCount < total {
		return nil
	}
	text := fmt.Sprintf("%s 订阅已完结", valueString(subscription["title"]))
	key := fmt.Sprintf("completed:%s:%d:%v", valueString(subscription["id"]), valueInt(subscription["season"]), subscription["lastDownloadTime"])
	return e.enqueueNotification(ctx, config, subscription, key, text, "COMPLETED")
}
