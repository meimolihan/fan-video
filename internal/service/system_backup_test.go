package service

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fan-video/fan-video/internal/config"
)

// TestHasDBConflictNested 回归：数据库位于数据目录子目录（二进制安装
// <data_dir>/data/nowen.db）时，备份还原必须拒绝覆盖任何层级的 SQLite 文件。
func TestHasDBConflictNested(t *testing.T) {
	conflicting := []string{
		"nowen.db",
		"nowen.db-wal",
		"nowen.db-shm",
		"data/nowen.db",
		"data/nowen.db-wal",
		"data/nowen.db-shm",
		".restore/nowen.db",
		"backups/x.zip",
	}
	for _, p := range conflicting {
		if !hasDBConflict(p) {
			t.Errorf("hasDBConflict(%q) = false, want true", p)
		}
	}
	safe := []string{
		"metadata.json",
		"data/config.yaml",
		"cache/covers/a.jpg",
		".jwt_secret",
	}
	for _, p := range safe {
		if hasDBConflict(p) {
			t.Errorf("hasDBConflict(%q) = true, want false", p)
		}
	}
}

// TestIsDBFile 回归：数据库文件（主库 + WAL + SHM）无论位于数据目录根部还是
// 子目录都应被排除，避免把运行中的 WAL 瞬时状态打包进备份。
func TestIsDBFile(t *testing.T) {
	s := &SystemBackupService{cfg: &config.Config{
		Database: config.DatabaseConfig{DBPath: "/var/lib/fan-video/data/nowen.db"},
	}}
	for _, p := range []string{
		"/var/lib/fan-video/data/nowen.db",
		"/var/lib/fan-video/data/nowen.db-wal",
		"/var/lib/fan-video/data/nowen.db-shm",
	} {
		if !s.isDBFile(p) {
			t.Errorf("isDBFile(%q) = false, want true", p)
		}
	}
	for _, p := range []string{
		"/var/lib/fan-video/data/metadata.json",
		"/var/lib/fan-video/nowen.db",
		"/var/lib/fan-video/backups/x.zip",
	} {
		if s.isDBFile(p) {
			t.Errorf("isDBFile(%q) = true, want false", p)
		}
	}
}

// TestExtractBackupZipSkipsLegacyNestedDB 回归：旧版生成的备份 zip 内包含
// data/data/nowen.db* 嵌套条目；解压时应跳过并给出警告，而不是整体拒绝，
// 同时保留权威的 database/nowen.db（VACUUM INTO 快照）。
func TestExtractBackupZipSkipsLegacyNestedDB(t *testing.T) {
	root := "fan-video-1.2.4-20260903-1200"
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	entry := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	entry(root+"/database/nowen.db", "DB-SNAPSHOT")
	entry(root+"/data/data/nowen.db", "STALE-MAIN")
	entry(root+"/data/data/nowen.db-wal", "STALE-WAL")
	entry(root+"/data/data/nowen.db-shm", "STALE-SHM")
	entry(root+"/data/config.yaml", "cfg")
	entry(root+"/manifest.json", `{"format":1,"app_version":"1.2.4"}`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "legacy.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	m, files, warnings, err := extractBackupZip(&zr.Reader, filepath.Join(tmp, "unzip"))
	if err != nil {
		t.Fatalf("extractBackupZip 不应整体拒绝旧备份: %v", err)
	}
	if m == nil || m.Format != 1 {
		t.Fatalf("manifest 未解析: %+v", m)
	}
	if _, ok := files["database/nowen.db"]; !ok {
		t.Errorf("database/nowen.db 应被保留，实际 files=%v", files)
	}
	if _, ok := files["data/config.yaml"]; !ok {
		t.Errorf("data/config.yaml 应被保留")
	}
	for _, bad := range []string{
		"data/data/nowen.db",
		"data/data/nowen.db-wal",
		"data/data/nowen.db-shm",
	} {
		if _, ok := files[bad]; ok {
			t.Errorf("%s 不应被解压/还原", bad)
		}
	}
	if len(warnings) == 0 {
		t.Errorf("期望有跳过数据库文件条目的警告")
	} else if !strings.Contains(warnings[0], "database/nowen.db") && !strings.Contains(warnings[0], "nowen.db") {
		t.Errorf("警告应指向数据库条目: %q", warnings[0])
	}
}
