package service

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

// 复现用户真实场景：混合库 + 「分类层/剧名文件夹」结构。
// 影视库/
//
//	电视剧/权力的游戏/S01E01.mp4 ...
//	电视剧/西部世界/S01E01.mp4 ...
//	电影/流浪地球.mkv
func TestReproCategoryLayerMixedLibrary(t *testing.T) {
	requireFFmpeg(t)

	cases := []struct {
		name    string
		shows   int // 电视剧分类下剧集数量（<3 会触发不同的穿透启发式）
		pattern string
	}{
		{name: "多部剧-标准SxxExx命名", shows: 3, pattern: "S%02dE%02d"},
		{name: "两部剧-标准SxxExx命名", shows: 2, pattern: "S%02dE%02d"},
		{name: "一部剧-标准SxxExx命名", shows: 1, pattern: "S%02dE%02d"},
		{name: "多部剧-中文第N集命名", shows: 2, pattern: "第%d集"},
		{name: "多部剧-无命名特征", shows: 2, pattern: "%s"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			repos := repository.NewRepositories(db)
			cfg := &config.Config{}
			cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
			cfg.App.FFprobePath = "ffprobe"
			cfg.App.FFmpegPath = "ffmpeg"

			root := t.TempDir()
			tvDir := filepath.Join(root, "电视剧")
			for s := 1; s <= tc.shows; s++ {
				showDir := filepath.Join(tvDir, "测试剧集"+string(rune('A'+s)))
				for e := 1; e <= 3; e++ {
					var fname string
					switch tc.pattern {
					case "S%02dE%02d":
						fname = sprintf("S01E%02d", e)
					case "第%d集":
						fname = sprintf("第%d集", e)
					default:
						fname = sprintf("视频片段%d", e)
					}
					touchVideo(t, filepath.Join(showDir, fname+".mp4"))
				}
			}
			movieDir := filepath.Join(root, "电影")
			touchVideo(t, filepath.Join(movieDir, "流浪地球.mkv"))

			library := &model.Library{
				ID:   "lib-repro-cat",
				Name: "混合库",
				Path: root,
				Type: "mixed",
			}
			if err := db.Create(library).Error; err != nil {
				t.Fatal(err)
			}

			scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())
			count, err := scanner.ScanLibrary(library)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("扫描入库媒体数: %d", count)

			list, total, err := repos.Series.List(1, 50, library.ID)
			if err != nil {
				t.Fatal(err)
			}
			if total == 0 {
				var all []model.Series
				db.Unscoped().Find(&all)
				var media []model.Media
				db.Unscoped().Find(&media)
				t.Fatalf("剧集列表为空！DB 中全部 Series(%d 行): %+v ; 全部 Media(%d 行) 前5条:", len(all), all, len(media))
			}
			for _, s := range list {
				t.Logf("列出剧集: %q 集数=%d 路径=%q", s.Title, s.EpisodeCount, s.FolderPath)
			}
			if total < int64(tc.shows) {
				t.Fatalf("应列出至少 %d 部剧集，实际 %d", tc.shows, total)
			}
		})
	}
}

func sprintf(format string, a ...interface{}) string {
	return fmt.Sprintf(format, a...)
}
