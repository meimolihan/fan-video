package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

func TestPlanLocalHighlightSegments(t *testing.T) {
	// 200s → n = 200/30 + 1 = 7 段 × 28s
	segs := planLocalHighlightSegments(200)
	if len(segs) != 7 {
		t.Fatalf("期望 7 段，实际 %d", len(segs))
	}
	want := []struct {
		idx   int
		start float64
		end   float64
		title string
	}{
		{1, 0, 28, "开场高能"},
		{2, 29, 57, "前期精彩"},
		{3, 57, 85, "精彩片段"},
		{4, 86, 114, "中点高潮"},
		{5, 115, 143, "后期转折"},
	}
	for i, w := range want {
		s := segs[i]
		if s.Index != w.idx || s.Start != w.start || s.End != w.end || s.Title != w.title {
			t.Fatalf("第 %d 段不匹配: %+v (期望 %+v)", i, s, w)
		}
	}
	if segs[6].Title != "结局高潮" {
		t.Fatalf("末段应为结局高潮，实际 %s", segs[6].Title)
	}

	// 短片 <15s → 单段「完整片段」
	short := planLocalHighlightSegments(10)
	if len(short) != 1 || short[0].Title != "完整片段" || short[0].Start != 0 || short[0].End != 10 {
		t.Fatalf("短片切分错误: %+v", short)
	}

	// 超长片：封顶 MAX_CLIPS=8
	long := planLocalHighlightSegments(6000)
	if len(long) != localHLMaxClips {
		t.Fatalf("超长片应封顶 %d 段，实际 %d", localHLMaxClips, len(long))
	}
}

func TestPartitionLocalHighlightDirs(t *testing.T) {
	root := t.TempDir()
	solo := filepath.Join(root, "单视频目录")
	multi := filepath.Join(root, "多视频目录")
	for _, d := range []string{solo, multi} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	videos := []model.Media{
		{ID: "m1", FilePath: filepath.Join(solo, "甲.mp4")},
		{ID: "m2", FilePath: filepath.Join(multi, "乙.mp4")},
		{ID: "m3", FilePath: filepath.Join(multi, "丙.mkv")},
		{ID: "m4", FilePath: filepath.Join(multi, "丁.mp4")},
	}
	eligible, skipped := partitionLocalHighlightDirs(videos)
	if len(eligible) != 1 || eligible[0].ID != "m1" {
		t.Fatalf("期望 1 个单视频目录（m1），实际 %+v", eligible)
	}
	if len(skipped) != 1 {
		t.Fatalf("期望 1 个多视频目录被跳过，实际 %d", len(skipped))
	}
	skip := skipped[0]
	if skip.Dir != multi {
		t.Fatalf("跳过目录应为 %s，实际 %s", multi, skip.Dir)
	}
	if len(skip.Videos) != 3 {
		t.Fatalf("期望 3 个文件被列出，实际 %+v", skip.Videos)
	}
	if !strings.Contains(skip.Reason, "3 个视频文件") || !strings.Contains(skip.Reason, "乙.mp4") {
		t.Fatalf("跳过原因应包含文件清单: %s", skip.Reason)
	}
}

func TestGenerateLocalHighlightsIntegration(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("环境无 ffmpeg，跳过集成测试")
	}

	root := t.TempDir()
	dir := filepath.Join(root, "单视频目录")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(dir, "测试大片.mp4")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=duration=10:size=160x120:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=10",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", videoPath)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("创建测试视频失败: %v: %s", err, data)
	}

	db := openMemoryDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.App.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFmpegPath = "ffmpeg"
	cfg.App.FFprobePath = "ffprobe"
	if err := db.Create(&model.Media{ID: "mGen", Title: "测试大片", FilePath: videoPath}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaAnalysisService(cfg, repos.Media, repos.VideoHighlight, repos.AIAnalysisTask, zap.NewNop().Sugar())

	status, err := svc.generateOneLocalMedia(model.Media{ID: "mGen", Title: "测试大片", FilePath: videoPath})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if status != "generated" {
		t.Fatalf("期望 generated，实际 %s", status)
	}

	// 磁盘产物：<茎>_01_完整片段.json + 缩略图
	hlDir := filepath.Join(dir, ".highlights")
	entries, err := os.ReadDir(hlDir)
	if err != nil {
		t.Fatal(err)
	}
	var jsonFiles, thumbs int
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasSuffix(name, ".json"):
			jsonFiles++
		case strings.HasSuffix(name, ".thumb.webp"), strings.HasSuffix(name, ".thumb.jpg"):
			thumbs++
		}
	}
	if jsonFiles != 1 || thumbs != 1 {
		t.Fatalf("期望 1 json + 1 缩略图，实际 json=%d thumb=%d", jsonFiles, thumbs)
	}

	// 数据库：1 条 manual，start/end 有值，title 来自侧车
	rows, err := repos.VideoHighlight.ListByMediaID("mGen")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Source != "manual" {
		t.Fatalf("期望 1 条 manual，实际 %+v", rows)
	}
	if rows[0].EndTime <= rows[0].StartTime || rows[0].Title != "完整片段" {
		t.Fatalf("manual 片段时间/标题错误: %+v", rows[0])
	}
	if rows[0].Thumbnail == "" {
		t.Fatal("手动片段应已关联缩略图路径")
	}

	// 幂等：再次运行 → already，不新增文件/记录
	status2, err2 := svc.generateOneLocalMedia(model.Media{ID: "mGen", Title: "测试大片", FilePath: videoPath})
	if err2 != nil {
		t.Fatalf("重复运行失败: %v", err2)
	}
	if status2 != "already" {
		t.Fatalf("重复运行应为 already，实际 %s", status2)
	}
	after, _ := os.ReadDir(hlDir)
	if len(after) != len(entries) {
		t.Fatalf("重复运行不应新增文件：%d → %d", len(entries), len(after))
	}
}

func TestCleanupLocalHighlights(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("环境无 ffmpeg，跳过集成测试")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "单视频目录")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(dir, "测试大片.mp4")
	if err := os.WriteFile(videoPath, []byte("not-a-real-video"), 0o644); err != nil {
		t.Fatal(err)
	}
	hlDir := filepath.Join(dir, ".highlights")
	if err := os.MkdirAll(hlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 本地生成产物 + 老的切割 mp4 + 高质量 json
	for _, f := range []string{
		"测试大片_01_开场高能.json",
		"测试大片_01_开场高能.thumb.webp",
		"测试大片_02_后续.mp4",
	} {
		if err := os.WriteFile(filepath.Join(hlDir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	db := openMemoryDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.App.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	if err := db.Create(&model.Media{ID: "mClean", Title: "测试大片", FilePath: videoPath}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VideoHighlight{MediaID: "mClean", Title: "手动", Source: "manual", StartTime: 1, EndTime: 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VideoHighlight{MediaID: "mClean", Title: "自动", Source: "ffmpeg", StartTime: 0, EndTime: 10}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaAnalysisService(cfg, repos.Media, repos.VideoHighlight, repos.AIAnalysisTask, zap.NewNop().Sugar())

	report, err := svc.CleanupLocalHighlights()
	if err != nil {
		t.Fatal(err)
	}
	if report.MediaAffected != 1 || report.FilesDeleted != 3 {
		t.Fatalf("期望清理 1 个视频 3 个文件，实际 %+v", report)
	}
	left, _ := os.ReadDir(hlDir)
	if len(left) != 0 {
		t.Fatalf(".highlights 应被清空，实际残留 %d", len(left))
	}
	if _, statErr := os.Stat(hlDir); !os.IsNotExist(statErr) {
		t.Fatal("空的 .highlights 目录应被删除")
	}

	// 数据库：manual 被删，自动保留
	var manual, auto int64
	db.Model(&model.VideoHighlight{}).Where("media_id = ? AND source = ?", "mClean", "manual").Count(&manual)
	db.Model(&model.VideoHighlight{}).Where("media_id = ? AND source = ?", "mClean", "ffmpeg").Count(&auto)
	if manual != 0 || auto != 1 {
		t.Fatalf("manual 应为 0、自动应为 1，实际 manual=%d auto=%d", manual, auto)
	}
}

func TestScanLocalHighlightDirsIncrementalNoProbe(t *testing.T) {
	root := t.TempDir()
	db := openMemoryDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.App.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	// 故意把 ffprobe 指到不存在位置：增量扫描若触发任何探测则会失败
	cfg.App.FFprobePath = filepath.Join(t.TempDir(), "no-such-ffprobe")
	// 已生成（时长缓存 10s + 真实产物）+ 待生成（无时长缓存）
	dirA := filepath.Join(root, "单视频目录")
	if err := os.MkdirAll(dirA, 0o755); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(dirA, "甲.mp4")
	if err := os.WriteFile(videoPath, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	hlDir := filepath.Join(dirA, ".highlights")
	if err := os.MkdirAll(hlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hlDir, "甲_01_完整片段.json"), []byte(`{"title":"完整片段"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hlDir, "甲_01_完整片段.thumb.webp"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := db.Create(&model.Media{
		ID: "mDone", Title: "已生成", FilePath: videoPath,
		LocalHLStatus: model.LocalHLStatusGenerated, LocalHLDuration: 10,
		LocalHLGeneratedAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	pendingPath := filepath.Join(root, "待生成目录")
	if err := os.MkdirAll(pendingPath, 0o755); err != nil {
		t.Fatal(err)
	}
	pendingVideo := filepath.Join(pendingPath, "乙.mp4")
	if err := os.WriteFile(pendingVideo, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Media{
		ID: "mPending", Title: "待生成", FilePath: pendingVideo,
		LocalHLStatus: model.LocalHLStatusPending,
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaAnalysisService(cfg, repos.Media, repos.VideoHighlight, repos.AIAnalysisTask, zap.NewNop().Sugar())

	report, err := svc.ScanLocalHighlightDirs(false)
	if err != nil {
		t.Fatalf("增量扫描不应因 ffprobe 缺失而失败: %v", err)
	}
	if report.EligibleDirs != 2 || report.AlreadyDirs != 1 {
		t.Fatalf("期望可生成 2、已生成 1，实际 eligible=%d already=%d (full=%v)", report.EligibleDirs, report.AlreadyDirs, report.FullScan)
	}
	// 明细只包含待生成项
	if len(report.Eligible) != 1 || report.Eligible[0].MediaID != "mPending" {
		t.Fatalf("明细应只含待生成 mPending，实际 %+v", report.Eligible)
	}
	if report.Eligible[0].Duration != 0 || report.Eligible[0].Clips != 0 {
		t.Fatalf("未缓存时长不应探测，Duration/Clips 应为 0，实际 %+v", report.Eligible[0])
	}
}

func TestScanIncrementalRepairsDeletedGenerated(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "单视频目录")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(dir, "甲.mp4")
	if err := os.WriteFile(videoPath, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := openMemoryDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.App.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = filepath.Join(t.TempDir(), "no-such-ffprobe")
	now := time.Now()
	if err := db.Create(&model.Media{
		ID: "mStale", Title: "已生成但产物被删", FilePath: videoPath,
		LocalHLStatus: model.LocalHLStatusGenerated, LocalHLDuration: 260,
		LocalHLGeneratedAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaAnalysisService(cfg, repos.Media, repos.VideoHighlight, repos.AIAnalysisTask, zap.NewNop().Sugar())

	report, err := svc.ScanLocalHighlightDirs(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.AlreadyDirs != 0 || len(report.Eligible) != 1 {
		t.Fatalf("产物缺失的 generated 应重置并列入待生成，实际 %+v", report)
	}
	// DB 状态应已被重置为 pending
	var st string
	if err := db.Model(&model.Media{}).Where("id = ?", "mStale").Pluck("local_hl_status", &st).Error; err != nil {
		t.Fatal(err)
	}
	if st != model.LocalHLStatusPending {
		t.Fatalf("期望状态回写 pending，实际 %s", st)
	}
}

func TestVerifyLocalHighlights(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "单视频目录")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(dir, "甲.mp4")
	if err := os.WriteFile(videoPath, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 完整产物（10s 单段「完整片段」）
	hlDir := filepath.Join(dir, ".highlights")
	if err := os.MkdirAll(hlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hlDir, "甲_01_完整片段.json"), []byte(`{"title":"完整片段"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hlDir, "甲_01_完整片段.thumb.webp"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := openMemoryDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.App.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = filepath.Join(t.TempDir(), "no-such-ffprobe")

	// 待生成但产物齐全 → 校验后补记为已生成；另一视频已生成但产物缺失 → 重置
	if err := db.Create(&model.Media{
		ID: "mFull", Title: "已齐全", FilePath: videoPath,
		LocalHLStatus: model.LocalHLStatusPending, LocalHLDuration: 10,
	}).Error; err != nil {
		t.Fatal(err)
	}
	brokenDir := filepath.Join(root, "坏目录")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	brokenVideo := filepath.Join(brokenDir, "乙.mp4")
	if err := os.WriteFile(brokenVideo, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Media{
		ID: "mBroken", Title: "产物缺失", FilePath: brokenVideo,
		LocalHLStatus: model.LocalHLStatusGenerated, LocalHLDuration: 10,
	}).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewMediaAnalysisService(cfg, repos.Media, repos.VideoHighlight, repos.AIAnalysisTask, zap.NewNop().Sugar())
	report, err := svc.VerifyLocalHighlights()
	if err != nil {
		t.Fatalf("校验不应触发 ffprobe 而失败: %v", err)
	}
	if report.Completed != 1 {
		t.Fatalf("期望 1 个待生成补记为已生成，实际 %+v", report)
	}
	if report.Repaired != 1 {
		t.Fatalf("期望 1 个破损重置为待生成，实际 %+v", report)
	}
	if report.TotalChecked != 2 {
		t.Fatalf("期望校验 2 个，实际 %+v", report)
	}
}

func TestBackfillLocalHLStatusUpgradesPending(t *testing.T) {
	db := openMemoryDB(t)
	repos := repository.NewRepositories(db)
	now := time.Now()

	// 已有 manual 片段却还是 pending（旧 source 回填残留）→ 应升级为 generated
	if err := db.Create(&model.Media{
		ID: "mUpgrade", Title: "升级候选", FilePath: "/media/a/全部.mp4",
		LocalHLStatus: model.LocalHLStatusPending,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VideoHighlight{MediaID: "mUpgrade", Title: "手动片段", Source: "manual", StartTime: 0, EndTime: 30, Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	// 已有 manual 片段且已 generated → 保持
	if err := db.Create(&model.Media{
		ID: "mKept", Title: "已生成", FilePath: "/media/b/保留.mp4",
		LocalHLStatus: model.LocalHLStatusGenerated, LocalHLDuration: 10,
		LocalHLGeneratedAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.VideoHighlight{MediaID: "mKept", Title: "手动片段", Source: "manual", StartTime: 0, EndTime: 30, Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	// 无片段仍为 pending → 保持
	if err := db.Create(&model.Media{
		ID: "mNew", Title: "新视频", FilePath: "/media/c/新的.mp4",
		LocalHLStatus: model.LocalHLStatusPending,
	}).Error; err != nil {
		t.Fatal(err)
	}
	// 远程流仍为 pending → 应升级为 skipped
	if err := db.Create(&model.Media{
		ID: "mStream", Title: "远程", FilePath: "/media/d/流.mp4", StreamURL: "http://x/y.mp4",
		LocalHLStatus: model.LocalHLStatusPending,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := repos.Media.BackfillLocalHLStatus(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"mUpgrade": model.LocalHLStatusGenerated,
		"mKept":    model.LocalHLStatusGenerated,
		"mNew":     model.LocalHLStatusPending,
		"mStream":  model.LocalHLStatusSkipped,
	}
	for id, status := range want {
		var m model.Media
		if err := db.Model(&model.Media{}).Where("id = ?", id).First(&m).Error; err != nil {
			t.Fatal(err)
		}
		if m.LocalHLStatus != status {
			t.Fatalf("media %s：期望 %s，实际 %s", id, status, m.LocalHLStatus)
		}
	}
}

func TestStartLocalHighlightGenerationIncremental(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "单视频目录")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(dir, "甲.mp4")
	if err := os.WriteFile(videoPath, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	pendingPath := filepath.Join(root, "待生成目录")
	if err := os.MkdirAll(pendingPath, 0o755); err != nil {
		t.Fatal(err)
	}
	pendingVideo := filepath.Join(pendingPath, "乙.mp4")
	if err := os.WriteFile(pendingVideo, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := openMemoryDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.App.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = filepath.Join(t.TempDir(), "no-such-ffprobe")
	cfg.App.FFmpegPath = filepath.Join(t.TempDir(), "no-such-ffmpeg")
	now := time.Now()
	// 已生成旧片：绝不应进入本轮任务
	if err := db.Create(&model.Media{
		ID: "mDone", Title: "旧片已生成", FilePath: videoPath,
		LocalHLStatus: model.LocalHLStatusGenerated, LocalHLDuration: 260,
		LocalHLGeneratedAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	// 混排目录：目录内有 1 个已生成 + 1 个待生成 → 整目录应被跳过
	mixDir := filepath.Join(root, "混排目录")
	if err := os.MkdirAll(mixDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Media{
		ID: "mMixDone", Title: "混排-已生成", FilePath: filepath.Join(mixDir, "丙.mp4"),
		LocalHLStatus: model.LocalHLStatusGenerated, LocalHLDuration: 100,
		LocalHLGeneratedAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Media{
		ID: "mMixPending", Title: "混排-待生成", FilePath: filepath.Join(mixDir, "丁.mp4"),
		LocalHLStatus: model.LocalHLStatusPending,
	}).Error; err != nil {
		t.Fatal(err)
	}
	// 独立待生成目录
	if err := db.Create(&model.Media{
		ID: "mPending", Title: "待生成", FilePath: pendingVideo,
		LocalHLStatus: model.LocalHLStatusPending,
	}).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewMediaAnalysisService(cfg, repos.Media, repos.VideoHighlight, repos.AIAnalysisTask, zap.NewNop().Sugar())
	status, err := svc.StartLocalHighlightGeneration(nil)
	if err != nil {
		t.Fatal(err)
	}
	if status.Total != 1 {
		t.Fatalf("增量任务应只处理 1 个独立待生成目录，实际 total=%d（已生成旧片、混排目录均不应计入）", status.Total)
	}
	// 等待异步任务结束并校验干净收尾
	deadline := 10
	for deadline > 0 {
		s := svc.SnapshotLocalHighlightGeneration()
		if !s.Running {
			break
		}
		time.Sleep(50 * time.Millisecond)
		deadline--
	}
	snap := svc.SnapshotLocalHighlightGeneration()
	if snap.Running {
		t.Fatal("增量任务未在预期时间内结束")
	}
	// 待生成且 ffprobe 缺失 → failed
	if snap.Failed != 1 {
		t.Fatalf("期望 1 个失败（探测失败），实际 %+v", snap)
	}
}
