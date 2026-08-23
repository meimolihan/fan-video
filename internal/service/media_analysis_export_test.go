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

func TestExportHighlightClip(t *testing.T) {
	requireFFmpeg(t)
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)

	root := t.TempDir()
	videoPath := filepath.Join(root, "sample.mp4")
	createTestVideo(t, videoPath, 6)

	cfg := &config.Config{}
	cfg.App.DataDir = filepath.Join(root, "data")
	cfg.App.FFmpegPath = "ffmpeg"

	media := &model.Media{
		ID:       "media-export-1",
		Title:    "测试视频",
		FilePath: videoPath,
		FileSize: 1,
	}
	if err := db.Create(media).Error; err != nil {
		t.Fatal(err)
	}
	hl := &model.VideoHighlight{
		MediaID:   media.ID,
		Title:     "精彩瞬间/第一段",
		StartTime: 1,
		EndTime:   3.5,
	}
	if err := db.Create(hl).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewMediaAnalysisService(cfg, repos.Media, repos.VideoHighlight, repos.AIAnalysisTask, zap.NewNop().Sugar())

	export, err := svc.ExportHighlightClip(media.ID, hl.ID)
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if export.SizeBytes <= 0 {
		t.Fatalf("导出文件大小异常: %+v", export)
	}
	wantPath := filepath.Join(cfg.App.DataDir, "exports", "highlights", media.ID, export.FileName)
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("导出文件应位于 %s: %v", wantPath, err)
	}

	// 列表能读回
	lists, err := svc.ListHighlightExports(media.ID)
	if err != nil || len(lists) != 1 {
		t.Fatalf("ListHighlightExports 应返回 1 条，实际 %d err=%v", len(lists), err)
	}
	if lists[0].HighlightID != hl.ID || lists[0].SizeBytes != export.SizeBytes {
		t.Fatalf("导出记录不匹配: %+v vs %+v", lists[0], export)
	}

	// 不属于该媒体的片段应拒绝
	other := &model.VideoHighlight{MediaID: "other-media", Title: "x", StartTime: 0, EndTime: 1}
	if err := db.Create(other).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExportHighlightClip(media.ID, other.ID); err == nil {
		t.Fatal("跨媒体导出应被拒绝")
	}

	// 删除导出文件
	if err := svc.DeleteHighlightExport(media.ID, hl.ID); err != nil {
		t.Fatalf("删除导出失败: %v", err)
	}
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Fatal("删除后文件应不存在")
	}
}
