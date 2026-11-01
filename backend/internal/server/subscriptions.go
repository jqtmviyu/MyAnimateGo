package server

import (
	"context"
	"errors"
	"strings"
	"time"
)

// SetSubscriptionCoverImporter connects imported image URLs to the local cover store.
func (s *Server) SetSubscriptionCoverImporter(importer func(context.Context, string) (string, error)) {
	s.coverMu.Lock()
	s.coverImporter = importer
	s.coverMu.Unlock()
}

func (s *Server) importCover(ctx context.Context, image string) (string, error) {
	s.coverMu.RLock()
	importer := s.coverImporter
	s.coverMu.RUnlock()
	if importer == nil {
		return "", errors.New("导入封面需要配置封面存储")
	}
	cover, err := importer(ctx, image)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cover) == "" {
		return "", errors.New("封面存储没有返回文件路径")
	}
	return cover, nil
}

// AddSubscription 通过同一条业务入口校验并保存新订阅。
func (s *Server) AddSubscription(ctx context.Context, subscription map[string]any, override bool) (map[string]any, error) {
	if strings.TrimSpace(text(subscription["title"])) == "" {
		return nil, errors.New("订阅标题不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	config, err := s.store.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	item := subscriptionDefaults(copySubscription(subscription), config)
	if text(item["id"]) == "" {
		id, err := randomString(16)
		if err != nil {
			return nil, err
		}
		item["id"] = id
	}
	if text(item["releaseDate"]) == "" && integer(item["year"]) == 0 {
		item["releaseDate"] = time.Now().Format("2006-01-02")
	}
	items, err := s.store.ListSubscriptions(ctx)
	if err != nil {
		return nil, err
	}
	itemID := text(item["id"])
	kept := make([]map[string]any, 0, len(items)+1)
	for _, old := range items {
		sameID := itemID != "" && text(old["id"]) == itemID
		sameTitleSeason := text(old["title"]) == text(item["title"]) && integer(old["season"]) == integer(item["season"])
		if sameID || sameTitleSeason {
			if !override {
				if sameID {
					return nil, errors.New("此订阅已存在")
				}
				return nil, errors.New("订阅标题重复")
			}
			continue
		}
		kept = append(kept, old)
	}
	kept = append(kept, item)
	if store, ok := s.store.(interface {
		ReplaceSubscriptionsAndScheduleRefresh(context.Context, []map[string]any, string, string) error
	}); ok {
		key, err := randomString(16)
		if err != nil {
			return nil, err
		}
		if err := store.ReplaceSubscriptionsAndScheduleRefresh(ctx, kept, text(item["id"]), "subscription-created:"+key); err != nil {
			return nil, err
		}
		return item, nil
	}
	if err := s.store.ReplaceSubscriptions(ctx, kept); err != nil {
		return nil, err
	}
	return item, nil
}

// ImportAni 按 SKIP 或 REPLACE 规则批量导入，并一次性保存全部改动。
func (s *Server) ImportAni(ctx context.Context, subscriptions []map[string]any, conflict string) error {
	if len(subscriptions) == 0 {
		return errors.New("导入列表为空")
	}
	conflict = strings.ToUpper(strings.TrimSpace(conflict))
	if conflict == "" {
		conflict = "REPLACE"
	}
	if conflict != "SKIP" && conflict != "REPLACE" {
		return errors.New("conflict 只接受 SKIP 或 REPLACE")
	}
	for _, item := range subscriptions {
		if strings.TrimSpace(text(item["title"])) == "" || integer(item["season"]) < 1 {
			return errors.New("导入订阅缺少标题或季数")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.store.ListSubscriptions(ctx)
	if err != nil {
		return err
	}
	items = append([]map[string]any(nil), items...)
	editedIDs := make(map[string]bool, len(subscriptions))
	for _, source := range subscriptions {
		incoming := copySubscription(source)
		match := -1
		for i, existing := range items {
			if text(existing["title"]) == text(incoming["title"]) && integer(existing["season"]) == integer(incoming["season"]) {
				match = i
				break
			}
		}
		if match >= 0 && conflict == "SKIP" {
			continue
		}
		image := strings.TrimSpace(text(incoming["image"]))
		if image != "" {
			cover, err := s.importCover(ctx, image)
			if err != nil {
				return err
			}
			incoming["cover"] = cover
		} else if match >= 0 {
			if cover, exists := items[match]["cover"]; exists {
				incoming["cover"] = cover
			}
		}
		if match >= 0 {
			items[match] = mergeImportedSubscription(items[match], incoming)
			editedIDs[text(items[match]["id"])] = true
			continue
		}
		id, err := randomString(16)
		if err != nil {
			return err
		}
		incoming["id"] = id
		items = append(items, incoming)
		editedIDs[id] = true
	}
	if len(editedIDs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(editedIDs))
	for id := range editedIDs {
		ids = append(ids, id)
	}
	store, ok := s.store.(interface {
		ReplaceSubscriptionsPreservingCurrent(context.Context, []map[string]any, []string) error
	})
	if !ok {
		return errors.New("导入订阅需要事务合并存储")
	}
	return store.ReplaceSubscriptionsPreservingCurrent(ctx, items, ids)
}

func mergeImportedSubscription(existing, incoming map[string]any) map[string]any {
	merged := copySubscription(existing)
	for key, value := range incoming {
		switch key {
		case "id", "currentEpisodeNumber", "lastDownloadTime":
			continue
		default:
			merged[key] = value
		}
	}
	return merged
}

func copySubscription(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
