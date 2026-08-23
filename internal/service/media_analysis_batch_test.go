package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

func waitBatchIdle(t *testing.T, svc *MediaAnalysisService) BatchHighlightStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		snap := svc.SnapshotBatchHighlights()
		if !snap.Running {
			return snap
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("批量任务超时未结束")
	return BatchHighlightStatus{}
}

func TestBatchHighlightsAndClearAll(t *testing.T) {
	requireFFmpeg(t)
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)

	root := t.TempDir()
	cfg := &config.Config{}
	cfg.App.DataDir = filepath.Join(root, "data")
	cfg.Cache.CacheDir = filepath.Join(root, "cache")
	cfg.App.FFmpegPath = "ffmpeg"

	makeVideo := func(name string) *model.Media {
		p := filepath.Join(root, name+".mp4")
		createTestVideo(t, p, 3)
		m := &model.Media{ID: "batch-" + name, Title: name, FilePath: p, FileSize: 1}
		if err := db.Create(m).Error; err != nil {
			t.Fatal(err)
		}
		return m
	}
	a := makeVideo("a")
	b := makeVideo("b")
	c := makeVideo("c")
	_ = b

	// c 已有精彩片段 → 批处理应跳过
	if err := db.Create(&model.VideoHighlight{MediaID: c.ID, Title: "已有", StartTime: 0.5, EndTime: 1.5}).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewMediaAnalysisService(cfg, repos.Media, repos.VideoHighlight, repos.AIAnalysisTask, zap.NewNop().Sugar())

	status, err := svc.StartBatchHighlights()
	if err != nil {
		t.Fatalf("启动批处理失败: %v", err)
	}
	if status.Total != 3 {
		t.Fatalf("总数应为 3，实际 %d", status.Total)
	}

	final := waitBatchIdle(t, svc)
	if final.Processed != 2 || final.Skipped != 1 || final.Failed != 0 {
		t.Fatalf("结果应为 成功2/跳过1/失败0，实际 %+v", final)
	}
	if final.Remaining != 0 {
		t.Fatalf("未处理应为 0，实际 %d", final.Remaining)
	}
	if final.FinishedAt == nil {
		t.Fatal("结束后应有完成时间")
	}

	// a、b 的分析任务应成功完成（3 秒无声测试视频可能产出 0 条片段，
	// 但任务状态必须是 completed——批处理编排断言以此为准）
	for _, id := range []string{a.ID, b.ID} {
		tasks, err := repos.AIAnalysisTask.ListByMediaID(id)
		if err != nil {
			t.Fatalf("读取任务失败 %s: %v", id, err)
		}
		ok := false
		for _, task := range tasks {
			if task.Status == "completed" {
				ok = true
				break
			}
		}
		if !ok {
			t.Fatalf("%s 应有 completed 状态的分析任务: %+v", id, tasks)
		}
	}

	// 清空全部。注意：ListAllMediaIDs 只覆盖有片段记录的媒体（此处为 c），
	// 用 c 的产物目录验证文件清理。
	assetDir := filepath.Join(cfg.Cache.CacheDir, "media-analysis", c.ID, "previews")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaCount, highlightCount, err := svc.ClearAllHighlights()
	if err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	if mediaCount < 1 || highlightCount < 1 {
		t.Fatalf("清空统计异常 media=%d highlights=%d", mediaCount, highlightCount)
	}
	n, _ := repos.VideoHighlight.CountAll()
	if n != 0 {
		t.Fatalf("清空后残留 %d 条", n)
	}
	if _, err := os.Stat(filepath.Join(cfg.Cache.CacheDir, "media-analysis", c.ID)); !os.IsNotExist(err) {
		t.Fatal("清空后媒体产物目录应被删除")
	}
}
