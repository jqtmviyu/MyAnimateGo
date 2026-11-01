package notifications_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/notifications"
)

func TestNotificationHTTPRoutesReturnWebhookDefaultsAndEmbyViews(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Library/MediaFolders":
			if r.Header.Get("X-Emby-Token") != "fixture-key" {
				t.Errorf("Emby token = %q", r.Header.Get("X-Emby-Token"))
			}
			_, _ = w.Write([]byte(`{"Items":[{"Id":"v1","Name":"Shows"},{"id":"v2","name":"Movies"}]}`))
		case "/notify":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["msg"] != "MyAnimeGo 测试通知 测试下载 S01E06 🎈" {
				t.Errorf("test notification body = %#v err=%v", body, err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	store := openNotificationStore(t, map[string]any{"notificationTemplate": "${title} ${text} ${emoji}"})
	defer store.Close()
	service, err := notifications.New(store, notifications.Options{HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]http.Handler{}
	for _, route := range service.Routes() {
		routes[route.Path] = route.Handler
	}
	defaults := routeJSON(t, routes["/api/newNotification"], `{}`)
	defaultData := defaults["data"].(map[string]any)
	if defaultData["notificationType"] != "WEB_HOOK" || defaultData["retry"] != float64(3) {
		t.Fatalf("new notification defaults = %#v", defaults)
	}
	views := routeJSON(t, routes["/api/getEmbyViews"], `{"embyHost":"`+upstream.URL+`","embyApiKey":"fixture-key"}`)
	if len(views["data"].([]any)) != 2 {
		t.Fatalf("Emby views = %#v", views)
	}
	testResponse := routeJSON(t, routes["/api/testNotification"], `{"notificationType":"WEB_HOOK","notificationTemplate":"${notification}","webHookMethod":"POST","webHookUrl":"`+upstream.URL+`/notify","webHookBody":"{\"msg\":\"${notification}\"}"}`)
	if testResponse["code"] != float64(200) {
		t.Fatalf("notification test route = %#v", testResponse)
	}
	failed := routeJSON(t, routes["/api/testNotification"], `{"notificationType":"WEB_HOOK","webHookMethod":"POST","webHookUrl":"","webHookBody":""}`)
	if failed["code"] != float64(500) {
		t.Fatalf("invalid notification test should report failure: %#v", failed)
	}
}

func routeJSON(t *testing.T, handler http.Handler, body string) map[string]any {
	t.Helper()
	if handler == nil {
		t.Fatal("route handler missing")
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	var result map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode route response %q: %v", recorder.Body.String(), err)
	}
	return result
}
