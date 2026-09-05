package service

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/version"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ==================== 全量备份 / 还原 ====================
//
// 备份内容：
//   - 数据库快照（VACUUM INTO，保证 WAL 模式下的一致性）
//   - 配置文件（<data_dir>/config 下的 yaml 分片文件）
//   - 数据目录中的其他业务文件（如 .jwt_secret 密钥文件）
//   - 业务资源目录（当前为海报缩略图目录 /cache/thumbs）
//
// 打包规则：zip 内以 fan-video-<版本>-<时间到分钟> 为根目录，
// 并携带 manifest.json 描述条目归属与来源路径。
//
// 还原策略：
//   - config 与资源目录：立即写回当前运行环境；
//   - 数据库：写入 <data_dir>/.restore 暂存目录并留下标记，
//     在下次服务启动、真正打开数据库前完成替换（SQLite 文件被进程
//     占用，无法在运行中直接覆盖）。

const (
	backupManifestFormat = 1
	backupRootPrefix     = "fan-video-"

	// RestoreConfirmToken 还原操作需要传入的强确认字符串
	RestoreConfirmToken = "CONFIRM_RESTORE"

	// 安全限制：单文件解压上限（2 GiB）与总条目数上限
	maxRestoredFileSize = 2 << 30
	maxRestoreEntries   = 50000
)

// 业务资源目录（thumbBaseDir 定义于 thumbnail.go）
var backupResourceDirs = []struct {
	name string
	dir  string
}{
	{name: "thumbs", dir: thumbBaseDir},
}

// BackupEntry 备份文件元信息
type BackupEntry struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	Version   string    `json:"version,omitempty"`
}

// BackupFileEntry manifest 中的单条文件记录
type BackupFileEntry struct {
	Type string `json:"type"` // database / config / data / resource
	Name string `json:"name"` // resource 条目的资源目录名
	Path string `json:"path"` // zip 内相对路径（根目录之后）
	Size int64  `json:"size"`
}

// BackupManifest 备份清单
type BackupManifest struct {
	Format     int                      `json:"format"`
	AppVersion string                   `json:"app_version"`
	CreatedAt  time.Time                `json:"created_at"`
	DataDir    string                   `json:"data_dir"`
	ConfigDir  string                   `json:"config_dir"`
	DBPath     string                   `json:"db_path"`
	Resources  []BackupManifestResource `json:"resources"`
	Entries    []BackupFileEntry        `json:"entries"`
}

// BackupManifestResource 资源目录记录（name 用于还原时映射到当前环境目录）
type BackupManifestResource struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// RestoreResult 还原结果
type RestoreResult struct {
	PreBackupName     string   `json:"pre_backup_name"`
	RestoredConfig    int      `json:"restored_config"`
	RestoredData      int      `json:"restored_data"`
	RestoredResources int      `json:"restored_resources"`
	StagedDatabase    bool     `json:"staged_database"`
	RestartRequired   bool     `json:"restart_required"`
	Warnings          []string `json:"warnings,omitempty"`
}

// PendingRestoreMarker 暂存还原标记（下次启动时应用）
type PendingRestoreMarker struct {
	Format     int       `json:"format"`
	AppVersion string    `json:"app_version"`
	SourceName string    `json:"source_name"`
	CreatedAt  time.Time `json:"created_at"`
	Applied    bool      `json:"applied"`
	AppliedAt  time.Time `json:"applied_at,omitempty"`
}

// SystemBackupService 全量备份 / 还原服务
type SystemBackupService struct {
	db        *gorm.DB
	cfg       *config.Config
	logger    *zap.SugaredLogger
	backupDir string
}

// NewSystemBackupService 创建备份服务
func NewSystemBackupService(db *gorm.DB, cfg *config.Config, logger *zap.SugaredLogger) *SystemBackupService {
	return &SystemBackupService{
		db:        db,
		cfg:       cfg,
		logger:    logger,
		backupDir: cfg.BackupDir(),
	}
}

// BackupDir 返回备份存储目录
func (s *SystemBackupService) BackupDir() string {
	return s.backupDir
}

// RestoreStagingDir 返回数据库暂存还原目录
func (s *SystemBackupService) RestoreStagingDir() string {
	return filepath.Join(s.cfg.App.DataDir, ".restore")
}

// ==================== 列表 / 删除 / 下载 ====================

// List 列出全部备份文件
func (s *SystemBackupService) List() ([]BackupEntry, error) {
	if err := os.MkdirAll(s.backupDir, 0755); err != nil {
		return nil, fmt.Errorf("创建备份目录失败: %w", err)
	}
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		return nil, fmt.Errorf("读取备份目录失败: %w", err)
	}
	out := make([]BackupEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupEntry{
			Name:      e.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
			Version:   extractBackupVersion(e.Name()),
		})
	}
	return out, nil
}

// Delete 删除指定备份文件
func (s *SystemBackupService) Delete(name string) error {
	full, err := s.resolveBackupPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil {
		return fmt.Errorf("删除备份文件失败: %w", err)
	}
	return nil
}

// DownloadPath 返回备份文件的绝对路径（供下载）
func (s *SystemBackupService) DownloadPath(name string) (string, error) {
	return s.resolveBackupPath(name)
}

// resolveBackupPath 防止路径穿越：仅允许备份目录下的纯文件名
func (s *SystemBackupService) resolveBackupPath(name string) (string, error) {
	safe := filepath.Base(name)
	if safe != name || !strings.HasSuffix(safe, ".zip") ||
		safe == "." || safe == ".." || strings.Contains(safe, string(os.PathSeparator)) {
		return "", fmt.Errorf("备份文件名不合法: %q", name)
	}
	full := filepath.Join(s.backupDir, safe)
	if _, err := os.Stat(full); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("备份文件不存在: %q", name)
		}
		return "", fmt.Errorf("访问备份文件失败: %w", err)
	}
	return full, nil
}

// extractBackupVersion 从文件名 fan-video-<version>-<ts>.zip 解析版本号
func extractBackupVersion(name string) string {
	base := strings.TrimSuffix(name, ".zip")
	if !strings.HasPrefix(base, backupRootPrefix) {
		return ""
	}
	rest := strings.TrimPrefix(base, backupRootPrefix)
	idx := strings.Index(rest, "-20")
	if idx < 0 {
		return rest
	}
	return rest[:idx]
}

// ==================== 创建备份 ====================

// Create 创建一份全量备份并返回其元信息
func (s *SystemBackupService) Create() (*BackupEntry, error) {
	if err := os.MkdirAll(s.backupDir, 0755); err != nil {
		return nil, fmt.Errorf("创建备份目录失败: %w", err)
	}

	ts := time.Now().Format("20060102-1504")
	name := s.nextFileName(ts)
	rootDir := strings.TrimSuffix(name, ".zip")

	// 数据库快照（WAL 安全）
	dbSnapshot := filepath.Join(s.backupDir, ".backup-"+name+".db")
	if err := s.snapshotDatabase(dbSnapshot); err != nil {
		return nil, fmt.Errorf("生成数据库快照失败: %w", err)
	}
	defer os.Remove(dbSnapshot)

	tmpZip := filepath.Join(s.backupDir, ".backup-"+name+".tmp")
	finalZip := filepath.Join(s.backupDir, name)

	f, err := os.Create(tmpZip)
	if err != nil {
		return nil, fmt.Errorf("创建备份文件失败: %w", err)
	}
	zw := zip.NewWriter(f)

	manifest := s.buildManifest()

	// 1) 数据库
	dbSize, err := addFileToZip(zw, rootDir, "database/nowen.db", dbSnapshot)
	if err != nil {
		f.Close()
		os.Remove(tmpZip)
		return nil, fmt.Errorf("写入数据库快照失败: %w", err)
	}
	manifest.Entries = append(manifest.Entries, BackupFileEntry{Type: "database", Path: "database/nowen.db", Size: dbSize})

	// 2) 配置文件（覆盖配置加载器的全部搜索目录，重复文件名保留高优先级来源）
	//    搜索优先级：/etc/fan-video/config > <data_dir>/config > ./config
	configCandidates := []string{
		filepath.Join(s.cfg.App.DataDir, "config"),
		"./config",
		"/etc/fan-video/config",
	}
	configFiles := map[string]string{} // 顶层文件名 -> 源文件绝对路径
	for _, dir := range configCandidates {
		resolved, rerr := filepath.Abs(dir)
		if rerr != nil {
			continue
		}
		entries, derr := os.ReadDir(resolved)
		if derr != nil {
			continue
		}
		for _, de := range entries {
			if de.IsDir() || strings.HasPrefix(de.Name(), ".") {
				continue
			}
			configFiles[de.Name()] = filepath.Join(resolved, de.Name())
		}
	}
	for filename, src := range configFiles {
		sz, werr := addFileToZip(zw, rootDir, "config/"+filename, src)
		if werr != nil {
			f.Close()
			os.Remove(tmpZip)
			return nil, fmt.Errorf("写入配置文件失败: %w", werr)
		}
		manifest.Entries = append(manifest.Entries, BackupFileEntry{Type: "config", Path: "config/" + filename, Size: sz})
	}

	// 3) 数据目录其他业务文件（排除 DB、备份、暂存）
	topEntries, _ := os.ReadDir(s.cfg.App.DataDir)
	for _, de := range topEntries {
		base := de.Name()
		if de.IsDir() {
			if base == "config" || base == "backups" || base == ".restore" {
				continue
			}
		} else if s.isDBFile(filepath.Join(s.cfg.App.DataDir, base)) {
			continue
		}
		p := filepath.Join(s.cfg.App.DataDir, base)
		if de.IsDir() {
			_ = filepath.Walk(p, func(p string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				if s.isDBFile(p) {
					return nil
				}
				rel, rerr := filepath.Rel(s.cfg.App.DataDir, p)
				if rerr != nil {
					return nil
				}
				rel = filepath.ToSlash(rel)
				sz, werr := addFileToZip(zw, rootDir, "data/"+rel, p)
				if werr != nil {
					return werr
				}
				manifest.Entries = append(manifest.Entries, BackupFileEntry{Type: "data", Path: "data/" + rel, Size: sz})
				return nil
			})
		} else {
			sz, werr := addFileToZip(zw, rootDir, "data/"+filepath.ToSlash(base), p)
			if werr != nil {
				f.Close()
				os.Remove(tmpZip)
				return nil, fmt.Errorf("收集数据文件失败: %w", werr)
			}
			manifest.Entries = append(manifest.Entries, BackupFileEntry{Type: "data", Path: "data/" + filepath.ToSlash(base), Size: sz})
		}
	}

	// 4) 业务资源目录
	for _, rd := range backupResourceDirs {
		_ = filepath.Walk(rd.dir, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			rel, rerr := filepath.Rel(rd.dir, p)
			if rerr != nil || strings.HasPrefix(rel, "..") {
				return nil
			}
			rel = filepath.ToSlash(rel)
			sz, werr := addFileToZip(zw, rootDir, "resources/"+rd.name+"/"+rel, p)
			if werr != nil {
				return werr
			}
			manifest.Entries = append(manifest.Entries, BackupFileEntry{Type: "resource", Name: rd.name, Path: "resources/" + rd.name + "/" + rel, Size: sz})
			return nil
		})
	}

	// 5) manifest
	if err := writeJSONToZip(zw, rootDir, "manifest.json", manifest); err != nil {
		f.Close()
		os.Remove(tmpZip)
		return nil, fmt.Errorf("写入备份清单失败: %w", err)
	}

	if err := zw.Close(); err != nil {
		f.Close()
		os.Remove(tmpZip)
		return nil, fmt.Errorf("打包备份失败: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpZip)
		return nil, fmt.Errorf("写入备份失败: %w", err)
	}
	if err := os.Rename(tmpZip, finalZip); err != nil {
		os.Remove(tmpZip)
		return nil, fmt.Errorf("保存备份失败: %w", err)
	}

	info, err := os.Stat(finalZip)
	if err != nil {
		return nil, fmt.Errorf("读取备份信息失败: %w", err)
	}
	s.logger.Infof("已创建全量备份 %s（%.1f MB）", name, float64(info.Size())/1024/1024)
	return &BackupEntry{Name: name, Size: info.Size(), CreatedAt: info.ModTime(), Version: extractBackupVersion(name)}, nil
}

// nextFileName 生成不冲突的文件名（同一分钟内追加 -2、-3…）
func (s *SystemBackupService) nextFileName(ts string) string {
	base := backupRootPrefix + version.Current() + "-" + ts
	candidate := base + ".zip"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(s.backupDir, candidate)); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d.zip", base, i)
	}
}

// buildManifest 构建清单（entries 由调用方追加）
func (s *SystemBackupService) buildManifest() BackupManifest {
	resources := make([]BackupManifestResource, 0, len(backupResourceDirs))
	for _, rd := range backupResourceDirs {
		resources = append(resources, BackupManifestResource{Name: rd.name, Path: rd.dir})
	}
	return BackupManifest{
		Format:     backupManifestFormat,
		AppVersion: version.Current(),
		CreatedAt:  time.Now(),
		DataDir:    s.cfg.App.DataDir,
		ConfigDir:  filepath.Join(s.cfg.App.DataDir, "config"),
		DBPath:     s.cfg.Database.DBPath,
		Resources:  resources,
	}
}

// isDBFile 判断绝对路径是否为当前数据库文件（含 WAL/SHM 伴生文件）。
// 默认布局下库文件位于 <data_dir>/nowen.db 根部；二进制/setup 安装经
// NOWEN_DATABASE_DB_PATH 可能落在 <data_dir>/data/nowen.db 子目录。
// 无论哪种布局，全量备份都必须排除运行中的 SQLite 文件（主库 + -wal + -shm），
// 防止把 WAL 模式的瞬时状态原始拷贝进备份包并污染还原流程。
func (s *SystemBackupService) isDBFile(abs string) bool {
	dbPath := s.cfg.Database.DBPath
	if dbPath == "" {
		return false
	}
	abs = filepath.Clean(abs)
	dbPath = filepath.Clean(dbPath)
	return abs == dbPath || strings.HasPrefix(abs, dbPath+"-")
}

// snapshotDatabase 使用 VACUUM INTO 生成一致性数据库快照
func (s *SystemBackupService) snapshotDatabase(dest string) error {
	sql := "VACUUM INTO '" + strings.ReplaceAll(dest, "'", "''") + "'"
	if err := s.db.Exec(sql).Error; err != nil {
		return err
	}
	info, err := os.Stat(dest)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("数据库快照为空")
	}
	return nil
}

// ==================== 还原 ====================

// Restore 从用户上传的 zip 流还原。执行前会先自动创建一份当前状态备份。
func (s *SystemBackupService) Restore(r io.Reader, sourceName string) (*RestoreResult, error) {
	// 1) 自动预备份当前状态
	preBackup, err := s.Create()
	if err != nil {
		return nil, fmt.Errorf("还原前自动备份失败，已中止还原: %w", err)
	}
	s.logger.Warn("⚠️  管理还原开始，已自动创建还原前备份 " + preBackup.Name)

	// 2) 将上传流写入临时文件，再以 ReaderAt 方式读取 zip
	uploadTmp, err := os.CreateTemp("", "fan-video-restore-*.zip")
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer os.Remove(uploadTmp.Name())
	if _, err := io.Copy(uploadTmp, r); err != nil {
		uploadTmp.Close()
		return nil, fmt.Errorf("读取上传备份失败: %w", err)
	}
	if err := uploadTmp.Close(); err != nil {
		return nil, fmt.Errorf("读取上传备份失败: %w", err)
	}
	if info, _ := uploadTmp.Stat(); info != nil && info.Size() == 0 {
		return nil, fmt.Errorf("上传的备份文件为空")
	}

	zr, err := zip.OpenReader(uploadTmp.Name())
	if err != nil {
		return nil, fmt.Errorf("备份文件不是有效的 zip 包: %w", err)
	}
	defer zr.Close()

	// 3) 安全解压到临时目录
	tmpDir, err := os.MkdirTemp("", "fan-video-restore-unzip-*")
	if err != nil {
		return nil, fmt.Errorf("创建解压目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	manifest, restoredFiles, zipWarnings, err := extractBackupZip(&zr.Reader, tmpDir)
	if err != nil {
		return nil, err
	}
	if manifest.Format != backupManifestFormat {
		return nil, fmt.Errorf("不支持的备份格式: format=%d", manifest.Format)
	}

	result := &RestoreResult{
		PreBackupName: preBackup.Name,
	}
	result.Warnings = append(result.Warnings, zipWarnings...)

	// 4) 数据库 → 暂存，下次启动替换
	if _, ok := restoredFiles["database/nowen.db"]; ok {
		if err := s.stageDatabase(filepath.Join(tmpDir, "database", "nowen.db"), sourceName); err != nil {
			return nil, fmt.Errorf("暂存还原数据库失败: %w", err)
		}
		result.StagedDatabase = true
		result.RestartRequired = true
	}

	// 5) 配置文件 → 立即写回 <data_dir>/config
	for rel := range restoredFiles {
		if !strings.HasPrefix(rel, "config/") {
			continue
		}
		inner := strings.TrimPrefix(rel, "config/")
		inner = strings.TrimPrefix(inner, "/")
		if !isSafeRestoreRel(inner) {
			result.Warnings = append(result.Warnings, "跳过受限配置文件: "+rel)
			continue
		}
		src := filepath.Join(tmpDir, filepath.FromSlash(rel))
		dest := filepath.Join(s.cfg.App.DataDir, "config", filepath.FromSlash(inner))
		if err := installFile(src, dest); err != nil {
			return nil, fmt.Errorf("还原配置文件失败: %w", err)
		}
		result.RestoredConfig++
	}

	// 6) 数据文件 → 立即写回 <data_dir>
	for rel := range restoredFiles {
		if !strings.HasPrefix(rel, "data/") {
			continue
		}
		inner := strings.TrimPrefix(rel, "data/")
		inner = strings.TrimPrefix(inner, "/")
		if !isSafeRestoreRel(inner) || hasDBConflict(inner) {
			result.Warnings = append(result.Warnings, "跳过受限数据文件: "+rel)
			continue
		}
		src := filepath.Join(tmpDir, filepath.FromSlash(rel))
		dest := filepath.Join(s.cfg.App.DataDir, filepath.FromSlash(inner))
		if err := installFile(src, dest); err != nil {
			return nil, fmt.Errorf("还原数据文件失败: %w", err)
		}
		result.RestoredData++
	}

	// 7) 业务资源目录 → 立即写回当前环境对应目录
	for _, rd := range backupResourceDirs {
		prefix := "resources/" + rd.name + "/"
		for rel := range restoredFiles {
			if !strings.HasPrefix(rel, prefix) {
				continue
			}
			inner := strings.TrimPrefix(rel, prefix)
			inner = strings.TrimPrefix(inner, "/")
			if !isSafeRestoreRel(inner) {
				result.Warnings = append(result.Warnings, "跳过受限资源文件: "+rel)
				continue
			}
			src := filepath.Join(tmpDir, filepath.FromSlash(rel))
			dest := filepath.Join(rd.dir, filepath.FromSlash(inner))
			if err := installFile(src, dest); err != nil {
				return nil, fmt.Errorf("还原资源文件失败: %w", err)
			}
			result.RestoredResources++
		}
	}

	s.logger.Warn("管理还原完成: " + summarizeRestore(result))
	return result, nil
}

// stageDatabase 将还原的数据库与标记写入 <data_dir>/.restore
func (s *SystemBackupService) stageDatabase(dbFile, sourceName string) error {
	dir := s.RestoreStagingDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建暂存目录失败: %w", err)
	}
	for _, old := range []string{"nowen.db", "nowen.db-wal", "nowen.db-shm", "rollback-nowen.db", "marker.json"} {
		_ = os.RemoveAll(filepath.Join(dir, old))
	}
	dest := filepath.Join(dir, "nowen.db")
	if err := copyFilePreserving(dbFile, dest); err != nil {
		return fmt.Errorf("暂存数据库失败: %w", err)
	}
	marker := PendingRestoreMarker{
		Format:     backupManifestFormat,
		AppVersion: version.Current(),
		SourceName: sourceName,
		CreatedAt:  time.Now(),
	}
	b, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return fmt.Errorf("生成还原标记失败: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marker.json"), b, 0644); err != nil {
		return fmt.Errorf("写入还原标记失败: %w", err)
	}
	return nil
}

// summarizeRestore 生成还原摘要（日志用）
func summarizeRestore(r *RestoreResult) string {
	return fmt.Sprintf(
		"预备份=%s, 配置=%d, 数据=%d, 资源=%d, 数据库待重启生效=%v",
		r.PreBackupName, r.RestoredConfig, r.RestoredData, r.RestoredResources, r.StagedDatabase,
	)
}

// ==================== 启动时应用暂存数据库 ====================

// ApplyPendingDatabaseRestore 在服务启动、打开数据库前调用。
// 若存在 <data_dir>/.restore 暂存还原，则替换当前数据库并清理暂存目录。
func ApplyPendingDatabaseRestore(cfg *config.Config, logger *zap.SugaredLogger) {
	stagedDir := filepath.Join(cfg.App.DataDir, ".restore")
	markerPath := filepath.Join(stagedDir, "marker.json")
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		return
	}

	b, err := os.ReadFile(markerPath)
	if err != nil {
		logger.Errorf("读取暂存还原标记失败: %v（忽略本次还原并清理暂存目录）", err)
		_ = os.RemoveAll(stagedDir)
		return
	}
	var marker PendingRestoreMarker
	if err := json.Unmarshal(b, &marker); err != nil {
		logger.Errorf("解析暂存还原标记失败: %v（忽略本次还原并清理暂存目录）", err)
		_ = os.RemoveAll(stagedDir)
		return
	}
	if marker.Applied {
		_ = os.RemoveAll(stagedDir)
		return
	}

	stagedDB := filepath.Join(stagedDir, "nowen.db")
	if _, err := os.Stat(stagedDB); err != nil {
		logger.Errorf("暂存数据库缺失: %v（忽略本次还原）", err)
		_ = os.RemoveAll(stagedDir)
		return
	}

	dbPath := cfg.Database.DBPath
	transientRollback := filepath.Join(stagedDir, "rollback-nowen.db")

	// 先将当前数据库移到暂存目录（作为进程内的瞬态回滚副本），
	// 成功后随暂存目录一并删除——真正可用的安全网是还原前自动创建的预备份 zip。
	if _, err := os.Stat(dbPath); err == nil {
		_ = os.Rename(dbPath, transientRollback)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(dbPath + suffix)
	}

	logger.Warn("⚠️  应用备份还原的数据库: " + dbPath)
	if err := os.Rename(stagedDB, dbPath); err != nil {
		logger.Errorf("替换数据库失败: %v", err)
		if _, rerr := os.Stat(transientRollback); rerr == nil {
			_ = os.Rename(transientRollback, dbPath)
		}
		return
	}

	marker.Applied = true
	marker.AppliedAt = time.Now()
	if mb, mErr := json.MarshalIndent(marker, "", "  "); mErr == nil {
		_ = os.WriteFile(markerPath, mb, 0644)
	}
	source := marker.SourceName
	if source == "" {
		source = cfg.Database.DBPath
	}
	logger.Warn("已应用备份还原的数据库（来源: " + source + "），还原前的自动预备份可通过备份列表下载回滚")
	_ = os.RemoveAll(stagedDir)
}

// ==================== zip 工具 ====================

// addFileToZip 将本地文件写入 zip 的 <root>/<zipPath>，返回文件大小
func addFileToZip(zw *zip.Writer, root, zipPath, src string) (int64, error) {
	fi, err := os.Stat(src)
	if err != nil {
		return 0, err
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	hdr := &zip.FileHeader{Name: root + "/" + zipPath}
	hdr.Method = zip.Deflate
	hdr.Modified = fi.ModTime()
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(w, in)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// writeJSONToZip 将 JSON 写入 zip 的 <root>/<zipPath>
func writeJSONToZip(zw *zip.Writer, root, zipPath string, v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	w, err := zw.CreateHeader(&zip.FileHeader{
		Name:     root + "/" + zipPath,
		Method:   zip.Deflate,
		Modified: time.Now(),
	})
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// extractBackupZip 安全解压备份 zip 到 dstDir（含路径穿越防护）。
// 每个条目需位于 <根目录>/<相对路径>：剥离首个路径段（根目录名）后，
// 校验相对路径必须属于 database / config / data / resources/<name> / manifest.json。
// 返回备份清单与所有有效文件的相对路径集合。
func extractBackupZip(zr *zip.Reader, dstDir string) (*BackupManifest, map[string]struct{}, []string, error) {
	var warnings []string
	if len(zr.File) < 1 {
		return nil, nil, warnings, fmt.Errorf("备份 zip 为空")
	}
	if len(zr.File) > maxRestoreEntries {
		return nil, nil, warnings, fmt.Errorf("备份条目过多（>%d），已拒绝解压", maxRestoreEntries)
	}

	var manifest *BackupManifest
	relFiles := make(map[string]struct{}, len(zr.File))

	for _, f := range zr.File {
		if f.FileInfo().IsDir() || f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		name := f.Name
		if strings.HasPrefix(name, "/") {
			return nil, nil, warnings, fmt.Errorf("备份包含绝对路径条目: %s", name)
		}

		// 使用 path 处理 zip 内的 '/' 分隔；拒绝任何未归一化（含 ".." / "."）的条目名
		clean := path.Clean(name)
		if clean != name || strings.Contains(name, "..") {
			return nil, nil, warnings, fmt.Errorf("备份包含非法路径（存在穿越风险）: %s", name)
		}

		seg := strings.Split(clean, "/")
		var rel string
		if len(seg) >= 2 {
			// 首个路径段为 zip 根目录名（如 fan-video-1.0.1-xxx）
			rel = path.Join(seg[1:]...)
		} else {
			rel = seg[0]
		}

		// manifest.json 允许出现在 zip 根目录或根目录下的任意层
		if rel == "manifest.json" {
			rc, e := f.Open()
			if e != nil {
				return nil, nil, warnings, fmt.Errorf("读取 %s 失败: %w", name, e)
			}
			data, e := io.ReadAll(io.LimitReader(rc, 16<<20))
			rc.Close()
			if e != nil {
				return nil, nil, warnings, fmt.Errorf("读取 %s 失败: %w", name, e)
			}
			var m BackupManifest
			if e := json.Unmarshal(data, &m); e != nil {
				return nil, nil, warnings, fmt.Errorf("解析备份清单失败: %w", e)
			}
			manifest = &m
			continue
		}

		if !isSafeRestoreRel(rel) {
			return nil, nil, warnings, fmt.Errorf("备份包含非法路径（存在穿越风险）: %s", name)
		}
		if !isSupportedRestoreCategory(rel) {
			return nil, nil, warnings, fmt.Errorf("备份包含未知目录条目，已拒绝: %s", name)
		}
		if strings.HasPrefix(rel, "data/") && hasDBConflict(strings.TrimPrefix(rel, "data/")) {
			// 旧版本备份可能包含运行中数据库的原始拷贝（<data_dir>/data/nowen.db*
			// 等嵌套条目）。它们不是一致性快照，且会覆盖还原期间的活库，直接跳过；
			// 权威数据库始终来自 database/nowen.db（VACUUM INTO 快照）。
			warnings = append(warnings, "跳过备份中的数据库文件条目，改用一致性快照: "+name)
			continue
		}

		dest := filepath.Join(dstDir, filepath.FromSlash(rel))
		if !pathWithin(dstDir, dest) {
			return nil, nil, warnings, fmt.Errorf("备份条目超出解压目录: %s", name)
		}
		if f.UncompressedSize64 > maxRestoredFileSize {
			return nil, nil, warnings, fmt.Errorf("备份包含过大文件（>2 GiB）: %s", name)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return nil, nil, warnings, fmt.Errorf("创建解压目录失败: %w", err)
		}
		out, err := os.Create(dest)
		if err != nil {
			return nil, nil, warnings, fmt.Errorf("解压失败: %w", err)
		}
		rc, err := f.Open()
		if err != nil {
			out.Close()
			return nil, nil, warnings, fmt.Errorf("解压 %s 失败: %w", name, err)
		}
		_, copyErr := io.Copy(out, io.LimitReader(rc, maxRestoredFileSize))
		rc.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return nil, nil, warnings, fmt.Errorf("解压 %s 失败: %w", name, copyErr)
		}
		if closeErr != nil {
			return nil, nil, warnings, fmt.Errorf("解压 %s 失败: %w", name, closeErr)
		}
		relFiles[rel] = struct{}{}
	}

	if manifest == nil {
		return nil, nil, warnings, fmt.Errorf("备份缺少 manifest.json 清单")
	}
	return manifest, relFiles, warnings, nil
}

// isSupportedRestoreCategory 判断相对路径是否属于允许的还原类别
func isSupportedRestoreCategory(rel string) bool {
	first := strings.SplitN(rel, "/", 2)[0]
	switch first {
	case "database", "config", "data":
		return true
	case "resources":
		rest := strings.TrimPrefix(rel, "resources/")
		if rest == "" {
			return false
		}
		resourceName := strings.SplitN(rest, "/", 2)[0]
		for _, rd := range backupResourceDirs {
			if rd.name == resourceName {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// isSafeRestoreRel 拒绝绝对路径、父目录回溯与空段
func isSafeRestoreRel(rel string) bool {
	if rel == "" || rel == "." {
		return false
	}
	clean := path.Clean(rel)
	if clean != rel || path.IsAbs(clean) {
		return false
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." || seg == "" || seg == "." {
			return false
		}
	}
	return true
}

// hasDBConflict 检测还原目标是否会覆盖 SQLite 数据库文件。
// 数据库既可能位于数据目录根部（<data_dir>/nowen.db），也可能位于子目录
// （二进制安装 <data_dir>/data/nowen.db），因此需逐段扫描而非只看首段。
func hasDBConflict(inner string) bool {
	segs := strings.Split(inner, "/")
	if len(segs) == 0 {
		return false
	}
	if segs[0] == ".restore" || segs[0] == "backups" {
		return true
	}
	for _, seg := range segs {
		if seg == "nowen.db" || strings.HasPrefix(seg, "nowen.db-") {
			return true
		}
	}
	return false
}

// installFile 将文件原子写回目标路径（先写临时文件再改名），并确保父目录存在
func installFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	tmp := dest + ".restoring.tmp"
	if err := copyFilePreserving(src, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// copyFilePreserving 复制文件内容并保留基本权限
func copyFilePreserving(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dest)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// pathWithin 校验 child 位于 base 之内
func pathWithin(base, child string) bool {
	rel, err := filepath.Rel(base, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
