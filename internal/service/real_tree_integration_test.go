package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

// 复刻用户真实目录树结构（Video-HD），验证各结构的归类结果。
func TestScanRealTreeIntegration(t *testing.T) {
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"
	cfg.App.FFmpegPath = "ffmpeg"

	root := t.TempDir()
	touchVideo(t, filepath.Join(root, "Album", "M痴女", "011921.mp4"))
	touchVideo(t, filepath.Join(root, "Album", "M痴女", "013021.mp4"))
	touchVideo(t, filepath.Join(root, "Album", "M痴女", "031922.mp4"))
	touchVideo(t, filepath.Join(root, "Album", "M痴女", "M痴女_封面", "011921.jpg"))

	touchVideo(t, filepath.Join(root, "Album", "图鉴-HD", "000000-000.mp4"))
	touchVideo(t, filepath.Join(root, "Album", "图鉴-HD", "011823.mp4"))
	touchVideo(t, filepath.Join(root, "Album", "图鉴-HD", "012022.mp4"))
	touchVideo(t, filepath.Join(root, "Album", "图鉴-HD", "图鉴-HD_封面", "011823.jpg"))

	touchVideo(t, filepath.Join(root, "Album", "图鉴-千百", "メイリン (マンコ図鑑).mp4"))
	touchVideo(t, filepath.Join(root, "Album", "图鉴-千百", "小向美奈子 (マンコ図鑑).mp4"))
	touchVideo(t, filepath.Join(root, "Album", "图鉴-千百", "松本メイ (マンコ図鑑).mp4"))
	touchVideo(t, filepath.Join(root, "Album", "图鉴-千百", "图鉴_封面", "メイリン (マンコ図鑑).JPG"))

	touchVideo(t, filepath.Join(root, "Album", "集锦-千百", "ゆうき美羽", "ゆうき美羽 (1).mp4"))
	touchVideo(t, filepath.Join(root, "Album", "集锦-千百", "ゆうき美羽", "ゆうき美羽 (2).mp4"))
	touchVideo(t, filepath.Join(root, "Album", "集锦-千百", "ゆうき美羽", "封面", "ゆうき美羽 (1).jpg"))
	touchVideo(t, filepath.Join(root, "Album", "集锦-千百", "松本メイ", "091915.mp4"))
	touchVideo(t, filepath.Join(root, "Album", "集锦-千百", "松本メイ", "102715.mp4"))
	touchVideo(t, filepath.Join(root, "Album", "集锦-千百", "松本メイ", "松本メイ(1).mp4"))
	touchVideo(t, filepath.Join(root, "Album", "集锦-千百", "清水理紗", "清水理紗 (1).mp4"))

	touchVideo(t, filepath.Join(root, "Alone", "りおん", "040624.mp4"))
	touchVideo(t, filepath.Join(root, "Alone", "りおん", "りおん_封面", "040624.jpg"))
	touchVideo(t, filepath.Join(root, "Alone", "上原茉咲", "HEYZO-2917.mp4"))
	touchVideo(t, filepath.Join(root, "Alone", "前田陽菜", "Heyzo-0319.mp4"))

	touchVideo(t, filepath.Join(root, "America", "Saved", "Saved_003.mp4"))
	touchVideo(t, filepath.Join(root, "America", "Saved", "Saved_004.mp4"))
	touchVideo(t, filepath.Join(root, "America", "Saved", "Saved_006.MP4")) // 大写扩展名
	touchVideo(t, filepath.Join(root, "America", "Saved", "Saved_007.MP4"))
	touchVideo(t, filepath.Join(root, "America", "Saved", "Saved_封面", "Saved_003_封面", "IMG_5053.jpg"))
	touchVideo(t, filepath.Join(root, "America", "合集", "合集001", "Saved_016.MP4"))
	touchVideo(t, filepath.Join(root, "America", "合集", "合集001", "Saved_028.MP4"))
	touchVideo(t, filepath.Join(root, "America", "合集", "合集001", "合集001_封面", "Saved_016_封面", "016 1.JPG"))
	touchVideo(t, filepath.Join(root, "America", "欧报", "BabyGotBoobs.20.12.10.mp4"))
	touchVideo(t, filepath.Join(root, "America", "欧报", "BrazzersExxtra.20.05.16.mp4"))
	touchVideo(t, filepath.Join(root, "America", "欧报", "DirtyMasseur.20.11.27.mp4"))
	touchVideo(t, filepath.Join(root, "America", "欧报", "AV欧美_封面", "BabyGotBoobs.20.12.10.jpg"))

	touchVideo(t, filepath.Join(root, "Category III film", "聊斋艳谭", "聊斋艳谭1-艳魔大战.mp4"))
	touchVideo(t, filepath.Join(root, "Category III film", "聊斋艳谭", "聊斋艳谭2-五通神.mp4"))
	touchVideo(t, filepath.Join(root, "Category III film", "偷窥无罪.mp4"))

	touchVideo(t, filepath.Join(root, "MD", "JDSY", "JDSY-069.mp4"))
	touchVideo(t, filepath.Join(root, "MD", "JDSY", "JDSY-445.mp4"))
	touchVideo(t, filepath.Join(root, "MD", "JDSY", "JDSY_封面", "JDSY-069.jpg"))
	touchVideo(t, filepath.Join(root, "MD", "NANA", "NANA-01.jpg")) // 图片与视频同级
	touchVideo(t, filepath.Join(root, "MD", "NANA", "NANA-01.mp4"))
	touchVideo(t, filepath.Join(root, "MD", "NANA", "NANA-02.mp4"))
	touchVideo(t, filepath.Join(root, "MD", "Nina", "Nina-01", "Nina-01.mp4")) // 每视频一层目录
	touchVideo(t, filepath.Join(root, "MD", "Nina", "Nina-02", "Nina-02.mp4"))
	touchVideo(t, filepath.Join(root, "MD", "Nina", "Nina-03", "Nina-03.mp4"))
	touchVideo(t, filepath.Join(root, "MD", "Short", "Short (01).mp4"))
	touchVideo(t, filepath.Join(root, "MD", "Short", "Short (02).mp4"))
	touchVideo(t, filepath.Join(root, "MD", "Short", "Short (03).mp4"))
	touchVideo(t, filepath.Join(root, "MD", "苍井空", "苍井空.mp4"))

	library := &model.Library{ID: "lib-real-tree", Name: "HD库", Path: root, Type: "mixed"}
	if err := db.Create(library).Error; err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())
	if _, err := scanner.scanMixedLibrary(library); err != nil {
		t.Fatal(err)
	}

	list, total, err := repos.Series.List(1, 100, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("=== 剧集总数: %d ===", total)
	for _, s := range list {
		t.Logf("SERIES %-24q eps=%d seasons=%d", s.Title, s.EpisodeCount, s.SeasonCount)
	}
	var movieCount int64
	db.Model(&model.Media{}).
		Where("library_id = ? AND (series_id = '' OR series_id IS NULL)", library.ID).
		Count(&movieCount)
	t.Logf("=== 独立电影数: %d ===", movieCount)
	var movies []model.Media
	db.Where("library_id = ? AND (series_id = '' OR series_id IS NULL)", library.ID).Find(&movies)
	for _, m := range movies {
		t.Logf("MOVIE  %-30q path=%s", m.Title, m.FilePath)
	}

	// 关键期望（逐项断言）
	gotTitles := map[string]model.Series{}
	for _, s := range list {
		gotTitles[s.Title] = s
	}
	check := func(title string, minEps int) {
		s, ok := gotTitles[title]
		if !ok {
			t.Errorf("缺少剧集 %q；现有: %+v", title, titles(list))
			return
		}
		if s.EpisodeCount < minEps {
			t.Errorf("剧集 %q 集数应 >= %d，实际 %d", title, minEps, s.EpisodeCount)
		}
	}
	check("M痴女", 3)
	check("图鉴-HD", 3)
	check("Saved", 4)
	check("JDSY", 2)
	check("NANA", 2)
	check("Short", 3)
	// [分类层保护] 聊斋艳谭应保持独立身份，不被 Category III film 吞并
	check("聊斋艳谭", 2)

	// [嵌套单视频] Nina/Nina-XX/Nina-XX.mp4 应归组为一部剧集
	check("Nina", 3)

	// [MMDDYY 日期] Alone 人名目录（单个日期视频）应归组为人名剧集
	check("りおん", 1)
	var rionEp model.Media
	if err := db.Where("series_id IN (SELECT id FROM series WHERE title = ?)", "りおん").First(&rionEp).Error; err == nil {
		// [个人影视库] 分集标题应为真实文件名（去扩展名），而非「第1集」或日期
		if rionEp.EpisodeTitle != "040624" {
			t.Errorf("りおん 分集标题应为真实文件名 040624，实际 %q", rionEp.EpisodeTitle)
		}
		if rionEp.Title != "040624" {
			t.Errorf("りおん 分集名称应为真实文件名 040624，实际 %q", rionEp.Title)
		}
	}

	// 大写扩展名 .MP4 必须被扫描到（GLOB 区分大小写）
	var upperCount int64
	db.Model(&model.Media{}).Where("library_id = ? AND file_path GLOB ?", library.ID, "*.MP4").Count(&upperCount)
	if upperCount != 4 { // Saved_006.MP4 Saved_007.MP4 Saved_016.MP4 Saved_028.MP4
		t.Errorf(".MP4 大写扩展名应入库 4 个，实际 %d", upperCount)
	}

	// === 本地海报挂载 ===
	findSeries := func(title string) model.Series {
		s, ok := gotTitles[title]
		if !ok {
			t.Fatalf("缺少剧集 %q", title)
		}
		return s
	}
	expectSeriesPoster := func(title, wantPart string) {
		s := findSeries(title)
		if s.PosterPath == "" {
			t.Errorf("剧集 %q 应有本地海报（%s），实际为空", title, wantPart)
			return
		}
		if !strings.Contains(s.PosterPath, wantPart) {
			t.Errorf("剧集 %q 海报应位于 %s 内，实际 %s", title, wantPart, s.PosterPath)
		}
	}
	// 剧集海报：封面子目录（xxx_封面/xxx.JPG）应被 FindLocalImagesDeep 命中
	expectSeriesPoster("M痴女", "M痴女_封面"+string(filepath.Separator))
	expectSeriesPoster("图鉴-HD", "图鉴-HD_封面"+string(filepath.Separator))
	expectSeriesPoster("りおん", "りおん_封面"+string(filepath.Separator))
	// 分集海报：封面子目录中的同名图片
	var mchiEp model.Media
	db.Where("series_id = ? AND file_path LIKE ?", findSeries("M痴女").ID, "%011921.mp4").First(&mchiEp)
	if mchiEp.ID != "" && !strings.HasSuffix(mchiEp.PosterPath, "M痴女_封面"+string(filepath.Separator)+"011921.jpg") {
		t.Errorf("M痴女/011921 分集海报应指向封面子目录同名图，实际 %q", mchiEp.PosterPath)
	}
	// 分集海报：与视频同级的同名图
	var nanaEp model.Media
	db.Where("series_id = ? AND file_path LIKE ?", findSeries("NANA").ID, "%NANA-01.mp4").First(&nanaEp)
	if nanaEp.ID != "" && !strings.HasSuffix(nanaEp.PosterPath, "NANA-01.jpg") {
		t.Errorf("NANA-01 分集海报应为同级同名图 NANA-01.jpg，实际 %q", nanaEp.PosterPath)
	}
}

func titles(list []model.Series) []string {
	out := []string{}
	for _, s := range list {
		out = append(out, s.Title)
	}
	return out
}
