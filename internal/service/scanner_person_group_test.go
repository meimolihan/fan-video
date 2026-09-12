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

// TestPersonFolderWithCoverDirGroupsIntoSeries 复现 /vol2/1000/mydisk/Video/Japanese/長峰しほ
// 结构（回归测试）：人物目录含日期子目录（各含 1 个视频 + .highlights）、纯封面图片目录，
// 以及 3+ 个日期子目录（各含 .highlights）时，都应整体归组为一部剧集而非拆散成独立电影。
// 同时保证单视频作品目录（白杞りり/031123）仍保持为独立电影。
func TestPersonFolderWithCoverDirGroupsIntoSeries(t *testing.T) {
	root := t.TempDir()
	japanese := filepath.Join(root, "Japanese")
	mkdir := func(p string) {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(pth string) {
		data := make([]byte, 4*1024*1024)
		if err := os.WriteFile(pth, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	newDateDir := func(person, date string) {
		mkdir(filepath.Join(person, date, ".highlights"))
		mk(filepath.Join(person, date, date+".mp4"))
		mk(filepath.Join(person, date, ".highlights", date+"_01_开场高能.mp4"))
	}

	// 長峰しほ：与生产完全一致（含封面目录）
	person := filepath.Join(japanese, "長峰しほ")
	mkdir(person)
	for _, d := range []string{"050522", "070522"} {
		newDateDir(person, d)
	}
	mkdir(filepath.Join(person, "長峰しほ_封面"))
	mk(filepath.Join(person, "長峰しほ_封面", "110822.webp"))

	// 余姿华：3 个日期目录
	person3 := filepath.Join(japanese, "余姿华")
	mkdir(person3)
	for _, d := range []string{"060101", "060102", "060103"} {
		newDateDir(person3, d)
	}

	// 白杞りり：单人、单作品目录 031123（含 .highlights）—— 必须保持独立电影，不归组剧集、不入库片段
	personMovie := filepath.Join(japanese, "白杞りり", "031123")
	mkdir(filepath.Join(personMovie, ".highlights"))
	mk(filepath.Join(personMovie, "031123.mp4"))
	mk(filepath.Join(personMovie, ".highlights", "031123_01_精彩片段.mp4"))

	// 上原茉咲：单人、单作品目录 HEYZO-2917（与用户提供的真实结构一致）
	// —— 同样必须保持为独立电影，不得归组为剧集，.highlights 片段不得入库
	personSingle := filepath.Join(japanese, "上原茉咲", "HEYZO-2917")
	mkdir(filepath.Join(personSingle, ".highlights"))
	mk(filepath.Join(personSingle, "HEYZO-2917.mp4"))
	mk(filepath.Join(personSingle, ".highlights", "HEYZO-2917_01_前期精彩.mp4"))
	mk(filepath.Join(personSingle, ".highlights", "HEYZO-2917_02_精彩片段.mp4"))

	db := openMemoryDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.App.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	cfg.App.FFprobePath = "ffprobe"
	cfg.App.FFmpegPath = "ffmpeg"

	scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())

	infos := scanner.collectMediaRootInfos(japanese, "mixed")
	t.Logf("媒体根展开: %d 个", len(infos))
	seen := map[string]bool{}
	for _, info := range infos {
		name := filepath.Base(info.Path)
		if !seen[name] {
			seen[name] = true
			t.Logf("  root=%s depth=%d", name, info.Depth)
		}
	}

	lib := &model.Library{ID: "c6f83b84-8da4-4d6c-b79f-41b338c7a5e3", Name: "日语电影", Path: japanese, Type: "mixed", EnableFileFilter: false}
	if err := db.Create(lib).Error; err != nil {
		t.Fatal(err)
	}
	// 预置生产中已入库的 movie 行（无剧集归属）
	for _, p := range []string{
		filepath.Join(person, "050522", "050522.mp4"),
		filepath.Join(person, "070522", "070522.mp4"),
	} {
		if err := db.Create(&model.Media{ID: "pre-" + filepath.Base(filepath.Dir(p)), LibraryID: lib.ID, Title: filepath.Base(filepath.Dir(p)), FilePath: p, MediaType: "movie", SeasonNum: 1, EpisodeNum: 0}).Error; err != nil {
			t.Fatal(err)
		}
	}

	n, err := scanner.scanMixedLibrary(lib)
	if err != nil {
		t.Fatalf("scanMixedLibrary: %v", err)
	}
	t.Logf("scanMixedLibrary 新增 %d", n)

	type row struct {
		title, mtype, series string
	}
	var rows []row
	var mediaList []model.Media
	db.Order("file_path").Find(&mediaList)
	for _, m := range mediaList {
		rows = append(rows, row{m.Title, m.MediaType, m.SeriesID})
		t.Logf("media: title=%q type=%q series=%q s%d e%d", m.Title, m.MediaType, m.SeriesID, m.SeasonNum, m.EpisodeNum)
	}
	var seriesList []model.Series
	db.Find(&seriesList)
	for _, s := range seriesList {
		t.Logf("series: title=%q folder=%q", s.Title, s.FolderPath)
	}

	// 断言：長峰しほ 与 余姿华 各归组为一部剧集
	if len(seriesList) != 2 {
		t.Fatalf("期望 2 部剧集（長峰しほ、余姿华），实际 %d 部", len(seriesList))
	}
	longSeries, err3 := repos.Series.FindByFolderPath(person)
	if err3 != nil {
		t.Fatalf("未找到長峰しほ 剧集: %v", err3)
	}
	yuSeries, err4 := repos.Series.FindByFolderPath(person3)
	if err4 != nil {
		t.Fatalf("未找到余姿华 剧集: %v", err4)
	}
	epCountFor := func(seriesID string) int {
		c := 0
		for _, m := range rows {
			if m.series == seriesID && m.mtype == "episode" {
				c++
			}
		}
		return c
	}
	if c := epCountFor(longSeries.ID); c != 2 {
		t.Fatalf("期望長峰しほ 剧集下 2 个分集，实际 %d", c)
	}
	if c := epCountFor(yuSeries.ID); c != 3 {
		t.Fatalf("期望余姿华 剧集下 3 个分集，实际 %d", c)
	}

	// 断言：单作品人物目录保持为独立电影，不归组为剧集，且 .highlights 片段不入库
	for _, m := range rows {
		if m.title == "031123" && m.mtype != "movie" {
			t.Fatalf("白杞りり/031123 应为独立电影，实际 type=%s", m.mtype)
		}
		if m.title == "HEYZO-2917" && m.mtype != "movie" {
			t.Fatalf("上原茉咲/HEYZO-2917 应为独立电影，实际 type=%s", m.mtype)
		}
		if m.title != "031123" && m.title != "HEYZO-2917" && m.title != "050522" && m.title != "070522" &&
			m.title != "060101" && m.title != "060102" && m.title != "060103" {
			t.Fatalf("不应出现额外入库的媒体: %q", m.title)
		}
	}
	// 断言：白杞りり 仍是独立电影，未进入任何剧集
	for _, m := range rows {
		if m.title == "031123" && m.series != "" {
			t.Fatalf("白杞りり/031123 不应进入剧集: series=%s", m.series)
		}
	}
}