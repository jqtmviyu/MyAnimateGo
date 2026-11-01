package app

import (
	"context"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

// runtimeStore 让生产任务始终运行，开发模式才读取持久化的暂停状态。
// 模式由启动参数决定，恢复备份和普通配置保存均不能改变它。
type runtimeStore struct {
	*storage.Store
	development bool
}

func (s *runtimeStore) GetConfig(ctx context.Context) (map[string]any, error) {
	config, err := s.Store.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	config["developmentMode"] = s.development
	if !s.development {
		config["automaticTasksPaused"] = false
	}
	return config, nil
}

func (s *runtimeStore) persistedConfig(config map[string]any) map[string]any {
	result := make(map[string]any, len(config))
	for key, value := range config {
		if key != "developmentMode" {
			result[key] = value
		}
	}
	if !s.development {
		result["automaticTasksPaused"] = false
	}
	return result
}

func (s *runtimeStore) SaveConfig(ctx context.Context, config map[string]any) error {
	return s.Store.SaveConfig(ctx, s.persistedConfig(config))
}

func (s *runtimeStore) UpdateConfigFields(ctx context.Context, fields map[string]any) error {
	return s.Store.UpdateConfigFields(ctx, s.persistedConfig(fields))
}
