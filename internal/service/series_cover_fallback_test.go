package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// newCoverTestStack 构建带首帧兜底能力的完整服务栈（DB + NFO + Stream + Series + Scanner）
func newCoverTestStack(t *testing.T) (*gorm.DB, *repository.Repositories, *config.Config, *NFOService, *StreamService, *SeriesService, *ScannerService) {
	t.Helper()
	ffmpegPath := requireFFmpeg(t)
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)

	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFmpegPath = ffmpegPath
	cfg.App.FFprobePath = "ffprobe"

	logger := zap.NewNop().Sugar()
	nfoSvc := NewNFOService(logger, cfg)
	execSvc, err := NewMediaExecutionService(db, cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	streamSvc := NewStreamService(repos.Media, repos.Series, execSvc, cfg, logger)
	streamSvc.SetNFOService(nfoSvc)
	seriesSvc := NewSeriesService(repos.Series, repos.Media, logger)
	seriesSvc.SetStreamService(streamSvc)
	scannerSvc := NewScannerService(repos.Media, repos.Series, cfg, logger)
	return db, repos, cfg, nfoSvc, streamSvc, seriesSvc, scannerSvc
}

func TestPickEpisodeIndexDeterministic(t *testing.T) {
	if got := pickEpisodeIndex("series-a", 1); got != 0 {
		t.Fatalf("count=1 应固定返回 0，实际 %d", got)
	}

	first := pickEpisodeIndex("series-a", 5)
	for i := 0; i < 20; i++ {
		if again := pickEpisodeIndex("series-a", 5); again != first {
			t.Fatalf("同一输入应返回相同下标（封面不能闪烁）: %d vs %d", first, again)
		}
	}

	if idx := pickEpisodeIndex("series-b", 3); idx < 0 || idx >= 3 {
		t.Fatalf("下标越界: %d", idx)
	}

	seen := map[int]bool{}
	for i := 0; i < 50; i++ {
		seen[pickEpisodeIndex(fmt.Sprintf("s-%d", i), 4)] = true
	}
	if len(seen) < 2 {
		t.Fatalf("不同剧集之间应能取到不同下标，实际分布: %v", seen)
	}
}

func TestIsFirstFrameCachePathClassification(t *testing.T) {
	svc := newTestNFOService(t)

	inside := filepath.Join(svc.firstFrameCacheDir(), "abc123.jpg")
	if !svc.IsFirstFrameCachePath(inside) {
		t.Fatalf("缓存目录内路径应被识别为借用封面: %s", inside)
	}
	outside := filepath.Join(t.TempDir(), "poster.jpg")
	if svc.IsFirstFrameCachePath(outside) {
		t.Fatalf("普通路径不应被识别为缓存封面: %s", outside)
	}
	if svc.IsFirstFrameCachePath("") {
		t.Fatal("空路径应返回 false")
	}
	var nilSvc *NFOService
	if nilSvc.IsFirstFrameCachePath("/tmp/x.jpg") {
		t.Fatal("nil 接收者应安全返回 false")
	}
}

func TestScanMixedLibrarySeriesCoverBorrowsEpisodePoster(t *testing.T) {
	db, repos, _, nfoSvc, streamSvc, seriesSvc, scanner := newCoverTestStack(t)

	root := t.TempDir()
	dir := filepath.Join(root, "流浪地球")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"part1.mp4", "part2.mp4"} {
		createTestVideo(t, filepath.Join(dir, name), 2)
	}

	library := &model.Library{ID: "lib-cover-test", Name: "封面库", Path: root, Type: "mixed"}
	if err := db.Create(library).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := scanner.scanMixedLibrary(library); err != nil {
		t.Fatal(err)
	}

	seriesList, err := repos.Series.ListByLibraryID(library.ID)
	if err != nil || len(seriesList) != 1 {
		t.Fatalf("应产生 1 部剧集: n=%d err=%v", len(seriesList), err)
	}
	series := seriesList[0]
	if series.Title != "流浪地球" {
		t.Fatalf("剧集名应为父目录名，实际: %s", series.Title)
	}

	// 无任何本地海报时：随机借用目录下一集的海报作为封面
	poster1, err := seriesSvc.GetSeriesPosterPath(series.ID)
	if err != nil || poster1 == "" {
		t.Fatalf("应回退到分集海报: poster=%s err=%v", poster1, err)
	}
	if !nfoSvc.IsFirstFrameCachePath(poster1) {
		t.Fatalf("借用的封面应是首帧缓存产物: %s", poster1)
	}
	if _, err := os.Stat(poster1); err != nil {
		t.Fatalf("封面文件应真实存在: %v", err)
	}

	// 回写校验：列表接口的 poster_path 字段随之生效
	fresh, err := repos.Series.FindByIDOnly(series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.PosterPath != poster1 {
		t.Fatalf("借用封面应回写到 series.PosterPath: %s vs %s", fresh.PosterPath, poster1)
	}

	// 稳定性：重复请求返回同一张封面，不闪烁
	poster2, err := seriesSvc.GetSeriesPosterPath(series.ID)
	if err != nil || poster2 != poster1 {
		t.Fatalf("重复请求应命中已回写的封面: %s vs %s err=%v", poster2, poster1, err)
	}

	// 分集独立性：其余分集应生成自己的首帧封面，而不是继承剧集借用的那张
	episodes, err := repos.Media.ListBySeriesID(series.ID)
	if err != nil || len(episodes) != 2 {
		t.Fatalf("应有 2 条分集记录: n=%d err=%v", len(episodes), err)
	}
	hasIndependent := false
	for _, ep := range episodes {
		p, err := streamSvc.GetPosterPath(ep.ID)
		if err != nil {
			t.Fatal(err)
		}
		if p != "" && p != poster1 {
			hasIndependent = true
		}
	}
	if !hasIndependent {
		t.Fatal("至少一集应拥有独立的首帧封面，而非全部显示同一张图")
	}
}

func TestGetSeriesPosterPathPrefersLocalArtworkOverBorrowing(t *testing.T) {
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	logger := zap.NewNop().Sugar()

	folder := t.TempDir()
	poster := filepath.Join(folder, "poster.jpg")
	if err := os.WriteFile(poster, []byte("\xFF\xD8\xFF\xe0fake"), 0644); err != nil {
		t.Fatal(err)
	}

	series := &model.Series{LibraryID: "lib-x", Title: "本地海报剧", FolderPath: folder}
	if err := repos.Series.Create(series); err != nil {
		t.Fatal(err)
	}

	// 不注入 StreamService：若实现误走分集借用路径，将拿不到任何海报
	svc := NewSeriesService(repos.Series, repos.Media, logger)
	got, err := svc.GetSeriesPosterPath(series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(poster) {
		t.Fatalf("存在本地 artwork 时不得借用分集封面: got=%s want=%s", got, poster)
	}

	fresh, err := repos.Series.FindByIDOnly(series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.PosterPath != "" {
		t.Fatalf("本地海报场景不应触发回写: %s", fresh.PosterPath)
	}
}
