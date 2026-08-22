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
