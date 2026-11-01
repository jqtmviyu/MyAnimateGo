package discovery

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type credentialProbe struct{ calls int }

func (p *credentialProbe) Do(r *http.Request) (*http.Response, error) {
	p.calls++
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[]}`)), Header: make(http.Header)}, nil
}
func TestTMDBSearchCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, base, key string
		wantCalls       int
	}{
		{"官方缺少凭据", "https://api.themoviedb.org", "", 0},
		{"官方配置凭据", "https://api.themoviedb.org", "fixture-key", 1},
		{"自定义免密代理", "https://metadata.example", "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &credentialProbe{}
			client := New(nil, probe, nil)
			_, _, err := client.tmdbSearch(context.Background(), map[string]any{"tmdbApi": tc.base, "tmdbApiKey": tc.key}, "作品", false)
			if probe.calls != tc.wantCalls {
				t.Fatalf("TMDB 请求次数=%d，期望=%d", probe.calls, tc.wantCalls)
			}
			if tc.wantCalls > 0 && err != nil {
				t.Fatal(err)
			}
		})
	}
}
