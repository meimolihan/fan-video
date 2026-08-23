package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

// 过小文件过滤完整性回归：
// 1) 阈值以下的文件不入库；
// 2) 已入库的过小文件（阈值提高前入库、或文件被替换为小文件）在重扫时被清除；
// 3) 正常大小的文件不受影响。
func TestScanLibraryMinFileSizeFilter(t *testing.T) {
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"
	cfg.App.FFmpegPath = "ffmpeg"

	root := t.TempDir()
	bigPath := filepath.Join(root, "正片电影.mp4")
	smallPath := filepath.Join(root, "太小样片.mp4")
	if err := os.WriteFile(bigPath, make([]byte, 4*1024*1024), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(smallPath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	library := &model.Library{
		ID:               "lib-minsize",
		Name:             "过滤库",
		Path:             root,
		Type:             "movie",
		EnableFileFilter: true,
		MinFileSize:      3,
	}
	if err := db.Create(library).Error; err != nil {
		t.Fatal(err)
	}

	// 模拟：小文件在阈值生效前已入库（或后来被替换成小文件）
	for _, p := range []string{bigPath, smallPath} {
		m := &model.Media{
			ID:        "m-" + filepath.Base(p),
			Title:     filepath.Base(p),
			FilePath:  p,
			FileSize:  1,
			LibraryID: library.ID,
		}
		if err := db.Create(m).Error; err != nil {
			t.Fatal(err)
		}
	}

	scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())
	if _, err := scanner.ScanLibrary(library); err != nil {
		t.Fatal(err)
	}

	if m, err := repos.Media.FindByFilePath(smallPath); err == nil && m != nil {
		t.Fatal("已入库的过小文件应在重扫时被清除")
	}
	if m, err := repos.Media.FindByFilePath(bigPath); err != nil || m == nil {
		t.Fatalf("正常大小文件的记录应保留: %v", err)
	}
}

// 关闭过滤开关后，过小文件应可正常入库。
func TestScanLibraryMinFileSizeDisabled(t *testing.T) {
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"
	cfg.App.FFmpegPath = "ffmpeg"

	root := t.TempDir()
	smallPath := filepath.Join(root, "小体积视频.mp4")
	if err := os.WriteFile(smallPath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	library := &model.Library{
		ID:               "lib-minsize-off",
		Name:             "不过滤库",
		Path:             root,
		Type:             "movie",
		EnableFileFilter: false,
		MinFileSize:      3,
	}
	if err := db.Create(library).Error; err != nil {
		t.Fatal(err)
	}
	// 注意：GORM 对 default:true 的布尔列会在 Create 后把零值回填为 true，
	// 生产端（handler）是先创建再 Update 落库的，这里同样在 Create 之后置 false。
	library.EnableFileFilter = false

	scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())
	count, err := scanner.ScanLibrary(library)
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		m, findErr := repos.Media.FindByFilePath(smallPath)
		if findErr != nil || m == nil {
			t.Fatal("关闭过滤开关时应允许过小文件入库")
		}
	}
	if m, err := repos.Media.FindByFilePath(smallPath); err != nil || m == nil {
		t.Fatalf("关闭过滤开关时应允许过小文件入库: %v", err)
	}
}
