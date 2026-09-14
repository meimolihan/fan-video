package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// setupPosterOverrideStack 构建复现「选择分集海报作为合集封面」所需服务栈。
func setupPosterOverrideStack(t *testing.T) (*gorm.DB, *repository.Repositories, *config.Config, *MetadataService, *SeriesService) {
	t.Helper()
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)

	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")

	logger := zap.NewNop().Sugar()
	nfoSvc := NewNFOService(logger, cfg)

	metadataSvc := NewMetadataService(repos.Media, repos.Series, cfg, logger)
	metadataSvc.SetNFOService(nfoSvc)

	seriesSvc := NewSeriesService(repos.Series, repos.Media, logger)

	return db, repos, cfg, metadataSvc, seriesSvc
}

func TestReproSetSeriesPosterFromMediaThenGetSeasons(t *testing.T) {
	_, repos, _, metadataSvc, seriesSvc := setupPosterOverrideStack(t)

	series := &model.Series{ID: "s-1", Title: "测试剧集", LibraryID: "lib-1"}
	if err := repos.Series.Create(series); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	ep1 := &model.Media{ID: "m-1", SeriesID: "s-1", SeasonNum: 1, EpisodeNum: 1, Title: "第一集", MediaType: "episode", FilePath: filepath.Join(dir, "e1.mp4"), PosterPath: filepath.Join(dir, "e1.jpg")}
	ep2 := &model.Media{ID: "m-2", SeriesID: "s-1", SeasonNum: 1, EpisodeNum: 2, Title: "第二集", MediaType: "episode", FilePath: filepath.Join(dir, "e2.mp4"), PosterPath: filepath.Join(dir, "e2.jpg")}
	if err := repos.Media.Create(ep1); err != nil {
		t.Fatal(err)
	}
	if err := repos.Media.Create(ep2); err != nil {
		t.Fatal(err)
	}
	// 创建真实海报文件，避免 SetSeriesPosterFromMedia 走失效兜底
	for _, p := range []string{ep1.PosterPath, ep2.PosterPath} {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("\xFF\xD8\xFF\xe0fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	t.Logf("初始 GetSeasons:")
	for _, s := range mustGetSeasons(t, seriesSvc, "s-1") {
		for _, ep := range s.Episodes {
			t.Logf("  ep=%s poster=%q", ep.ID, ep.PosterPath)
		}
	}

	// 用第一集海报手动设置合集封面
	newPath, err := metadataSvc.SetSeriesPosterFromMedia("s-1", "m-1")
	if err != nil {
		t.Fatalf("SetSeriesPosterFromMedia 失败: %v", err)
	}
	t.Logf("设置后 series poster = %s", newPath)

	fresh, err := repos.Series.FindByID("s-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("DB 中 series.PosterPath = %q", fresh.PosterPath)

	t.Logf("设置后 GetSeasons:")
	for _, s := range mustGetSeasons(t, seriesSvc, "s-1") {
		for _, ep := range s.Episodes {
			t.Logf("  ep=%s poster=%q", ep.ID, ep.PosterPath)
		}
	}

	// 断言：手动设置合集封面后，分集海报不应全部消失
	for _, s := range mustGetSeasons(t, seriesSvc, "s-1") {
		for _, ep := range s.Episodes {
			if ep.PosterPath == "" {
				t.Errorf("分集 %s 的海报被置空（剧集海报手动设置后崩溃）", ep.ID)
			}
		}
	}
}

func mustGetSeasons(t *testing.T, svc *SeriesService, seriesID string) []SeasonInfo {
	t.Helper()
	seasons, err := svc.GetSeasons(seriesID)
	if err != nil {
		t.Fatalf("GetSeasons 失败: %v", err)
	}
	return seasons
}