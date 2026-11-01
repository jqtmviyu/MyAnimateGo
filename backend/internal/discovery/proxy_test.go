package discovery

import (
	"net/http"
	"testing"
)

func TestProxyUsesConfigAndOnlyMatchesAllowedHosts(t *testing.T) {
	config := map[string]any{"proxy": true, "proxyHost": "proxy.test", "proxyPort": 8899, "proxyUsername": "user", "proxyPassword": "pass"}
	proxy := proxyFor(config, []string{"mikanani.me", "bgm.tv"})
	request, _ := http.NewRequest(http.MethodGet, "https://sub.mikanani.me/Home/Search", nil)
	got, err := proxy(request)
	if err != nil || got == nil || got.Host != "proxy.test:8899" || got.User.Username() != "user" {
		t.Fatalf("匹配站点代理=%v err=%v", got, err)
	}
	request, _ = http.NewRequest(http.MethodGet, "https://example.test/data", nil)
	got, err = proxy(request)
	if err != nil || got != nil {
		t.Fatalf("非允许站点不应代理：%v err=%v", got, err)
	}
	config["proxy"] = false
	disabled := proxyFor(config, []string{"mikanani.me"})
	if disabled != nil {
		got, err = disabled(request)
	}
	if err != nil || got != nil {
		t.Fatalf("关闭代理后不应使用代理：%v err=%v", got, err)
	}
}
