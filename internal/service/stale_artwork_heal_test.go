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

// TestHealStaleArtworkRebindsRenamedPoster 复现：封面文件被重命名（.jpg -> .webp）后，
// 数据库仍记录旧 .jpg 路径。执行扫描级失效封面修复时，应重新匹配到同名 .webp 并回写。
func TestHealStaleArtworkRebindsRenamedPoster(t *testing.T) {
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")

	root := t.TempDir()
	video := filepath.Join(root, "100121.mp4")
	touchVideo(t, video)

	// 旧版封面：数据库指向的路径（模拟重命名为 .webp 后已不存在）
	stalePoster := filepath.Join(root, "100121.jpg")
	// 新的同名 .webp 已存在于磁盘
	newPoster := filepath.Join(root, "100121.webp")
	if err := os.WriteFile(newPoster, []byte{0xFF, 0xD8, 0xFF, 0xDB}, 0644); err != nil {
		t.Fatal(err)
	}

	library := &model.Library{
		ID:   "lib-stale-art",
		Name: "失效封面库",
		Path: root,
		Type: "movie",
	}
	if err := db.Create(library).Error; err != nil {
		t.Fatal(err)
	}
	media := &model.Media{
		ID:         "m-stale-art",
		Title:      "重命名封面影片",
		LibraryID:  library.ID,
		FilePath:   video,
		MediaType:  "movie",
		PosterPath: stalePoster,
	}
	if err := repos.Media.Create(media); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stalePoster); !os.IsNotExist(err) {
		t.Fatalf("前置条件失败：旧封面路径不应仍然存在: %v", err)
	}

	scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())
	scanner.healStaleArtwork(library)

	updated, err := repos.Media.FindByID(media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PosterPath != newPoster {
		t.Fatalf("失效封面应重新绑定到同名 .webp: want=%q got=%q", newPoster, updated.PosterPath)
	}
}

// TestHealStaleArtworkSkipsMissingVideo 视频文件本身已删除的媒体不应参与失效封面修复
// （该记录应由失效媒体清理逻辑处理，避免对已消失的视频做无意义的首帧提取）。
func TestHealStaleArtworkSkipsMissingVideo(t *testing.T) {
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")

	root := t.TempDir()
	video := filepath.Join(root, "gone.mp4")

	library := &model.Library{
		ID:   "lib-stale-art2",
		Name: "失效封面库2",
		Path: root,
		Type: "movie",
	}
	if err := db.Create(library).Error; err != nil {
		t.Fatal(err)
	}
	media := &model.Media{
		ID:         "m-stale-art2",
		Title:      "已删除视频",
		LibraryID:  library.ID,
		FilePath:   video,
		MediaType:  "movie",
		PosterPath: filepath.Join(root, "poster.jpg"),
	}
	if err := repos.Media.Create(media); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())
	scanner.healStaleArtwork(library)

	updated, err := repos.Media.FindByID(media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PosterPath != media.PosterPath {
		t.Fatalf("视频已删除时不应改写封面路径: want=%q got=%q", media.PosterPath, updated.PosterPath)
	}
}

// TestHealStaleArtworkKeepsValidPoster 封面路径仍然有效时，不应重复匹配或改写。
func TestHealStaleArtworkKeepsValidPoster(t *testing.T) {
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")

	root := t.TempDir()
	video := filepath.Join(root, "valid.mp4")
	touchVideo(t, video)
	validPoster := filepath.Join(root, "poster.jpg")
	if err := os.WriteFile(validPoster, []byte{0xFF, 0xD8, 0xFF, 0xDB}, 0644); err != nil {
		t.Fatal(err)
	}

	library := &model.Library{
		ID:   "lib-stale-art3",
		Name: "失效封面库3",
		Path: root,
		Type: "movie",
	}
	if err := db.Create(library).Error; err != nil {
		t.Fatal(err)
	}
	media := &model.Media{
		ID:         "m-stale-art3",
		Title:      "有效封面影片",
		LibraryID:  library.ID,
		FilePath:   video,
		MediaType:  "movie",
		PosterPath: validPoster,
	}
	if err := repos.Media.Create(media); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())
	scanner.healStaleArtwork(library)

	updated, err := repos.Media.FindByID(media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PosterPath != validPoster {
		t.Fatalf("有效封面不应被改写: want=%q got=%q", validPoster, updated.PosterPath)
	}
}