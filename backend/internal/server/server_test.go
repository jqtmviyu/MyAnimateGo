package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

type memoryStore struct {
	mu            sync.Mutex
	config        map[string]any
	subscriptions map[string]map[string]any
	order         []string
}

func (m *memoryStore) ReplaceSubscriptionsAndEnqueue(ctx context.Context, values []map[string]any, _, _ string, _ json.RawMessage) error {
	return m.ReplaceSubscriptions(ctx, values)
}

func (m *memoryStore) GetConfig(context.Context) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return clone(m.config), nil
}
func (m *memoryStore) SaveConfig(_ context.Context, value map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = clone(value)
	return nil
}
func (m *memoryStore) UpdateConfigFields(_ context.Context, fields map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		m.config = map[string]any{}
	}
	for key, value := range clone(fields) {
		m.config[key] = value
	}
	return nil
}
func (m *memoryStore) ListSubscriptions(context.Context) ([]map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]map[string]any, 0, len(m.subscriptions))
	seen := make(map[string]bool, len(m.order))
	for _, id := range m.order {
		if value, ok := m.subscriptions[id]; ok {
			items = append(items, clone(value))
			seen[id] = true
		}
	}
	for id, value := range m.subscriptions {
		if !seen[id] {
			items = append(items, clone(value))
		}
	}
	return items, nil
}
func (m *memoryStore) PutSubscription(_ context.Context, value map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := value["id"].(string)
	if _, exists := m.subscriptions[id]; !exists {
		m.order = append(m.order, id)
	}
	m.subscriptions[id] = clone(value)
	return nil
}
func (m *memoryStore) UpdateSubscriptionFields(_ context.Context, id string, fields map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, exists := m.subscriptions[id]
	if !exists {
		return errors.New("subscription not found")
	}
	item = clone(item)
	for key, value := range fields {
		if key != "id" && key != "currentEpisodeNumber" && key != "lastDownloadTime" {
			item[key] = value
		}
	}
	m.subscriptions[id] = item
	return nil
}
func (m *memoryStore) UpdateSubscriptionFieldsBatch(_ context.Context, ids []string, fields map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		if item, exists := m.subscriptions[id]; exists {
			item = clone(item)
			for key, value := range fields {
				if key != "id" && key != "currentEpisodeNumber" && key != "lastDownloadTime" {
					item[key] = value
				}
			}
			m.subscriptions[id] = item
		}
	}
	return nil
}
func (m *memoryStore) DeleteSubscriptions(_ context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		delete(m.subscriptions, id)
	}
	order := m.order[:0]
	for _, id := range m.order {
		if _, exists := m.subscriptions[id]; exists {
			order = append(order, id)
		}
	}
	m.order = order
	return nil
}
func (m *memoryStore) ReplaceSubscriptions(_ context.Context, values []map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscriptions = map[string]map[string]any{}
	m.order = nil
	for _, value := range values {
		id := value["id"].(string)
		if _, exists := m.subscriptions[id]; !exists {
			m.order = append(m.order, id)
		}
		m.subscriptions[id] = clone(value)
	}
	return nil
}
func clone(value map[string]any) map[string]any {
	data, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(data, &result)
	return result
}

func setup(t *testing.T) http.Handler {
	app, _ := setupWithStore(t)
	return app
}

func setupWithStore(t *testing.T) (http.Handler, *memoryStore) {
	t.Helper()
	store := &memoryStore{config: map[string]any{}, subscriptions: map[string]map[string]any{}}
	app, err := server.New(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	return app, store
}

func request(t *testing.T, app http.Handler, method, path, token string, body any) map[string]any {
	return requestFrom(t, app, method, path, token, body, "")
}

func requestFrom(t *testing.T, app http.Handler, method, path, token string, body any, remote string) map[string]any {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	if remote != "" {
		req.RemoteAddr = remote
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("HTTP状态：%d %s", res.Code, res.Body.String())
	}
	if !strings.HasPrefix(res.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("响应类型：%q", res.Header().Get("Content-Type"))
	}
	var result map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSessionIPBindingAndLoginLimit(t *testing.T) {
	app := setup(t)
	token := login(t, app)
	settings := map[string]any{"verifyLoginIp": true, "multiLoginForbidden": false}
	if got := request(t, app, "POST", "/api/setConfig", token, settings); got["code"] != float64(200) {
		t.Fatal(got)
	}
	token = login(t, app)
	if got := requestFrom(t, app, "POST", "/api/listAni", token, nil, "198.51.100.9:43000"); got["code"] != float64(403) {
		t.Fatal("启用IP绑定后不同IP不得复用会话")
	}
	for i := 0; i < 30; i++ {
		requestFrom(t, app, "POST", "/api/login", "", map[string]any{"username": "invalid", "password": "invalid"}, "198.51.100.10:43000")
	}
	blocked := requestFrom(t, app, "POST", "/api/login", "", map[string]any{"username": "admin", "password": "21232f297a57a5a743894a0e4a801fc3"}, "198.51.100.10:43000")
	if blocked["code"] != float64(403) {
		t.Fatalf("达到尝试次数后应限制登录：%v", blocked)
	}
}

func login(t *testing.T, app http.Handler) string {
	t.Helper()
	result := request(t, app, "POST", "/api/login", "", map[string]any{"username": "admin", "password": "21232f297a57a5a743894a0e4a801fc3"})
	token, _ := result["data"].(string)
	if result["code"] != float64(200) || result["message"] != "登录成功" || strings.TrimSpace(token) == "" {
		t.Fatalf("登录结果：%v", result)
	}
	return token
}

func TestJavaHTTPBaseline(t *testing.T) {
	app := setup(t)
	started := time.Now().UnixMilli()
	ping := request(t, app, "GET", "/api/PING", "", nil)
	if ping["code"] != float64(200) || ping["message"] != "success" {
		t.Fatalf("ping：%v", ping)
	}
	timestamp, ok := ping["t"].(float64)
	if !ok || timestamp < float64(started) || timestamp > float64(time.Now().UnixMilli()) {
		t.Fatalf("响应时间应为当前 epoch 毫秒：%v", ping["t"])
	}
	if _, exists := ping["data"]; exists {
		t.Fatal("null data 应省略")
	}
	denied := request(t, app, "POST", "/api/listAni", "", nil)
	if denied["code"] != float64(403) || denied["message"] != "登录已失效" {
		t.Fatalf("认证：%v", denied)
	}
	token := login(t, app)
	for _, path := range []string{"/api/listAni", "/api/listAni?s=" + token} {
		headerToken := token
		if path != "/api/listAni" {
			headerToken = ""
		}
		result := request(t, app, "POST", path, headerToken, nil)
		data := result["data"].(map[string]any)
		if data["total"] != float64(0) || len(data["weekList"].([]any)) != 7 || len(data["releaseDateList"].([]any)) != 0 {
			t.Fatalf("订阅结构：%v", data)
		}
		for _, group := range data["weekList"].([]any) {
			if len(group.(map[string]any)["items"].([]any)) != 0 {
				t.Fatalf("空列表包含订阅：%v", group)
			}
		}
	}
}

func TestFeatureRoutesShareAuthenticationAndAllowPublicStreams(t *testing.T) {
	app := setup(t)
	err := app.(*server.Server).Register(server.Route{Method: "POST", Path: "/api/feature", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.Respond(w, 200, "success", map[string]any{"ready": true})
	})}, server.Route{Method: "GET", Path: "/api/public-stream", Public: true, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("stream"))
	})})
	if err != nil {
		t.Fatal(err)
	}
	if got := request(t, app, "POST", "/api/FEATURE", "", nil); got["code"] != float64(403) {
		t.Fatal(got)
	}
	token := login(t, app)
	if got := request(t, app, "POST", "/api/FEATURE", token, nil); got["data"].(map[string]any)["ready"] != true {
		t.Fatal(got)
	}
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/public-stream", nil))
	if recorder.Code != 200 || recorder.Body.String() != "stream" {
		t.Fatal("公共资源路由应保留原始响应")
	}
}

func TestSubscriptionEditsPreserveProgressAndOverrideDuplicates(t *testing.T) {
	app := setup(t)
	token := login(t, app)
	item := map[string]any{"id": "original", "title": "示例作品", "season": 1, "enable": false, "releaseDate": "2025-04-06", "currentEpisodeNumber": 3, "customField": "保留"}
	if got := request(t, app, "POST", "/api/addAni", token, item); got["code"] != float64(200) {
		t.Fatal(got)
	}
	item["currentEpisodeNumber"] = 99
	if got := request(t, app, "POST", "/api/setAni", token, item); got["code"] != float64(200) {
		t.Fatal(got)
	}
	list := request(t, app, "POST", "/api/listAni", token, nil)["data"].(map[string]any)
	var stored map[string]any
	for _, group := range list["weekList"].([]any) {
		for _, entry := range group.(map[string]any)["items"].([]any) {
			stored = entry.(map[string]any)
		}
	}
	if stored["currentEpisodeNumber"] != float64(3) || stored["customField"] != "保留" {
		t.Fatal(stored)
	}
	if got := request(t, app, "POST", "/api/addAni", token, item); got["code"] != float64(500) {
		t.Fatalf("重复添加应明确报错：%v", got)
	}
	item["id"] = "replacement"
	if got := request(t, app, "POST", "/api/addAni?override=true", token, item); got["code"] != float64(200) {
		t.Fatalf("覆盖同名同季订阅：%v", got)
	}
	list = request(t, app, "POST", "/api/listAni", token, nil)["data"].(map[string]any)
	if list["total"] != float64(1) {
		t.Fatalf("覆盖后应保留一条：%v", list)
	}
}

func TestConfigMasksPasswordAndRevokesSessionAfterChange(t *testing.T) {
	app := setup(t)
	token := login(t, app)
	config := request(t, app, "POST", "/api/config", token, nil)["data"].(map[string]any)
	if config["login"].(map[string]any)["password"] != "" || config["jwtKey"] != "" {
		t.Fatal("配置读取应清空登录密码和签名密钥")
	}
	config["login"].(map[string]any)["password"] = "new-password-digest"
	if got := request(t, app, "POST", "/api/setConfig", token, config); got["code"] != float64(200) {
		t.Fatal(got)
	}
	if got := request(t, app, "POST", "/api/listAni", token, nil); got["code"] != float64(403) {
		t.Fatal("改密后旧会话应失效")
	}
	newLogin := request(t, app, "POST", "/api/login", "", map[string]any{"username": "admin", "password": "new-password-digest"})
	if newLogin["code"] != float64(200) {
		t.Fatal(newLogin)
	}
}

func TestConfigComputesCurrentAuthorizationExpiry(t *testing.T) {
	store := &memoryStore{config: map[string]any{"expirationTime": time.Now().Add(time.Hour).UnixMilli()}, subscriptions: map[string]map[string]any{}}
	app, err := server.New(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	token := login(t, app)
	data := request(t, app, "POST", "/api/config", token, nil)["data"].(map[string]any)
	if data["verifyExpirationTime"] != true {
		t.Fatal("配置读取应根据当前有效期计算授权状态")
	}
}

func TestEnabledSubscriptionCanBeSavedWhileAutomaticTasksPaused(t *testing.T) {
	app := setup(t)
	token := login(t, app)
	item := map[string]any{"id": "paused-import", "title": "重生之门", "season": 1, "enable": true, "releaseDate": "2025-04-06"}
	if got := request(t, app, "POST", "/api/addAni", token, item); got["code"] != float64(200) {
		t.Fatal(got)
	}
	config := request(t, app, "POST", "/api/config", token, nil)["data"].(map[string]any)
	if config["automaticTasksPaused"] != true {
		t.Fatal("新实例默认保持全局自动任务暂停")
	}
	if got := request(t, app, "POST", "/api/setConfig", token, map[string]any{"automaticTasksPaused": false}); got["code"] != float64(200) {
		t.Fatalf("普通设置保存忽略运行状态：%v", got)
	}
	config = request(t, app, "POST", "/api/config", token, nil)["data"].(map[string]any)
	if config["automaticTasksPaused"] != true {
		t.Fatal("普通设置保存后全局运行状态应保持不变")
	}
	data := request(t, app, "POST", "/api/listAni", token, nil)["data"].(map[string]any)
	for _, group := range data["weekList"].([]any) {
		for _, entry := range group.(map[string]any)["items"].([]any) {
			sub := entry.(map[string]any)
			if sub["pinyin"] != "chongshengzhimen" || sub["pinyinInitials"] != "cszm" {
				t.Fatalf("前端搜索字段：%v", sub)
			}
			if _, ok := sub["standbyRssList"].([]any); !ok {
				t.Fatal("补齐原版前端所需订阅默认字段")
			}
		}
	}
}

func TestSetAniPreservesUnknownFieldsOmittedFromEdit(t *testing.T) {
	app, store := setupWithStore(t)
	token := login(t, app)
	if got := request(t, app, "POST", "/api/addAni", token, map[string]any{
		"id": "future-field", "title": "扩展字段作品", "season": 1, "releaseDate": "2025-04-06", "futureField": "preserve-me",
	}); got["code"] != float64(200) {
		t.Fatal(got)
	}
	if got := request(t, app, "POST", "/api/setAni", token, map[string]any{
		"id": "future-field", "title": "扩展字段作品", "season": 1,
	}); got["code"] != float64(200) {
		t.Fatal(got)
	}
	items, err := store.ListSubscriptions(context.Background())
	if err != nil || len(items) != 1 || items[0]["futureField"] != "preserve-me" {
		t.Fatalf("setAni 丢失了未发送的未知字段：%#v，%v", items, err)
	}
}

func TestGroupedDoesNotInventDatesForMissingOrMonthPrecision(t *testing.T) {
	app, store := setupWithStore(t)
	if err := store.ReplaceSubscriptions(context.Background(), []map[string]any{
		{"id": "missing", "title": "无日期", "season": 1, "score": 3},
		{"id": "month", "title": "仅有年月", "season": 1, "year": 2025, "month": 4, "releaseDate": "2025-04", "releaseDatePrecision": "month", "score": 2},
		{"id": "day", "title": "完整日期", "season": 1, "releaseDate": "2025-04-06", "releaseDatePrecision": "day", "score": 1},
	}); err != nil {
		t.Fatal(err)
	}
	data := request(t, app, "POST", "/api/listAni", login(t, app), nil)["data"].(map[string]any)
	months := data["releaseDateList"].([]any)
	if len(months) != 1 || months[0] != "2025-04" {
		t.Fatalf("无日期订阅不应制造当前月份：%v", months)
	}
	var groupedIDs []string
	for _, rawGroup := range data["weekList"].([]any) {
		group := rawGroup.(map[string]any)
		for _, rawItem := range group["items"].([]any) {
			groupedIDs = append(groupedIDs, rawItem.(map[string]any)["id"].(string))
		}
	}
	if len(groupedIDs) != 1 || groupedIDs[0] != "day" {
		t.Fatalf("周分组只能使用精确日期，得到：%v", groupedIDs)
	}
}

func TestGroupedSortKeepsInputOrderWhenSortKeysMatch(t *testing.T) {
	app, store := setupWithStore(t)
	if err := store.ReplaceSubscriptions(context.Background(), []map[string]any{
		{"id": "first", "title": "乙作品", "season": 1, "releaseDate": "2025-04-06", "score": 8},
		{"id": "second", "title": "甲作品", "season": 1, "releaseDate": "2025-04-06", "score": 8},
	}); err != nil {
		t.Fatal(err)
	}
	data := request(t, app, "POST", "/api/listAni", login(t, app), nil)["data"].(map[string]any)
	var got []string
	for _, rawGroup := range data["weekList"].([]any) {
		for _, rawItem := range rawGroup.(map[string]any)["items"].([]any) {
			got = append(got, rawItem.(map[string]any)["id"].(string))
		}
	}
	if !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("相同排序键应稳定保留输入顺序：%v", got)
	}
}

func TestIPWhitelistAllowsConfiguredClientWithoutToken(t *testing.T) {
	app := setup(t)
	token := login(t, app)
	if got := request(t, app, "POST", "/api/setConfig", token, map[string]any{
		"ipWhitelist": true, "ipWhitelistStr": "198.51.100.0/24",
	}); got["code"] != float64(200) {
		t.Fatal(got)
	}
	result := requestFrom(t, app, "POST", "/api/listAni", "", nil, "198.51.100.7:40000")
	if result["code"] != float64(200) {
		t.Fatalf("白名单客户端应通过认证：%v", result)
	}
}

func TestAPIKeyHeaderAndQueryAuthentication(t *testing.T) {
	app := setup(t)
	token := login(t, app)
	if got := request(t, app, "POST", "/api/setConfig", token, map[string]any{"apiKey": "test-api-key"}); got["code"] != float64(200) {
		t.Fatal(got)
	}
	for _, test := range []struct {
		name   string
		path   string
		header string
	}{
		{name: "header", path: "/api/listAni", header: "api-key"},
		{name: "query", path: "/api/listAni?api-key=test-api-key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", test.path, nil)
			if test.header != "" {
				req.Header.Set(test.header, "test-api-key")
			}
			res := httptest.NewRecorder()
			app.ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("HTTP状态：%d %s", res.Code, res.Body.String())
			}
			var result map[string]any
			if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result["code"] != float64(200) {
				t.Fatalf("API key 未通过认证：%v", result)
			}
		})
	}
}

func TestBatchEnableAndDeleteSubscriptionRecords(t *testing.T) {
	app, store := setupWithStore(t)
	token := login(t, app)
	if got := request(t, app, "POST", "/api/addAni", token, map[string]any{
		"id": "crud", "title": "CRUD 作品", "season": 1, "releaseDate": "2025-04-06", "enable": true,
	}); got["code"] != float64(200) {
		t.Fatal(got)
	}
	if got := request(t, app, "POST", "/api/batchEnable?value=false", token, []string{"crud"}); got["code"] != float64(200) {
		t.Fatal(got)
	}
	items, err := store.ListSubscriptions(context.Background())
	if err != nil || len(items) != 1 || items[0]["enable"] != false {
		t.Fatalf("批量停用未保存：%#v，%v", items, err)
	}
	if got := request(t, app, "POST", "/api/deleteAni?deleteFiles=true", token, []string{"crud"}); got["code"] != float64(500) {
		t.Fatalf("媒体删除未接入时应拒绝并保留订阅：%v", got)
	}
	if got := request(t, app, "POST", "/api/setAni?move=true", token, map[string]any{
		"id": "crud", "title": "CRUD 作品", "season": 1, "releaseDate": "2025-04-06",
	}); got["code"] != float64(500) {
		t.Fatalf("媒体移动未接入时应拒绝：%v", got)
	}
	items, err = store.ListSubscriptions(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("拒绝媒体删除时订阅记录被删：%#v，%v", items, err)
	}
	if got := request(t, app, "POST", "/api/deleteAni?deleteFiles=false", token, []string{"crud"}); got["code"] != float64(200) {
		t.Fatal(got)
	}
	items, err = store.ListSubscriptions(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatalf("删除订阅记录失败：%#v，%v", items, err)
	}
}
