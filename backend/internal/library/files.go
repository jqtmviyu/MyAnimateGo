package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type MoveOptions struct {
	CopyMode         bool
	DeleteOldEpisode bool
}

type RenameResult struct {
	Renamed []string `json:"renamed"`
	Skipped []string `json:"skipped,omitempty"`
}

// ReconcileCompletedFile 检查单文件完结迁移是否已完成；调用方负责确认关联任务已切换到 targetRoot。
func (s *Service) ReconcileCompletedFile(ctx context.Context, sourceRoot, targetRoot, source, target string) (bool, error) {
	sourceRoot, source, err := scopedMediaFile(sourceRoot, source)
	if err != nil {
		return false, err
	}
	targetRoot, target, err = scopedMediaFile(targetRoot, target)
	if err != nil {
		return false, err
	}
	if err := differentPaths(sourceRoot, targetRoot); err != nil {
		return false, err
	}
	if _, err := os.Lstat(source); err == nil {
		if info, err := os.Lstat(source); err != nil {
			return false, err
		} else if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("完结迁移源必须是普通文件")
		}
		if _, err := os.Lstat(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if info, err := os.Lstat(target); err == nil {
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("完结迁移目标必须是普通文件")
		}
		return true, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return false, errors.New("源文件和目标文件均不存在")
	} else {
		return false, err
	}
}

// MoveCompletedFile 原子地移动单个媒体文件，可在目标已复制但源未删除时安全重试。
func (s *Service) MoveCompletedFile(ctx context.Context, sourceRoot, targetRoot, source, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	done, err := s.ReconcileCompletedFile(ctx, sourceRoot, targetRoot, source, target)
	if err != nil || done {
		return err
	}
	sourceRoot, source, err = scopedMediaFile(sourceRoot, source)
	if err != nil {
		return err
	}
	targetRoot, target, err = scopedMediaFile(targetRoot, target)
	if err != nil {
		return err
	}
	if err := differentPaths(sourceRoot, targetRoot); err != nil {
		return err
	}
	if err := ensureDirectory(targetRoot); err != nil {
		return err
	}
	if err := copyFile(source, target); err != nil {
		return err
	}
	if err := os.Remove(source); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func scopedMediaFile(root, path string) (string, string, error) {
	root, err := canonicalPath(root)
	if err != nil {
		return "", "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	if info, err := os.Lstat(absolute); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("完结迁移文件不能是符号链接")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	file, err := canonicalPath(absolute)
	if err != nil {
		return "", "", err
	}
	relative, err := filepath.Rel(root, file)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", errors.New("完结迁移文件超出订阅目录")
	}
	return root, file, nil
}

func (s *Service) writeScoped(root, target string, data []byte) error {
	return writeScoped(root, target, data)
}

var episodeInFilename = regexp.MustCompile(`(?i)S(\d+)E(\d+(?:\.5)?)`)

// MoveFiles 只移动配置路径下的普通文件；剧集模式只处理命名规范的视频和字幕。
func (s *Service) MoveFiles(ctx context.Context, subscription map[string]any, target string, options MoveOptions) error {
	source, err := s.resolvedPath(ctx, subscription)
	if err != nil {
		return err
	}
	target, err = absoluteDir(target)
	if err != nil {
		return err
	}
	if err := differentPaths(source, target); err != nil {
		return err
	}
	if err := ensureDirectory(target); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !valueBool(subscription["ova"]) && options.DeleteOldEpisode {
		if err := deleteOldEpisodes(entries, target); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := entry.Name()
		if !valueBool(subscription["ova"]) && (!mediaFile(name) || episodeInFilename.FindStringSubmatch(name) == nil) {
			continue
		}
		sourceFile := filepath.Join(source, name)
		targetFile := filepath.Join(target, name)
		if stat, err := os.Lstat(targetFile); err == nil && !valueBool(subscription["ova"]) {
			if stat.Mode()&os.ModeSymlink != 0 || stat.IsDir() {
				return errors.New("媒体目标必须是普通文件")
			}
			sourceInfo, sourceErr := entry.Info()
			if sourceErr == nil && stat.Size() == sourceInfo.Size() {
				equal, compareErr := sameFileContents(sourceFile, targetFile)
				if compareErr != nil {
					return compareErr
				}
				if equal {
					if !options.CopyMode {
						if err := os.Remove(sourceFile); err != nil && !errors.Is(err, os.ErrNotExist) {
							return err
						}
					}
					continue
				}
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := copyFile(sourceFile, targetFile); err != nil {
			return err
		}
		if !options.CopyMode {
			if err := os.Remove(sourceFile); err != nil {
				return err
			}
		}
	}
	if !options.CopyMode {
		s.ClearDir(source, true, true, 2)
	}
	return nil
}

// MoveToTemplate 使用流水线相同的路径规则展开文件移动目标模板。
func (s *Service) MoveToTemplate(ctx context.Context, subscription map[string]any, template string, options MoveOptions) error {
	if strings.TrimSpace(template) == "" {
		return errors.New("文件移动目标模板不能为空")
	}
	copy := cloneMap(subscription)
	copy["customDownloadPath"] = true
	copy["customDownloadPathTemplate"] = template
	target, err := s.resolvedPath(ctx, copy)
	if err != nil {
		return err
	}
	return s.MoveFiles(ctx, subscription, target, options)
}

func deleteOldEpisodes(source []os.DirEntry, target string) error {
	sourceEpisode := map[string]bool{}
	sourceSizes := map[string]int64{}
	for _, entry := range source {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !mediaFile(entry.Name()) {
			continue
		}
		match := episodeInFilename.FindStringSubmatch(entry.Name())
		if len(match) != 3 {
			continue
		}
		key := strings.ToUpper(match[0])
		sourceEpisode[key] = true
		if info, err := entry.Info(); err == nil {
			sourceSizes[entry.Name()] = info.Size()
		}
	}
	if len(sourceEpisode) == 0 {
		return nil
	}
	targetEntries, err := os.ReadDir(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range targetEntries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !mediaFile(entry.Name()) {
			continue
		}
		match := episodeInFilename.FindStringSubmatch(entry.Name())
		if len(match) != 3 || !sourceEpisode[strings.ToUpper(match[0])] {
			continue
		}
		if info, err := entry.Info(); err == nil && info.Size() == sourceSizes[entry.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(target, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func mediaFile(name string) bool { return videoFile(name) || subtitleFile(name) }

// RenameEpisodeFiles 将规则计算出的目标名称应用到指定集的视频和字幕文件。
// 调用方传入已验证的订阅路径和 rules.Rename 结果名称。
func (s *Service) RenameEpisodeFiles(ctx context.Context, subscription map[string]any, episode float64, targetBase string) (RenameResult, error) {
	dir, err := s.resolvedPath(ctx, subscription)
	if err != nil {
		return RenameResult{}, err
	}
	if targetBase == "" || filepath.Base(targetBase) != targetBase || strings.ContainsAny(targetBase, "/\\") || targetBase == "." || targetBase == ".." {
		return RenameResult{}, errors.New("重命名目标必须是文件名")
	}
	season := valueInt(subscription["season"])
	ova := valueBool(subscription["ova"])
	result := RenameResult{Renamed: []string{}, Skipped: []string{}}
	err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !mediaFile(entry.Name()) {
			return nil
		}
		name := entry.Name()
		episodeText := episodeInFilename.FindStringSubmatch(name)
		if !ova {
			if len(episodeText) != 3 {
				return nil
			}
			fileSeason, err := strconv.Atoi(episodeText[1])
			if err != nil || fileSeason != season {
				return nil
			}
			fileEpisode, err := strconv.ParseFloat(episodeText[2], 64)
			if err != nil || fileEpisode != episode {
				return nil
			}
		}
		ext := strings.ToLower(filepath.Ext(name))
		newName := targetBase
		if subtitleFile(name) {
			main := strings.TrimSuffix(name, filepath.Ext(name))
			nestedExt := filepath.Ext(main)
			if nestedExt != "" {
				newName += nestedExt
			}
		}
		newName += ext
		oldPath, newPath := path, filepath.Join(filepath.Dir(path), newName)
		if oldPath == newPath {
			result.Skipped = append(result.Skipped, name)
			return nil
		}
		if _, err := os.Lstat(newPath); err == nil {
			return fmt.Errorf("重命名目标已存在：%s", newName)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			return err
		}
		relative, _ := filepath.Rel(dir, newPath)
		result.Renamed = append(result.Renamed, relative)
		return nil
	})
	if err != nil {
		return result, err
	}
	return result, nil
}

// MoveSubscription 对应 setAni?move：先调整下载器任务保存路径，再移动本地文件。
func (s *Service) MoveSubscription(ctx context.Context, oldSubscription, newSubscription map[string]any) error {
	oldPath, err := s.resolvedPath(ctx, oldSubscription)
	if err != nil {
		return err
	}
	newPath, err := s.resolvedPath(ctx, newSubscription)
	if err != nil {
		return err
	}
	if oldPath == newPath {
		return nil
	}
	if err := differentPaths(oldPath, newPath); err != nil {
		return err
	}
	if s.options.Tasks == nil {
		return errors.New("媒体移动需要下载任务管理器确认任务路径")
	}
	if err := s.options.Tasks.MoveSubscription(ctx, oldSubscription, newPath); err != nil {
		return fmt.Errorf("移动下载任务失败：%w", err)
	}
	if err := ensureDirectory(newPath); err != nil {
		return err
	}
	entries, err := os.ReadDir(oldPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if err := moveEntry(filepath.Join(oldPath, entry.Name()), filepath.Join(newPath, entry.Name())); err != nil {
			return err
		}
	}
	s.ClearDir(oldPath, true, true, 2)
	return nil
}

// DeleteFiles 只删除路径解析器为该订阅确认的下载目录。
func (s *Service) DeleteFiles(ctx context.Context, subscription map[string]any) error {
	dir, err := s.resolvedPath(ctx, subscription)
	if err != nil {
		return err
	}
	if err := s.checkRemovableDirectory(dir); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// DeleteSubscription 提供 deleteAni?deleteFiles 所需的任务和媒体删除操作。
func (s *Service) DeleteSubscription(ctx context.Context, subscription map[string]any, deleteFiles bool) error {
	dir := ""
	if deleteFiles {
		var err error
		dir, err = s.resolvedPath(ctx, subscription)
		if err != nil {
			return err
		}
		if err := s.checkRemovableDirectory(dir); err != nil {
			return err
		}
		if s.options.Tasks == nil {
			return errors.New("删除订阅文件需要下载任务管理器")
		}
		if err := s.options.Tasks.DeleteSubscription(ctx, subscription, true); err != nil {
			return fmt.Errorf("删除下载任务失败：%w", err)
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	return s.removeTorrentCache(subscription)
}

func (s *Service) removeTorrentCache(subscription map[string]any) error {
	id := valueString(subscription["id"])
	if id == "" || filepath.Base(id) != id || id == "." || id == ".." {
		return errors.New("订阅 ID 无效，无法清理种子缓存")
	}
	root := filepath.Join(s.store.DataDir(), "torrents")
	if err := ensureDirectory(root); err != nil {
		return err
	}
	cacheDir := filepath.Join(root, id[:1], id)
	info, err := os.Lstat(cacheDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("种子缓存目录不能是符号链接")
	}
	return os.RemoveAll(cacheDir)
}

// MoveCompleted 按完结设置迁移订阅目录和关联下载任务。
func (s *Service) MoveCompleted(ctx context.Context, subscription map[string]any) error {
	config, err := s.config(ctx)
	if err != nil {
		return err
	}
	if !valueBool(subscription["completed"]) || valueInt(subscription["totalEpisodeNumber"]) < 1 || valueInt(subscription["currentEpisodeNumber"]) < valueInt(subscription["totalEpisodeNumber"]) || valueBool(subscription["enable"]) || valueBool(subscription["ova"]) || !valueBool(config["autoDisabled"]) || !valueBool(config["completed"]) {
		return nil
	}
	oldPath, err := s.resolvedPath(ctx, subscription)
	if err != nil {
		return err
	}
	if _, err := os.Stat(oldPath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	archive := cloneMap(subscription)
	if valueBool(archive["customCompleted"]) && valueString(archive["customCompletedPathTemplate"]) != "" {
		archive["customDownloadPathTemplate"] = archive["customCompletedPathTemplate"]
	} else {
		archive["customDownloadPathTemplate"] = valueString(config["completedPathTemplate"])
	}
	archive["customDownloadPath"] = true
	newPath, err := s.resolvedPath(ctx, archive)
	if err != nil {
		return err
	}
	if oldPath == newPath {
		return nil
	}
	if err := differentPaths(oldPath, newPath); err != nil {
		return err
	}
	if s.options.Tasks == nil {
		return errors.New("完结迁移需要下载任务管理器确认任务路径")
	}
	if err := s.options.Tasks.MoveSubscription(ctx, subscription, newPath); err != nil {
		return err
	}
	if err := ensureDirectory(newPath); err != nil {
		return err
	}
	entries, err := os.ReadDir(oldPath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if err := moveEntry(filepath.Join(oldPath, entry.Name()), filepath.Join(newPath, entry.Name())); err != nil {
			return err
		}
	}
	s.ClearDir(oldPath, true, true, 2)
	return nil
}

// ClearDir 向上清理空目录，并按参数保留 NFO、图片及应用元数据。
func (s *Service) ClearDir(path string, excludeImage, excludeNFO bool, maxDepth int) {
	current := path
	for i := 0; i < maxDepth; i++ {
		if current == "" {
			return
		}
		if _, err := os.Lstat(current); errors.Is(err, os.ErrNotExist) {
			current = filepath.Dir(current)
			continue
		}
		empty, err := s.directoryEmpty(current, excludeImage, excludeNFO)
		if err != nil || !empty {
			return
		}
		if err := os.Remove(current); err != nil {
			return
		}
		current = filepath.Dir(current)
	}
}

func (s *Service) directoryEmpty(path string, excludeImage, excludeNFO bool) (bool, error) {
	info, statErr := os.Lstat(path)
	if errors.Is(statErr, os.ErrNotExist) {
		return true, nil
	}
	if statErr != nil {
		return false, statErr
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	if !info.IsDir() {
		return true, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			return false, nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if excludeImage && isImage(ext) {
			continue
		}
		if excludeNFO && ext == ".nfo" {
			continue
		}
		switch strings.ToLower(name) {
		case ".ds_store", "thumbs.db", "bangumi.ini":
			continue
		}
		return false, nil
	}
	return true, nil
}

// ClearCover 清除未被订阅引用的附件文件，并返回释放的字节数。
func (s *Service) ClearCover(ctx context.Context) (int64, error) {
	subs, err := s.store.ListSubscriptions(ctx)
	if err != nil {
		return 0, err
	}
	filesRoot := filepath.Join(s.store.DataDir(), "files")
	if err := ensureDirectory(filesRoot); err != nil {
		return 0, err
	}
	used := map[string]bool{}
	for _, subscription := range subs {
		cover := valueString(subscription["cover"])
		if cover == "" {
			continue
		}
		used[filepath.Clean(filepath.Join(filesRoot, cover))] = true
	}
	var removed int64
	err = filepath.WalkDir(filesRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		absolute, _ := filepath.Abs(path)
		if used[filepath.Clean(absolute)] {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		removed += info.Size()
		if err := os.Remove(path); err != nil {
			return err
		}
		s.ClearDir(path, false, true, 2)
		return nil
	})
	return removed, err
}

func (s *Service) checkRemovableDirectory(dir string) error {
	clean, err := canonicalPath(dir)
	if err != nil {
		return err
	}
	dataDir := filepath.Clean(s.store.DataDir())
	if clean == string(filepath.Separator) || clean == dataDir || clean == filepath.Join(dataDir, "files") || clean == filepath.Join(dataDir, "torrents") || clean == filepath.Join(dataDir, "cache") {
		return errors.New("拒绝删除应用数据根目录")
	}
	for _, protected := range []string{filepath.Join(dataDir, "files"), filepath.Join(dataDir, "torrents"), filepath.Join(dataDir, "cache")} {
		if pathContains(protected, clean) {
			return errors.New("拒绝删除应用数据目录")
		}
	}
	if pathContains(clean, dataDir) {
		return errors.New("拒绝删除包含应用数据的父目录")
	}
	return nil
}
func ensureDirectory(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("媒体路径不能是符号链接")
		}
		if !info.IsDir() {
			return errors.New("媒体路径不是目录")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.MkdirAll(path, 0755)
}
func absoluteDir(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("目标目录不能为空")
	}
	result, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	result = filepath.Clean(result)
	if info, err := os.Lstat(result); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("目标目录不能是符号链接")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return canonicalPath(result)
}

func pathContains(parent, child string) bool {
	parentAbs, errParent := filepath.Abs(parent)
	childAbs, errChild := filepath.Abs(child)
	if errParent != nil || errChild != nil {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(parentAbs), filepath.Clean(childAbs))
	return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))))
}

func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	cursor := absolute
	var suffix []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(cursor)
		if resolveErr == nil {
			for _, part := range suffix {
				resolved = filepath.Join(resolved, part)
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(resolveErr, os.ErrNotExist) {
			return "", resolveErr
		}
		parent := filepath.Dir(cursor)
		if parent == cursor {
			return "", resolveErr
		}
		suffix = append([]string{filepath.Base(cursor)}, suffix...)
		cursor = parent
	}
}

func differentPaths(source, target string) error {
	if source == target {
		return errors.New("源目录和目标目录相同")
	}
	if pathContains(source, target) || pathContains(target, source) {
		return errors.New("源目录和目标目录不能互相包含")
	}
	return nil
}
func moveEntry(source, target string) error {
	parent, err := canonicalPath(filepath.Dir(target))
	if err != nil {
		return err
	}
	if parent != filepath.Clean(filepath.Dir(target)) {
		return errors.New("移动目标路径不能穿过符号链接")
	}
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("移动目标不能是符号链接")
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
	}
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDirectory(source, target)
	}
	if err := copyFile(source, target); err != nil {
		return err
	}
	return os.Remove(source)
}
func copyDirectory(source, target string) error {
	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if err := moveEntry(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
			return err
		}
	}
	return os.RemoveAll(source)
}
func isImage(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".svg":
		return true
	}
	return false
}
