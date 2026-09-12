package handler

import (
	"encoding/json"
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

// 本地精彩片段生成相关接口的端到端测试（扫描/状态/删除；生成接口为后台任务，
// 通过 Service 集成测试覆盖完整切分+落盘+导入）。
func TestLocalHighlightEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()

	// 单视频目录 + 多视频目录
	soloDir := filepath.Join(root, "单视频目录")
	multiDir := filepath.Join(root, "多视频目录")
	if err := os.MkdirAll(soloDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(multiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	soloVideo := filepath.Join(soloDir, "甲.mp4")
	if err := os.WriteFile(soloVideo, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Media{}, &model.VideoHighlight{}, &model.AIAnalysisTask{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.NewRepositories(db)
	if err := db.Create(&model.Media{ID: "mSolo", Title: "甲", FilePath: soloVideo}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Media{ID: "mMulti1", Title: "乙", FilePath: filepath.Join(multiDir, "乙.mp4")}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Media{ID: "mMulti2", Title: "丙", FilePath: filepath.Join(multiDir, "丙.mp4")}).Error; err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	cfg.App.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"

	svc := service.NewMediaAnalysisService(cfg, repos.Media, repos.VideoHighlight, repos.AIAnalysisTask, zap.NewNop().Sugar())
	r := gin.New()
	h := NewMediaAnalysisHandler(svc, zap.NewNop().Sugar())
	r.GET("/api/admin/media-analysis/highlights-local/scan", h.ScanLocalHighlightDirs)
	r.GET("/api/admin/media-analysis/highlights-local/status", h.LocalHighlightGenerationStatus)
	r.POST("/api/admin/media-analysis/highlights-local/cleanup", h.CleanupLocalHighlights)
	r.POST("/api/admin/media-analysis/highlights-local/verify", h.VerifyLocalHighlights)

	// 扫描：多视频目录报错并列出原因；单视频目录（文件无效占位）不在可生成集合
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/admin/media-analysis/highlights-local/scan", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("扫描接口应返回 200，实际 %d: %s", w.Code, w.Body.String())
	}
	var scanBody struct {
		Data service.LocalHighlightScanReport `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &scanBody); err != nil {
		t.Fatal(err)
	}
	if scanBody.Data.SkippedDirs != 1 {
		t.Fatalf("期望 1 个目录被跳过，实际 %d", scanBody.Data.SkippedDirs)
	}
	if len(scanBody.Data.Skipped) != 1 || scanBody.Data.Skipped[0].Dir != multiDir {
		t.Fatalf("被跳过目录应为多视频目录 %s，实际 %+v", multiDir, scanBody.Data.Skipped)
	}
	if len(scanBody.Data.Skipped[0].Videos) != 2 {
		t.Fatalf("跳过的多视频目录应列出 2 个文件，实际 %+v", scanBody.Data.Skipped[0].Videos)
	}

	// 全量扫描模式（逐片 ffprobe）：单视频目录因文件无效不计入，多视频目录仍被跳过
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/admin/media-analysis/highlights-local/scan?full=true", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("全量扫描接口应返回 200，实际 %d: %s", w.Code, w.Body.String())
	}
	var fullBody struct {
		Data service.LocalHighlightScanReport `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &fullBody); err != nil {
		t.Fatal(err)
	}
	if !fullBody.Data.FullScan {
		t.Fatal("full=true 应标记 full_scan")
	}
	if fullBody.Data.SkippedDirs != 1 {
		t.Fatalf("全量扫描仍应跳过多视频目录，实际 %d", fullBody.Data.SkippedDirs)
	}
	if fullBody.Data.EligibleDirs != 0 {
		t.Fatalf("无效视频文件不应进入可生成集合，实际 %d", fullBody.Data.EligibleDirs)
	}

	// 增量一致性校验接口
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/admin/media-analysis/highlights-local/verify", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("校验接口应返回 200，实际 %d: %s", w.Code, w.Body.String())
	}

	// 状态：空闲快照可正常返回
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/admin/media-analysis/highlights-local/status", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("状态接口应返回 200，实际 %d: %s", w.Code, w.Body.String())
	}

	// 清理：无产物的单视频目录 no-op
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/admin/media-analysis/highlights-local/cleanup", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("清理接口应返回 200，实际 %d: %s", w.Code, w.Body.String())
	}
}
