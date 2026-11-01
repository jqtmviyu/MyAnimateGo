package filesapi

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/migration"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

const backupManifest = "backend-backup.json"
const backupFormat = "backend-backup"
const maxBackupBytes = int64(8 << 30)

var backupAssetRoots = []string{"files", "torrents", "peerfilter"}

type assetMove struct{ live, staged, old string }

type backupDescriptor struct {
	Format     string   `json:"format"`
	Version    int      `json:"version"`
	CreatedAt  string   `json:"createdAt"`
	Database   string   `json:"database"`
	AssetRoots []string `json:"assetRoots"`
}

func (s *Service) exportBackup(w http.ResponseWriter, r *http.Request) {
	file, err := s.createBackup(r.Context())
	if err != nil {
		http.Error(w, "创建备份失败", http.StatusInternalServerError)
		return
	}
	defer os.Remove(file)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `inline; filename="ani-rss.backup.go.zip"`)
	http.ServeFile(w, r, file)
}

func (s *Service) createBackup(ctx context.Context) (string, error) {
	release, err := s.freezeWrites(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	stage, err := os.MkdirTemp(s.store.DataDir(), ".backup-stage-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	stable := false
	for attempt := 0; attempt < 3; attempt++ {
		for _, rootName := range backupAssetRoots {
			_ = os.RemoveAll(filepath.Join(stage, rootName))
		}
		_ = os.Remove(filepath.Join(stage, "state.db"))
		for _, rootName := range backupAssetRoots {
			source := filepath.Join(s.store.DataDir(), rootName)
			target := filepath.Join(stage, rootName)
			if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return "", err
			}
			if err := copyTree(source, target); err != nil {
				return "", err
			}
		}
		if err := s.store.SnapshotDB(ctx, filepath.Join(stage, "state.db")); err != nil {
			return "", err
		}
		matches, err := assetTreesMatch(s.store.DataDir(), stage)
		if err != nil {
			return "", err
		}
		if matches {
			stable = true
			break
		}
	}
	if !stable {
		return "", errors.New("备份期间媒体附件持续变化，请稍后重试")
	}
	output, err := os.CreateTemp(s.store.DataDir(), ".ani-rss-backup-*.zip")
	if err != nil {
		return "", err
	}
	name := output.Name()
	archive := zip.NewWriter(output)
	descriptor := backupDescriptor{Format: backupFormat, Version: 1, CreatedAt: s.options.Now().UTC().Format(time.RFC3339), Database: "state.db", AssetRoots: append([]string(nil), backupAssetRoots...)}
	manifest, _ := json.Marshal(descriptor)
	if err := addZipBytes(archive, backupManifest, manifest); err != nil {
		archive.Close()
		output.Close()
		os.Remove(name)
		return "", err
	}
	err = filepath.WalkDir(stage, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(stage, path)
		if err != nil {
			return err
		}
		return addZipFile(archive, filepath.ToSlash(rel), path)
	})
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if syncErr := output.Sync(); err == nil {
		err = syncErr
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(name)
		return "", fmt.Errorf("写入备份 ZIP 失败：%w", err)
	}
	return name, nil
}

// Backup is the scheduler callback for deterministic automatic archives.
func (s *Service) Backup(ctx context.Context, jobID string) error {
	if jobID == "" {
		return errors.New("自动备份任务 ID 不能为空")
	}
	destinationDir := filepath.Join(s.store.DataDir(), "backup")
	if err := os.MkdirAll(destinationDir, 0700); err != nil {
		return err
	}
	destination := filepath.Join(destinationDir, "auto-"+safeJobID(jobID)+".zip")
	if _, err := os.Stat(destination); err == nil {
		valid, verifyErr := validBackup(destination)
		if verifyErr != nil {
			return verifyErr
		}
		if valid {
			return nil
		}
		return errors.New("自动备份目标已存在但归档无效")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := s.createBackup(ctx)
	if err != nil {
		return err
	}
	defer os.Remove(file)
	if err := os.Rename(file, destination); err != nil {
		return fmt.Errorf("安装自动备份失败：%w", err)
	}
	if err := s.clearExpiredBackups(ctx, destinationDir); err != nil {
		s.AppendLog(LogEntry{Level: "WARN", Message: "自动备份成功，清理旧备份失败：" + err.Error(), Logger: "filesapi.backup"})
	}
	return nil
}

func (s *Service) clearExpiredBackups(ctx context.Context, directory string) error {
	config, err := s.store.GetConfig(ctx)
	if err != nil {
		return err
	}
	days := integer(config["configBackupDay"])
	if days <= 0 {
		return nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	expiration := s.options.Now().AddDate(0, 0, -days)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "auto-") || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(expiration) {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReconcileBackup confirms whether the deterministic backup for this job is already complete.
func (s *Service) ReconcileBackup(_ context.Context, jobID string) (bool, error) {
	if jobID == "" {
		return false, errors.New("自动备份任务 ID 不能为空")
	}
	path := filepath.Join(s.store.DataDir(), "backup", "auto-"+safeJobID(jobID)+".zip")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return validBackup(path)
}

func (s *Service) importBackup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupBytes+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		server.Respond(w, http.StatusBadRequest, "ZIP 备份上传格式错误", nil)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		server.Respond(w, http.StatusBadRequest, "缺少备份文件字段 file", nil)
		return
	}
	defer file.Close()
	if !strings.EqualFold(filepath.Ext(header.Filename), ".zip") {
		server.Respond(w, http.StatusBadRequest, "导入格式异常", nil)
		return
	}
	stage, err := os.MkdirTemp(s.store.DataDir(), ".restore-stage-")
	if err != nil {
		respondError(w, err)
		return
	}
	defer os.RemoveAll(stage)
	upload := filepath.Join(stage, "upload.zip")
	copyErr := copyLimit(upload, file, maxBackupBytes)
	if copyErr != nil {
		server.Respond(w, http.StatusBadRequest, copyErr.Error(), nil)
		return
	}
	prepared, err := prepareBackup(r.Context(), upload, filepath.Join(stage, "prepared"))
	if err != nil {
		server.Respond(w, http.StatusBadRequest, "备份校验失败："+err.Error(), nil)
		return
	}
	if err := s.installBackup(r.Context(), prepared); err != nil {
		respondError(w, err)
		return
	}
	server.Respond(w, http.StatusOK, "导入成功", nil)
}

func prepareBackup(ctx context.Context, archivePath, preparedDir string) (string, error) {
	if err := os.MkdirAll(preparedDir, 0700); err != nil {
		return "", err
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	var manifest *zip.File
	for _, entry := range reader.File {
		if entry.Name == backupManifest {
			manifest = entry
			break
		}
	}
	_ = reader.Close()
	if manifest != nil {
		if err := extractBackup(archivePath, preparedDir); err != nil {
			return "", err
		}
		manifestBytes, err := os.ReadFile(filepath.Join(preparedDir, backupManifest))
		if err != nil {
			return "", err
		}
		var descriptor backupDescriptor
		if err := json.Unmarshal(manifestBytes, &descriptor); err != nil || descriptor.Format != backupFormat || descriptor.Version != 1 || descriptor.Database != "state.db" {
			return "", errors.New("备份格式或版本无效")
		}
		legacyStore, err := storage.Open(filepath.Join(preparedDir, "state.db"))
		if err != nil {
			return "", fmt.Errorf("打开备份数据库失败：%w", err)
		}
		if err := legacyStore.UpdateConfigFields(ctx, map[string]any{"automaticTasksPaused": true}); err != nil {
			_ = legacyStore.Close()
			return "", err
		}
		if err := legacyStore.Close(); err != nil {
			return "", err
		}
		return preparedDir, nil
	}
	legacySource := filepath.Join(preparedDir, "legacy-source")
	if err := extractBackup(archivePath, legacySource); err != nil {
		return "", err
	}
	legacyStore, err := storage.Open(filepath.Join(preparedDir, "state.db"))
	if err != nil {
		return "", err
	}
	_, err = migration.Import(ctx, legacyStore, legacySource)
	closeErr := legacyStore.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	return preparedDir, nil
}

func (s *Service) installBackup(ctx context.Context, prepared string) error {
	release, err := s.freezeWrites(ctx)
	if err != nil {
		return err
	}
	defer release()
	rollback := filepath.Join(s.store.DataDir(), ".restore-rollback")
	_ = os.RemoveAll(rollback)
	if err := os.MkdirAll(rollback, 0700); err != nil {
		return err
	}
	moves := make([]assetMove, 0, len(backupAssetRoots))
	for _, name := range backupAssetRoots {
		live := filepath.Join(s.store.DataDir(), name)
		staged := filepath.Join(prepared, name)
		old := filepath.Join(rollback, name)
		if _, err := os.Lstat(live); err == nil {
			if err := os.Rename(live, old); err != nil {
				rollbackMoves(moves)
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			rollbackMoves(moves)
			return err
		}
		moves = append(moves, assetMove{live: live, staged: staged, old: old})
		if _, err := os.Lstat(staged); err == nil {
			if err := os.Rename(staged, live); err != nil {
				rollbackMoves(moves)
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			rollbackMoves(moves)
			return err
		}
	}
	database := filepath.Join(prepared, "state.db")
	if _, err := os.Stat(database); err != nil {
		rollbackMoves(moves)
		return errors.New("备份缺少 SQLite 数据库")
	}
	if err := s.store.RestoreSnapshot(ctx, database); err != nil {
		rollbackMoves(moves)
		return err
	}
	_ = os.RemoveAll(rollback)
	if s.options.OnRestore != nil {
		s.options.OnRestore()
	}
	return nil
}

func rollbackMoves(moves []assetMove) {
	for i := len(moves) - 1; i >= 0; i-- {
		_ = os.RemoveAll(moves[i].live)
		if _, err := os.Lstat(moves[i].old); err == nil {
			_ = os.Rename(moves[i].old, moves[i].live)
		}
	}
}

func extractBackup(source, target string) error {
	archive, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer archive.Close()
	seen := map[string]bool{}
	var total uint64
	var written uint64
	for _, entry := range archive.File {
		clean := filepath.Clean(filepath.FromSlash(entry.Name))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("ZIP 包含越界路径 %q", entry.Name)
		}
		if seen[clean] {
			return fmt.Errorf("ZIP 包含重复路径 %q", entry.Name)
		}
		seen[clean] = true
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("ZIP 不允许符号链接 %q", entry.Name)
		}
		if entry.UncompressedSize64 > uint64(maxBackupBytes)-total {
			return errors.New("备份展开后超过大小限制")
		}
		total += entry.UncompressedSize64
		destination := filepath.Join(target, clean)
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, 0700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return err
		}
		sourceFile, err := entry.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			sourceFile.Close()
			return err
		}
		remaining := maxBackupBytes - int64(written)
		copied, copyErr := io.Copy(output, io.LimitReader(sourceFile, remaining+1))
		closeInputErr := sourceFile.Close()
		closeOutputErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeInputErr != nil {
			return closeInputErr
		}
		if closeOutputErr != nil {
			return closeOutputErr
		}
		if copied > remaining {
			return errors.New("备份展开后超过大小限制")
		}
		written += uint64(copied)
	}
	return nil
}

func validBackup(path string) (bool, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return false, err
	}
	defer archive.Close()
	var manifest, database bool
	for _, entry := range archive.File {
		if entry.Name == "state.db" {
			database = true
		}
		if entry.Name == backupManifest {
			manifest = true
			reader, err := entry.Open()
			if err != nil {
				return false, err
			}
			var descriptor backupDescriptor
			err = json.NewDecoder(io.LimitReader(reader, 1<<20)).Decode(&descriptor)
			_ = reader.Close()
			if err != nil || descriptor.Format != backupFormat || descriptor.Version != 1 || descriptor.Database != "state.db" {
				return false, err
			}
		}
	}
	if !manifest || !database {
		return false, nil
	}
	for _, entry := range archive.File {
		reader, err := entry.Open()
		if err != nil {
			return false, err
		}
		_, copyErr := io.Copy(io.Discard, reader)
		closeErr := reader.Close()
		if copyErr != nil {
			return false, copyErr
		}
		if closeErr != nil {
			return false, closeErr
		}
	}
	return true, nil
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("备份目录不允许符号链接：%s", path)
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		to := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(to, 0700)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0700); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeInErr := in.Close()
		closeOutErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeInErr != nil {
			return closeInErr
		}
		return closeOutErr
	})
}

func assetTreesMatch(sourceRoot, snapshotRoot string) (bool, error) {
	for _, name := range backupAssetRoots {
		source := filepath.Join(sourceRoot, name)
		snapshot := filepath.Join(snapshotRoot, name)
		sourceHashes, err := hashTree(source)
		if err != nil {
			return false, err
		}
		snapshotHashes, err := hashTree(snapshot)
		if err != nil {
			return false, err
		}
		if len(sourceHashes) != len(snapshotHashes) {
			return false, nil
		}
		for path, digest := range sourceHashes {
			if snapshotHashes[path] != digest {
				return false, nil
			}
		}
	}
	return true, nil
}

func hashTree(root string) (map[string][32]byte, error) {
	result := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) && path == root {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("备份目录不允许符号链接：%s", path)
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		rel, err := filepath.Rel(filepath.Dir(root), path)
		if err != nil {
			return err
		}
		var digest [32]byte
		copy(digest[:], hash.Sum(nil))
		result[filepath.ToSlash(rel)] = digest
		return nil
	})
	return result, err
}

func addZipBytes(archive *zip.Writer, name string, data []byte) error {
	entry, err := archive.Create(name)
	if err != nil {
		return err
	}
	_, err = entry.Write(data)
	return err
}

func addZipFile(archive *zip.Writer, name, path string) error {
	entry, err := archive.Create(name)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(entry, file)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func copyLimit(path string, input io.Reader, limit int64) error {
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	count, copyErr := io.Copy(output, io.LimitReader(input, limit+1))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if count > limit {
		return errors.New("备份文件超过大小限制")
	}
	return nil
}

func safeJobID(value string) string {
	var b strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			b.WriteRune(char)
		}
	}
	if b.Len() == 0 {
		return "invalid"
	}
	return b.String()
}
