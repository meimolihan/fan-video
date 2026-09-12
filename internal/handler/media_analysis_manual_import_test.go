package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"github.com/fan-video/fan-video/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TestImportManualHighlightsEndpoint 端到端验证手动导入接口（路由+服务链路）。
func TestImportManualHighlightsEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Media{}, &model.VideoHighlight{}, &model.AIAnalysisTask{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.NewRepositories(db)

	cfg := &config.Config{}
	cfg.App.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"

	videoDir := filepath.Join(root, "视频库")
	if err := os.MkdirAll(videoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 手工片段：子目录模式
	clipsDir := filepath.Join(videoDir, ".highlights", "大片")
	if err := os.MkdirAll(clipsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clipsDir, "01_名场面.mp4"), []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := db.Create(&model.Media{ID: "m1", Title: "大片", FilePath: filepath.Join(videoDir, "大片.mp4")}).Error; err != nil {
		t.Fatal(err)
	}
	// 该视频已有自动片段，不应被覆盖
	if err := db.Create(&model.VideoHighlight{MediaID: "m1", Title: "自动片段", Source: "ffmpeg", StartTime: 0, EndTime: 30, Version: 2}).Error; err != nil {
		t.Fatal(err)
	}

	svc := service.NewMediaAnalysisService(cfg, repos.Media, repos.VideoHighlight, repos.AIAnalysisTask, zap.NewNop().Sugar())
	r := gin.New()
	h := NewMediaAnalysisHandler(svc, zap.NewNop().Sugar())
	r.POST("/api/admin/media-analysis/highlights-import-manual", h.ImportManualHighlights)
	r.GET("/api/admin/media-analysis/highlights-stats", h.HighlightStorageStats)

	// 首次导入
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/media-analysis/highlights-import-manual", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("导入接口应返回 200，实际 %d: %s", w.Code, w.Body.String())
	}

	var manual int64
	db.Model(&model.VideoHighlight{}).Where("source = ?", "manual").Count(&manual)
	if manual != 1 {
		t.Fatalf("期望 1 条 manual，实际 %d", manual)
	}
	var auto int64
	db.Model(&model.VideoHighlight{}).Where("source = ?", "ffmpeg").Count(&auto)
	if auto != 1 {
		t.Fatalf("自动片段不应被覆盖，实际残留 %d", auto)
	}

	// 幂等：再次导入不翻倍
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/api/admin/media-analysis/highlights-import-manual", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("第二次导入应返回 200，实际 %d: %s", w2.Code, w2.Body.String())
	}
	db.Model(&model.VideoHighlight{}).Where("source = ?", "manual").Count(&manual)
	if manual != 1 {
		t.Fatalf("重复导入后 manual 应为 1，实际 %d", manual)
	}
}