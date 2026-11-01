package migration

// 旧版的独立开关转为渠道列表，事件选择和连接参数随渠道保留。
func normalizeLegacyNotifications(config map[string]any) []string {
	if _, exists := config["notificationConfigList"]; exists {
		return nil
	}
	if _, exists := config["notificationTemplate"]; !exists {
		if template, ok := config["messageTemplate"].(string); ok {
			config["notificationTemplate"] = template
		} else {
			config["notificationTemplate"] = "${text}"
		}
	}
	statuses, exists := config["messageList"]
	if !exists {
		statuses = []any{"DOWNLOAD_START", "DOWNLOAD_END", "OMIT", "ERROR", "COMPLETED"}
	}
	var channels []any
	for _, spec := range []struct {
		flag, kind string
		fields     []string
	}{
		{"webHook", "WEB_HOOK", []string{"webHookUrl", "webHookMethod", "webHookHeader", "webHookBody"}},
		{"embyRefresh", "EMBY_REFRESH", []string{"embyHost", "embyApiKey", "embyRefreshViewIds", "embyDelayed"}},
	} {
		enabled, exists := config[spec.flag].(bool)
		if !exists {
			continue
		}
		channel := map[string]any{"enable": enabled, "sort": len(channels), "retry": 1, "comment": "旧版配置导入", "notificationType": spec.kind, "notificationTemplate": "${notification}", "statusList": statuses}
		for _, field := range spec.fields {
			if value, exists := config[field]; exists {
				channel[field] = value
			}
		}
		channels = append(channels, channel)
	}
	if len(channels) > 0 {
		config["notificationConfigList"] = channels
	}
	return nil
}
