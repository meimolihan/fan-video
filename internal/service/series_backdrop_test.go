package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

func TestGetSeriesBackdropPathPrefersDBPathThenFolderBackdrop(t *testing.T) {
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	logger := zap.NewNop().Sugar()
	svc := NewSeriesService(repos.Series, repos.Media, logger)

	root := t.TempDir()

	mkFolder := func(name string) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	writeJpg := func(path string) {
		if err := os.WriteFile(path, []byte("\xFF\xD8\xFF\xe0fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// 目录下放一张 fanart.jpg（fanart 与 backdrop 兼容）
	folder := mkFolder("剧集A")
	fanart := filepath.Join(folder, "fanart.jpg")
	writeJpg(fanart)
	folderB := mkFolder("剧集B")
	backdropDB := filepath.Join(root, "db-backdrop.jpg")
	writeJpg(backdropDB)

	// 1. 目录无任何背景图：返回空串
	plain := &model.Series{LibraryID: "lib-x", Title: "无背景图", FolderPath: mkFolder("无背景")}
	if err := repos.Series.Create(plain); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.GetSeriesBackdropPath(plain.ID); err != nil || got != "" {
		t.Fatalf("无背景图时应返回空串: got=%q err=%v", got, err)
	}

	// 2. 目录下有标准 fanart.jpg：回退命中
	withFanart := &model.Series{LibraryID: "lib-x", Title: "目录有fanart", FolderPath: folder}
	if err := repos.Series.Create(withFanart); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetSeriesBackdropPath(withFanart.ID)
	if err != nil || filepath.Clean(got) != filepath.Clean(fanart) {
		t.Fatalf("应命中目录 fanart.jpg: got=%q err=%v", got, err)
	}

	// 3. DB 路径有效时优先返回 DB 路径
	withDB := &model.Series{LibraryID: "lib-x", Title: "DB有背景", FolderPath: folderB, BackdropPath: backdropDB}
	if err := repos.Series.Create(withDB); err != nil {
		t.Fatal(err)
	}
	got, err = svc.GetSeriesBackdropPath(withDB.ID)
	if err != nil || filepath.Clean(got) != filepath.Clean(backdropDB) {
		t.Fatalf("应优先返回 DB 背景图: got=%q err=%v", got, err)
	}

	// 4. DB 路径失效时回退到目录背景图
	stale := &model.Series{
		LibraryID:    "lib-x",
		Title:        "DB路径已失效",
		FolderPath:   mkFolder("DB路径已失效"),
		BackdropPath: filepath.Join(root, "gone.jpg"),
	}
	writeJpg(filepath.Join(stale.FolderPath, "backdrop.jpg"))
	if err := repos.Series.Create(stale); err != nil {
		t.Fatal(err)
	}
	got, err = svc.GetSeriesBackdropPath(stale.ID)
	want := filepath.Join(stale.FolderPath, "backdrop.jpg")
	if err != nil || filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("DB 路径失效时应回退到目录背景图: got=%q want=%q err=%v", got, want, err)
	}
}