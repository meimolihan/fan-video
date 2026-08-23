package handler

import (
	"net/http"
	"net/http/httptest"
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

// TestClearAllHighlightsEndpoint 端到端验证清空接口（绕过鉴权中间件，仅测路由+服务链路）。
func TestClearAllHighlightsEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
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

	media := &model.Media{ID: "m1", Title: "t", FilePath: "/nonexistent/a.mp4", FileSize: 1}
	if err := db.Create(media).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VideoHighlight{MediaID: media.ID, Title: "h", StartTime: 0, EndTime: 1}).Error; err != nil {
		t.Fatal(err)
	}

	svc := service.NewMediaAnalysisService(cfg, repos.Media, repos.VideoHighlight, repos.AIAnalysisTask, zap.NewNop().Sugar())
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewMediaAnalysisHandler(svc, zap.NewNop().Sugar())
	r.DELETE("/api/admin/media-analysis/highlights-all", h.ClearAllHighlights)
	r.GET("/api/admin/media-analysis/batch/status", h.BatchHighlightsStatus)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/media-analysis/highlights-all", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("清空接口应返回 200，实际 %d: %s", w.Code, w.Body.String())
	}

	var n int64
	db.Model(&model.VideoHighlight{}).Count(&n)
	if n != 0 {
		t.Fatalf("清空后残留 %d 条", n)
	}
}
