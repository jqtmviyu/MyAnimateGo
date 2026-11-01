// Package settings 实现配置、授权及运维相关的兼容接口。
package settings

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

const defaultAfdianEndpoint = "https://afdian.wushuo.top"

// ConfigStore 是这些接口使用的公开配置存储边界。
type ConfigStore interface {
	GetConfig(context.Context) (map[string]any, error)
	SaveConfig(context.Context, map[string]any) error
}

// HTTPDoer 允许测试通过固定上游响应验证接口行为。
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Options struct {
	Store          ConfigStore
	HTTPClient     HTTPDoer
	AfdianEndpoint string
	Stop           func(restart bool) error
	WhitelistTest  func(*http.Request, map[string]any) bool
}

type Service struct{ options Options }

func New(options Options) *Service {
	if options.AfdianEndpoint == "" {
		options.AfdianEndpoint = defaultAfdianEndpoint
	}
	return &Service{options: options}
}

// Routes 返回兼容接口；自定义资源和白名单探测沿用 Java 版的公开访问范围。
func (s *Service) Routes() []server.Route {
	return []server.Route{
		{Method: http.MethodPost, Path: "/api/stop", Handler: http.HandlerFunc(s.stop)},
		{Method: http.MethodPost, Path: "/api/verifyNo", Handler: http.HandlerFunc(s.verifyNo)},
		{Method: http.MethodPost, Path: "/api/testProxy", Handler: http.HandlerFunc(s.testProxy)},
		{Method: http.MethodPost, Path: "/api/testIpWhitelist", Public: true, Handler: http.HandlerFunc(s.testWhitelist)},
		{Method: http.MethodPost, Path: "/api/trackersUpdate", Handler: http.HandlerFunc(s.trackersUpdate)},
	}
}

func (s *Service) stop(w http.ResponseWriter, r *http.Request) {
	status, err := strconv.Atoi(r.URL.Query().Get("status"))
	if err != nil || (status != 0 && status != 1) {
		server.Respond(w, http.StatusInternalServerError, "status 参数只接受 0（重启）或 1（关闭）", nil)
		return
	}
	if s.options.Stop == nil {
		server.Respond(w, http.StatusInternalServerError, "停止与重启回调尚未接入", nil)
		return
	}
	restart := status == 0
	if err := s.options.Stop(restart); err != nil {
		server.Respond(w, http.StatusInternalServerError, "执行停止或重启失败："+err.Error(), nil)
		return
	}
	message := "正在关闭"
	if restart {
		message = "正在重启"
	}
	server.Respond(w, http.StatusOK, message, nil)
}

func (s *Service) testWhitelist(w http.ResponseWriter, r *http.Request) {
	if s.options.WhitelistTest == nil {
		server.Respond(w, http.StatusInternalServerError, "IP 白名单测试回调尚未接入", nil)
		return
	}
	config, err := s.config(r.Context())
	if err != nil {
		server.Respond(w, http.StatusInternalServerError, "读取配置失败", nil)
		return
	}
	if !s.options.WhitelistTest(r, config) {
		server.Respond(w, http.StatusInternalServerError, "当前 IP 不在白名单中", nil)
		return
	}
	server.Respond(w, http.StatusOK, "success", nil)
}

func (s *Service) verifyNo(w http.ResponseWriter, r *http.Request) {
	var input struct {
		OutTradeNo string `json:"outTradeNo"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil || strings.TrimSpace(input.OutTradeNo) == "" {
		server.Respond(w, http.StatusInternalServerError, "订单号为空或请求格式错误", nil)
		return
	}
	config, err := s.config(r.Context())
	if err != nil {
		server.Respond(w, http.StatusInternalServerError, "读取配置失败", nil)
		return
	}
	endpoint, err := url.Parse(s.options.AfdianEndpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		server.Respond(w, http.StatusInternalServerError, "赞助订单验证地址无效", nil)
		return
	}
	query := endpoint.Query()
	query.Set("out_trade_no", input.OutTradeNo)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint.String(), nil)
	if err != nil {
		server.Respond(w, http.StatusInternalServerError, "创建赞助验证请求失败", nil)
		return
	}
	response, err := s.client(config, endpoint.String(), 5*time.Second).Do(request)
	if err != nil {
		server.Respond(w, http.StatusInternalServerError, "赞助订单验证请求失败："+sanitizeRequestError(err, endpoint), nil)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		server.Respond(w, http.StatusInternalServerError, fmt.Sprintf("赞助订单验证失败：HTTP %d", response.StatusCode), nil)
		return
	}
	var upstream struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&upstream); err != nil {
		server.Respond(w, http.StatusInternalServerError, "赞助订单验证响应格式错误", nil)
		return
	}
	if upstream.Code == http.StatusOK {
		config["outTradeNo"] = input.OutTradeNo
		config["expirationTime"] = time.Now().AddDate(999, 0, 0).UnixMilli()
		config["tryOut"] = false
		var saveErr error
		if store, ok := s.options.Store.(interface {
			UpdateConfigFields(context.Context, map[string]any) error
		}); ok {
			saveErr = store.UpdateConfigFields(r.Context(), map[string]any{"outTradeNo": config["outTradeNo"], "expirationTime": config["expirationTime"], "tryOut": false})
		} else {
			saveErr = s.options.Store.SaveConfig(r.Context(), config)
		}
		if saveErr != nil {
			server.Respond(w, http.StatusInternalServerError, "保存赞助授权状态失败", nil)
			return
		}
	}
	message := upstream.Message
	if message == "" {
		message = "success"
	}
	server.Respond(w, upstream.Code, message, nil)
}

func (s *Service) testProxy(w http.ResponseWriter, r *http.Request) {
	encoded := r.URL.Query().Get("url")
	target, err := decodeURL(encoded)
	if err != nil {
		server.Respond(w, http.StatusInternalServerError, "代理测试 URL 格式错误", nil)
		return
	}
	parsed, err := url.ParseRequestURI(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		server.Respond(w, http.StatusInternalServerError, "代理测试 URL 必须是完整的 HTTP 或 HTTPS 地址", nil)
		return
	}
	config, err := decodeConfig(r)
	if err != nil {
		server.Respond(w, http.StatusInternalServerError, "代理配置请求格式错误", nil)
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		server.Respond(w, http.StatusInternalServerError, "创建代理测试请求失败", nil)
		return
	}
	start := time.Now()
	response, err := s.client(config, target, 20*time.Second).Do(request)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		server.Respond(w, http.StatusInternalServerError, "代理测试请求失败："+sanitizeRequestError(err, parsed), nil)
		return
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		server.Respond(w, http.StatusInternalServerError, "读取代理测试响应失败", nil)
		return
	}
	title := parseTitle(string(body))
	server.Respond(w, http.StatusOK, "success", map[string]any{"title": title, "status": response.StatusCode, "time": duration})
}

func (s *Service) trackersUpdate(w http.ResponseWriter, _ *http.Request) {
	server.Respond(w, http.StatusInternalServerError, "Transmission 当前不支持更新 trackers", nil)
}

func (s *Service) config(ctx context.Context) (map[string]any, error) {
	if s.options.Store == nil {
		return nil, errors.New("配置存储未接入")
	}
	return s.options.Store.GetConfig(ctx)
}

// Verify 检查已保存的赞助订单、有效期与试用状态。
func (s *Service) Verify(ctx context.Context, config map[string]any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if strings.TrimSpace(stringValue(config["outTradeNo"])) == "" || config["tryOut"] == true {
		return false, nil
	}
	return integer64Value(config["expirationTime"]) > time.Now().UnixMilli(), nil
}

func (s *Service) client(config map[string]any, target string, timeout time.Duration) HTTPDoer {
	if s.options.HTTPClient != nil {
		return s.options.HTTPClient
	}
	return &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: proxyFor(config)}}
}

func proxyFor(config map[string]any) func(*http.Request) (*url.URL, error) {
	return func(request *http.Request) (*url.URL, error) {
		if config["proxy"] != true || !proxyDomainAllowed(request.URL.Hostname(), stringValue(config["proxyList"])) {
			return nil, nil
		}
		host := strings.TrimSpace(stringValue(config["proxyHost"]))
		port := integerValue(config["proxyPort"])
		if host == "" || port < 1 || port > 65535 {
			return nil, nil
		}
		if parsed, err := url.Parse(host); err == nil && parsed.Hostname() != "" {
			host = parsed.Hostname()
		}
		proxyURL := &url.URL{Scheme: "http", Host: net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))}
		username, password := stringValue(config["proxyUsername"]), stringValue(config["proxyPassword"])
		if username != "" && password != "" {
			proxyURL.User = url.UserPassword(username, password)
		}
		return proxyURL, nil
	}
}

func proxyDomainAllowed(host, rawList string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, entry := range strings.Split(rawList, "\n") {
		entry = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(entry, ".")))
		if entry != "" && (host == entry || strings.HasSuffix(host, "."+entry)) {
			return true
		}
	}
	return false
}

func decodeURL(value string) (string, error) {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		decoded, err := encoding.DecodeString(value)
		if err == nil {
			return string(decoded), nil
		}
	}
	return "", errors.New("Base64 地址编码无效")
}

var titlePattern = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title\s*>`)
var tagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

func parseTitle(document string) string {
	match := titlePattern.FindStringSubmatch(document)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(tagPattern.ReplaceAllString(match[1], "")))
}

func decodeConfig(r *http.Request) (map[string]any, error) {
	var config map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&config); err != nil || config == nil {
		return nil, errors.New("代理配置格式错误")
	}
	return config, nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func integerValue(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		parsed, _ := strconv.Atoi(number.String())
		return parsed
	case string:
		parsed, _ := strconv.Atoi(number)
		return parsed
	default:
		return 0
	}
}

func integer64Value(value any) int64 {
	switch number := value.(type) {
	case int:
		return int64(number)
	case int64:
		return number
	case float64:
		return int64(number)
	case json.Number:
		parsed, _ := strconv.ParseInt(number.String(), 10, 64)
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(number, 10, 64)
		return parsed
	default:
		return 0
	}
}

func sanitizeRequestError(err error, target *url.URL) string {
	message := err.Error()
	if target != nil {
		message = strings.ReplaceAll(message, target.String(), target.Scheme+"://"+target.Host+target.Path)
		message = strings.ReplaceAll(message, target.Redacted(), target.Scheme+"://"+target.Host+target.Path)
	}
	return message
}
