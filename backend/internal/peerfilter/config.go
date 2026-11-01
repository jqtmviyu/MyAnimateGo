// Package peerfilter 管理公开地址规则、本地节点识别及 Transmission 名单下发。
package peerfilter

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const ConfigKey = "peerFilter"

type Source struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Attribution string `json:"attribution"`
}

var DefaultSources = []Source{
	{"btn", "PBH / BTN 公共规则", "https://raw.githubusercontent.com/PBH-BTN/BTN-Collected-Rules/main/combine/all.txt", "PBH-BTN / BTN-Collected-Rules · CC-BY 4.0 · 已转换地址格式"},
	{"chinanet", "中国电信扩展地址集", "https://raw.githubusercontent.com/zealic/autorosvpn/master/isp/cn/route-isp-chinanet.txt", "zealic/autorosvpn"},
	{"cmcc", "中国移动扩展地址集", "https://raw.githubusercontent.com/zealic/autorosvpn/master/isp/cn/route-isp-cn-cmcc.txt", "zealic/autorosvpn"},
	{"cncgroup", "中国联通 / 铁通扩展地址集", "https://raw.githubusercontent.com/zealic/autorosvpn/master/isp/cn/route-isp-cncgroup.txt", "zealic/autorosvpn"},
}

type Rule struct {
	ID         string `json:"id"`
	Enabled    bool   `json:"enabled"`
	Method     string `json:"method"`
	Content    string `json:"content"`
	IgnoreCase bool   `json:"ignoreCase"`
	Source     string `json:"source"`
	Original   string `json:"original,omitempty"`
}
type Settings struct {
	Enabled        bool     `json:"enabled"`
	BaseURL        string   `json:"baseUrl"`
	SourceIDs      []string `json:"sources"`
	RefreshMinutes int      `json:"refreshMinutes"`
	Identify       bool     `json:"identify"`
	ScanSeconds    int      `json:"scanSeconds"`
	BanHours       int      `json:"banHours"`
	MaxBans        int      `json:"maxBans"`
	Rules          []Rule   `json:"rules"`
	Allow          []string `json:"allow"`
}

func Defaults(config map[string]any) Settings {
	s := Settings{SourceIDs: []string{"btn", "chinanet", "cmcc", "cncgroup"}, RefreshMinutes: 1440, Identify: true, ScanSeconds: 15, BanHours: 24, MaxBans: 10000, Allow: []string{}, Rules: []Rule{
		{ID: "xunlei", Enabled: true, Method: "CONTAINS", Content: "Xunlei", IgnoreCase: true, Source: "内置"}, {ID: "thunder", Enabled: true, Method: "STARTS_WITH", Content: "Thunder", IgnoreCase: true, Source: "内置"},
		{ID: "hp-torrent", Enabled: true, Method: "CONTAINS", Content: "hp/torrent", IgnoreCase: true, Source: "内置"}, {ID: "dt-torrent", Enabled: true, Method: "CONTAINS", Content: "dt/torrent", IgnoreCase: true, Source: "内置"},
	}}
	if old, ok := config["transmissionBlocklist"].(map[string]any); ok {
		s.Enabled = old["enabled"] == true
	}
	return s
}
func ReadSettings(config map[string]any) (Settings, bool, error) {
	s := Defaults(config)
	v, exists := config[ConfigKey]
	if !exists {
		return s, false, nil
	}
	b, err := json.Marshal(v)
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if s.SourceIDs == nil {
		s.SourceIDs = []string{}
	}
	if s.Rules == nil {
		s.Rules = []Rule{}
	}
	if s.Allow == nil {
		s.Allow = []string{}
	}
	return s, true, err
}
func ValidateConfig(config map[string]any) error {
	s, exists, err := ReadSettings(config)
	if err != nil {
		return errors.New("节点过滤配置格式错误")
	}
	if !exists {
		return nil
	}
	return Validate(s)
}
func Validate(s Settings) error {
	if s.RefreshMinutes < 1 || s.RefreshMinutes > 10080 || s.ScanSeconds < 2 || s.ScanSeconds > 3600 || s.BanHours < 1 || s.BanHours > 720 || s.MaxBans < 1 || s.MaxBans > 100000 {
		return errors.New("规则更新间隔1至10080分钟，扫描间隔2至3600秒，封禁1至720小时，容量1至100000条")
	}
	if s.Enabled || s.BaseURL != "" {
		u, err := url.Parse(s.BaseURL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("请填写 Transmission 可访问的 MyAnimeGo HTTP(S) 地址，地址中不包含凭据、查询或片段")
		}
	}
	seen := map[string]bool{}
	for _, id := range s.SourceIDs {
		found := false
		for _, src := range DefaultSources {
			found = found || src.ID == id
		}
		if !found || seen[id] {
			return errors.New("上游规则来源无效或重复")
		}
		seen[id] = true
	}
	if len(s.Rules) > 100 || len(s.Allow) > 1000 {
		return errors.New("最多100条名称规则及1000条允许地址")
	}
	seen = map[string]bool{}
	for _, r := range s.Rules {
		if r.ID == "" || seen[r.ID] {
			return errors.New("名称规则标识为空或重复")
		}
		seen[r.ID] = true
		if _, err := compileRule(r); err != nil {
			return err
		}
	}
	for _, line := range s.Allow {
		if _, err := parseRange(line); err != nil {
			return fmt.Errorf("允许地址 %q 无效：%w", line, err)
		}
	}
	return nil
}

type matcher struct {
	rule    Rule
	content string
	re      *regexp.Regexp
}

func compileRule(r Rule) (matcher, error) {
	m := matcher{rule: r, content: r.Content}
	if len(r.Content) == 0 || len(r.Content) > 2048 {
		return m, errors.New("名称匹配内容须为1至2048字节")
	}
	if r.IgnoreCase {
		m.content = strings.ToLower(m.content)
	}
	switch r.Method {
	case "EQUALS", "CONTAINS", "STARTS_WITH":
	case "REGEX":
		p := r.Content
		if r.IgnoreCase {
			p = "(?i)" + p
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return m, fmt.Errorf("正则不受 Go/RE2 支持：%w", err)
		}
		m.re = re
	default:
		return m, fmt.Errorf("不支持的名称匹配方法：%s", r.Method)
	}
	return m, nil
}
func (m matcher) matches(name string) bool {
	if !m.rule.Enabled || name == "" {
		return false
	}
	if m.re != nil {
		return m.re.MatchString(name)
	}
	if m.rule.IgnoreCase {
		name = strings.ToLower(name)
	}
	switch m.rule.Method {
	case "EQUALS":
		return name == m.content
	case "CONTAINS":
		return strings.Contains(name, m.content)
	case "STARTS_WITH":
		return strings.HasPrefix(name, m.content)
	}
	return false
}

// ImportRules 接收 PBH profile 中的 JSON matcher 行，只导入可表达的名称规则。
func ImportRules(input string) ([]Rule, error) {
	var rules []Rule
	for _, line := range strings.Split(input, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if len(line) > 1 && ((line[0] == '\'' && line[len(line)-1] == '\'') || (line[0] == '"' && line[len(line)-1] == '"')) {
			if line[0] == '\'' {
				line = strings.ReplaceAll(line[1:len(line)-1], "''", "'")
			} else {
				var value string
				if err := json.Unmarshal([]byte(line), &value); err != nil {
					return nil, err
				}
				line = value
			}
		}
		var v struct {
			Method  string `json:"method"`
			Content string `json:"content"`
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			return nil, fmt.Errorf("第%d条不是有效的 PBH JSON matcher：%w", len(rules)+1, err)
		}
		for key := range fields {
			if key != "method" && key != "content" {
				return nil, fmt.Errorf("PBH字段 %s 暂不支持，请仅导入简单的method/content名称规则", key)
			}
		}
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, err
		}
		content := v.Content
		if v.Method == "REGEX" {
			content = `\A(?:` + content + `)\z`
		}
		r := Rule{ID: fmt.Sprintf("pbh-%d", len(rules)+1), Enabled: false, Method: v.Method, Content: content, IgnoreCase: v.Method != "REGEX", Source: "PBH 导入", Original: v.Content}
		if _, err := compileRule(r); err != nil {
			return nil, err
		}
		rules = append(rules, r)
		if len(rules) > 100 {
			return nil, errors.New("一次最多导入100条规则")
		}
	}
	if len(rules) == 0 {
		return nil, errors.New("没有可导入的名称规则")
	}
	return rules, nil
}
