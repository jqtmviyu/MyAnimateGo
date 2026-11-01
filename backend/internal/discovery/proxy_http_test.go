package discovery

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

type proxyStore struct{ config map[string]any }

func (s *proxyStore) GetConfig(context.Context) (map[string]any, error) { return s.config, nil }
func (s *proxyStore) UpdateConfigFields(_ context.Context, fields map[string]any) error {
	for key, value := range fields {
		s.config[key] = value
	}
	return nil
}
func (*proxyStore) SaveConfig(context.Context, map[string]any) error             { return nil }
func (*proxyStore) ListSubscriptions(context.Context) ([]map[string]any, error)  { return nil, nil }
func (*proxyStore) PutSubscription(context.Context, map[string]any) error        { return nil }
func (*proxyStore) DeleteSubscriptions(context.Context, []string) error          { return nil }
func (*proxyStore) ReplaceSubscriptions(context.Context, []map[string]any) error { return nil }

func TestDefaultTransportReadsProxySettingsFromStoreForEachRequest(t *testing.T) {
	forwarded := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded++
		if !strings.Contains(r.URL.String(), "mikan.fixture.invalid/Home/Search") {
			t.Errorf("代理收到的 URL=%s", r.URL)
		}
		_, _ = w.Write([]byte(`<ul class="an-ul"></ul>`))
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	host, portText, _ := net.SplitHostPort(proxyURL.Host)
	port, _ := strconv.Atoi(portText)
	store := &proxyStore{config: map[string]any{"proxy": true, "proxyHost": host, "proxyPort": port, "proxyList": "fixture.invalid", "mikanHost": "http://mikan.fixture.invalid"}}
	client := New(store, nil, nil)
	if _, err := client.searchMikan(context.Background(), "test", nil); err != nil {
		t.Fatal(err)
	}
	if forwarded != 1 {
		t.Fatal("默认 HTTP 客户端没有按 store 中的代理配置转发")
	}
	proxy2Count := 0
	proxy2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy2Count++
		_, _ = w.Write([]byte(`<ul class="an-ul"></ul>`))
	}))
	defer proxy2.Close()
	proxyURL2, _ := url.Parse(proxy2.URL)
	host2, portText2, _ := net.SplitHostPort(proxyURL2.Host)
	port2, _ := strconv.Atoi(portText2)
	store.config["proxyHost"], store.config["proxyPort"] = host2, port2
	if _, err := client.searchMikan(context.Background(), "again", nil); err != nil {
		t.Fatal(err)
	}
	if proxy2Count != 1 {
		t.Fatal("客户端没有在后续请求读取新代理配置")
	}
}
