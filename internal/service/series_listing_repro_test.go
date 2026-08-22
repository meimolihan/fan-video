package service

import (
	"path/filepath"
	"testing"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

// 复现「影视库，剧集中没有匹配到内容」：
// 用前端实际使用的 SeriesRepo.List（episode_count > 0 过滤）逐场景验证。
func TestSeriesListingRepro(t *testing.T) {
	requireFFmpeg(t)

	cases := []struct {
		name      string
		libType   string
		build     func(root string)
		wantTitle string
	}{
		{
			name:    "剧集库-标准目录-无集号文件",
			libType: "tvshow",
			build: func(root string) {
				touchVideo(t, filepath.Join(root, "庆余年", "第一集.mp4"))
				touchVideo(t, filepath.Join(root, "庆余年", "第二集.mp4"))
			},
			wantTitle: "庆余年",
		},
		{
			name:    "剧集库-Season子目录",
			libType: "tvshow",
			build: func(root string) {
				touchVideo(t, filepath.Join(root, "琅琊榜", "Season 1", "S01E01.mkv"))
				touchVideo(t, filepath.Join(root, "琅琊榜", "Season 1", "S01E02.mkv"))
				touchVideo(t, filepath.Join(root, "琅琊榜", "Season 2", "S02E01.mkv"))
			},
			wantTitle: "琅琊榜",
		},
		{
			name:    "剧集库-根散落带系列名文件",
			libType: "tvshow",
			build: func(root string) {
				touchVideo(t, filepath.Join(root, "脱口秀 第1期.mp4"))
				touchVideo(t, filepath.Join(root, "脱口秀 第2期.mp4"))
			},
			wantTitle: "",
		},
		{
			name:    "混合库-单季目录-SxxExx命名",
			libType: "mixed",
			build: func(root string) {
				touchVideo(t, filepath.Join(root, "权力的游戏", "S01E01.mp4"))
				touchVideo(t, filepath.Join(root, "权力的游戏", "S01E02.mp4"))
			},
			wantTitle: "权力的游戏",
		},
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
			tc.build(root)

			library := &model.Library{
				ID:   "lib-" + tc.libType + "-repro",
				Name: tc.name,
				Path: root,
				Type: tc.libType,
			}
			if err := db.Create(library).Error; err != nil {
				t.Fatal(err)
			}

			scanner := NewScannerService(repos.Media, repos.Series, cfg, zap.NewNop().Sugar())
			if _, err := scanner.ScanLibrary(library); err != nil {
				t.Fatal(err)
			}

			list, total, err := repos.Series.List(1, 50, library.ID)
			if err != nil {
				t.Fatal(err)
			}
			if total == 0 || len(list) == 0 {
				var all []model.Series
				db.Unscoped().Find(&all)
				t.Fatalf("剧集列表为空（前端将显示无内容）；数据库中全部 Series 行: %+v", all)
			}
			for _, s := range list {
				t.Logf("列出剧集: title=%q episode_count=%d folder=%q", s.Title, s.EpisodeCount, s.FolderPath)
			}
			if tc.wantTitle != "" && list[0].EpisodeCount <= 0 {
				t.Fatalf("%s 应有分集，实际 episode_count=%d", tc.wantTitle, list[0].EpisodeCount)
			}
		})
	}
}
