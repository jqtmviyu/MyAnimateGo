package storage

import "strings"

// 配置读写时只保留 Webhook 和 Emby 刷新，并清理旧渠道字段。
func cleanNotificationStatuses(config map[string]any) map[string]any {
	config = withoutFields(config, "aliveLimit", "barkDeviceKeys", "barkGroup", "barkLevel", "barkServerUrl", "barkUseMarkdown", "barkVolume", "embyRefresh", "fileMoveCopyModel", "fileMoveDeleteOldEpisode", "fileMoveOvaTarget", "fileMoveTarget", "mailAddressee", "mailFrom", "mailImage", "mailPassword", "mailSMTPHost", "mailSMTPPort", "mailSSLEnable", "mailTLSEnable", "openListUploadApiKey", "openListUploadDeleteLocalFile", "openListUploadDeleteOldEpisode", "openListUploadHost", "openListUploadOvaPath", "openListUploadPath", "serverChan3ApiUrl", "serverChanSendKey", "serverChanTitleAction", "serverChanType", "shell", "systemMsg", "telegramApiHost", "telegramBotToken", "telegramChatId", "telegramFormat", "telegramImage", "telegramTopicId", "mail", "mailAccount", "telegram", "serverChan", "systemMsg", "alist")
	changes := map[string]any{}
	if statuses, changed := activeNotificationStatuses(config["messageList"]); changed {
		changes["messageList"] = statuses
	}
	var channels []any
	switch value := config["notificationConfigList"].(type) {
	case []any:
		channels = value
	case []map[string]any:
		channels = make([]any, len(value))
		for i, channel := range value {
			channels[i] = channel
		}
	}
	cleaned := make([]any, 0, len(channels))
	changed := false
	for _, raw := range channels {
		channel, ok := raw.(map[string]any)
		if !ok {
			cleaned = append(cleaned, raw)
			continue
		}
		kind, _ := channel["notificationType"].(string)
		if !strings.EqualFold(kind, "WEB_HOOK") && !strings.EqualFold(kind, "EMBY_REFRESH") {
			changed = true
			continue
		}
		copy := withoutFields(channel, "aliveLimit", "barkDeviceKeys", "barkGroup", "barkLevel", "barkServerUrl", "barkUseMarkdown", "barkVolume", "embyRefresh", "fileMoveCopyModel", "fileMoveDeleteOldEpisode", "fileMoveOvaTarget", "fileMoveTarget", "mailAddressee", "mailFrom", "mailImage", "mailPassword", "mailSMTPHost", "mailSMTPPort", "mailSSLEnable", "mailTLSEnable", "openListUploadApiKey", "openListUploadDeleteLocalFile", "openListUploadDeleteOldEpisode", "openListUploadHost", "openListUploadOvaPath", "openListUploadPath", "serverChan3ApiUrl", "serverChanSendKey", "serverChanTitleAction", "serverChanType", "shell", "systemMsg", "telegramApiHost", "telegramBotToken", "telegramChatId", "telegramFormat", "telegramImage", "telegramTopicId")
		if len(copy) != len(channel) {
			changed = true
		}
		if statuses, retired := activeNotificationStatuses(copy["statusList"]); retired {
			updated := make(map[string]any, len(copy))
			for key, value := range copy {
				updated[key] = value
			}
			updated["statusList"] = statuses
			copy = updated
			changed = true
		}
		cleaned = append(cleaned, copy)
	}
	if changed {
		changes["notificationConfigList"] = cleaned
	}
	if len(changes) == 0 {
		return config
	}
	result := make(map[string]any, len(config))
	for key, value := range config {
		result[key] = value
	}
	for key, value := range changes {
		result[key] = value
	}
	return result
}

func activeNotificationStatuses(value any) (any, bool) {
	switch values := value.(type) {
	case []any:
		result := make([]any, 0, len(values))
		for _, item := range values {
			status, _ := item.(string)
			if status != "PROCRASTINATING" {
				result = append(result, item)
			}
		}
		return result, len(result) != len(values)
	case []string:
		result := make([]string, 0, len(values))
		for _, status := range values {
			if status != "PROCRASTINATING" {
				result = append(result, status)
			}
		}
		return result, len(result) != len(values)
	}
	return value, false
}
