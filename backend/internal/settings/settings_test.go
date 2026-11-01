package settings_test

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"

	"strings"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/settings"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestSettingsRoutesAreCompleteAndPreservePublicBoundaries(t *testing.T) {
	service := settings.New(settings.Options{})
	got := map[string]server.Route{}
	for _, route := range service.Routes() {
		got[strings.ToLower(route.Path)] = route
	}
	want := []struct {
		path, method string
		public       bool
	}{
		{"/api/stop", http.MethodPost, false},
		{"/api/verifyNo", http.MethodPost, false},
		{"/api/testProxy", http.MethodPost, false},
		{"/api/testIpWhitelist", http.MethodPost, true},
		{"/api/trackersUpdate", http.MethodPost, false},
	}
	if len(got) != len(want) {
		t.Fatalf("registered route count = %d, expected %d", len(got), len(want))
	}
	for _, item := range want {
		route, ok := got[strings.ToLower(item.path)]
		if !ok || route.Method != item.method || route.Public != item.public {
			t.Errorf("route %s = %#v, exists=%v", item.path, route, ok)
		}
	}
}

func TestServiceVerifiesCachedEntitlementFromOrderAndExpiration(t *testing.T) {
	service := settings.New(settings.Options{})
	future := time.Now().Add(time.Hour).UnixMilli()
	cases := []struct {
		name   string
		config map[string]any
		want   bool
	}{
		{name: "valid cached order", config: map[string]any{"outTradeNo": "order", "expirationTime": future, "tryOut": false}, want: true},
		{name: "missing order", config: map[string]any{"expirationTime": future, "tryOut": false}},
		{name: "expired", config: map[string]any{"outTradeNo": "order", "expirationTime": time.Now().Add(-time.Hour).UnixMilli(), "tryOut": false}},
		{name: "trial", config: map[string]any{"outTradeNo": "order", "expirationTime": future, "tryOut": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := service.Verify(context.Background(), tc.config)
			if err != nil || got != tc.want {
				t.Fatalf("Verify() = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestVerifyOrderUpdatesAuthorizationOnlyAfterSuccessfulAfdianResponse(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		wantUpdate bool
	}{
		{name: "valid order", status: http.StatusOK, body: `{"code":200,"message":"verified"}`, wantUpdate: true},
		{name: "rejected order", status: http.StatusOK, body: `{"code":403,"message":"invalid order"}`},
		{name: "upstream unavailable", status: http.StatusBadGateway, body: `gateway failure`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotTradeNo string
			client := doerFunc(func(r *http.Request) (*http.Response, error) {
				gotTradeNo = r.URL.Query().Get("out_trade_no")
				return jsonResponse(tc.status, tc.body), nil
			})
			app, _, address := serveRoutes(t, settings.Options{HTTPClient: client})
			token := login(t, address)
			result := post(t, address, "/api/verifyNo", token, map[string]any{"outTradeNo": "order&no=secret"})
			if gotTradeNo != "order&no=secret" {
				t.Fatalf("order number query = %q", gotTradeNo)
			}
			if tc.wantUpdate {
				if result["code"] != float64(200) {
					t.Fatalf("valid order rejected: %#v", result)
				}
			} else if result["code"] != float64(403) && result["code"] != float64(500) {
				t.Fatalf("failed verification reported success: %#v", result)
			}
			config := post(t, address, "/api/config", token, nil)["data"].(map[string]any)
			if tc.wantUpdate {
				if config["outTradeNo"] != "order&no=secret" || config["tryOut"] != false || config["expirationTime"].(float64) <= float64(time.Now().UnixMilli()) {
					t.Fatalf("verified order did not update authorization: %#v", config)
				}
			} else if config["outTradeNo"] != "" || config["expirationTime"] != float64(0) || config["tryOut"] != false {
				t.Fatalf("failed verification changed authorization: %#v", config)
			}
			_ = app
		})
	}
}

func TestProxyTestUsesEncodedURLAndReturnsRealResponseFields(t *testing.T) {
	var requested string
	client := doerFunc(func(r *http.Request) (*http.Response, error) {
		requested = r.URL.String()
		return jsonResponse(http.StatusTeapot, `<html><head><title> Fixture &amp; Title </title></head></html>`), nil
	})
	_, _, address := serveRoutes(t, settings.Options{HTTPClient: client})
	token := login(t, address)
	target := "https://fixture.invalid/path?q=1"
	encoded := base64.StdEncoding.EncodeToString([]byte(target))
	result := postURL(t, address, "/api/testProxy?url="+url.QueryEscape(encoded), token, map[string]any{"proxy": false})
	if result["code"] != float64(200) {
		t.Fatalf("proxy test request failed: %#v", result)
	}
	data := result["data"].(map[string]any)
	if requested != target || data["title"] != "Fixture & Title" || data["status"] != float64(http.StatusTeapot) || data["time"] == nil {
		t.Fatalf("proxy result = %#v, requested URL = %q", data, requested)
	}
}

func TestProxyTestUsesRequestProxyConfigurationAndReportsNetworkFailure(t *testing.T) {
	var proxied bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied = true
		if !strings.Contains(r.URL.String(), "mikanani.me") {
			t.Errorf("proxy received an unexpected target %q", r.URL.String())
		}
		_, _ = io.WriteString(w, `<title>via proxy</title>`)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	_, _, address := serveRoutes(t, settings.Options{})
	token := login(t, address)
	config := map[string]any{"proxy": true, "proxyHost": proxyURL.Hostname(), "proxyPort": proxyURL.Port(), "proxyList": "mikanani.me"}
	target := base64.StdEncoding.EncodeToString([]byte("http://mikanani.me/test"))
	result := postURL(t, address, "/api/testProxy?url="+url.QueryEscape(target), token, config)
	if result["code"] != float64(200) || !proxied || result["data"].(map[string]any)["title"] != "via proxy" {
		t.Fatalf("configured proxy was not used: %#v, proxied=%v", result, proxied)
	}

	failed := doerFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("fixture dial failed") })
	_, _, failingAddress := serveRoutes(t, settings.Options{HTTPClient: failed})
	failedResult := postURL(t, failingAddress, "/api/testProxy?url="+url.QueryEscape(base64.StdEncoding.EncodeToString([]byte("https://fixture.invalid"))), login(t, failingAddress), map[string]any{})
	if failedResult["code"] != float64(500) || !strings.Contains(failedResult["message"].(string), "fixture dial failed") {
		t.Fatalf("network failure must remain visible: %#v", failedResult)
	}
}

func TestStopAndWhitelistCallbacksAreExplicit(t *testing.T) {
	var restart *bool
	app, _, address := serveRoutes(t, settings.Options{
		Stop:          func(value bool) error { restart = &value; return nil },
		WhitelistTest: func(*http.Request, map[string]any) bool { return true },
	})
	token := login(t, address)
	result := postURL(t, address, "/api/stop?status=0", token, nil)
	if result["code"] != float64(200) || restart == nil || !*restart {
		t.Fatalf("restart callback was not invoked: %#v, %v", result, restart)
	}
	result = post(t, address, "/api/testIpWhitelist", "", nil)
	if result["code"] != float64(200) {
		t.Fatalf("public whitelist check failed: %#v", result)
	}

	withoutCallbacks, _, noCallbackAddress := serveRoutes(t, settings.Options{})
	_ = withoutCallbacks
	result = postURL(t, noCallbackAddress, "/api/stop?status=1", login(t, noCallbackAddress), nil)
	if result["code"] != float64(500) || !strings.Contains(result["message"].(string), "尚未接入") {
		t.Fatalf("missing stop callback must be explicit: %#v", result)
	}
	result = post(t, noCallbackAddress, "/api/testIpWhitelist", "", nil)
	if result["code"] != float64(500) || !strings.Contains(result["message"].(string), "尚未接入") {
		t.Fatalf("missing whitelist callback must be explicit: %#v", result)
	}
	_ = app
}

func TestUnsupportedTrackerUpdateIsNotReportedAsSuccess(t *testing.T) {
	_, _, address := serveRoutes(t, settings.Options{})
	result := post(t, address, "/api/trackersUpdate", login(t, address), map[string]any{"trackersUpdateUrls": "https://example.invalid/trackers"})
	if result["code"] != float64(500) || !strings.Contains(result["message"].(string), "Transmission") {
		t.Fatalf("unsupported tracker update: %#v", result)
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func serveRoutes(t *testing.T, options settings.Options) (*server.Server, *storage.Store, string) {
	t.Helper()
	store, err := storage.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	app, err := server.New(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	options.Store = store
	if err := app.Register(settings.New(options).Routes()...); err != nil {
		t.Fatal(err)
	}
	listener := httptest.NewServer(app)
	t.Cleanup(listener.Close)
	return app, store, strings.TrimPrefix(listener.URL, "http://")
}

func login(t *testing.T, address string) string {
	t.Helper()
	digest := md5.Sum([]byte("admin"))
	result := post(t, address, "/api/login", "", map[string]any{"username": "admin", "password": hex.EncodeToString(digest[:])})
	token, _ := result["data"].(string)
	if result["code"] != float64(200) || token == "" {
		t.Fatalf("login failed: %#v", result)
	}
	return token
}

func post(t *testing.T, address, path, token string, body any) map[string]any {
	return postURL(t, address, path, token, body)
}

func postURL(t *testing.T, address, path, token string, body any) map[string]any {
	t.Helper()
	var payload io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		payload = strings.NewReader(string(data))
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+address+path, payload)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
