package service

import (
	"path/filepath"
	"testing"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

// 个人视频预览场景：文件夹名是人名，视频名是日期。
//
//	影视库/
//	  小明/
//	    2024-03-22.mp4
//	    2023-12-25.mp4
//	    20240115.mp4          （紧凑格式）
//	    2024年2月8日.mp4      （中文格式）
//	  小红/
//	    VID_20240501_223045.mp4
func TestScanMixedLibraryPersonalVideoFolders(t *testing.T) {
	requireFFmpeg(t)
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"
	cfg.App.FFmpegPath = "ffmpeg"

	root := t.TempDir()
	xiaoming := filepath.Join(root, "小明")
	touchVideo(t, filepath.Join(xiaoming, "2024-03-22.mp4"))
	touchVideo(t, filepath.Join(xiaoming, "2023-12-25.mp4"))
	touchVideo(t, filepath.Join(xiaoming, "20240115.mp4"))
	touchVideo(t, filepath.Join(xiaoming, "2024年2月8日.mp4"))
	xiaohong := filepath.Join(root, "小红")
	touchVideo(t, filepath.Join(xiaohong, "VID_20240501_223045.mp4"))
	touchVideo(t, filepath.Join(xiaohong, "VID_20240618_101112.mp4"))
	// 单个日期视频的人名目录不再归组为剧集，作为独立电影入库
	xiaogang := filepath.Join(root, "小刚")
	touchVideo(t, filepath.Join(xiaogang, "2025-01-01.mp4"))

	library := &model.Library{
		ID:   "lib-personal-video",
		Name: "个人视频库",
		Path: root,
		Type: "mixed",
	}
	if err := db.Create(library).Error; err != nil {
		t.Fatal(err)
	}
	library.EnableFileFilter = false // 测试假文件过小：Create 后显式关闭大小过滤

	scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())
	if _, err := scanner.scanMixedLibrary(library); err != nil {
		t.Fatal(err)
	}

	list, total, err := repos.Series.List(1, 50, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("应列出 2 部剧集（小明/小红，单视频的小刚不归组），实际 total=%d: %+v", total, list)
	}

	byTitle := map[string]model.Series{}
	for _, s := range list {
		byTitle[s.Title] = s
	}
	ming, okMing := byTitle["小明"]
	hong, okHong := byTitle["小红"]
	if !okMing || !okHong {
		t.Fatalf("剧集标题应为人名目录名，实际: %+v", list)
	}
	// 单日期视频目录「小刚」不再归组为剧集，而是作为独立电影入库
	_, okGang := byTitle["小刚"]
	if okGang {
		t.Fatalf("单日期视频目录「小刚」不应归组为剧集，实际被列出: %+v", list)
	}
	var gangMovie model.Media
	if err := db.Where("series_id IS NULL OR series_id = ''").Where("file_path LIKE ?", "%小刚%").First(&gangMovie).Error; err != nil {
		t.Fatalf("单日期视频目录「小刚」应作为独立电影入库: %v", err)
	}
	if gangMovie.MediaType != "movie" {
		t.Fatalf("单日期视频目录「小刚」应为 movie 类型: %+v", gangMovie)
	}
	if ming.EpisodeCount != 4 {
		t.Fatalf("小明应有 4 集，实际 %d", ming.EpisodeCount)
	}
	if hong.EpisodeCount != 2 {
		t.Fatalf("小红应有 2 集，实际 %d", hong.EpisodeCount)
	}

	// 小明的分集：按日期时间顺序编号，标题为真实文件名（去扩展名），全部在第 1 季
	var eps []model.Media
	if err := db.Where("series_id = ?", ming.ID).Order("episode_num").Find(&eps).Error; err != nil {
		t.Fatal(err)
	}
	wantTitles := []string{"2023-12-25", "20240115", "2024年2月8日", "2024-03-22"}
	for i, ep := range eps {
		if ep.SeasonNum != 1 || ep.EpisodeNum != i+1 {
			t.Fatalf("个人视频分集应为 S01 E%d（时间顺序），实际 S%02d E%02d", i+1, ep.SeasonNum, ep.SeasonNum)
		}
		if ep.EpisodeTitle != wantTitles[i] {
			t.Fatalf("第 %d 集标题应为文件名 %q，实际 %q", i+1, wantTitles[i], ep.EpisodeTitle)
		}
	}

	// 紧凑格式（手机导出）也应被识别并按文件名展示
	var hongEps []model.Media
	if err := db.Where("series_id = ?", hong.ID).Order("episode_num").Find(&hongEps).Error; err != nil {
		t.Fatal(err)
	}
	if len(hongEps) != 2 || hongEps[0].EpisodeTitle != "VID_20240501_223045" || hongEps[1].EpisodeTitle != "VID_20240618_101112" {
		t.Fatalf("手机导出命名分集标题错误: %+v", hongEps)
	}
}

// 单元测试：同一天多个文件按路径稳定排序
func TestCollectEpisodesSameDayStableOrder(t *testing.T) {
	scanner := newTestScanner(t)
	dir := t.TempDir()
	touchVideo(t, filepath.Join(dir, "20240115_b.mp4"))
	touchVideo(t, filepath.Join(dir, "20240115_a.mp4"))
	touchVideo(t, filepath.Join(dir, "2024-01-20.mp4"))

	eps := scanner.collectEpisodes(dir)
	if len(eps) != 3 {
		t.Fatalf("应收集 3 个分集，实际 %d", len(eps))
	}
	for _, ep := range eps {
		if ep.SeasonNum != 1 {
			t.Fatalf("全日期目录应归入第 1 季，实际 S%d", ep.SeasonNum)
		}
		if ep.AirDate == "" {
			t.Fatalf("%s 应识别出 AirDate", filepath.Base(ep.FilePath))
		}
	}
	// 时间顺序：同日内按文件路径稳定排序
	if base(eps[0].FilePath) != "20240115_a.mp4" || base(eps[1].FilePath) != "20240115_b.mp4" || base(eps[2].FilePath) != "2024-01-20.mp4" {
		t.Fatalf("排序错误: %s, %s, %s", base(eps[0].FilePath), base(eps[1].FilePath), base(eps[2].FilePath))
	}
	if eps[0].EpisodeNum != 1 || eps[1].EpisodeNum != 2 || eps[2].EpisodeNum != 3 {
		t.Fatalf("顺序编号错误: %d,%d,%d", eps[0].EpisodeNum, eps[1].EpisodeNum, eps[2].EpisodeNum)
	}
}

func base(p string) string { return filepath.Base(p) }
