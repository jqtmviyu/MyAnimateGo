// Package migration 预览并导入旧版 ani-rss 数据，同时保留原始来源。
package migration

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

// Report 只包含脱敏的转换摘要，不包含配置值。
type Report struct {
	SourceVersion        string   `json:"sourceVersion"`
	DeclaredVersion      string   `json:"declaredVersion,omitempty"`
	ConfigFields         []string `json:"configFields"`
	SubscriptionCount    int      `json:"subscriptionCount"`
	UnknownFields        []string `json:"unknownFields"`
	Warnings             []string `json:"warnings"`
	PendingFields        []string `json:"pendingFields"`
	AutomaticTasksPaused bool     `json:"automaticTasksPaused"`
	IncludedPaths        []string `json:"includedPaths"`
	OmittedPaths         []string `json:"omittedPaths"`
	UnconvertedPaths     []string `json:"unconvertedPaths"`
}

// Preview 读取旧版目录或 ZIP，返回转换摘要且不写入数据。
func Preview(source string) (Report, error) {
	prepared, err := prepare(source)
	return prepared.report, err
}

type snapshot struct {
	report        Report
	config        map[string]any
	subscriptions []map[string]any
	attachments   []attachment
}

type attachment struct {
	path string
	data []byte
}

func prepare(source string) (snapshot, error) {
	files, err := readSource(source)
	if err != nil {
		return snapshot{}, err
	}
	version, configName, subsName, err := identify(files)
	if err != nil {
		return snapshot{}, err
	}
	var config map[string]any
	if err := json.Unmarshal(files[configName], &config); err != nil {
		return snapshot{}, fmt.Errorf("parse legacy config: %w", err)
	}
	var subscriptions []map[string]any
	if err := json.Unmarshal(files[subsName], &subscriptions); err != nil {
		return snapshot{}, fmt.Errorf("parse legacy subscriptions: %w", err)
	}
	attachments, included, omitted, unconverted, err := readAttachments(source)
	if err != nil {
		return snapshot{}, err
	}
	report := Report{SourceVersion: version, SubscriptionCount: len(subscriptions), AutomaticTasksPaused: true}
	if declared, ok := config["version"].(string); ok && regexp.MustCompile(`^v?\d+\.\d+\.\d+$`).MatchString(declared) {
		report.DeclaredVersion = declared
		if strings.TrimPrefix(declared, "v") != strings.TrimPrefix(version, "v") {
			report.Warnings = append(report.Warnings, "来源声明版本与格式基线不同；按字段格式转换，未知字段继续保留并报告")
		}
	}
	report.IncludedPaths = included
	report.OmittedPaths = omitted
	report.UnconvertedPaths = unconverted
	if len(unconverted) > 0 {
		report.Warnings = append(report.Warnings, "来源含未转换的文件或目录，请保留原始来源并在启用前核对报告中的未转换项")
	}
	knownConfig := configFields(version)
	for field := range config {
		report.ConfigFields = append(report.ConfigFields, field)
		if !knownConfig[field] {
			report.UnknownFields = append(report.UnknownFields, "config."+field)
		}
	}
	for i, item := range subscriptions {
		if _, ok := item["id"].(string); !ok || item["id"] == "" {
			return snapshot{}, fmt.Errorf("subscription %d has no stable string id", i)
		}
		known := subscriptionFields(version)
		for field := range item {
			if !known[field] {
				report.UnknownFields = append(report.UnknownFields, fmt.Sprintf("subscriptions[%d].%s", i, field))
			}
		}
		normalizeSubscription(item, version)
	}
	pending, conversionWarnings := normalizeConfig(config, version)
	report.PendingFields = append(report.PendingFields, pending...)
	report.Warnings = append(report.Warnings, conversionWarnings...)
	report.Warnings = append(report.Warnings, "RSS 扫描间隔按分钟解释并保留")
	if version == "v1.1.50" {
		report.Warnings = append(report.Warnings, "重命名扫描间隔从分钟转换为秒")
		report.Warnings = append(report.Warnings, "部分发布日期只有年月，已保留月份精度")
		report.Warnings = append(report.Warnings, "旧备用源没有字幕组标签，使用未知标签标记；偏移沿用订阅设置")
	} else if version == "v1.2.82" {
		report.Warnings = append(report.Warnings, "重命名扫描间隔以秒保留")
		report.Warnings = append(report.Warnings, "备用源列表映射到新字段，标签与偏移均保留")
	} else {
		report.Warnings = append(report.Warnings, "重命名扫描间隔以秒保留")
	}
	if len(attachments) > 0 {
		report.Warnings = append(report.Warnings, "附件先在数据库目录暂存；目标子目录跨文件系统时硬链接安装会失败并回滚")
		report.Warnings = append(report.Warnings, "SQLite 与文件系统不能组成跨系统事务；进程或系统崩溃可能留下未关联文件")
	}
	config["automaticTasksPaused"] = true
	sort.Strings(report.ConfigFields)
	sort.Strings(report.UnknownFields)
	sort.Strings(report.PendingFields)
	sort.Strings(report.IncludedPaths)
	sort.Strings(report.OmittedPaths)
	sort.Strings(report.UnconvertedPaths)
	sort.Strings(report.Warnings)
	// 报告随配置和订阅在同一事务安装，备份恢复后仍可重新查看。
	config["migrationReport"] = report
	return snapshot{report: report, config: config, subscriptions: subscriptions, attachments: attachments}, nil
}

// Import 转换并原子合并旧数据，且不修改原始目录或 ZIP。
func Import(ctx context.Context, store *storage.Store, source string) (Report, error) {
	prepared, err := prepare(source)
	if err != nil {
		return Report{}, err
	}
	if err := importWithAttachments(ctx, store, prepared); err != nil {
		return Report{}, err
	}
	return prepared.report, nil
}

func normalizeConfig(config map[string]any, version string) ([]string, []string) {
	var pending, warnings []string
	for _, key := range []string{"customJs", "customCss"} {
		if _, ok := config[key]; ok {
			delete(config, key)
			warnings = append(warnings, "已忽略停用的前端注入配置："+key)
		}
	}
	if version != "v3.2.52" {
		pending = append(pending, normalizeLegacyPaths(config)...)
		pending = append(pending, normalizeLegacyNotifications(config)...)
		if len(pending) > 0 {
			warnings = append(warnings, "旧配置含待确认项，已保留原字段并在 pendingFields 中列出")
		}
	}

	downloader, _ := config["downloadToolType"].(string)
	if downloader == "" {
		downloader, _ = config["download"].(string)
	}
	if downloader == "" || strings.EqualFold(strings.TrimSpace(downloader), "Transmission") {
		for _, field := range []struct{ source, target string }{{"host", "downloadToolHost"}, {"username", "downloadToolUsername"}, {"password", "downloadToolPassword"}} {
			if _, exists := config[field.target]; !exists {
				if value, found := config[field.source]; found {
					config[field.target] = value
				}
			}
		}
	} else {
		for _, key := range []string{"downloadToolHost", "downloadToolUsername", "downloadToolPassword"} {
			delete(config, key)
		}
		warnings = append(warnings, "已忽略其他下载器连接配置，请在设置中填写 Transmission 连接")
	}
	for _, key := range []string{"download", "host", "username", "password", "downloadToolType", "legacyDownloader", "qbUseDownloadPath", "qbContentLayout", "qbRenameTitle", "ratioLimit", "seedingTimeLimit", "inactiveSeedingTimeLimit", "priorityKeywordsEnable", "priorityKeywords", "autoTrackersUpdate", "trackersUpdateUrls", "scrape", "followDay", "bangumiIniEnabled", "tmdbImage", "procrastinating", "procrastinatingDay", "procrastinatingMasterOnly", "bgmTokenType", "bgmAppID", "bgmAppSecret", "bgmRefreshToken", "bgmRedirectUri", "bgmOAuthBaseURL", "githubToken", "autoUpdate", "autoStart", "version", "tmdbIdPlexMode"} {
		delete(config, key)
	}
	if _, ok := config["rssSleepMinutes"]; !ok {
		if value, exists := config["sleep"]; exists {
			config["rssSleepMinutes"] = value
		}
	}
	if _, ok := config["standbyRss"]; !ok {
		if value, exists := config["backRss"]; exists {
			config["standbyRss"] = value
		}
	}
	if version == "v1.1.50" {
		if minutes, ok := number(config["renameSleep"]); ok {
			config["renameSleepSeconds"] = minutes * 60
		}
	}
	return pending, warnings
}

// 旧版按首字母、月份或年份追加目录，最后再追加作品名和季目录。
func normalizeLegacyPaths(config map[string]any) []string {
	var pending []string
	enabled := func(key string) bool { value, _ := config[key].(bool); return value }
	suffix := ""
	switch {
	case enabled("acronym"):
		suffix = "/${letter}"
	case enabled("quarter"):
		suffix = "/${year}-${monthFormat}"
		if enabled("quarterMerge") {
			suffix = "/${year}-${legacyQuarterFormat}"
		}
	case enabled("yearStorage"):
		suffix = "/${year}"
	}
	season, _ := config["seasonName"].(string)
	seasonSuffix := map[string]string{"Season 1": "/Season ${season}", "S01": "/S${seasonFormat}"}[season]
	if season == "Season 01" {
		seasonSuffix = "/Season ${seasonFormat}"
	}
	if season == "S1" {
		seasonSuffix = "/S${season}"
	}

	for _, field := range []string{"downloadPath", "ovaDownloadPath"} {
		target := field + "Template"
		if value, _ := config[target].(string); strings.TrimSpace(value) != "" {
			continue
		}
		raw, exists := config[field]
		if !exists && field == "ovaDownloadPath" {
			raw, exists = "", true
		}
		if !exists {
			continue
		}
		root, ok := raw.(string)
		if !ok || strings.Contains(root, "${") {
			pending = append(pending, field)
			continue
		}
		if strings.TrimSpace(root) == "" {
			if field == "ovaDownloadPath" {
				root, _ = config["downloadPath"].(string)
				if strings.TrimSpace(root) == "" || strings.Contains(root, "${") {
					continue
				}
			} else {
				pending = append(pending, field)
				continue
			}
		}
		template := strings.TrimRight(strings.ReplaceAll(root, `\`, "/"), "/") + suffix + "/${title}"
		if field == "downloadPath" {
			template += seasonSuffix
		}
		config[target] = template
	}
	return pending
}

func normalizeSubscription(item map[string]any, version string) {
	if version != "v3.2.52" {
		if _, exists := item["customDownloadPathTemplate"]; !exists {
			if value, ok := item["downloadPath"].(string); ok {
				item["customDownloadPathTemplate"] = value
			}
		}
	}

	if _, exists := item["standbyRssList"]; !exists {
		oldList, _ := item["backRssList"].([]any)
		oldURLs, _ := item["backRss"].([]any)
		if len(oldList) > 0 {
			item["standbyRssList"] = oldList
		} else if len(oldURLs) > 0 {
			offset, exists := item["offset"]
			if !exists || offset == nil {
				offset = float64(0)
			}
			list := make([]any, 0, len(oldURLs))
			for _, url := range oldURLs {
				list = append(list, map[string]any{"label": "未知字幕组", "url": url, "offset": offset})
			}
			item["standbyRssList"] = list
		} else if oldList != nil {
			item["standbyRssList"] = oldList
		}
	}
	if version != "v3.2.52" {
		if list, ok := item["standbyRssList"].([]any); ok {
			parentOffset, hasParentOffset := item["offset"]
			if !hasParentOffset || parentOffset == nil {
				parentOffset = float64(0)
			}
			for _, entry := range list {
				standby, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				if standby["offset"] == nil {
					standby["offset"] = parentOffset
				}
			}
		}
	}
	if _, exists := item["releaseDate"]; !exists {
		year, yearOK := number(item["year"])
		month, monthOK := number(item["month"])
		if yearOK && monthOK {
			day, dayOK := number(item["date"])
			if dayOK {
				item["releaseDate"] = fmt.Sprintf("%04d-%02d-%02d", int(year), int(month), int(day))
				item["releaseDatePrecision"] = "day"
			} else {
				item["releaseDate"] = fmt.Sprintf("%04d-%02d", int(year), int(month))
				item["releaseDatePrecision"] = "month"
			}
		}
	} else if strings.Count(fmt.Sprint(item["releaseDate"]), "-") == 2 {
		item["releaseDatePrecision"] = "day"
	}
}

func number(v any) (float64, bool) {
	n, ok := v.(float64)
	return n, ok
}

func set(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func identify(files map[string][]byte) (string, string, string, error) {
	for _, candidate := range []struct{ version, config, subs string }{
		{"v3.2.52", "config.v2.json", "ani.v2.json"},
		{"v1.2.82", "config.json", "ani.json"},
		{"v1.1.50", "config.json", "ani.json"},
	} {
		if _, c := files[candidate.config]; c {
			if _, s := files[candidate.subs]; s {
				version := candidate.version
				if version == "v1.2.82" {
					var cfg map[string]any
					_ = json.Unmarshal(files[candidate.config], &cfg)
					if _, newer := cfg["renameSleepSeconds"]; !newer {
						version = "v1.1.50"
					}
				}
				return version, candidate.config, candidate.subs, nil
			}
		}
	}
	return "", "", "", errors.New("source must contain a recognized config and subscription JSON pair")
}

func readSource(source string) (map[string][]byte, error) {
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		reader, err := zip.OpenReader(source)
		if err != nil {
			return nil, errors.New("source must be a directory or ZIP archive")
		}
		defer reader.Close()
		files := map[string][]byte{}
		for _, entry := range reader.File {
			name := filepath.Base(entry.Name)
			if !recognized(name) {
				continue
			}
			if _, exists := files[name]; exists {
				return nil, errors.New("source contains ambiguous migration JSON files")
			}
			file, err := entry.Open()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(io.LimitReader(file, 16<<20))
			file.Close()
			if err != nil {
				return nil, err
			}
			files[name] = data
		}
		return files, nil
	}
	files := map[string][]byte{}
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 && recognized(entry.Name()) {
			return errors.New("source contains a symbolic link to a migration JSON file")
		}
		if entry.IsDir() || !recognized(entry.Name()) {
			return nil
		}
		if _, exists := files[entry.Name()]; exists {
			return errors.New("source contains ambiguous migration JSON files")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[entry.Name()] = data
		return nil
	})
	return files, err
}

func recognized(name string) bool {
	switch name {
	case "config.json", "ani.json", "config.v2.json", "ani.v2.json":
		return true
	default:
		return false
	}
}

func readAttachments(source string) ([]attachment, []string, []string, []string, error) {
	info, err := os.Stat(source)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if info.IsDir() {
		return readDirectoryAttachments(source)
	}
	return readZipAttachments(source)
}

func readDirectoryAttachments(source string) ([]attachment, []string, []string, []string, error) {
	var attachments []attachment
	var included []string
	omitted := []string{"webui"}
	for _, root := range []string{"files", "torrents", "cache"} {
		rootPath := filepath.Join(source, root)
		info, err := os.Lstat(rootPath)
		if errors.Is(err, os.ErrNotExist) {
			omitted = append(omitted, root)
			continue
		}
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, nil, nil, fmt.Errorf("source contains a symbolic link at an importable directory")
		}
		if !info.IsDir() {
			return nil, nil, nil, nil, fmt.Errorf("source import path is not a directory")
		}
		included = append(included, root)
		err = filepath.WalkDir(rootPath, func(filePath string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if filePath == rootPath {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("source contains a symbolic link in an importable directory")
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return errors.New("source contains a non-regular file in an importable directory")
			}
			rel, err := filepath.Rel(source, filePath)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if err := validateAssetPath(rel); err != nil {
				return err
			}
			data, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			attachments = append(attachments, attachment{path: rel, data: data})
			return nil
		})
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var unconverted []string
	for _, entry := range entries {
		name := entry.Name()
		if recognized(name) || name == "files" || name == "torrents" || name == "cache" || name == "webui" {
			continue
		}
		unconverted = append(unconverted, name)
	}
	return attachments, included, omitted, unconverted, nil
}

func readZipAttachments(source string) ([]attachment, []string, []string, []string, error) {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return nil, nil, nil, nil, errors.New("source must be a directory or ZIP archive")
	}
	defer reader.Close()
	var attachments []attachment
	seen := make(map[string]bool)
	includedRoots := make(map[string]bool)
	unconvertedRoots := make(map[string]bool)
	for _, entry := range reader.File {
		name := entry.Name
		if err := validateZipPath(name); err != nil {
			return nil, nil, nil, nil, err
		}
		rel := strings.TrimSuffix(name, "/")
		importable := rel == "files" || rel == "torrents" || strings.HasPrefix(rel, "files/") || strings.HasPrefix(rel, "torrents/")
		if rel != "" && !importable && !recognized(path.Base(rel)) {
			unconvertedRoots[strings.SplitN(rel, "/", 2)[0]] = true
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return nil, nil, nil, nil, errors.New("ZIP contains a symbolic link")
		}
		if !strings.HasPrefix(rel, "files/") && !strings.HasPrefix(rel, "torrents/") {
			continue
		}
		if !entry.Mode().IsRegular() {
			return nil, nil, nil, nil, errors.New("ZIP contains a non-regular attachment")
		}
		if err := validateAssetPath(rel); err != nil {
			return nil, nil, nil, nil, err
		}
		if seen[rel] {
			return nil, nil, nil, nil, errors.New("ZIP contains duplicate attachment paths")
		}
		seen[rel] = true
		file, err := entry.Open()
		if err != nil {
			return nil, nil, nil, nil, err
		}
		data, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil {
			return nil, nil, nil, nil, readErr
		}
		if closeErr != nil {
			return nil, nil, nil, nil, closeErr
		}
		attachments = append(attachments, attachment{path: rel, data: data})
		includedRoots[strings.SplitN(rel, "/", 2)[0]] = true
	}
	var included []string
	var omitted []string
	for _, root := range []string{"files", "torrents"} {
		if includedRoots[root] {
			included = append(included, root)
		} else {
			omitted = append(omitted, root)
		}
	}
	var unconverted []string
	for root := range unconvertedRoots {
		unconverted = append(unconverted, root)
	}
	return attachments, included, append(omitted, "cache", "webui"), unconverted, nil
}

func validateZipPath(name string) error {
	if strings.ContainsRune(name, '\x00') || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") || (len(name) >= 2 && name[1] == ':') {
		return errors.New("ZIP contains an unsafe path")
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return errors.New("ZIP contains a path traversal entry")
		}
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return errors.New("ZIP contains a path traversal entry")
	}
	return nil
}

func validateAssetPath(name string) error {
	if strings.Contains(name, `\`) || strings.ContainsRune(name, '\x00') || path.IsAbs(name) {
		return errors.New("source contains an unsafe attachment path")
	}
	clean := path.Clean(name)
	if clean != name || strings.HasPrefix(clean, "../") || clean == ".." {
		return errors.New("source contains an unsafe attachment path")
	}
	if !strings.HasPrefix(clean, "files/") && !strings.HasPrefix(clean, "torrents/") && !strings.HasPrefix(clean, "cache/") {
		return errors.New("source attachment path is outside supported directories")
	}
	return nil
}

func importWithAttachments(ctx context.Context, store *storage.Store, prepared snapshot) error {
	installed, createdDirs, cleanup, err := stageAndInstall(store.DataDir(), prepared.attachments)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := store.ImportSnapshot(ctx, prepared.config, prepared.subscriptions); err != nil {
		rollbackErr := rollbackInstalled(installed, createdDirs)
		if rollbackErr != nil {
			return fmt.Errorf("import database: %w; attachment rollback: %v", err, rollbackErr)
		}
		return fmt.Errorf("import database: %w", err)
	}
	return nil
}

func stageAndInstall(dataDir string, attachments []attachment) ([]string, []string, func(), error) {
	if len(attachments) == 0 {
		return nil, nil, func() {}, nil
	}
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := validateDestinationRoot(root); err != nil {
		return nil, nil, nil, err
	}
	var toInstall []attachment
	seen := make(map[string]bool)
	for _, item := range attachments {
		if err := validateAssetPath(item.path); err != nil {
			return nil, nil, nil, err
		}
		if seen[item.path] {
			return nil, nil, nil, errors.New("source contains duplicate attachment paths")
		}
		seen[item.path] = true
		destination := filepath.Join(root, filepath.FromSlash(item.path))
		if err := validateExistingParents(root, filepath.Dir(destination)); err != nil {
			return nil, nil, nil, err
		}
		info, statErr := os.Lstat(destination)
		if statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
			return nil, nil, nil, fmt.Errorf("attachment destination is not a regular file: %s", item.path)
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return nil, nil, nil, fmt.Errorf("inspect destination attachment: %w", statErr)
		}
		existing, err := os.ReadFile(destination)
		if err == nil {
			if !bytes.Equal(existing, item.data) {
				return nil, nil, nil, fmt.Errorf("attachment conflict at %s", item.path)
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil, fmt.Errorf("inspect destination attachment: %w", err)
		}
		toInstall = append(toInstall, item)
	}
	if len(toInstall) == 0 {
		return nil, nil, func() {}, nil
	}
	stageDir, err := os.MkdirTemp(root, ".ani-rss-import-")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create attachment staging directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(stageDir) }
	for _, item := range toInstall {
		stagePath := filepath.Join(stageDir, filepath.FromSlash(item.path))
		if err := os.MkdirAll(filepath.Dir(stagePath), 0700); err != nil {
			cleanup()
			return nil, nil, nil, err
		}
		if err := os.WriteFile(stagePath, item.data, 0600); err != nil {
			cleanup()
			return nil, nil, nil, err
		}
	}
	var installed, createdDirs []string
	for _, item := range toInstall {
		destination := filepath.Join(root, filepath.FromSlash(item.path))
		dirs, err := makeDestinationParents(root, filepath.Dir(destination))
		createdDirs = append(createdDirs, dirs...)
		if err != nil {
			rollbackErr := rollbackInstalled(installed, createdDirs)
			cleanup()
			return nil, nil, nil, errors.Join(err, rollbackErr)
		}
		stagePath := filepath.Join(stageDir, filepath.FromSlash(item.path))
		if err := os.Link(stagePath, destination); err != nil {
			rollbackErr := rollbackInstalled(installed, createdDirs)
			cleanup()
			return nil, nil, nil, errors.Join(fmt.Errorf("install attachment %s: %w", item.path, err), rollbackErr)
		}
		installed = append(installed, destination)
		if err := os.Remove(stagePath); err != nil {
			rollbackErr := rollbackInstalled(installed, createdDirs)
			cleanup()
			return nil, nil, nil, errors.Join(err, rollbackErr)
		}
	}
	return installed, createdDirs, cleanup, nil
}

func validateDestinationRoot(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("attachment data root must be a real directory")
	}
	return nil
}

func validateExistingParents(root, parent string) error {
	rel, err := filepath.Rel(root, parent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("attachment destination escapes data directory")
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("attachment destination contains a symbolic link or non-directory")
		}
	}
	return nil
}

func makeDestinationParents(root, parent string) ([]string, error) {
	rel, err := filepath.Rel(root, parent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("attachment destination escapes data directory")
	}
	var created []string
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0700); err != nil {
				return created, err
			}
			created = append(created, current)
			continue
		}
		if err != nil {
			return created, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return created, errors.New("attachment destination contains a symbolic link or non-directory")
		}
	}
	return created, nil
}

func rollbackInstalled(files, dirs []string) error {
	var errs []error
	for i := len(files) - 1; i >= 0; i-- {
		if err := os.Remove(files[i]); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Remove(dirs[i]); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
