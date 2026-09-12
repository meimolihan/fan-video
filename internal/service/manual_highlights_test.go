package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TestImportManualHighlights 覆盖手动片段导入的三种归属方式：
//  1. 子目录模式（子目录名 = 视频茎）
//  2. 平铺 + 侧车 json source 消歧
//  3. 平铺 + <视频茎>_ 前缀命名消歧
//
// 并验证：自动生成片段不被覆盖、重复导入幂等、无法归属的片段被跳过。
func TestImportManualHighlights(t *testing.T) {
	root := t.TempDir()
	db := openMemoryDB(t)
	repos := repository.NewRepositories(db)

	cfg := &config.Config{}
	cfg.App.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"

	dir1 := filepath.Join(root, "dir1")
	dir2 := filepath.Join(root, "dir2")
	dir3 := filepath.Join(root, "dir3")
	for _, d := range []string{dir1, dir2, dir3} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// 媒体库：dir1 两部、dir2 一部、dir3 一部
	mA := &model.Media{ID: "mA", LibraryID: "l1", Title: "电影A", FilePath: filepath.Join(dir1, "电影A.mp4")}
	mB := &model.Media{ID: "mB", LibraryID: "l1", Title: "电影B", FilePath: filepath.Join(dir1, "电影B.mp4")}
	mC := &model.Media{ID: "mC", LibraryID: "l1", Title: "独行侠", FilePath: filepath.Join(dir2, "独行侠.mkv")}
	mD := &model.Media{ID: "mD", LibraryID: "l1", Title: "孤儿", FilePath: filepath.Join(dir3, "孤儿.mp4")}
	for _, m := range []*model.Media{mA, mB, mC, mD} {
		if err := db.Create(m).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 电影A 已有自动生成片段（应被保留）
	if err := db.Create(&model.VideoHighlight{MediaID: "mA", Title: "自动片段", Source: "ffmpeg", StartTime: 0, EndTime: 30, Version: 2}).Error; err != nil {
		t.Fatal(err)
	}

	mk := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// dir1: 子目录模式 + 平铺侧车 source + 平铺前缀命名
	hlA := filepath.Join(dir1, ".highlights", "电影A")
	hlB := filepath.Join(dir1, ".highlights", "电影B")
	if err := os.MkdirAll(hlA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(hlB, 0o755); err != nil {
		t.Fatal(err)
	}
	mk(filepath.Join(hlA, "01_开场.mp4"), "dummy")
	mk(filepath.Join(hlA, "01_开场.thumb.webp"), "dummy")
	mk(filepath.Join(hlA, "01_开场.json"), `{"title":"经典开场","score":7.2,"tags":"开场,动作","start_time":755,"end_time":785}`)
	mk(filepath.Join(hlB, "01_高潮.mp4"), "dummy")
	mk(filepath.Join(dir1, ".highlights", "02_平铺.mp4"), "dummy")
	mk(filepath.Join(dir1, ".highlights", "02_平铺.json"), `{"title":"补刀时刻","source":"电影A.mp4","start_time":10,"end_time":40}`)
	mk(filepath.Join(dir1, ".highlights", "电影A_09_追加.mp4"), "dummy")

	// dir2: 子目录模式
	hlC := filepath.Join(dir2, ".highlights", "独行侠")
	if err := os.MkdirAll(hlC, 0o755); err != nil {
		t.Fatal(err)
	}
	mk(filepath.Join(hlC, "01_名场面.mp4"), "dummy")

	// dir3: .highlights 存在但子目录没有同名视频 → 跳过
	hlD := filepath.Join(dir3, ".highlights", "不存在的影片")
	if err := os.MkdirAll(hlD, 0o755); err != nil {
		t.Fatal(err)
	}
	mk(filepath.Join(hlD, "01_xx.mp4"), "dummy")

	svc := NewMediaAnalysisService(cfg, repos.Media, repos.VideoHighlight, repos.AIAnalysisTask, zap.NewNop().Sugar())

	report, err := svc.ImportManualHighlights()
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if report.DirsFound != 3 {
		t.Fatalf("期望发现 3 个 .highlights 目录，实际 %d", report.DirsFound)
	}
	if report.MediaScanned != 3 {
		t.Fatalf("期望写入 3 个媒体的 manual 片段，实际 %d", report.MediaScanned)
	}
	if report.ClipsImported != 5 {
		t.Fatalf("期望导入 5 条片段，实际 %d", report.ClipsImported)
	}
	if report.ClipsSkipped != 1 {
		t.Fatalf("期望跳过 1 条片段，实际 %d", report.ClipsSkipped)
	}

	// 电影A：3 条 manual + 1 条自动保留
	rowsA, err := repos.VideoHighlight.ListByMediaID("mA")
	if err != nil {
		t.Fatal(err)
	}
	if len(rowsA) != 4 {
		t.Fatalf("电影A 期望 4 条（3 manual + 1 ffmpeg），实际 %d", len(rowsA))
	}
	var manualA []model.VideoHighlight
	for _, h := range rowsA {
		if h.Source == "manual" {
			manualA = append(manualA, h)
		}
	}
	if len(manualA) != 3 {
		t.Fatalf("电影A 期望 3 条 manual，实际 %d", len(manualA))
	}
	byTitle := map[string]model.VideoHighlight{}
	hasAuto := false
	for _, h := range rowsA {
		byTitle[h.Title] = h
		if h.Source == "ffmpeg" {
			hasAuto = true
		}
	}
	if !hasAuto {
		t.Fatal("自动生成片段不应被手动导入覆盖")
	}
	if h := byTitle["经典开场"]; h.StartTime != 755 || h.EndTime != 785 || h.Score != 7.2 || h.Tags != "开场,动作" || h.AnalysisMethod != "manual" {
		t.Fatalf("侧车字段未正确导入: %+v", h)
	}
	if h := byTitle["补刀时刻"]; h.StartTime != 10 || h.EndTime != 40 {
		t.Fatalf("平铺侧车 source 消歧失败: %+v", h)
	}
	if h := byTitle["追加"]; h.Source != "manual" {
		t.Fatal("平铺前缀消歧失败")
	}
	// 开场片段应带缩略图路径
	if h := byTitle["经典开场"]; h.Thumbnail != filepath.Join(hlA, "01_开场.thumb.webp") {
		t.Fatalf("缩略图路径错误: %q", h.Thumbnail)
	}

	// 电影B：1 条 manual
	rowsB, err := repos.VideoHighlight.ListByMediaID("mB")
	if err != nil {
		t.Fatal(err)
	}
	if len(rowsB) != 1 || rowsB[0].Title != "高潮" {
		t.Fatalf("电影B 期望 1 条 manual，实际 %d %+v", len(rowsB), rowsB)
	}

	// 孤儿媒体：没有任何 manual 行
	rowsD, err := repos.VideoHighlight.ListByMediaID("mD")
	if err != nil {
		t.Fatal(err)
	}
	if len(rowsD) != 0 {
		t.Fatalf("孤儿媒体不应写入片段，实际 %d", len(rowsD))
	}

	// 重复导入幂等
	report2, err := svc.ImportManualHighlights()
	if err != nil {
		t.Fatalf("第二次导入失败: %v", err)
	}
	if report2.ClipsImported != 5 {
		t.Fatalf("第二次导入期望仍 5 条，实际 %d", report2.ClipsImported)
	}
	rowsA2, _ := repos.VideoHighlight.ListByMediaID("mA")
	if len(rowsA2) != 4 {
		t.Fatalf("重复导入后电影A 期望仍 4 条，实际 %d", len(rowsA2))
	}
}

func TestImportTimelineOnlyManualHighlights(t *testing.T) {
	root := t.TempDir()
	db := openMemoryDB(t)
	repos := repository.NewRepositories(db)

	cfg := &config.Config{}
	cfg.App.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"

	dir1 := filepath.Join(root, "dir1")
	if err := os.MkdirAll(dir1, 0o755); err != nil {
		t.Fatal(err)
	}
	mA := &model.Media{ID: "mT1", LibraryID: "l1", Title: "甲", FilePath: filepath.Join(dir1, "甲.mp4")}
	mB := &model.Media{ID: "mT2", LibraryID: "l1", Title: "乙", FilePath: filepath.Join(dir1, "乙.mp4")}
	for _, m := range []*model.Media{mA, mB} {
		if err := db.Create(m).Error; err != nil {
			t.Fatal(err)
		}
	}

	mk := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 平铺时间线模式：只有 json + thumb，无 mp4；侧车 source 消歧
	if err := os.MkdirAll(filepath.Join(dir1, ".highlights"), 0o755); err != nil {
		t.Fatal(err)
	}
	mk(filepath.Join(dir1, ".highlights", "甲_01_开场.json"), `{"title":"甲开场","source":"甲.mp4","start_time":100,"end_time":130}`)
	mk(filepath.Join(dir1, ".highlights", "甲_01_开场.thumb.webp"), "dummy")

	// 子目录时间线模式：json 无 mp4
	hlB := filepath.Join(dir1, ".highlights", "乙")
	if err := os.MkdirAll(hlB, 0o755); err != nil {
		t.Fatal(err)
	}
	mk(filepath.Join(hlB, "01_高潮.json"), `{"title":"乙高潮","start_time":30,"end_time":60}`)

	// json + mp4 同时存在：仍按片段模式导入一条（不应翻倍）
	mk(filepath.Join(dir1, ".highlights", "甲_02_结尾.mp4"), "dummy")
	mk(filepath.Join(dir1, ".highlights", "甲_02_结尾.json"), `{"title":"甲结尾","source":"甲.mp4","start_time":900,"end_time":930}`)

	// 无有效时间线的 json：应跳过
	mk(filepath.Join(dir1, ".highlights", "乙_99_无效.json"), `{"title":"无效","start_time":0,"end_time":0}`)

	svc := NewMediaAnalysisService(cfg, repos.Media, repos.VideoHighlight, repos.AIAnalysisTask, zap.NewNop().Sugar())
	report, err := svc.ImportManualHighlights()
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if report.ClipsImported != 3 {
		t.Fatalf("期望导入 3 条片段，实际 %d", report.ClipsImported)
	}
	if report.ClipsSkipped != 1 {
		t.Fatalf("期望跳过 1 条片段，实际 %d", report.ClipsSkipped)
	}

	// 甲：2 条 manual（时间线 + 带 mp4 片段）
	rowsA, err := repos.VideoHighlight.ListByMediaID("mT1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rowsA) != 2 {
		t.Fatalf("甲 期望 2 条 manual，实际 %d", len(rowsA))
	}
	byTitle := map[string]model.VideoHighlight{}
	for _, h := range rowsA {
		byTitle[h.Title] = h
	}
	if h := byTitle["甲开场"]; h.StartTime != 100 || h.EndTime != 130 || h.Thumbnail != filepath.Join(dir1, ".highlights", "甲_01_开场.thumb.webp") {
		t.Fatalf("时间线片段导入错误: %+v", h)
	}
	if h := byTitle["甲结尾"]; h.StartTime != 900 || h.EndTime != 930 {
		t.Fatalf("带 mp4 的 sidecar 未正确导入: %+v", h)
	}

	// 乙：1 条 manual（子目录时间线）
	rowsB, err := repos.VideoHighlight.ListByMediaID("mT2")
	if err != nil {
		t.Fatal(err)
	}
	if len(rowsB) != 1 || rowsB[0].Title != "乙高潮" || rowsB[0].StartTime != 30 || rowsB[0].EndTime != 60 {
		t.Fatalf("乙 子目录时间线导入错误: %+v", rowsB)
	}

	// 重复导入幂等
	report2, err := svc.ImportManualHighlights()
	if err != nil {
		t.Fatalf("第二次导入失败: %v", err)
	}
	if report2.ClipsImported != 3 {
		t.Fatalf("重复导入期望仍 3 条，实际 %d", report2.ClipsImported)
	}
}

func openMemoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Media{}, &model.VideoHighlight{}, &model.AIAnalysisTask{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}
