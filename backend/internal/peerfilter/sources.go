package peerfilter

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *Service) prepare(settings Settings, now time.Time) error {
	data, _ := json.Marshal(settings.Rules)
	signature := string(data)
	if signature != s.rulesSignature {
		s.compiled = nil
		for _, r := range settings.Rules {
			m, e := compileRule(r)
			if e != nil {
				return e
			}
			s.compiled = append(s.compiled, m)
		}
		s.rulesSignature = signature
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
	for ip, b := range s.state.Bans {
		a, _ := address(ip)
		keep := b.RuleID == "manual"
		for _, m := range s.compiled {
			if m.rule.ID == b.RuleID && m.matches(b.Client) {
				keep = true
				reason := m.rule.Method + " · " + m.rule.Content
				if b.Reason != reason {
					b.Reason = reason
					s.state.Bans[ip] = b
					s.dirty = true
				}
			}
		}
		if (!settings.Identify && b.RuleID != "manual") || !keep || b.ExpiresAt <= now.UnixMilli() || contains(allowed, a) {
			s.archive(b, now)
			delete(s.state.Bans, ip)
			s.dirty = true
		}
	}
	for _, id := range settings.SourceIDs {
		if _, ok := s.cache[id]; ok {
			continue
		}
		meta := s.state.Sources[id]
		if meta.File == "" {
			continue
		}
		if filepath.Base(meta.File) != meta.File {
			return errors.New("规则快照路径无效")
		}
		raw, e := os.ReadFile(filepath.Join(s.dir, meta.File))
		var rows []Range
		if e == nil {
			e = json.Unmarshal(raw, &rows)
		}
		if e != nil {
			meta.File = ""
			meta.Error = "规则快照缺失，请重新更新"
			s.state.Sources[id] = meta
			s.dirty = true
			continue
		}
		s.cache[id] = rows
	}
	return nil
}
func (s *Service) ready(settings Settings) bool {
	for _, id := range settings.SourceIDs {
		if s.state.Sources[id].File == "" {
			return false
		}
	}
	return true
}
func (s *Service) refreshSources(ctx context.Context, settings Settings) error {
	var errs []error
	for _, src := range s.sources {
		if !has(settings.SourceIDs, src.ID) {
			continue
		}
		meta := s.state.Sources[src.ID]
		meta.CheckedAt = s.options.Now().UnixMilli()
		if err := s.fetchSource(ctx, src, &meta); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			meta.Error = err.Error()
			errs = append(errs, fmt.Errorf("%s：%w", src.Name, err))
		} else {
			meta.Error = ""
		}
		s.state.Sources[src.ID] = meta
		s.dirty = true
	}
	s.state.LastRefresh = s.options.Now().UnixMilli()
	return errors.Join(errs...)
}
func (s *Service) fetchSource(ctx context.Context, src Source, meta *sourceState) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return err
	}
	if meta.File != "" {
		req.Header.Set("If-None-Match", meta.ETag)
		req.Header.Set("If-Modified-Since", meta.Modified)
	}
	resp, err := s.options.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		if meta.File == "" {
			return errors.New("来源返回304但本地没有快照")
		}
		meta.SucceededAt = s.options.Now().UnixMilli()
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	limited := &io.LimitedReader{R: resp.Body, N: 64<<20 + 1}
	rows, err := ParseRanges(limited)
	if err != nil {
		return err
	}
	if limited.N <= 0 {
		return errors.New("规则文件超过64MiB")
	}
	if limited.N == 64<<20+1 {
		return errors.New("来源返回空内容，继续使用上一版")
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	name := fmt.Sprintf("source-%s-%x.json", src.ID, digest[:8])
	if err = atomicWrite(s.dir, name, data); err != nil {
		return err
	}
	old := meta.File
	meta.File = name
	meta.Count = len(rows)
	meta.ETag = resp.Header.Get("ETag")
	meta.Modified = resp.Header.Get("Last-Modified")
	meta.SucceededAt = s.options.Now().UnixMilli()
	s.cache[src.ID] = rows
	if old != "" && old != name {
		s.obsolete = append(s.obsolete, old)
	}
	return nil
}
func atomicWrite(dir, name string, data []byte) error {
	f, e := os.CreateTemp(dir, ".pending-*")
	if e != nil {
		return e
	}
	path := f.Name()
	defer os.Remove(path)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return e
	}
	return os.Rename(path, filepath.Join(dir, name))
}
func (s *Service) merge(settings Settings) error {
	parts := append([]string(nil), settings.Allow...)
	for _, id := range settings.SourceIDs {
		parts = append(parts, "source:"+id+":"+s.state.Sources[id].File)
	}
	for ip := range s.state.Bans {
		parts = append(parts, "ip:"+ip)
	}
	sort.Strings(parts)
	signature := strings.Join(parts, "\n")
	if s.mergeSignature == signature && s.state.Revision != "" {
		return nil
	}
	rows := []Range{}
	for _, id := range settings.SourceIDs {
		rows = append(rows, s.cache[id]...)
	}
	for ip := range s.state.Bans {
		a, e := address(ip)
		if e == nil {
			rows = append(rows, Range{a, a})
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
	rows = subtract(normalize(rows), normalize(allowed))
	f, e := os.CreateTemp(s.dir, ".feed-*")
	if e != nil {
		return e
	}
	path := f.Name()
	defer os.Remove(path)
	hash := sha256.New()
	writer := bufio.NewWriter(io.MultiWriter(f, hash))
	fmt.Fprintln(writer, "# MyAnimeGo: aggregated and converted IP ranges")
	if has(settings.SourceIDs, "btn") {
		fmt.Fprintln(writer, "# PBH-BTN/BTN-Collected-Rules https://github.com/PBH-BTN/BTN-Collected-Rules · CC-BY-4.0 https://creativecommons.org/licenses/by/4.0/")
	}
	for _, src := range s.sources {
		if has(settings.SourceIDs, src.ID) && src.ID != "btn" {
			fmt.Fprintf(writer, "# Source: %s %s\n", src.Name, src.URL)
		}
	}
	for _, r := range rows {
		if _, e = fmt.Fprintf(writer, "anirss:%s-%s\n", r.First, r.Last); e != nil {
			break
		}
	}
	// 空名单仍返回有效文本，Transmission 可据此解除最后一条封禁。
	if len(rows) == 0 {
		_, e = writer.WriteString("# MyAnimeGo: no blocked addresses\n")
	}
	if e == nil {
		e = writer.Flush()
	}
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return e
	}
	revision := hex.EncodeToString(hash.Sum(nil))
	name := "feed-" + revision + ".dat"
	dest := filepath.Join(s.dir, name)
	if e = os.Rename(path, dest); e != nil {
		return e
	}
	s.feedMu.Lock()
	old := s.feed
	s.feed = snapshot{Path: dest, Revision: revision, Count: len(rows)}
	s.feedMu.Unlock()
	if old.Path != "" && old.Path != dest {
		s.obsolete = append(s.obsolete, filepath.Base(old.Path))
	}
	if s.state.Revision != revision {
		s.state.Revision = revision
		s.dirty = true
	}
	s.state.RangeCount = len(rows)
	s.mergeSignature = signature
	return nil
}
func trimURL(v string) string { return strings.TrimRight(strings.TrimSpace(v), "/") }
