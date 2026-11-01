package peerfilter

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

func (s *Service) Routes() []server.Route {
	return []server.Route{
		{Method: http.MethodGet, Path: "/api/peerFilter", Handler: http.HandlerFunc(s.statusHTTP)},
		{Method: http.MethodGet, Path: "/api/peerFilterBans", Handler: http.HandlerFunc(s.bansHTTP)},
		{Method: http.MethodPost, Path: "/api/peerFilterAction", Handler: http.HandlerFunc(s.actionHTTP)},
		{Method: http.MethodGet, Path: "/api/peerFilterFeed", Public: true, Handler: http.HandlerFunc(s.feedHTTP)},
	}
}
func (s *Service) statusHTTP(w http.ResponseWriter, r *http.Request) {
	config, err := s.store.GetConfig(r.Context())
	if err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	settings, exists, err := ReadSettings(config)
	if err != nil {
		server.Respond(w, 400, err.Error(), nil)
		return
	}
	v := s.Status()
	v.Settings = settings
	v.NeedsMigration = !exists
	v.Paused = config["automaticTasksPaused"] != false
	server.Respond(w, 200, "success", v)
}
func (s *Service) bansHTTP(w http.ResponseWriter, r *http.Request) {
	rows := s.records.Load()
	if r.URL.Query().Get("view") == "released" {
		rows = s.history.Load()
	}
	result := []Ban{}
	query := strings.ToLower(r.URL.Query().Get("q"))
	if rows != nil {
		for _, b := range *rows {
			if query == "" || strings.Contains(strings.ToLower(b.IP+" "+b.Client+" "+b.Reason), query) {
				result = append(result, b)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LastSeen > result[j].LastSeen })
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	page = min(page, max(1, (len(result)+99)/100))
	start := min((page-1)*100, len(result))
	end := min(start+100, len(result))
	server.Respond(w, 200, "success", map[string]any{"rows": result[start:end], "total": len(result), "page": page})
}
func (s *Service) actionHTTP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action string `json:"action"`
		IP     string `json:"ip"`
		Text   string `json:"text"`
		Name   string `json:"name"`
		Rules  []Rule `json:"rules"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		server.Respond(w, 400, "节点过滤请求格式错误", nil)
		return
	}
	if input.Action == "import" {
		rules, err := ImportRules(input.Text)
		if err != nil {
			server.Respond(w, 400, err.Error(), nil)
			return
		}
		server.Respond(w, 200, "导入后请核对并保存规则", rules)
		return
	}
	if input.Action == "preview" {
		if len(input.Rules) > 100 {
			server.Respond(w, 400, "最多100条规则", nil)
			return
		}
		hits := []string{}
		for _, rule := range input.Rules {
			m, err := compileRule(rule)
			if err != nil {
				server.Respond(w, 400, err.Error(), nil)
				return
			}
			if m.matches(input.Name) {
				hits = append(hits, rule.ID)
			}
		}
		server.Respond(w, 200, "success", hits)
		return
	}
	if !has([]string{"refresh", "apply", "scan", "release", "allow", "ban"}, input.Action) {
		server.Respond(w, 400, "不支持的节点过滤操作", nil)
		return
	}
	err := s.work(r.Context(), input.Action, input.IP)
	if err != nil {
		server.Respond(w, 502, err.Error(), s.Status())
		return
	}
	s.statusHTTP(w, r)
}
func (s *Service) feedHTTP(w http.ResponseWriter, r *http.Request) {
	// 打开快照仅持有短读锁，RPC更新期间也能回拉；令牌与快照原子读取。
	s.feedMu.RLock()
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(s.token)) != 1 {
		s.feedMu.RUnlock()
		http.Error(w, "名单访问凭据无效", http.StatusUnauthorized)
		return
	}
	snap := s.feed
	file, err := os.Open(snap.Path)
	s.feedMu.RUnlock()
	if err != nil {
		http.Error(w, "名单尚未准备完成", http.StatusServiceUnavailable)
		return
	}
	defer file.Close()
	_, err = file.Stat()
	if err != nil {
		http.Error(w, "读取名单失败", 500)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", `"`+snap.Revision+`"`)
	w.Header().Set("X-MyAnimeGo-Revision", snap.Revision)
	http.ServeContent(w, r, "ipfilter.dat", time.Time{}, file)
}
