package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
)

const JobSubscriptionEffects = "subscription-effects"

type SubscriptionEffects struct {
	Action        string           `json:"action"`
	Old           map[string]any   `json:"old,omitempty"`
	New           map[string]any   `json:"new,omitempty"`
	Subscriptions []map[string]any `json:"subscriptions,omitempty"`
	DeleteFiles   bool             `json:"deleteFiles"`
}

// SetSubscriptionPathResolver 将排队时的真实路径固定到持久意图，避免配置变化后搬错位置。
func (s *Server) SetSubscriptionPathResolver(resolver func(context.Context, map[string]any) (string, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pathResolver = resolver
}

func (s *Server) freezeSubscriptionPath(ctx context.Context, item map[string]any) (map[string]any, error) {
	if s.pathResolver == nil {
		return nil, errors.New("订阅媒体路径解析尚未接入")
	}
	path, err := s.pathResolver(ctx, item)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errors.New("订阅媒体路径为空")
	}
	copy := copySubscription(item)
	copy["customDownloadPath"], copy["customDownloadPathTemplate"] = true, path
	return copy, nil
}

func (s *Server) saveSubscriptionEffects(ctx context.Context, items []map[string]any, effect SubscriptionEffects) error {
	store, ok := s.store.(interface {
		ReplaceSubscriptionsAndEnqueue(context.Context, []map[string]any, string, string, json.RawMessage) error
	})
	if !ok {
		return errors.New("订阅变更需要持久任务存储")
	}
	request, err := subscriptionEffectsRequest(effect)
	if err != nil {
		return err
	}
	return store.ReplaceSubscriptionsAndEnqueue(ctx, items, request.Kind, request.Key, request.Payload)
}

func subscriptionEffectsRequest(effect SubscriptionEffects) (jobs.EnqueueRequest, error) {
	payload, err := json.Marshal(effect)
	if err != nil {
		return jobs.EnqueueRequest{}, err
	}
	key, err := randomString(16)
	if err != nil {
		return jobs.EnqueueRequest{}, err
	}
	return jobs.EnqueueRequest{Kind: JobSubscriptionEffects, Key: "subscription-effects:" + key, Payload: payload}, nil
}

func subscriptionRefreshRequest(subscriptionID string) (jobs.EnqueueRequest, error) {
	payload, err := json.Marshal(map[string]any{"subscriptionId": subscriptionID, "automatic": false})
	if err != nil {
		return jobs.EnqueueRequest{}, err
	}
	key, err := randomString(16)
	if err != nil {
		return jobs.EnqueueRequest{}, err
	}
	return jobs.EnqueueRequest{Kind: "rss-refresh", Key: "subscription-updated:" + key, Payload: payload}, nil
}

func (s *Server) updateSubscriptionFieldsAndEnqueue(ctx context.Context, id string, fields map[string]any, requests []jobs.EnqueueRequest) error {
	store, ok := s.store.(interface {
		UpdateSubscriptionFieldsAndEnqueue(context.Context, string, map[string]any, []jobs.EnqueueRequest) error
	})
	if !ok {
		return errors.New("修改订阅需要原子保存字段和后台任务")
	}
	return store.UpdateSubscriptionFieldsAndEnqueue(ctx, id, fields, requests)
}

// UpdateSubscription 保留进度，按用户选择持久记录媒体迁移意图。
func (s *Server) UpdateSubscription(ctx context.Context, provided map[string]any, move bool) error {
	if text(provided["id"]) == "" || text(provided["title"]) == "" {
		return errors.New("订阅 ID 和标题不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	config, err := s.store.GetConfig(ctx)
	if err != nil {
		return err
	}
	items, err := s.store.ListSubscriptions(ctx)
	if err != nil {
		return err
	}
	index := -1
	for i, old := range items {
		if old["id"] == provided["id"] {
			index = i
			break
		}
	}
	if index < 0 {
		return errors.New("订阅不存在")
	}
	old := items[index]
	merged := copySubscription(old)
	for key, value := range provided {
		if value != nil {
			merged[key] = value
		}
	}
	item := subscriptionDefaults(merged, config)
	item["currentEpisodeNumber"], item["lastDownloadTime"] = old["currentEpisodeNumber"], old["lastDownloadTime"]
	for i, other := range items {
		if i != index && text(other["title"]) == text(item["title"]) && integer(other["season"]) == integer(item["season"]) {
			return errors.New("订阅标题重复")
		}
	}

	fields := make(map[string]any, len(provided)+2)
	for key, value := range provided {
		if value != nil && key != "id" && key != "currentEpisodeNumber" && key != "lastDownloadTime" {
			fields[key] = item[key]
		}
	}
	if _, titleChanged := provided["title"]; titleChanged {
		full, initials := pinyinTitle(text(item["title"]))
		fields["pinyin"], fields["pinyinInitials"] = full, initials
	}
	sourceChanged := boolean(item["enable"]) && subscriptionRSSSourcesChanged(old, item)
	if !move && !sourceChanged {
		store, ok := s.store.(interface {
			UpdateSubscriptionFields(context.Context, string, map[string]any) error
		})
		if !ok {
			return errors.New("修改订阅需要事务字段存储")
		}
		return store.UpdateSubscriptionFields(ctx, text(item["id"]), fields)
	}

	requests := make([]jobs.EnqueueRequest, 0, 2)
	if move {
		before, err := s.freezeSubscriptionPath(ctx, old)
		if err != nil {
			return err
		}
		after, err := s.freezeSubscriptionPath(ctx, item)
		if err != nil {
			return err
		}
		request, err := subscriptionEffectsRequest(SubscriptionEffects{Action: "move", Old: before, New: after})
		if err != nil {
			return err
		}
		requests = append(requests, request)
	}
	if sourceChanged {
		request, err := subscriptionRefreshRequest(text(item["id"]))
		if err != nil {
			return err
		}
		requests = append(requests, request)
	}
	return s.updateSubscriptionFieldsAndEnqueue(ctx, text(item["id"]), fields, requests)
}

func subscriptionRSSSourcesChanged(old, updated map[string]any) bool {
	oldURLs := subscriptionRSSURLs(old)
	newURLs := subscriptionRSSURLs(updated)
	if len(oldURLs) != len(newURLs) {
		return true
	}
	for index := range oldURLs {
		if oldURLs[index] != newURLs[index] {
			return true
		}
	}
	return false
}

func subscriptionRSSURLs(subscription map[string]any) []string {
	urls := []string{strings.TrimSpace(text(subscription["url"]))}
	appendStandby := func(entries []any) {
		for _, entry := range entries {
			if source, ok := entry.(map[string]any); ok {
				urls = append(urls, strings.TrimSpace(text(source["url"])))
			}
		}
	}
	switch entries := subscription["standbyRssList"].(type) {
	case []any:
		appendStandby(entries)
	case []map[string]any:
		for _, source := range entries {
			urls = append(urls, strings.TrimSpace(text(source["url"])))
		}
	}
	return urls
}

// DeleteSubscriptions 保存删除前快照，再让工作者清理缓存与用户选择的媒体。
func (s *Server) DeleteSubscriptions(ctx context.Context, ids []string, deleteFiles bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.store.ListSubscriptions(ctx)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	kept, removed := make([]map[string]any, 0, len(items)), make([]map[string]any, 0)
	for _, item := range items {
		if !wanted[text(item["id"])] {
			kept = append(kept, item)
			continue
		}
		if deleteFiles {
			item, err = s.freezeSubscriptionPath(ctx, item)
			if err != nil {
				return err
			}
		}
		removed = append(removed, item)
	}
	if len(removed) == 0 {
		return errors.New("订阅不存在")
	}
	return s.saveSubscriptionEffects(ctx, kept, SubscriptionEffects{Action: "delete", Subscriptions: removed, DeleteFiles: deleteFiles})
}
