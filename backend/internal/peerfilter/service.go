package peerfilter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/transmission"
)

type Store interface {
	GetConfig(context.Context) (map[string]any, error)
	UpdateConfigFields(context.Context, map[string]any) error
	LoadPeerFilterState(context.Context) (json.RawMessage, error)
	SavePeerFilterState(context.Context, json.RawMessage) error
}
type Options struct {
	Store      Store
	Directory  string
	Sources    []Source
	HTTPClient *http.Client
	Now        func() time.Time
	OnError    func(error)
	Guard      func(context.Context) (func(), error)
}
type sourceState struct {
	File        string `json:"file"`
	ETag        string `json:"etag"`
	Modified    string `json:"modified"`
	CheckedAt   int64  `json:"checkedAt"`
	SucceededAt int64  `json:"succeededAt"`
	Count       int    `json:"count"`
	Error       string `json:"error"`
}
type Ban struct {
	IP           string   `json:"ip"`
	Client       string   `json:"client"`
	RuleID       string   `json:"ruleId"`
	Reason       string   `json:"reason"`
	FirstSeen    int64    `json:"firstSeen"`
	LastSeen     int64    `json:"lastSeen"`
	ExpiresAt    int64    `json:"expiresAt"`
	ReleasedAt   int64    `json:"releasedAt,omitempty"`
	Sources      []string `json:"sources"`
	StillBlocked bool     `json:"stillBlocked"`
}
type state struct {
	RangeCount    int                    `json:"rangeCount"`
	Token         string                 `json:"token"`
	Sources       map[string]sourceState `json:"sources"`
	Bans          map[string]Ban         `json:"bans"`
	History       []Ban                  `json:"history"`
	Revision      string                 `json:"revision"`
	Applied       string                 `json:"applied"`
	AppliedAt     int64                  `json:"appliedAt"`
	LastRefresh   int64                  `json:"lastRefresh"`
	LastScan      int64                  `json:"lastScan"`
	ExpectedURL   string                 `json:"expectedUrl"`
	RemoteVersion string                 `json:"remoteVersion"`
	RemoteSize    int                    `json:"remoteSize"`
	RemoteURL     string                 `json:"remoteUrl"`
	RemoteEnabled bool                   `json:"remoteEnabled"`
	Connection    string                 `json:"connection"`
	Managed       bool                   `json:"managed"`
	Conflict      bool                   `json:"conflict"`
	Error         string                 `json:"error"`
}
type SourceStatus struct {
	Source
	Enabled     bool   `json:"enabled"`
	Ready       bool   `json:"ready"`
	Count       int    `json:"count"`
	CheckedAt   int64  `json:"checkedAt"`
	SucceededAt int64  `json:"succeededAt"`
	Error       string `json:"error"`
}
type Status struct {
	Settings        Settings       `json:"settings"`
	NeedsMigration  bool           `json:"needsMigration"`
	LegacyEnabled   bool           `json:"legacyEnabled"`
	Paused          bool           `json:"paused"`
	Sources         []SourceStatus `json:"sources"`
	DynamicCount    int            `json:"dynamicCount"`
	CapacityReached bool           `json:"capacityReached"`
	LastScan        int64          `json:"lastScan"`
	LastRefresh     int64          `json:"lastRefresh"`
	Revision        string         `json:"revision"`
	Applied         string         `json:"applied"`
	AppliedAt       int64          `json:"appliedAt"`
	RangeCount      int            `json:"rangeCount"`
	Version         string         `json:"version"`
	RemoteSize      int            `json:"remoteSize"`
	Managed         bool           `json:"managed"`
	Conflict        bool           `json:"conflict"`
	Error           string         `json:"error"`
	Busy            bool           `json:"busy"`
	Operation       string         `json:"operation"`
}
type snapshot struct {
	Path     string
	Revision string
	Count    int
}
type Service struct {
	settingsSignature  string
	nextExpiry         int64
	lastScanAttempt    time.Time
	reloadRequested    atomic.Bool
	options            Options
	store              Store
	dir                string
	sources            []Source
	state              state
	op                 sync.Mutex
	view               atomic.Pointer[Status]
	records            atomic.Pointer[[]Ban]
	history            atomic.Pointer[[]Ban]
	feedMu             sync.RWMutex
	feed               snapshot
	token              string
	cache              map[string][]Range
	compiled           []matcher
	rulesSignature     string
	mergeSignature     string
	dirty              bool
	obsolete           []string
	client             *transmission.Client
	connection         [3]string
	lastRemoteCheck    time.Time
	lastApplyAttempt   time.Time
	lastRefreshAttempt time.Time
	wake               chan struct{}
	cancelMu           sync.Mutex
	autoCancel         context.CancelFunc
}

func New(ctx context.Context, o Options) (*Service, error) {
	if o.Store == nil || o.Directory == "" {
		return nil, errors.New("节点过滤需要存储和快照目录")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if o.Sources == nil {
		o.Sources = DefaultSources
	}
	if err := os.MkdirAll(o.Directory, 0700); err != nil {
		return nil, err
	}
	s := &Service{options: o, store: o.Store, dir: o.Directory, sources: o.Sources, cache: map[string][]Range{}, wake: make(chan struct{}, 1)}
	if err := s.loadState(ctx); err != nil {
		return nil, err
	}
	config, err := o.Store.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	settings, exists, err := ReadSettings(config)
	if err != nil {
		return nil, err
	}
	s.publishView(settings, exists, config, "")
	return s, nil
}
func (s *Service) loadState(ctx context.Context) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	s.state = state{}
	s.cache = map[string][]Range{}
	s.compiled = nil
	s.rulesSignature = ""
	s.settingsSignature = ""
	s.mergeSignature = ""
	s.obsolete = nil
	s.dirty = false
	s.lastRemoteCheck = time.Time{}
	s.lastRefreshAttempt = time.Time{}
	s.lastApplyAttempt = time.Time{}
	raw, err := s.store.LoadPeerFilterState(ctx)
	if err != nil {
		return err
	}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &s.state); err != nil {
			return err
		}
	}
	if s.state.Sources == nil {
		s.state.Sources = map[string]sourceState{}
	}
	if s.state.Bans == nil {
		s.state.Bans = map[string]Ban{}
	}
	if s.state.Token == "" {
		var b [32]byte
		if _, err = rand.Read(b[:]); err != nil {
			return err
		}
		s.state.Token = hex.EncodeToString(b[:])
		if err = s.persist(ctx); err != nil {
			return err
		}
	}
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	s.token = s.state.Token
	s.feed = snapshot{}
	if s.state.Revision != "" {
		path := filepath.Join(s.dir, "feed-"+s.state.Revision+".dat")
		if _, err := os.Stat(path); err == nil {
			s.feed = snapshot{Path: path, Revision: s.state.Revision, Count: s.state.RangeCount}
		} else {
			s.state.Revision = ""
		}
	}
	return nil
}
func (s *Service) Reload() { s.reloadRequested.Store(true); s.Notify() }
func (s *Service) persist(ctx context.Context) error {
	raw, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	if err = s.store.SavePeerFilterState(ctx, raw); err != nil {
		return err
	}
	s.dirty = false
	for _, name := range s.obsolete {
		_ = os.Remove(filepath.Join(s.dir, name))
	}
	s.obsolete = nil
	return nil
}

// ConfigChanged 只在过滤设置或连接变化时中断采集，普通设置保存继续沿用当前任务。
func (s *Service) ConfigChanged(updates map[string]any) {
	for _, key := range []string{ConfigKey, "downloadToolHost", "downloadToolUsername", "downloadToolPassword"} {
		if _, changed := updates[key]; changed {
			s.Notify()
			return
		}
	}
}
func (s *Service) Notify() {
	s.cancelMu.Lock()
	if s.autoCancel != nil {
		s.autoCancel()
	}
	s.cancelMu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) Run(ctx context.Context) {
	reported := ""
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-timer.C:
		}
		work, cancel := context.WithCancel(ctx)
		s.cancelMu.Lock()
		s.autoCancel = cancel
		s.cancelMu.Unlock()
		err := s.work(work, "automatic", "")
		cancel()
		s.cancelMu.Lock()
		s.autoCancel = nil
		s.cancelMu.Unlock()
		if err != nil && !errors.Is(err, context.Canceled) && s.options.OnError != nil {
			if err.Error() != reported {
				s.options.OnError(err)
				reported = err.Error()
			}
		} else if err == nil {
			reported = ""
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(time.Second)
	}
}
func (s *Service) Status() Status {
	v := s.view.Load()
	if v == nil {
		return Status{}
	}
	return *v
}
func (s *Service) publishView(settings Settings, exists bool, config map[string]any, operation string) {
	v := &Status{Settings: settings, NeedsMigration: !exists, Paused: config["automaticTasksPaused"] != false, DynamicCount: len(s.state.Bans), CapacityReached: len(s.state.Bans) >= settings.MaxBans, LastScan: s.state.LastScan, LastRefresh: s.state.LastRefresh, Revision: s.state.Revision, Applied: s.state.Applied, AppliedAt: s.state.AppliedAt, Version: s.state.RemoteVersion, RemoteSize: s.state.RemoteSize, Managed: s.state.Managed, Conflict: s.state.Conflict, Error: s.state.Error, Busy: operation != "", Operation: operation, Sources: []SourceStatus{}}
	if old, ok := config["transmissionBlocklist"].(map[string]any); ok {
		v.LegacyEnabled = old["enabled"] == true
	}
	s.feedMu.RLock()
	v.RangeCount = s.feed.Count
	s.feedMu.RUnlock()
	for _, src := range s.sources {
		m := s.state.Sources[src.ID]
		enabled := has(settings.SourceIDs, src.ID)
		v.Sources = append(v.Sources, SourceStatus{Source: src, Enabled: enabled, Ready: m.File != "", Count: m.Count, CheckedAt: m.CheckedAt, SucceededAt: m.SucceededAt, Error: m.Error})
	}
	s.nextExpiry = 0
	allowed := []Range{}
	for _, line := range settings.Allow {
		if r, err := parseRange(line); err == nil {
			allowed = append(allowed, r)
		}
	}
	allowed = normalize(allowed)
	decorate := func(b Ban) Ban {
		b.Sources = []string{}
		a, _ := address(b.IP)
		for _, src := range s.sources {
			if has(settings.SourceIDs, src.ID) && contains(s.cache[src.ID], a) {
				b.Sources = append(b.Sources, src.Name)
			}
		}
		b.StillBlocked = len(b.Sources) > 0 && !contains(allowed, a)
		return b
	}
	rows := make([]Ban, 0, len(s.state.Bans))
	for _, b := range s.state.Bans {
		if s.nextExpiry == 0 || b.ExpiresAt < s.nextExpiry {
			s.nextExpiry = b.ExpiresAt
		}
		rows = append(rows, decorate(b))
	}
	history := make([]Ban, 0, len(s.state.History))
	for _, b := range s.state.History {
		history = append(history, decorate(b))
	}
	s.records.Store(&rows)
	s.history.Store(&history)
	s.view.Store(v)
}
func has(rows []string, id string) bool {
	for _, v := range rows {
		if v == id {
			return true
		}
	}
	return false
}

func (s *Service) work(ctx context.Context, action, ip string) (result error) {
	if !s.op.TryLock() {
		if action == "automatic" {
			return nil
		}
		return errors.New("节点过滤正在处理上一项操作，请稍后重试")
	}
	defer s.op.Unlock()
	if s.options.Guard != nil {
		release, err := s.options.Guard(ctx)
		if err != nil {
			return err
		}
		defer release()
	}
	if s.reloadRequested.Swap(false) {
		if err := s.loadState(ctx); err != nil {
			s.reloadRequested.Store(true)
			return err
		}
	}
	config, err := s.store.GetConfig(ctx)
	if err != nil {
		return err
	}
	settings, exists, err := ReadSettings(config)
	if err != nil {
		return err
	}
	now := s.options.Now()
	signatureData, _ := json.Marshal([]any{settings, exists, config["automaticTasksPaused"], config["downloadToolHost"], config["downloadToolUsername"], config["downloadToolPassword"]})
	signature := string(signatureData)
	settingsChanged := signature != s.settingsSignature
	if action == "automatic" && signature == s.settingsSignature && !s.dirty {
		paused := config["automaticTasksPaused"] != false
		if !exists || !settings.Enabled && !s.state.Managed || paused && settings.Enabled {
			return nil
		}
		scanDue := settings.Identify && now.Sub(s.lastScanAttempt) >= time.Duration(settings.ScanSeconds)*time.Second
		sourceDue := (s.state.LastRefresh == 0 || !s.ready(settings) || now.UnixMilli()-s.state.LastRefresh >= int64(time.Duration(settings.RefreshMinutes)*time.Minute/time.Millisecond)) && now.Sub(s.lastRefreshAttempt) >= time.Minute
		expiryDue := s.nextExpiry > 0 && now.UnixMilli() >= s.nextExpiry
		remoteDue := now.Sub(s.lastRemoteCheck) >= time.Minute && now.Sub(s.lastApplyAttempt) >= 15*time.Second
		pending := s.state.Revision != s.state.Applied && now.Sub(s.lastApplyAttempt) >= 15*time.Second
		if !scanDue && !sourceDue && !expiryDue && !remoteDue && !pending {
			return nil
		}
	}
	if settingsChanged {
		s.lastRefreshAttempt = time.Time{}
		s.lastApplyAttempt = time.Time{}
		s.lastScanAttempt = time.Time{}
	}
	s.settingsSignature = signature
	s.publishView(settings, exists, config, action)
	defer func() {
		if result != nil && !errors.Is(result, context.Canceled) {
			if s.state.Error != result.Error() {
				s.state.Error = result.Error()
				s.dirty = true
			}
		}
		if s.dirty {
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			result = errors.Join(result, s.persist(persistCtx))
		}
		s.publishView(settings, exists, config, "")
	}()
	if !exists {
		if action != "automatic" {
			return errors.New("请先保存节点过滤设置，再执行操作")
		}
		return nil
	}
	if err = Validate(settings); err != nil {
		return err
	}
	manual := action != "automatic"
	if !settings.Enabled {
		if s.state.Managed {
			if err = s.disable(ctx, config); err != nil {
				return err
			}
			s.dirty = true
			return nil
		}
		return nil
	}
	if !manual && config["automaticTasksPaused"] != false {
		return nil
	}
	if action == "ban" {
		a, err := address(ip)
		if err != nil || !a.IsGlobalUnicast() || a.IsPrivate() {
			return errors.New("请填写公网节点IP")
		}
		if len(s.state.Bans) >= settings.MaxBans {
			return errors.New("封禁记录已达容量上限")
		}
		ip = a.String()
		s.state.Bans[ip] = Ban{IP: ip, RuleID: "manual", Reason: "手动封禁", FirstSeen: now.UnixMilli(), LastSeen: now.UnixMilli(), ExpiresAt: now.Add(time.Duration(settings.BanHours) * time.Hour).UnixMilli()}
		s.dirty = true
	}
	if action == "release" || action == "allow" {
		a, err := address(ip)
		if err != nil {
			return errors.New("解除地址无效")
		}
		ip = a.String()
		if action == "allow" {
			if !has(settings.Allow, ip) {
				settings.Allow = append(settings.Allow, ip)
				if err = Validate(settings); err != nil {
					return err
				}
				if err = s.store.UpdateConfigFields(ctx, map[string]any{ConfigKey: settings}); err != nil {
					return err
				}
				config[ConfigKey] = settings
			}
		}
		if b, ok := s.state.Bans[ip]; ok {
			s.archive(b, now)
			delete(s.state.Bans, ip)
			s.dirty = true
		}
	}
	if err = s.prepare(settings, now); err != nil {
		return err
	}
	refresh := action == "refresh" || s.state.LastRefresh == 0 || !s.ready(settings) || now.UnixMilli()-s.state.LastRefresh >= int64(time.Duration(settings.RefreshMinutes)*time.Minute/time.Millisecond)
	if !manual && now.Sub(s.lastRefreshAttempt) < time.Minute {
		refresh = false
	}
	var sourceErr error
	if refresh {
		s.lastRefreshAttempt = now
		sourceErr = s.refreshSources(ctx, settings)
		s.dirty = true
	}
	if sourceErr != nil {
		s.state.Error = sourceErr.Error()
	}
	if !s.ready(settings) {
		if sourceErr != nil {
			return sourceErr
		}
		return errors.New("部分启用的规则尚无有效快照，请先更新上游规则")
	}
	if settings.Identify && (action == "scan" || action == "refresh" || action == "automatic" && now.Sub(s.lastScanAttempt) >= time.Duration(settings.ScanSeconds)*time.Second) {
		s.lastScanAttempt = now
		if err = s.scan(ctx, config, settings, now); err != nil {
			return err
		}
	}
	if err = s.merge(settings); err != nil {
		return err
	}
	if manual || settingsChanged || now.Sub(s.lastRemoteCheck) >= time.Minute || s.state.Applied != s.state.Revision {
		if !manual && now.Sub(s.lastApplyAttempt) < 15*time.Second {
			return nil
		}
		s.lastApplyAttempt = now
		if err = s.apply(ctx, config, settings, manual); err != nil {
			return err
		}
		s.dirty = true
	}
	if sourceErr == nil && s.state.Error != "" {
		s.state.Error = ""
		s.dirty = true
	}
	return sourceErr
}
func (s *Service) archive(b Ban, now time.Time) {
	b.ReleasedAt = now.UnixMilli()
	s.state.History = append(s.state.History, b)
	if len(s.state.History) > 500 {
		s.state.History = s.state.History[len(s.state.History)-500:]
	}
}
