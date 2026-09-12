package service

import (
	"path/filepath"
	"testing"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

func TestIsHiddenDirPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/vol/Video/日本/白杞りり/031123/.highlights/01_开场.mp4", true},
		{"/vol/Video/日本/白杞りり/031123/031123.mp4", false},
		{"/vol/Video/.highlights/01_开场.mp4", true},
		{"webdav://host/share/v/.thumbnails/x.mp4", true},
		{"/a/..nav/b.mp4", true},
		{"/a/./b.mp4", false},
		{".strm", false},
		{"/vol/Video/a.strm", false},
		{"..nav", false},
		{"webdav://host/share", false},
	}
	for _, c := range cases {
		if got := isHiddenDirPath(c.path); got != c.want {
			t.Errorf("isHiddenDirPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsXiaoyaSkipDirHidden(t *testing.T) {
	if !isXiaoyaSkipDir(".highlights") {
		t.Fatal(".highlights 应被视为跳过目录")
	}
	if !isXiaoyaSkipDir(".highlights/") {
		t.Fatal(".highlights/ （带尾斜杠）应被视为跳过目录")
	}
	if isXiaoyaSkipDir("highlights") {
		t.Fatal("不带点前缀的同名目录不应被误判为隐藏目录")
	}
	if !isXiaoyaSkipDir("iso") {
		t.Fatal("原有 xiaoyaSkipDirs 行为不应被破坏")
	}
}

// TestPurgeHiddenDirRecords 验证：路径位于隐藏目录（.highlights）下的存量记录被清理，
// 正常媒体记录不受影响。
func TestPurgeHiddenDirRecords(t *testing.T) {
	root := t.TempDir()
	db := openMemoryDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")

	lib := &model.Library{ID: "lib1", Name: "测试库"}
	if err := db.Create(lib).Error; err != nil {
		t.Fatal(err)
	}

	hiddenPath := filepath.Join(root, "视频", ".highlights", "01_开场.mp4")
	normalPath := filepath.Join(root, "视频", "正片.mp4")
	medias := []*model.Media{
		{ID: "m-hidden", LibraryID: lib.ID, Title: "误入库片段", FilePath: hiddenPath},
		{ID: "m-normal", LibraryID: lib.ID, Title: "正片", FilePath: normalPath},
	}
	for _, m := range medias {
		if err := db.Create(m).Error; err != nil {
			t.Fatal(err)
		}
	}

	s := &ScannerService{
		mediaRepo: repos.Media,
		cfg:       cfg,
		logger:    zap.NewNop().Sugar(),
	}
	s.purgeHiddenDirRecords(lib)

	var remaining []model.Media
	if err := db.Order("id").Find(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != "m-normal" {
		t.Fatalf("期望仅保留正片记录，实际 %+v", remaining)
	}
}