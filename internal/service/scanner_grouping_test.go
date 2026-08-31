package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

// newTestScanner 构建仅依赖文件系统的 ScannerService（分类器测试用）
func newTestScanner(t *testing.T) *ScannerService {
	t.Helper()
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	return NewScannerService(nil, nil, cfg, zap.NewNop().Sugar())
}

func touchVideo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestIsSameDirVideoGroup(t *testing.T) {
	scanner := newTestScanner(t)

	t.Run("无剧集命名的多个视频应归组为剧集", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "下载的原始文件名1.mp4"))
		touchVideo(t, filepath.Join(dir, "下载的原始文件名2.mp4"))
		touchVideo(t, filepath.Join(dir, "03.mp4"))

		if !scanner.isSameDirVideoGroup(dir) {
			t.Fatal("同目录下 3 个视频（无剧集命名特征）应归组为一部剧集")
		}
	})

	t.Run("单个视频不归组", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "阿凡达.mkv"))

		if scanner.isSameDirVideoGroup(dir) {
			t.Fatal("目录下只有 1 个视频时不应归组为剧集")
		}
	})

	t.Run("特典与样片不参与计数", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "正片.mkv"))
		touchVideo(t, filepath.Join(dir, "sample.mp4"))
		touchVideo(t, filepath.Join(dir, "trailer.mp4"))
		touchVideo(t, filepath.Join(dir, "extras", "behind.mkv"))
		touchVideo(t, filepath.Join(dir, "正片-trailer.mkv"))

		if scanner.isSameDirVideoGroup(dir) {
			t.Fatal("单正片 + 特典/样片不应被误判为剧集")
		}
	})

	t.Run("一层子目录中的视频参与计数", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "CD1", "video.mkv"))
		touchVideo(t, filepath.Join(dir, "CD2", "video.mkv"))

		if !scanner.isSameDirVideoGroup(dir) {
			t.Fatal("子目录中的视频也应参与同目录归组判定")
		}
	})
}

func TestIsSameDirOrDatedGroupSkipsMultiGroupWrappers(t *testing.T) {
	scanner := newTestScanner(t)

	t.Run("多个多视频子目录的包装层不整体归组", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "作品A", "a1.mp4"))
		touchVideo(t, filepath.Join(dir, "作品A", "a2.mp4"))
		touchVideo(t, filepath.Join(dir, "作品B", "b1.mp4"))
		touchVideo(t, filepath.Join(dir, "作品B", "b2.mp4"))

		if scanner.isSameDirOrDatedGroup(dir) {
			t.Fatal("包装层下有多个各自含多视频的作品目录时，不应整体归组为一部剧集（应下钻让各作品独立归组）")
		}
	})

	t.Run("单包装链仍归组", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "合集", "v1.mp4"))
		touchVideo(t, filepath.Join(dir, "合集", "v2.mp4"))
		touchVideo(t, filepath.Join(dir, "合集", "v3.mp4"))

		if !scanner.isSameDirOrDatedGroup(dir) {
			t.Fatal("单一多视频子目录的包装链应维持归组行为")
		}
	})

	t.Run("嵌套单视频集合仍归组", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "Part-01", "Part-01.mp4"))
		touchVideo(t, filepath.Join(dir, "Part-02", "Part-02.mp4"))

		if !scanner.isSameDirOrDatedGroup(dir) {
			t.Fatal("各子目录恰好一个视频的嵌套集合应维持归组行为")
		}
	})

	t.Run("日期视频的多组包装层同样不整体归组", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "作品A", "2024-04-06.mp4"))
		touchVideo(t, filepath.Join(dir, "作品A", "2024-04-07.mp4"))
		touchVideo(t, filepath.Join(dir, "作品B", "2024-05-12.mp4"))

		if scanner.isSameDirOrDatedGroup(dir) {
			t.Fatal("多个日期视频子目录并列时不应整体归组")
		}
	})

	t.Run("直属多视频不受包装层判定影响", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "01.mp4"))
		touchVideo(t, filepath.Join(dir, "02.mp4"))

		if !scanner.isSameDirOrDatedGroup(dir) {
			t.Fatal("直属多视频目录应归组")
		}
	})
}

func TestIsSameDirOrDatedGroupSingleVideoNotGrouped(t *testing.T) {
	scanner := newTestScanner(t)

	t.Run("单个日期命名视频不归组为剧集", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "2025-01-01.mp4"))

		if scanner.isSameDirOrDatedGroup(dir) {
			t.Fatal("目录下只有一个日期视频不应归组为剧集，应作为独立视频处理")
		}
	})

	t.Run("单个普通视频目录不归组为剧集", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "正片.mkv"))

		if scanner.isSameDirOrDatedGroup(dir) {
			t.Fatal("目录下只有一个视频不应归组为剧集，应作为独立视频处理")
		}
	})

	t.Run("多个日期视频仍归组为剧集", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "2024-01-15.mp4"))
		touchVideo(t, filepath.Join(dir, "2024-02-20.mp4"))

		if !scanner.isSameDirOrDatedGroup(dir) {
			t.Fatal("目录下多个日期视频应归组为剧集")
		}
	})

	t.Run("单个视频+封面图片目录仍不归组", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "062723.mp4"))
		if err := os.MkdirAll(filepath.Join(dir, "_封面"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "_封面", "poster.jpg"), []byte("img"), 0644); err != nil {
			t.Fatal(err)
		}

		if scanner.isSameDirOrDatedGroup(dir) {
			t.Fatal("单个视频+封面图片目录不应归组为剧集")
		}
	})
}

func TestIsTVShowFolderStaysStrictForRoots(t *testing.T) {
	scanner := newTestScanner(t)

	root := t.TempDir()
	touchVideo(t, filepath.Join(root, "电影A.mp4"))
	touchVideo(t, filepath.Join(root, "电影B.mp4"))
	touchVideo(t, filepath.Join(root, "电影C.mp4"))

	if scanner.isTVShowFolder(root) {
		t.Fatal("媒体根下的散落电影不应因数量多被严格判定为剧集（归组由 isSameDirVideoGroup 在子目录层生效）")
	}
	if !scanner.isSameDirVideoGroup(root) {
		t.Fatal("isSameDirVideoGroup 应能识别多视频目录")
	}
}

func TestCollectEpisodesAssignsNumbersToUnnumberedFiles(t *testing.T) {
	scanner := newTestScanner(t)
	dir := t.TempDir()

	// 一部分带集号，一部分不带
	touchVideo(t, filepath.Join(dir, "第2集.mp4"))
	touchVideo(t, filepath.Join(dir, "花絮B.mp4"))
	touchVideo(t, filepath.Join(dir, "花絮A.mp4"))
	touchVideo(t, filepath.Join(dir, "第1集.mp4"))

	episodes := scanner.collectEpisodes(dir)

	numbered := map[string]int{}
	for _, ep := range episodes {
		base := filepath.Base(ep.FilePath)
		if ep.EpisodeNum <= 0 {
			t.Fatalf("%s 应获得有效集号，实际 %d", base, ep.EpisodeNum)
		}
		numbered[base] = ep.EpisodeNum
	}
	if numbered["第1集.mp4"] != 1 || numbered["第2集.mp4"] != 2 {
		t.Fatalf("已解析集号不应被改动: %+v", numbered)
	}
	// 无集号文件按路径顺序接在最大集号之后
	if numbered["花絮A.mp4"] != 3 || numbered["花絮B.mp4"] != 4 {
		t.Fatalf("无集号文件应顺序编号: %+v", numbered)
	}
}

func TestCollectEpisodesAutoNumberWhenAllUnnumbered(t *testing.T) {
	scanner := newTestScanner(t)
	dir := t.TempDir()
	touchVideo(t, filepath.Join(dir, "b.mkv"))
	touchVideo(t, filepath.Join(dir, "a.mkv"))
	touchVideo(t, filepath.Join(dir, "c.mkv"))

	episodes := scanner.collectEpisodes(dir)
	if len(episodes) != 3 {
		t.Fatalf("应收集到 3 个分集，实际 %d", len(episodes))
	}
	for i, ep := range episodes {
		if ep.EpisodeNum != i+1 {
			t.Fatalf("自动编号错误: index=%d episodeNum=%d", i, ep.EpisodeNum)
		}
	}
}

func TestScanMixedLibraryGroupsSameDirVideosAsSeries(t *testing.T) {
	requireFFmpeg(t)
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"
	cfg.App.FFmpegPath = "ffmpeg"

	root := t.TempDir()
	seriesDir := filepath.Join(root, "测试连续剧")
	for _, name := range []string{"第一段.mp4", "第二段.mp4", "第三段.mp4"} {
		touchVideo(t, filepath.Join(seriesDir, name))
	}
	movieDir := filepath.Join(root, "某电影")
	touchVideo(t, filepath.Join(movieDir, "正片.mkv"))

	library := &model.Library{
		ID:               "lib-mixed-test",
		Name:             "混合库",
		Path:             root,
		Type:             "mixed",
		EnableFileFilter: false,
	}
	if err := db.Create(library).Error; err != nil {
		t.Fatal(err)
	}
	library.EnableFileFilter = false // 测试假文件过小：Create 后显式关闭大小过滤

	scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())
	count, err := scanner.scanMixedLibrary(library)
	if err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("应入库 4 个媒体，实际 %d", count)
	}

	// 同目录视频 → 归组为一部剧集 + 自动编号的分集
	seriesList, err := repos.Series.ListByLibraryID(library.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(seriesList) != 1 {
		t.Fatalf("同目录 3 个视频应只产生 1 部剧集，实际 %d 部: %+v", len(seriesList), seriesList)
	}
	series := seriesList[0]
	if series.EpisodeCount != 3 {
		t.Fatalf("剧集中应有 3 集，实际 %d", series.EpisodeCount)
	}

	var mediaList []model.Media
	if err := db.Where("series_id = ?", series.ID).Order("episode_num").Find(&mediaList).Error; err != nil {
		t.Fatal(err)
	}
	if len(mediaList) != 3 {
		t.Fatalf("剧集下应有 3 条媒体记录，实际 %d", len(mediaList))
	}
	for i, m := range mediaList {
		if m.EpisodeNum != i+1 || m.SeasonNum != 1 || m.MediaType != "episode" {
			t.Fatalf("分集编号错误: %+v", m)
		}
	}

	// 单视频目录 → 仍是独立电影
	var movie model.Media
	if err := db.Where("series_id IS NULL OR series_id = ''").First(&movie).Error; err != nil {
		t.Fatalf("单视频目录应保持为电影: %v", err)
	}
	if movie.MediaType != "movie" {
		t.Fatalf("单视频目录应为 movie 类型: %+v", movie)
	}
}

func TestScanMixedLibraryDoesNotCollapseRootWhenSubdirsHaveEpisodes(t *testing.T) {
	requireFFmpeg(t)
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"
	cfg.App.FFmpegPath = "ffmpeg"

	// 经典混合库结构：库根下每部剧各自一个目录，分集为标准剧集命名。
	// 回归背景：isTVShowFolder(库根) 会把子目录里的分集算作根目录证据，
	// 导致整库被折叠成一部以库根目录名命名的剧集，前端「剧集」页无法正常展示。
	root := t.TempDir()
	touchVideo(t, filepath.Join(root, "权力的游戏", "S01E01.mp4"))
	touchVideo(t, filepath.Join(root, "权力的游戏", "S01E02.mp4"))
	touchVideo(t, filepath.Join(root, "西部世界", "S01E01.mp4"))
	touchVideo(t, filepath.Join(root, "西部世界", "S01E02.mp4"))

	library := &model.Library{
		ID:   "lib-root-collapse",
		Name: "混合库",
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

	// 用与前端一致的列表口径（episode_count > 0）断言
	list, total, err := repos.Series.List(1, 50, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("应列出 2 部独立剧集（而不是整库折叠成一部），实际 total=%d: %+v", total, list)
	}

	byTitle := map[string]model.Series{}
	for _, s := range list {
		if s.Title == filepath.Base(root) {
			t.Fatalf("剧集标题不应是库根目录名 %q: %+v", s.Title, list)
		}
		byTitle[s.Title] = s
	}
	got1, ok1 := byTitle["权力的游戏"]
	got2, ok2 := byTitle["西部世界"]
	if !ok1 || !ok2 {
		t.Fatalf("剧集标题应为各剧目录名，实际: %+v", list)
	}
	if got1.EpisodeCount != 2 || got2.EpisodeCount != 2 {
		t.Fatalf("分集数错误: 权力的游戏=%d 西部世界=%d", got1.EpisodeCount, got2.EpisodeCount)
	}
	for title, s := range map[string]model.Series{"权力的游戏": got1, "西部世界": got2} {
		var n int64
		if err := db.Model(&model.Media{}).Where("series_id = ?", s.ID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("%s 应有 2 条分集记录，实际 %d", title, n)
		}
	}
}

func TestScanMixedLibraryRootWithDirectEpisodeFilesBecomesSeries(t *testing.T) {
	requireFFmpeg(t)
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"
	cfg.App.FFmpegPath = "ffmpeg"

	// 库路径直接指向某剧目录（根目录直属剧集命名的分集文件）→ 整体成为一部剧集
	root := t.TempDir()
	touchVideo(t, filepath.Join(root, "S01E01.mkv"))
	touchVideo(t, filepath.Join(root, "S01E02.mkv"))
	touchVideo(t, filepath.Join(root, "S01E03.mkv"))

	library := &model.Library{
		ID:   "lib-root-direct-series",
		Name: "直指剧集目录的混合库",
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
	if total != 1 || len(list) != 1 {
		t.Fatalf("根目录直属分集应归为一部剧集，实际 total=%d: %+v", total, list)
	}
	if list[0].EpisodeCount != 3 {
		t.Fatalf("应有 3 集，实际 %d", list[0].EpisodeCount)
	}
}

func TestScanMixedLibraryKeepsLooseMoviesAtConfiguredRoot(t *testing.T) {
	requireFFmpeg(t)
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"
	cfg.App.FFmpegPath = "ffmpeg"

	root := t.TempDir()
	for _, name := range []string{"流浪地球.mkv", "疯狂动物城.mp4", "星际穿越.mp4"} {
		touchVideo(t, filepath.Join(root, name))
	}

	library := &model.Library{
		ID:   "lib-loose-movies",
		Name: "散落电影库",
		Path: root,
		Type: "mixed",
	}
	if err := db.Create(library).Error; err != nil {
		t.Fatal(err)
	}
	library.EnableFileFilter = false // 测试假文件过小：Create 后显式关闭大小过滤

	scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())
	count, err := scanner.scanMixedLibrary(library)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("应入库 3 个电影，实际 %d", count)
	}

	seriesList, err := repos.Series.ListByLibraryID(library.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(seriesList) != 0 {
		t.Fatalf("库根散落的多个电影不应被归组为一部剧集，实际 %d 部: %+v", len(seriesList), seriesList)
	}
}

func TestScanMixedLibraryRelinksPreexistingMoviesAsEpisodes(t *testing.T) {
	requireFFmpeg(t)
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"
	cfg.App.FFmpegPath = "ffmpeg"

	// 模拟用户真实场景：
	// 旧版本扫描把同目录多个视频当作独立电影入库（series_id 为空）；
	// 升级后全库重扫，这些文件应归组为剧集并补挂到剧集合集下，
	// 否则剧集因 episode_count=0 被列表过滤，前端「剧集」页一直为空。
	root := t.TempDir()
	showDir := filepath.Join(root, "测试连续剧")
	var paths []string
	for _, name := range []string{"第一集.mp4", "第二集.mp4"} {
		p := filepath.Join(showDir, name)
		touchVideo(t, p)
		paths = append(paths, p)
	}

	library := &model.Library{
		ID:   "lib-relink-mixed",
		Name: "混合库",
		Path: root,
		Type: "mixed",
	}
	if err := db.Create(library).Error; err != nil {
		t.Fatal(err)
	}
	library.EnableFileFilter = false // 测试假文件过小：Create 后显式关闭大小过滤

	// 预置「历史电影」记录（无剧集归属）
	for i, p := range paths {
		old := &model.Media{
			LibraryID: library.ID,
			Title:     filepath.Base(p),
			FilePath:  p,
			FileSize:  4,
			MediaType: "movie",
		}
		if err := db.Create(old).Error; err != nil {
			t.Fatal(err)
		}
		_ = i
	}

	scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())
	if _, err := scanner.scanMixedLibrary(library); err != nil {
		t.Fatal(err)
	}

	list, total, err := repos.Series.List(1, 50, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("重扫后应列出 1 部剧集（历史电影行需补挂），实际 total=%d: %+v", total, list)
	}
	if list[0].EpisodeCount != 2 {
		t.Fatalf("剧集应有 2 集，实际 %d", list[0].EpisodeCount)
	}

	var linked []model.Media
	if err := db.Where("series_id = ?", list[0].ID).Find(&linked).Error; err != nil {
		t.Fatal(err)
	}
	if len(linked) != 2 {
		t.Fatalf("应有 2 条媒体记录挂到剧集下，实际 %d", len(linked))
	}
	for _, m := range linked {
		if m.MediaType != "episode" || m.EpisodeNum <= 0 {
			t.Fatalf("补挂后应为有效分集: %+v", m)
		}
	}

	// 不应残留无归属的旧行
	var orphans int64
	db.Model(&model.Media{}).
		Where("library_id = ?", library.ID).
		Where("series_id IS NULL OR series_id = ''").
		Count(&orphans)
	if orphans != 0 {
		t.Fatalf("不应残留无归属媒体行，实际 %d 条", orphans)
	}
}

func TestScanMixedLibraryDrillsIntoMultiVideoWrapper(t *testing.T) {
	requireFFmpeg(t)
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"
	cfg.App.FFmpegPath = "ffmpeg"

	// 回归背景：演员/作者名等包装目录下并列多个「各自含 N 个视频」的作品目录时，
	// 旧逻辑把整棵子树折叠成一个以包装层命名的错误大剧集，
	// 用户看到的是大量「目录下有 n 个视频」的目录没有匹配成各自的剧集。
	root := t.TempDir()
	wrapper := filepath.Join(root, "演员合集")
	for _, name := range []string{"a1.mp4", "a2.mp4"} {
		touchVideo(t, filepath.Join(wrapper, "作品A", name))
	}
	for _, name := range []string{"b1.mp4", "b2.mp4"} {
		touchVideo(t, filepath.Join(wrapper, "作品B", name))
	}

	library := &model.Library{
		ID:   "lib-wrapper-drill",
		Name: "混合库",
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
		t.Fatalf("包装层应下钻为 2 部独立剧集，实际 total=%d: %+v", total, list)
	}
	if list[0].Title == "演员合集" || list[1].Title == "演员合集" {
		t.Fatalf("不应出现以包装层目录名命名的剧集: %+v", list)
	}
	for _, s := range list {
		if s.EpisodeCount != 2 {
			t.Fatalf("%s 应有 2 集，实际 %d", s.Title, s.EpisodeCount)
		}
	}
}

// TestScanMixedLibraryAlbumTree 修复用户真实库结构的回归测试：
// 库根下 N 个人名目录，每个目录含若干「MMDDYY 日期 / HEYZO 番号」命名的视频，
// 以及一个「人名_封面」子目录存放同名 JPG。
// 期望：每个人名目录各自归组为一部剧集（84 个目录 → 84 部剧），
// 且分集海报命中 _封面 子目录里的同名图片。
func TestScanMixedLibraryAlbumTree(t *testing.T) {
	requireFFmpeg(t)
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"
	cfg.App.FFmpegPath = "ffmpeg"

	type actressDir struct {
		name   string
		videos []string
		cover  string // 封面子目录名；空串表示无封面子目录
		covers []string
		stray  string // 直接放在人名目录下的杂散图片；空串表示无
	}
	dirs := []actressDir{
		{name: "いずみ美耶", videos: []string{"061220.mp4", "072023.mp4", "081620.mp4"}, cover: "いずみ美耶_封面", covers: []string{"061220.jpg", "072023.jpg", "081620.jpg"}},
		{name: "すみれ美香", videos: []string{"020522.mp4", "031321.mp4", "052121.mp4", "062620.mp4"}, cover: "すみれ美香_封面", covers: []string{"020522.jpg", "031321.jpg"}},
		{name: "メイリン", videos: []string{"123020.mp4", "HEYZO-2926.mp4"}, cover: "メイリン_封面", covers: []string{"123020.jpg", "HEYZO-2926.jpg"}},
		{name: "小衣くるみ", videos: []string{"HEYZO-2350.mp4", "HEYZO-2408.mp4", "HEYZO-2572.mp4"}, cover: "小衣くるみ_封面", covers: []string{"HEYZO-2350.jpg", "HEYZO-2408.jpg", "HEYZO-2572.jpg"}},
		{name: "川村りな", videos: []string{"022423.mp4", "042924.mp4", "HEYZO-2793.mp4", "HEYZO_3066.mp4"}, cover: "川村りな_封面", covers: []string{"022423.jpg", "HEYZO_3066.jpg"}},
		{name: "上山奈々", videos: []string{"012222.mp4", "043021.mp4", "051620.mp4", "082221.mp4", "082722.mp4", "112721.mp4", "HEYZO-2577.mp4", "HEYZO-2652.mp4", "HEYZO-2675.mp4"}, cover: "上山奈々_封面", covers: []string{"012222.jpg", "HEYZO-2577.jpg"}},
		{name: "友利七葉", videos: []string{"072922.mp4", "111621.mp4", "HEYZO-2893.mp4"}, cover: "友利七葉 _封面", covers: []string{"072922.jpg"}}, // 封面目录名带空格
		{name: "如月結衣", videos: []string{"010322.mp4", "042323.mp4", "062023_001.mp4", "081722.mp4"}, cover: "如月結衣_封面", covers: []string{"042323.jpg", "062023_001.jpg"}, stray: "042323.jpg"},
	}

	root := t.TempDir()
	for _, d := range dirs {
		for _, v := range d.videos {
			touchVideo(t, filepath.Join(root, d.name, v))
		}
		if d.cover != "" {
			for _, c := range d.covers {
				touchVideo(t, filepath.Join(root, d.name, d.cover, c))
			}
		}
		if d.stray != "" {
			if err := os.WriteFile(filepath.Join(root, d.name, d.stray), []byte("img"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}

	library := &model.Library{
		ID:   "lib-album-tree",
		Name: "混合库",
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

	list, total, err := repos.Series.List(1, 100, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	if total != int64(len(dirs)) || len(list) != len(dirs) {
		names := make([]string, 0, len(list))
		for _, s := range list {
			names = append(names, s.Title)
		}
		t.Fatalf("每个演员目录应各自成剧：期望 %d 部，实际 total=%d 列表=%v", len(dirs), total, names)
	}
	byTitle := make(map[string]*model.Series, len(list))
	for i := range list {
		byTitle[list[i].Title] = &list[i]
	}
	for _, d := range dirs {
		s, ok := byTitle[d.name]
		if !ok {
			t.Fatalf("缺少以 %s 命名的剧集: %+v", d.name, byTitle)
		}
		if s.EpisodeCount != len(d.videos) {
			t.Fatalf("%s 应有 %d 集，实际 %d", d.name, len(d.videos), s.EpisodeCount)
		}
		var n int64
		db.Model(&model.Media{}).Where("series_id = ?", s.ID).Count(&n)
		if n != int64(len(d.videos)) {
			t.Fatalf("%s 名下分集记录应为 %d 条，实际 %d", d.name, len(d.videos), n)
		}
		// 分集海报规则：有同名封面的分集必须精确指向自己的那张图；
		// 没有同名封面的分集必须有首帧兜底海报，绝不能为空或共享他人图片。
		var eps []model.Media
		db.Model(&model.Media{}).Where("series_id = ?", s.ID).Find(&eps)
		if d.cover == "" {
			continue
		}
		for _, ep := range eps {
			base := strings.TrimSuffix(filepath.Base(ep.FilePath), filepath.Ext(ep.FilePath))
			// 规则优先级：① 同级目录同名图 > ② 封面子目录同名图
			want := ""
			if d.stray != "" && strings.TrimSuffix(d.stray, filepath.Ext(d.stray)) == base {
				want = filepath.Join(root, d.name, d.stray)
			}
			if want == "" {
				for _, c := range d.covers {
					cb := strings.TrimSuffix(c, filepath.Ext(c))
					if cb == base {
						want = filepath.Join(root, d.name, d.cover, c)
						break
					}
				}
			}
			if want != "" && ep.PosterPath != want {
				t.Fatalf("%s/%s 应命中封面 %s，实际 %q", d.name, base, want, ep.PosterPath)
			}
			// 无同名封面时应有海报：真视频为首帧缓存路径；本测试的假视频无法抽帧，
			// 允许为空（运行时 GetPosterPath 会懒生成），但绝不能指向别人的图片
			if want == "" && ep.PosterPath != "" && !strings.Contains(filepath.Base(ep.PosterPath), "first") &&
				!strings.HasPrefix(filepath.Base(ep.PosterPath), base) {
				t.Fatalf("%s/%s 的兜底海报 %q 疑似共享他人封面", d.name, base, ep.PosterPath)
			}
		}
	}
}

func TestScanTVShowLibraryRelinksUnlinkedLooseFiles(t *testing.T) {
	requireFFmpeg(t)
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"
	cfg.App.FFmpegPath = "ffmpeg"

	root := t.TempDir()
	var paths []string
	for _, name := range []string{"某综艺 第1期.mp4", "某综艺 第2期.mp4"} {
		p := filepath.Join(root, name)
		touchVideo(t, p)
		paths = append(paths, p)
	}

	library := &model.Library{
		ID:   "lib-relink-tv",
		Name: "剧集库",
		Path: root,
		Type: "tvshow",
	}
	if err := db.Create(library).Error; err != nil {
		t.Fatal(err)
	}
	library.EnableFileFilter = false // 测试假文件过小：Create 后显式关闭大小过滤

	// 预置无归属的散落记录
	for _, p := range paths {
		old := &model.Media{
			LibraryID: library.ID,
			Title:     filepath.Base(p),
			FilePath:  p,
			FileSize:  4,
			MediaType: "episode",
		}
		if err := db.Create(old).Error; err != nil {
			t.Fatal(err)
		}
	}

	scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())
	if _, err := scanner.scanTVShowLibrary(library); err != nil {
		t.Fatal(err)
	}

	list, total, err := repos.Series.List(1, 50, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	if total < 1 {
		t.Fatalf("根散落文件应归类为虚拟合集并列出，实际 total=%d", total)
	}
	found := false
	for _, s := range list {
		var n int64
		db.Model(&model.Media{}).Where("series_id = ?", s.ID).Count(&n)
		if n >= 2 && s.EpisodeCount >= 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("无归属的散落文件应被补挂到同一虚拟合集下（各合集集数应 ≥2）: %+v", list)
	}
}
