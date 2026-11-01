package notifications

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

func (s *Service) Routes() []server.Route {
	return []server.Route{
		s.route("/api/testNotification", s.testNotificationRoute),
		s.route("/api/newNotification", s.newNotificationRoute),
		s.route("/api/getEmbyViews", s.getEmbyViewsRoute),
	}
}

type notificationEndpoint func(*http.Request) (any, string, error)

func (s *Service) route(path string, endpoint notificationEndpoint) server.Route {
	return server.Route{Method: http.MethodPost, Path: path, Public: false, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, message, err := endpoint(r)
		if err != nil {
			server.Respond(w, 500, err.Error(), nil)
			return
		}
		if message == "" {
			message = "success"
		}
		server.Respond(w, 200, message, data)
	})}
}

func notificationBody(r *http.Request, target any) error {
	if r.Body == nil {
		return errors.New("请求体不能为空")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	if err := decoder.Decode(target); err != nil {
		return errors.New("JSON 请求格式错误")
	}
	return nil
}

func (s *Service) testNotificationRoute(r *http.Request) (any, string, error) {
	var channel map[string]any
	if err := notificationBody(r, &channel); err != nil {
		return nil, "", err
	}
	kind := strings.ToUpper(stringValue(channel["notificationType"]))
	if kind == "" {
		return nil, "", errors.New("notificationType 不能为空")
	}
	if !supportedChannel(kind) {
		return nil, "", errors.New("仅支持 Webhook 和 Emby 媒体库刷新")
	}
	global, err := s.store.GetConfig(r.Context())
	if err != nil {
		return nil, "", err
	}
	subscription := map[string]any{
		"id": "notification-test", "title": "MyAnimeGo 测试通知", "season": 1,
		"currentEpisodeNumber": 6, "totalEpisodeNumber": 12, "subgroup": "示例字幕组", "message": true, "ova": false,
		"image":  "https://mikanime.tv/images/Bangumi/202404/0fd46fc8.jpg",
		"bgmUrl": "https://bgm.tv",
	}
	// 集数变量从通知文本提取，样本保持与订阅里的当前集数一致。
	if err := s.deliverChannel(r.Context(), global, channel, subscription, "测试下载 S01E06", "DOWNLOAD_START"); err != nil {
		return nil, "", err
	}
	return nil, "测试成功", nil
}

func (s *Service) newNotificationRoute(_ *http.Request) (any, string, error) {
	return defaultNotificationConfig(), "success", nil
}

func defaultNotificationConfig() map[string]any {
	return map[string]any{
		"enable": true, "retry": 3, "sort": 10, "notificationType": "WEB_HOOK", "notificationTemplate": "${notification}", "comment": "",
		"statusList":  []string{"DOWNLOAD_START", "OMIT", "ERROR"},
		"webHookBody": "", "webHookUrl": "", "webHookHeader": "", "webHookMethod": "POST",
		"embyHost": "", "embyApiKey": "", "embyRefreshViewIds": []string{}, "embyDelayed": 0,
	}
}

func (s *Service) getEmbyViewsRoute(r *http.Request) (any, string, error) {
	var channel map[string]any
	if err := notificationBody(r, &channel); err != nil {
		return nil, "", err
	}
	views, err := s.embyViews(r.Context(), channel)
	return views, "success", err
}
