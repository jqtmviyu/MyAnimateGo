package peerfilter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

type fixture struct {
	mu           sync.Mutex
	filter       *Service
	store        *storage.Store
	config       Settings
	now          time.Time
	source       *httptest.Server
	rpc          *httptest.Server
	api          *httptest.Server
	sourceText   string
	sourceStatus int
	feedURL      string
	enabled      bool
	updates      int
	calls        int
	peers        []peer
	lastFeed     string
	version      string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{now: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), sourceText: "1.2.3.0/24\nfd00::/126\n", sourceStatus: 200, version: "4.1.3", peers: []peer{{"8.8.8.8", "Xunlei 1.0"}, {"2001:4860::8888", "hp/torrent 1"}, {"10.0.0.1", "Xunlei"}, {"9.9.9.9", "qBittorrent"}}}
	var err error
	dir := t.TempDir()
	f.store, err = storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.store.Close() })
	f.source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		body, status := f.sourceText, f.sourceStatus
		f.mu.Unlock()
		w.Header().Set("ETag", "fixture")
		if status == 200 && r.Header.Get("If-None-Match") == "fixture" {
			f.mu.Lock()
			same := f.sourceText == "1.2.3.0/24\nfd00::/126\n"
			f.mu.Unlock()
			if same {
				w.WriteHeader(304)
				return
			}
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(f.source.Close)
	f.rpc = httptest.NewServer(http.HandlerFunc(f.rpcHTTP))
	t.Cleanup(f.rpc.Close)
	f.config = Defaults(nil)
	f.config.SourceIDs = []string{"btn"}
	f.config.Enabled = true
	if err = f.store.SaveConfig(context.Background(), map[string]any{ConfigKey: f.config, "automaticTasksPaused": true, "downloadToolHost": f.rpc.URL, "apiKey": "test-peer-filter"}); err != nil {
		t.Fatal(err)
	}
	f.filter, err = New(context.Background(), Options{Store: f.store, Directory: filepath.Join(dir, "peerfilter"), Now: func() time.Time { return f.now }, Sources: []Source{{ID: "btn", Name: "Fixture BTN", URL: f.source.URL}}})
	if err != nil {
		t.Fatal(err)
	}
	api, err := server.New(context.Background(), f.store)
	if err != nil {
		t.Fatal(err)
	}
	api.SetConfigHooks(ValidateConfig, f.filter.ConfigChanged)
	routes := f.filter.Routes()
	for i := range routes {
		path := routes[i].Path
		routes[i].Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			current := f.filter
			f.mu.Unlock()
			for _, route := range current.Routes() {
				if route.Path == path {
					route.Handler.ServeHTTP(w, r)
					return
				}
			}
		})
	}
	if err = api.Register(routes...); err != nil {
		t.Fatal(err)
	}
	f.api = httptest.NewServer(api)
	t.Cleanup(f.api.Close)
	f.config.BaseURL = f.api.URL
	f.save(t)
	return f
}
func (f *fixture) save(t *testing.T) {
	t.Helper()
	if err := f.store.UpdateConfigFields(context.Background(), map[string]any{ConfigKey: f.config}); err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) rpcHTTP(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Method string                     `json:"method"`
		Args   map[string]json.RawMessage `json:"arguments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	args := map[string]any{}
	switch request.Method {
	case "session-get":
		f.mu.Lock()
		args = map[string]any{"version": f.version, "blocklist-url": f.feedURL, "blocklist-enabled": f.enabled, "blocklist-size": f.updates}
		f.mu.Unlock()
	case "session-set":
		f.mu.Lock()
		if value := request.Args["blocklist-url"]; value != nil {
			json.Unmarshal(value, &f.feedURL)
		}
		if value := request.Args["blocklist-enabled"]; value != nil {
			json.Unmarshal(value, &f.enabled)
		}
		f.mu.Unlock()
	case "torrent-get":
		if request.Args["ids"] == nil {
			args["torrents"] = []any{map[string]any{"id": 1, "status": 6, "peersConnected": 4}}
		} else {
			f.mu.Lock()
			rows := append([]peer(nil), f.peers...)
			f.mu.Unlock()
			args["torrents"] = []any{map[string]any{"id": 1, "peers": rows}}
		}
	case "blocklist-update":
		f.mu.Lock()
		url := f.feedURL
		f.mu.Unlock()
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{"result": err.Error()})
			return
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			json.NewEncoder(w).Encode(map[string]any{"result": "feed failed"})
			return
		}
		f.mu.Lock()
		f.lastFeed = string(data)
		f.updates++
		f.mu.Unlock()
		rows, err := ParseRanges(strings.NewReader(string(data)))
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{"result": err.Error()})
			return
		}
		args["blocklist-size"] = len(rows)
	default:
		json.NewEncoder(w).Encode(map[string]any{"result": "unexpected method"})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"result": "success", "arguments": args})
}
func (f *fixture) feedRanges(t *testing.T) []Range {
	t.Helper()
	f.mu.Lock()
	body := f.lastFeed
	f.mu.Unlock()
	rows, err := ParseRanges(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestLifecycleUsesFeedWithoutGlobalLockAndMergesPeers(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.filter.work(ctx, "refresh", ""); err != nil {
		t.Fatal(err)
	}
	v := f.filter.Status()
	if !v.Managed || v.Applied != v.Revision || v.DynamicCount != 2 {
		t.Fatalf("status=%+v", v)
	}
	rows := f.feedRanges(t)
	for _, ip := range []string{"1.2.3.1", "fd00::1", "8.8.8.8", "2001:4860::8888"} {
		if !contains(rows, netip.MustParseAddr(ip)) {
			t.Fatalf("missing %s", ip)
		}
	}
	if err := f.filter.work(ctx, "allow", "1.2.3.1"); err != nil {
		t.Fatal(err)
	}
	if contains(f.feedRanges(t), netip.MustParseAddr("1.2.3.1")) {
		t.Fatal("allow did not subtract static range")
	}
	if err := f.filter.work(ctx, "release", "8.8.8.8"); err != nil {
		t.Fatal(err)
	}
	if contains(f.feedRanges(t), netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("release remains in feed")
	}
	f.mu.Lock()
	f.sourceStatus = 503
	f.mu.Unlock()
	if err := f.filter.work(ctx, "refresh", ""); err == nil {
		t.Fatal("missing source failure")
	}
	if !contains(f.feedRanges(t), netip.MustParseAddr("1.2.3.2")) {
		t.Fatal("source failure removed last snapshot")
	}
	if f.filter.Status().Sources[0].Error == "" {
		t.Fatal("source failure invisible")
	}
	f.mu.Lock()
	f.sourceStatus = 200
	f.sourceText = "5.6.7.0/24\n"
	f.mu.Unlock()
	if err := f.filter.work(ctx, "refresh", ""); err != nil {
		t.Fatal(err)
	}
	if contains(f.feedRanges(t), netip.MustParseAddr("1.2.3.2")) {
		t.Fatal("removed upstream rules accumulated")
	}
}
func TestPauseExpiryRestartConflictAndDisable(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.filter.work(ctx, "automatic", ""); err != nil {
		t.Fatal(err)
	}
	if f.calls != 0 {
		t.Fatal("paused automatic task touched downloader")
	}
	if err := f.filter.work(ctx, "refresh", ""); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(ctx, f.filter.options)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Status().DynamicCount != 2 || restarted.token != f.filter.token {
		t.Fatal("state not restored")
	}
	f.mu.Lock()
	f.filter = restarted
	f.mu.Unlock()
	f.now = f.now.Add(25 * time.Hour)
	f.mu.Lock()
	f.peers = nil
	f.mu.Unlock()
	if err = f.filter.work(ctx, "apply", ""); err != nil {
		t.Fatal(err)
	}
	if f.filter.Status().DynamicCount != 0 {
		t.Fatal("expired bans survived")
	}
	f.store.UpdateConfigFields(ctx, map[string]any{"automaticTasksPaused": false})
	f.mu.Lock()
	f.feedURL = "https://external.example/list"
	f.mu.Unlock()
	f.now = f.now.Add(2 * time.Minute)
	if err = f.filter.work(ctx, "automatic", ""); err == nil || !f.filter.Status().Conflict {
		t.Fatal("external config overwrite was not detected")
	}
	if err = f.filter.work(ctx, "apply", ""); err != nil {
		t.Fatal(err)
	}
	f.config.Enabled = false
	f.save(t)
	f.store.UpdateConfigFields(ctx, map[string]any{"automaticTasksPaused": true})
	if err = f.filter.work(ctx, "automatic", ""); err != nil {
		t.Fatal(err)
	}
	if f.enabled {
		t.Fatal("disabling while paused failed")
	}
}
func TestVersionGateAndReadOnlyFeedAuthentication(t *testing.T) {
	f := setup(t)
	f.mu.Lock()
	f.version = "4.0.6"
	f.mu.Unlock()
	if err := f.filter.work(context.Background(), "refresh", ""); err == nil || !strings.Contains(err.Error(), "4.1") {
		t.Fatal("unsupported version not reported")
	}
	if f.enabled || f.updates != 0 {
		t.Fatal("unsupported downloader mutated")
	}
	res, err := http.Get(f.api.URL + "/api/peerFilterFeed?token=wrong")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal(res.StatusCode)
	}
	res, err = http.Get(f.api.URL + "/api/peerFilter")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Code int `json:"code"`
	}
	json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	if body.Code != 403 {
		t.Fatal("management endpoint became public")
	}
}

func TestIdleTicksDoNotRewriteStateAndRestoreReloadsToken(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.filter.work(ctx, "refresh", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateConfigFields(ctx, map[string]any{"automaticTasksPaused": false}); err != nil {
		t.Fatal(err)
	}
	if err := f.filter.work(ctx, "automatic", ""); err != nil {
		t.Fatal(err)
	}
	before, err := f.store.LoadPeerFilterState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	if err = f.filter.work(ctx, "automatic", ""); err != nil {
		t.Fatal(err)
	}
	after, err := f.store.LoadPeerFilterState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("idle tick wrote persistent state")
	}
	if err = f.store.UpdateConfigFields(ctx, map[string]any{"automaticTasksPaused": true}); err != nil {
		t.Fatal(err)
	}
	if err = f.filter.work(ctx, "automatic", ""); err != nil {
		t.Fatal(err)
	}
	var restored state
	if err = json.Unmarshal(before, &restored); err != nil {
		t.Fatal(err)
	}
	restored.Token = strings.Repeat("a", 64)
	restored.Bans = map[string]Ban{}
	data, _ := json.Marshal(restored)
	if err = f.store.SavePeerFilterState(ctx, data); err != nil {
		t.Fatal(err)
	}
	if err = f.store.UpdateConfigFields(ctx, map[string]any{"automaticTasksPaused": true}); err != nil {
		t.Fatal(err)
	}
	f.filter.Reload()
	if err = f.filter.work(ctx, "automatic", ""); err != nil {
		t.Fatal(err)
	}
	if f.filter.token != restored.Token || f.filter.Status().DynamicCount != 0 {
		t.Fatal("restored state was overwritten by old in-memory state")
	}
}

func TestChangingReachableAddressAppliesImmediately(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.filter.work(ctx, "refresh", ""); err != nil {
		t.Fatal(err)
	}
	f.store.UpdateConfigFields(ctx, map[string]any{"automaticTasksPaused": false})
	// 同一HTTP服务的等价地址用于检查配置变化，不依赖远端下载器。
	f.config.BaseURL = strings.Replace(f.api.URL, "127.0.0.1", "localhost", 1)
	f.save(t)
	if err := f.filter.work(ctx, "automatic", ""); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	url := f.feedURL
	f.mu.Unlock()
	if !strings.HasPrefix(url, f.config.BaseURL+"/") {
		t.Fatal("new address did not apply immediately")
	}
}

func TestManagementHTTPRefreshAndAllowPersistSettings(t *testing.T) {
	f := setup(t)
	request := func(method, path string, payload any) map[string]any {
		t.Helper()
		var body io.Reader
		if payload != nil {
			data, _ := json.Marshal(payload)
			body = strings.NewReader(string(data))
		}
		req, _ := http.NewRequest(method, f.api.URL+path, body)
		req.Header.Set("api-key", "test-peer-filter")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), f.filter.token) {
			t.Fatal("management response exposed feed credential")
		}
		var result struct {
			Code    int            `json:"code"`
			Message string         `json:"message"`
			Data    map[string]any `json:"data"`
		}
		if err = json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if result.Code != 200 {
			t.Fatal(result.Message)
		}
		return result.Data
	}
	request("POST", "/api/peerFilterAction", map[string]any{"action": "refresh"})
	data := request("GET", "/api/peerFilterBans", nil)
	if data["total"] != float64(2) {
		t.Fatal("ban list did not reflect detected peers")
	}
	data = request("POST", "/api/peerFilterAction", map[string]any{"action": "allow", "ip": "8.8.8.8"})
	settings := data["settings"].(map[string]any)
	if len(settings["allow"].([]any)) != 1 {
		t.Fatal("allow operation did not return updated saved settings")
	}
	if contains(f.feedRanges(t), netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("allow operation not applied to downloader")
	}
	data = request("GET", "/api/peerFilterBans?view=released", nil)
	if data["total"] != float64(1) {
		t.Fatal("release history missing")
	}
}

func TestPublicSourceFormats(t *testing.T) {
	if os.Getenv("ANI_RSS_TEST_PUBLIC_RULES") != "1" {
		t.Skip("set ANI_RSS_TEST_PUBLIC_RULES=1 for read-only upstream format acceptance")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	for _, source := range DefaultSources {
		t.Run(source.ID, func(t *testing.T) {
			resp, err := client.Get(source.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatal(resp.Status)
			}
			rows, err := ParseRanges(io.LimitReader(resp.Body, 64<<20))
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) == 0 {
				t.Fatal("public source is empty")
			}
			t.Logf("%s: %d normalized ranges", source.ID, len(rows))
		})
	}
}
