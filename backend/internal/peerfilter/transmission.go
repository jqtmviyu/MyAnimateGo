package peerfilter

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/transmission"
)

func text(v any) string { s, _ := v.(string); return s }
func (s *Service) rpc(config map[string]any) *transmission.Client {
	c := [3]string{strings.TrimSpace(text(config["downloadToolHost"])), text(config["downloadToolUsername"]), text(config["downloadToolPassword"])}
	if s.client == nil || s.connection != c {
		s.client = transmission.New(transmission.Config{URL: c[0], Username: c[1], Password: c[2], HTTPClient: &http.Client{Timeout: 2 * time.Minute, Transport: s.options.HTTPClient.Transport}})
		s.connection = c
		s.lastRemoteCheck = time.Time{}
	}
	return s.client
}
func (s *Service) connectionKey() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(s.connection[:], "\n"))))
}

var versionPattern = regexp.MustCompile(`^(\d+)\.(\d+)`)

func supportedVersion(v string) bool {
	p := versionPattern.FindStringSubmatch(v)
	if len(p) != 3 {
		return false
	}
	major, _ := strconv.Atoi(p[1])
	minor, _ := strconv.Atoi(p[2])
	return major > 4 || major == 4 && minor >= 1
}

type remoteSession struct {
	Version string `json:"version"`
	URL     string `json:"blocklist-url"`
	Enabled bool   `json:"blocklist-enabled"`
	Size    int    `json:"blocklist-size"`
}

func (s *Service) remote(ctx context.Context, client *transmission.Client) (remoteSession, error) {
	var session remoteSession
	data, err := client.Call(ctx, "session-get", map[string]any{"fields": []string{"version", "blocklist-enabled", "blocklist-url", "blocklist-size"}})
	if err == nil {
		err = json.Unmarshal(data, &session)
	}
	if err == nil {
		s.state.RemoteVersion = session.Version
		s.state.RemoteURL = session.URL
		s.state.RemoteEnabled = session.Enabled
		s.state.RemoteSize = session.Size
		s.lastRemoteCheck = s.options.Now()
	}
	return session, err
}
func (s *Service) apply(ctx context.Context, config map[string]any, settings Settings, force bool) error {
	client := s.rpc(config)
	session, err := s.remote(ctx, client)
	if err != nil {
		return err
	}
	if !supportedVersion(session.Version) {
		return fmt.Errorf("节点过滤需要 Transmission 4.1 或更新版本，当前为 %s", session.Version)
	}
	expected := trimURL(settings.BaseURL) + "/api/peerFilterFeed?token=" + s.token
	connection := s.connectionKey()
	managed := s.state.Managed && s.state.Connection == connection
	if managed && !force && (session.URL != s.state.ExpectedURL || !session.Enabled) {
		s.state.Conflict = true
		s.dirty = true
		return errors.New("Transmission 黑名单配置已被其他程序修改，请点击重新应用")
	}
	s.state.Conflict = false
	if session.URL != expected || !session.Enabled {
		if _, err = client.Call(ctx, "session-set", map[string]any{"blocklist-enabled": true, "blocklist-url": expected}); err != nil {
			return err
		}
		s.dirty = true
	}
	if !force && managed && s.state.Applied == s.state.Revision && s.state.ExpectedURL == expected {
		return nil
	}
	s.state.Managed = true
	s.state.Connection = connection
	s.state.ExpectedURL = expected
	s.dirty = true
	data, err := client.Call(ctx, "blocklist-update", nil)
	if err != nil {
		return fmt.Errorf("Transmission 获取名单失败，请确认它能访问 MyAnimeGo 地址：%w", err)
	}
	var result struct {
		Size int `json:"blocklist-size"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return err
	}
	s.state.RemoteSize = result.Size
	s.state.Applied = s.state.Revision
	s.state.AppliedAt = s.options.Now().UnixMilli()
	s.state.RemoteEnabled = true
	s.state.RemoteURL = expected
	return nil
}
func (s *Service) disable(ctx context.Context, config map[string]any) error {
	client := s.rpc(config)
	session, err := s.remote(ctx, client)
	if err != nil {
		return err
	}
	if s.state.Connection == s.connectionKey() && session.URL == s.state.ExpectedURL {
		if _, err = client.Call(ctx, "session-set", map[string]any{"blocklist-enabled": false}); err != nil {
			return err
		}
	}
	s.state.Managed = false
	s.state.Conflict = false
	s.state.Applied = ""
	s.state.RemoteEnabled = false
	s.state.Error = ""
	return nil
}

type peer struct {
	Address string `json:"address"`
	Client  string `json:"clientName"`
}

func (s *Service) scan(ctx context.Context, config map[string]any, settings Settings, now time.Time) error {
	client := s.rpc(config)
	data, err := client.Call(ctx, "torrent-get", map[string]any{"fields": []string{"id", "status", "peersConnected"}})
	if err != nil {
		return err
	}
	var list struct {
		Torrents []struct {
			ID     int `json:"id"`
			Status int `json:"status"`
			Peers  int `json:"peersConnected"`
		} `json:"torrents"`
	}
	if err = json.Unmarshal(data, &list); err != nil {
		return err
	}
	ids := []int{}
	for _, t := range list.Torrents {
		if t.Peers > 0 && t.Status != 0 {
			ids = append(ids, t.ID)
		}
	}
	allowed := []Range{}
	for _, line := range settings.Allow {
		r, e := parseRange(line)
		if e != nil {
			return e
		}
		allowed = append(allowed, r)
	}
	allowed = normalize(allowed)
	seen := map[netip.Addr]bool{}
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		data, err = client.Call(ctx, "torrent-get", map[string]any{"ids": ids[start:end], "fields": []string{"id", "peers"}})
		if err != nil {
			return err
		}
		var detail struct {
			Torrents []struct {
				Peers []peer `json:"peers"`
			} `json:"torrents"`
		}
		if err = json.Unmarshal(data, &detail); err != nil {
			return err
		}
		for _, t := range detail.Torrents {
			for _, p := range t.Peers {
				a, e := address(p.Address)
				if e != nil || !a.IsGlobalUnicast() || a.IsPrivate() || contains(allowed, a) || seen[a] {
					continue
				}
				for _, m := range s.compiled {
					if !m.matches(p.Client) {
						continue
					}
					seen[a] = true
					ip := a.String()
					b, ok := s.state.Bans[ip]
					if !ok {
						if len(s.state.Bans) >= settings.MaxBans {
							break
						}
						b = Ban{IP: ip, FirstSeen: now.UnixMilli(), ExpiresAt: now.Add(time.Duration(settings.BanHours) * time.Hour).UnixMilli()}
					}
					b.Client = p.Client
					b.RuleID = m.rule.ID
					b.Reason = m.rule.Method + " · " + m.rule.Content
					b.LastSeen = now.UnixMilli()
					s.state.Bans[ip] = b
					s.dirty = true
					break
				}
			}
		}
	}
	s.state.LastScan = now.UnixMilli()
	return nil
}
