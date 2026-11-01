package transmission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnconfiguredDownloaderProvidesAddressGuidance(t *testing.T) {
	_, err := New(Config{}).List(context.Background(), []string{"name"})
	if err == nil || !strings.Contains(err.Error(), "配置 Transmission 服务地址") {
		t.Fatalf("未配置时应提示填写下载器地址：%v", err)
	}
}

func TestCallRetriesSessionChallengeWithLegacyProtocol(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/transmission/rpc" {
			t.Errorf("RPC path = %q", r.URL.Path)
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != "alice" || pass != "secret" {
			t.Errorf("unexpected basic auth")
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		var method string
		_ = json.Unmarshal(body["method"], &method)
		if method != "torrent-get" {
			t.Errorf("legacy method = %q", method)
		}
		if len(body["arguments"]) == 0 || len(body["params"]) != 0 {
			t.Errorf("request should use arguments: %s", body["arguments"])
		}
		if requests == 1 {
			w.Header().Set("X-Transmission-Session-Id", "session-token")
			w.WriteHeader(http.StatusConflict)
			return
		}
		if got := r.Header.Get("X-Transmission-Session-Id"); got != "session-token" {
			t.Errorf("session id = %q", got)
		}
		_, _ = w.Write([]byte(`{"arguments":{"torrents":[]},"result":"success"}`))
	}))
	defer server.Close()

	client := New(Config{URL: server.URL, Username: "alice", Password: "secret"})
	result, err := client.Call(context.Background(), "torrent-get", map[string]any{"fields": []string{"name"}})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if string(result) != `{"torrents":[]}` {
		t.Fatalf("result = %s", result)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestCallReportsLegacyStringError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"arguments":{},"result":"method name not recognized"}`))
	}))
	defer server.Close()

	client := New(Config{URL: server.URL})
	_, err := client.Call(context.Background(), "unknown-method", nil)
	if err == nil || err.Error() != "transmission RPC error: method name not recognized" {
		t.Fatalf("Call() error = %v", err)
	}
}

func TestAddReportsDuplicateTorrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string                     `json:"method"`
			Args   map[string]json.RawMessage `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Method != "torrent-add" {
			t.Errorf("method = %q", request.Method)
		}
		var magnet string
		_ = json.Unmarshal(request.Args["filename"], &magnet)
		if magnet != "magnet:?xt=urn:btih:abc" {
			t.Errorf("filename = %q", magnet)
		}
		_, _ = w.Write([]byte(`{"arguments":{"torrent-duplicate":{"hashString":"abc"}},"result":"success"}`))
	}))
	defer server.Close()

	got, err := New(Config{URL: server.URL}).Add(context.Background(), AddRequest{Magnet: "magnet:?xt=urn:btih:abc", DownloadDir: "/downloads", Labels: []string{"ani-rss"}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got.Hash != "abc" || !got.Duplicate {
		t.Fatalf("Add() = %+v", got)
	}
}

func TestRemoveSendsExplicitDataDeletionChoice(t *testing.T) {
	for _, deleteFiles := range []bool{false, true} {
		t.Run(map[bool]string{false: "keep-files", true: "delete-files"}[deleteFiles], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Method string                     `json:"method"`
					Args   map[string]json.RawMessage `json:"arguments"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if request.Method != "torrent-remove" {
					t.Errorf("method = %q", request.Method)
				}
				var actual bool
				if err := json.Unmarshal(request.Args["delete-local-data"], &actual); err != nil {
					t.Fatal(err)
				}
				if actual != deleteFiles {
					t.Errorf("delete-local-data = %v, want %v", actual, deleteFiles)
				}
				var ids []string
				_ = json.Unmarshal(request.Args["ids"], &ids)
				if len(ids) != 1 || ids[0] != "hash-1" {
					t.Errorf("ids = %v", ids)
				}
				_, _ = w.Write([]byte(`{"arguments":{},"result":"success"}`))
			}))
			defer server.Close()
			if err := New(Config{URL: server.URL}).Remove(context.Background(), "hash-1", deleteFiles); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
		})
	}
}

func TestCallRejectsMissingOrNonStringResult(t *testing.T) {
	for name, response := range map[string]string{
		"missing": `{"arguments":{}}`,
		"number":  `{"arguments":{},"result":0}`,
		"null":    `{"arguments":{},"result":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()
			if _, err := New(Config{URL: server.URL}).Call(context.Background(), "torrent-get", nil); err == nil {
				t.Fatal("Call() succeeded without a string result")
			}
		})
	}
}

func TestListRejectsMissingTorrentsAndAcceptsEmptyList(t *testing.T) {
	for _, test := range []struct {
		name    string
		args    string
		wantErr bool
	}{
		{name: "missing", args: `{}`, wantErr: true},
		{name: "null", args: `{"torrents":null}`, wantErr: true},
		{name: "empty", args: `{"torrents":[]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"arguments":%s,"result":"success"}`, test.args)
			}))
			defer server.Close()
			got, err := New(Config{URL: server.URL}).List(context.Background(), []string{"name"})
			if test.wantErr && err == nil {
				t.Fatal("List() succeeded without a torrents array")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if !test.wantErr && len(got) != 0 {
				t.Fatalf("List() = %v, want empty list", got)
			}
		})
	}
}

func TestCallDoesNotReturnHTTPErrorBody(t *testing.T) {
	const secret = "upstream-private-response"
	const credential = "url-private-password"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, secret, http.StatusBadGateway)
	}))
	defer server.Close()
	credentialURL := strings.Replace(server.URL, "http://", "http://private-user:"+credential+"@", 1)
	_, err := New(Config{URL: credentialURL}).Call(context.Background(), "torrent-get", nil)
	if err == nil {
		t.Fatal("Call() succeeded for HTTP 502")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), credential) || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("HTTP error leaks response content or request address: %v", err)
	}
}
