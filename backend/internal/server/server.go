package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mozillazg/go-pinyin"
)

type Store interface {
	GetConfig(context.Context) (map[string]any, error)
	SaveConfig(context.Context, map[string]any) error
	UpdateConfigFields(context.Context, map[string]any) error
	ListSubscriptions(context.Context) ([]map[string]any, error)
	PutSubscription(context.Context, map[string]any) error
	DeleteSubscriptions(context.Context, []string) error
	ReplaceSubscriptions(context.Context, []map[string]any) error
}

// 默认设置随 Go 服务内置，实例路径与随机密钥在启动时生成。
//
//go:embed defaults.json
var defaultsJSON []byte

//go:embed ani-defaults.json
var aniDefaultsJSON []byte

type Server struct {
	development      bool
	configValidate   func(map[string]any) error
	configChanged    func(map[string]any)
	taskPauseHandler func(context.Context, bool) error
	pathResolver     func(context.Context, map[string]any) (string, error)
	store            Store
	mu               sync.Mutex
	coverMu          sync.RWMutex
	coverImporter    func(context.Context, string) (string, error)
	session          string
	attempts         map[string]loginAttempt
	routesMu         sync.RWMutex
	routes           map[string]Route
}

type loginAttempt struct {
	Count int
	Until time.Time
}

type result struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Time    int64  `json:"t"`
}

func New(ctx context.Context, store Store) (*Server, error) {
	config, err := store.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	defaults := map[string]any{}
	if err = json.Unmarshal(defaultsJSON, &defaults); err != nil {
		return nil, err
	}
	for key, value := range config {
		if value != nil {
			defaults[key] = value
		}
	}
	for _, key := range []string{"jwtKey", "tokenId", "uuid", "apiKey"} {
		if text(defaults[key]) == "" {
			value, err := randomString(32)
			if err != nil {
				return nil, err
			}
			defaults[key] = value
		}
	}
	if _, exists := defaults["automaticTasksPaused"]; !exists {
		defaults["automaticTasksPaused"] = true
	}
	if err = store.SaveConfig(ctx, defaults); err != nil {
		return nil, err
	}
	session := "-"
	if boolean(defaults["multiLoginForbidden"]) {
		session, err = randomString(16)
	}
	if err != nil {
		return nil, err
	}
	return &Server{development: boolean(config["developmentMode"]), store: store, session: session, attempts: map[string]loginAttempt{}, routes: map[string]Route{}}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	value := result{Code: 200, Message: "success"}
	config, err := s.store.GetConfig(r.Context())
	if err != nil {
		s.write(w, result{Code: 500, Message: "读取配置失败"})
		return
	}
	path := strings.ToLower(r.URL.Path)
	client := clientIP(r, config)
	if boolean(config["innerIP"]) {
		addr, parseErr := netip.ParseAddr(client)
		if parseErr != nil || (!addr.IsPrivate() && !addr.IsLoopback()) {
			http.Error(w, "禁止公网访问", http.StatusForbidden)
			return
		}
	}
	if boolean(config["allowCors"]) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
	}
	s.routesMu.RLock()
	route, exists := s.routes[path]
	s.routesMu.RUnlock()
	if exists {
		// HEAD 复用 GET 处理器，保留同一访问控制及真实资源响应头。
		if route.Method != "" && route.Method != r.Method && !(route.Method == http.MethodGet && r.Method == http.MethodHead) {
			s.write(w, result{Code: 404, Message: "404 Not Found !"})
			return
		}
		if !route.Public {
			if boolean(config["limitLoginAttempts"]) && s.loginLimited(client) {
				s.write(w, result{Code: 403, Message: "失败次数过多，已限制登录"})
				return
			}
			if !s.authorized(r, config) {
				if boolean(config["limitLoginAttempts"]) {
					s.failedLogin(client)
				}
				s.write(w, result{Code: 403, Message: "登录已失效"})
				return
			}
		}
		route.Handler.ServeHTTP(w, r)
		return
	}
	if path == "/api/ping" {
		s.write(w, value)
		return
	}
	if r.Method != http.MethodPost {
		s.write(w, result{Code: 404, Message: "404 Not Found !"})
		return
	}
	if boolean(config["limitLoginAttempts"]) && s.loginLimited(client) {
		s.write(w, result{Code: 403, Message: "失败次数过多，已限制登录"})
		return
	}
	if path == "/api/login" {
		var login map[string]any
		err = decode(r, &login)
		if err == nil {
			configured, _ := config["login"].(map[string]any)
			if text(login["username"]) == "" || text(login["password"]) == "" {
				err = errors.New("用户名和密码不能为空")
			} else if !equal(text(login["username"]), text(configured["username"])) || !equal(text(login["password"]), text(configured["password"])) {
				err = errors.New("用户名或密码错误")
			} else {
				value.Data, err = s.token(config, client)
				value.Message = "登录成功"
				s.mu.Lock()
				delete(s.attempts, client)
				s.mu.Unlock()
			}
		}
		if err != nil && boolean(config["limitLoginAttempts"]) {
			s.failedLogin(client)
		}
	} else if !s.authorized(r, config) {
		value = result{Code: 403, Message: "登录已失效"}
		if boolean(config["limitLoginAttempts"]) {
			s.failedLogin(client)
		}
	} else {
		value, err = s.dispatch(r, path, config)
	}
	if err != nil {
		value = result{Code: 500, Message: err.Error()}
	}
	s.write(w, value)
}

func (s *Server) write(w http.ResponseWriter, value result) {
	writeResult(w, value)
}

func writeResult(w http.ResponseWriter, value result) {
	value.Time = time.Now().UnixMilli()
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) dispatch(r *http.Request, path string, config map[string]any) (result, error) {
	value := result{Code: 200, Message: "success"}
	switch path {
	case "/api/config":
		config["jwtKey"] = ""
		config["verifyExpirationTime"] = number(config["expirationTime"]) > float64(time.Now().UnixMilli())
		if login, ok := config["login"].(map[string]any); ok {
			login["password"] = ""
		}
		value.Data = config
	case "/api/setconfig":
		var next map[string]any
		if err := decode(r, &next); err != nil {
			return value, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		config, err := s.store.GetConfig(r.Context())
		if err != nil {
			return value, err
		}
		updates := map[string]any{}
		for key, item := range next {
			if item == nil || key == "transmissionBlocklistState" || key == "transmissionBlocklist" || key == "automaticTasksPaused" || key == "developmentMode" || key == "migrationReport" || key == "jwtKey" || key == "tokenId" || key == "expirationTime" || key == "outTradeNo" || key == "tryOut" {
				continue
			}
			if key == "login" {
				newLogin, ok := item.(map[string]any)
				if !ok {
					return value, errors.New("登录配置格式错误")
				}
				oldLogin, _ := config["login"].(map[string]any)
				if text(newLogin["password"]) == "" {
					newLogin["password"] = oldLogin["password"]
				}
				if !equal(text(newLogin["username"]), text(oldLogin["username"])) || !equal(text(newLogin["password"]), text(oldLogin["password"])) {
					id, err := randomString(32)
					if err != nil {
						return value, err
					}
					config["tokenId"] = id
					updates["tokenId"] = id
				}
			}
			if !reflect.DeepEqual(config[key], item) {
				updates[key] = item
			}
			config[key] = item
		}
		if s.configValidate != nil {
			if err := s.configValidate(config); err != nil {
				return value, err
			}
		}
		value.Message = "修改成功"
		err = s.store.UpdateConfigFields(r.Context(), updates)
		if err == nil && s.configChanged != nil {
			s.configChanged(updates)
		}
		return value, err
	case "/api/setautomatictaskspaused":
		if !s.development {
			return result{Code: 404, Message: "自动任务暂停仅在开发模式可用"}, nil
		}
		var change struct {
			Paused   *bool `json:"paused"`
			Reviewed bool  `json:"reviewed"`
		}
		if err := decode(r, &change); err != nil {
			return value, err
		}
		if change.Paused == nil {
			return value, errors.New("请明确选择暂停或启用自动任务")
		}
		if !*change.Paused && !change.Reviewed {
			return value, errors.New("请先核对迁移报告、下载路径和待执行任务")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.taskPauseHandler == nil {
			return value, errors.New("任务调度尚未就绪")
		}
		if err := s.taskPauseHandler(r.Context(), *change.Paused); err != nil {
			return value, err
		}
		value.Data = map[string]any{"automaticTasksPaused": *change.Paused}
		value.Message = "自动任务已启用"
		if *change.Paused {
			value.Message = "自动任务已暂停"
		}
	case "/api/listani":
		items, err := s.store.ListSubscriptions(r.Context())
		if err != nil {
			return value, err
		}
		value.Data = grouped(items, config)
	case "/api/addani":
		var item map[string]any
		if err := decode(r, &item); err != nil {
			return value, err
		}
		if booleanParam(r, "move") {
			return value, errors.New("媒体移动尚未接入，请保持原路径")
		}
		_, err := s.AddSubscription(r.Context(), item, booleanParam(r, "override"))
		return value, err
	case "/api/setani":
		var item map[string]any
		if err := decode(r, &item); err != nil {
			return value, err
		}
		if err := s.UpdateSubscription(r.Context(), item, booleanParam(r, "move")); err != nil {
			return value, err
		}
		value.Message = "修改成功"
	case "/api/importani":
		var input struct {
			AniList  []map[string]any `json:"aniList"`
			Conflict string           `json:"conflict"`
		}
		if err := decode(r, &input); err != nil {
			return value, err
		}
		if err := s.ImportAni(r.Context(), input.AniList, input.Conflict); err != nil {
			return value, err
		}
		value.Message = "导入成功"
	case "/api/deleteani", "/api/batchenable":
		var ids []string
		if err := decode(r, &ids); err != nil {
			return value, err
		}
		if len(ids) == 0 {
			return value, errors.New("未选择订阅")
		}
		if path == "/api/deleteani" {
			value.Message = "删除订阅成功"
			return value, s.DeleteSubscriptions(r.Context(), ids, booleanParam(r, "deleteFiles"))
		}
		store, ok := s.store.(interface {
			UpdateSubscriptionFieldsBatch(context.Context, []string, map[string]any) error
		})
		if !ok {
			return value, errors.New("批量修改订阅需要事务字段存储")
		}
		if err := store.UpdateSubscriptionFieldsBatch(r.Context(), ids, map[string]any{"enable": booleanParam(r, "value")}); err != nil {
			return value, err
		}
		value.Message = "修改完成"
	default:
		value = result{Code: 404, Message: "404 Not Found !"}
	}
	return value, nil
}

func decode(r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	if err := decoder.Decode(target); err != nil {
		return errors.New("JSON 请求格式错误")
	}
	return nil
}

func grouped(items []map[string]any, config map[string]any) map[string]any {
	labels := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	groups := make([]map[string]any, 7)
	today := int(time.Now().Weekday())
	for i := range groups {
		day := (today - i + 7) % 7
		groups[i] = map[string]any{"weekLabel": labels[day], "items": []map[string]any{}}
	}
	months := map[string]bool{}
	for i, item := range items {
		items[i] = subscriptionDefaults(item, config)
		full, initials := pinyinTitle(text(item["title"]))
		items[i]["pinyin"] = full
		items[i]["pinyinInitials"] = initials
	}
	sort.SliceStable(items, func(i, j int) bool {
		switch text(config["sortType"]) {
		case "PINYIN":
			return text(items[i]["pinyin"]) < text(items[j]["pinyin"])
		case "DOWNLOAD_TIME":
			return integer(items[i]["lastDownloadTime"]) > integer(items[j]["lastDownloadTime"])
		default:
			return number(items[i]["score"]) > number(items[j]["score"])
		}
	})
	for index, item := range items {
		item["sort"] = index
		month, weekday, hasExactDate := subscriptionDate(item)
		if month != "" {
			months[month] = true
		}
		if !hasExactDate {
			delete(item, "weekLabel")
			continue
		}
		item["weekLabel"] = labels[weekday]
		for _, group := range groups {
			if group["weekLabel"] == item["weekLabel"] {
				group["items"] = append(group["items"].([]map[string]any), item)
			}
		}
	}
	monthList := []string{}
	for month := range months {
		monthList = append(monthList, month)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(monthList)))
	return map[string]any{"total": len(items), "releaseDateList": monthList, "weekList": groups}
}

func subscriptionDate(item map[string]any) (month string, weekday time.Weekday, exact bool) {
	releaseDate := strings.TrimSpace(text(item["releaseDate"]))
	if day, err := time.ParseInLocation("2006-01-02", releaseDate, time.Local); err == nil {
		return day.Format("2006-01"), day.Weekday(), true
	}
	if monthDate, err := time.ParseInLocation("2006-01", releaseDate, time.Local); err == nil {
		return monthDate.Format("2006-01"), 0, false
	}

	year, monthNumber := integer(item["year"]), integer(item["month"])
	if year <= 0 || monthNumber < 1 || monthNumber > 12 {
		return "", 0, false
	}
	monthDate := time.Date(year, time.Month(monthNumber), 1, 0, 0, 0, 0, time.Local)
	dayNumber := integer(item["date"])
	if dayNumber < 1 {
		return monthDate.Format("2006-01"), 0, false
	}
	day := time.Date(year, time.Month(monthNumber), dayNumber, 0, 0, 0, 0, time.Local)
	if day.Year() != year || int(day.Month()) != monthNumber || day.Day() != dayNumber {
		return monthDate.Format("2006-01"), 0, false
	}
	return monthDate.Format("2006-01"), day.Weekday(), true
}

func subscriptionDefaults(item, config map[string]any) map[string]any {
	defaults := map[string]any{}
	_ = json.Unmarshal(aniDefaultsJSON, &defaults)
	defaults["season"] = 1
	for _, key := range []string{"customEpisode", "customEpisodeStr", "customEpisodeGroupIndex", "upload"} {
		defaults[key] = config[key]
	}
	defaults["customRenameTemplate"] = config["renameTemplate"]
	for key, value := range item {
		if value != nil {
			defaults[key] = value
		}
	}
	return defaults
}

func pinyinTitle(title string) (string, string) {
	polyphonic := map[string][]string{"重庆": {"chong", "qing"}, "重启": {"chong", "qi"}, "重回": {"chong", "hui"}, "重生": {"chong", "sheng"}, "重来": {"chong", "lai"}, "调教": {"tiao", "jiao"}}
	args := pinyin.NewArgs()
	args.Fallback = func(r rune, _ pinyin.Args) []string { return []string{string(r)} }
	var full, initials strings.Builder
	runes := []rune(title)
	for i := 0; i < len(runes); i++ {
		if i+1 < len(runes) {
			if words, ok := polyphonic[string(runes[i:i+2])]; ok {
				for _, word := range words {
					full.WriteString(word)
					initials.WriteByte(word[0])
				}
				i++
				continue
			}
		}
		words := pinyin.SinglePinyin(runes[i], args)
		word := strings.ToLower(words[0])
		full.WriteString(word)
		initials.WriteRune([]rune(word)[0])
	}
	return full.String(), initials.String()
}

func number(value any) float64 {
	switch n := value.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		v, _ := n.Float64()
		return v
	}
	return 0
}

func (s *Server) token(config map[string]any, ip string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if boolean(config["multiLoginForbidden"]) {
		value, err := randomString(16)
		if err != nil {
			return "", err
		}
		s.session = value
	} else {
		s.session = "-"
	}
	expires := int64(0)
	if hours := integer(config["loginEffectiveHours"]); hours > 0 {
		expires = time.Now().Add(time.Duration(hours) * time.Hour).UnixMilli()
	}
	claims := map[string]any{"sessionId": s.session, "expireTime": expires, "tokenId": config["tokenId"], "ip": ip}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	data := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	return data + "." + signature(data, text(config["jwtKey"])), nil
}

func (s *Server) authorized(r *http.Request, config map[string]any) bool {
	if WhitelistMatches(r, config) {
		return true
	}
	key := text(config["apiKey"])
	for _, name := range []string{"api-key", "x-api-key", "s"} {
		value := r.Header.Get(name)
		if value == "" {
			value = r.URL.Query().Get(name)
		}
		if key != "" && value != "" && equal(key, value) {
			return true
		}
	}
	for _, token := range []string{r.Header.Get("Authorization"), r.URL.Query().Get("s")} {
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			continue
		}
		if !equal(parts[2], signature(parts[0]+"."+parts[1], text(config["jwtKey"]))) {
			continue
		}
		data, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			continue
		}
		var claims map[string]any
		if json.Unmarshal(data, &claims) != nil {
			continue
		}
		s.mu.Lock()
		session := s.session
		s.mu.Unlock()
		if text(claims["sessionId"]) != session || text(claims["tokenId"]) != text(config["tokenId"]) {
			continue
		}
		if boolean(config["verifyLoginIp"]) && text(claims["ip"]) != clientIP(r, config) {
			continue
		}
		if expiry := integer(claims["expireTime"]); expiry > 0 && int64(expiry) < time.Now().UnixMilli() {
			continue
		}
		return true
	}
	return false
}

func (s *Server) loginLimited(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt := s.attempts[ip]
	if time.Now().After(attempt.Until) {
		delete(s.attempts, ip)
		return false
	}
	return attempt.Count >= 30
}

func (s *Server) failedLogin(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt := s.attempts[ip]
	if time.Now().After(attempt.Until) {
		attempt.Count = 0
	}
	attempt.Count++
	attempt.Until = time.Now().Add(24 * time.Hour)
	s.attempts[ip] = attempt
}

func clientIP(r *http.Request, config map[string]any) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	if boolean(config["reverseProxyTrustIpListEnabled"]) {
		trusted, _ := config["reverseProxyTrustIpList"].([]any)
		for _, item := range trusted {
			if text(item) == peer {
				first := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0])
				if addr, err := netip.ParseAddr(first); err == nil {
					return addr.Unmap().String()
				}
			}
		}
	}
	return peer
}

func whitelisted(ip, list string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(line)
		if line == ip {
			return true
		}
		if prefix, err := netip.ParsePrefix(line); err == nil && prefix.Contains(addr) {
			return true
		}
		if strings.Contains(line, "*") && addr.Is4() {
			pattern, parts := strings.Split(line, "."), strings.Split(ip, ".")
			if len(pattern) == 4 {
				match := true
				for i, part := range pattern {
					if part != "*" && part != parts[i] {
						match = false
					}
				}
				if match {
					return true
				}
			}
		}
		if start, end, ok := strings.Cut(line, "-"); ok {
			a, e1 := netip.ParseAddr(start)
			b, e2 := netip.ParseAddr(end)
			if e1 == nil && e2 == nil && addr.Compare(a) >= 0 && addr.Compare(b) <= 0 {
				return true
			}
		}
	}
	return false
}

// WhitelistMatches 供登录页探测入口复用真实的白名单和受信代理规则。
func WhitelistMatches(r *http.Request, config map[string]any) bool {
	return boolean(config["ipWhitelist"]) && whitelisted(clientIP(r, config), text(config["ipWhitelistStr"]))
}

func signature(data, key string) string {
	secret, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		secret = []byte(key)
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func randomString(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}
func text(value any) string  { s, _ := value.(string); return s }
func boolean(value any) bool { b, _ := value.(bool); return b }
func integer(value any) int {
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		value, _ := strconv.ParseInt(string(n), 10, 64)
		return int(value)
	}
	return 0
}
func booleanParam(r *http.Request, name string) bool {
	value, _ := strconv.ParseBool(r.URL.Query().Get(name))
	return value
}

var _ http.Handler = (*Server)(nil)

// SetTaskPauseHandler 接入调度器的暂停生命周期，普通设置保存保留当前运行状态。
func (s *Server) SetTaskPauseHandler(handler func(context.Context, bool) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskPauseHandler = handler
}

// SetConfigHooks 校验节点过滤设置，持久化后唤醒独立后台服务。
func (s *Server) SetConfigHooks(validate func(map[string]any) error, changed func(map[string]any)) {
	s.configValidate = validate
	s.configChanged = changed
}
