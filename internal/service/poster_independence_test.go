package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fan-video/fan-video/internal/model"
)

// 规则：优先同名图片 > 同级子目录(任意)同名图片 > 首帧兜底，且每个视频独立。
func TestFindLocalImagesForMediaPerVideoIndependence(t *testing.T) {
	svc := newTestNFOService(t)

	t.Run("同级目录同名图片优先", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "a.mp4"))
		touchVideo(t, filepath.Join(dir, "b.mp4"))
		writeFakeJPEG(t, filepath.Join(dir, "a.jpg"))

		posterA, _ := svc.FindLocalImagesForMedia(filepath.Join(dir, "a.mp4"))
		if filepath.Base(posterA) != "a.jpg" {
			t.Fatalf("a.mp4 应匹配同级同名 a.jpg，实际 %s", posterA)
		}
		posterB, _ := svc.FindLocalImagesForMedia(filepath.Join(dir, "b.mp4"))
		if strings.TrimSpace(posterB) != "" && !svc.IsFirstFrameCachePath(posterB) {
			t.Fatalf("b.mp4 无同名图时应走首帧兜底，实际 %s", posterB)
		}
	})

	t.Run("同级任意子目录中的同名图片次优先", func(t *testing.T) {
		dir := t.TempDir()
		touchVideo(t, filepath.Join(dir, "c.mp4"))
		writeFakeJPEG(t, filepath.Join(dir, "随便什么名字的封面目录", "c.png"))

		poster, _ := svc.FindLocalImagesForMedia(filepath.Join(dir, "c.mp4"))
		if !strings.HasSuffix(poster, filepath.Join("随便什么名字的封面目录", "c.png")) {
			t.Fatalf("应匹配子目录中的同名图 c.png，实际 %s", poster)
		}
	})
}

func TestIsLegacySharedCoverDetection(t *testing.T) {
	svc := newTestNFOService(t)

	if !svc.IsLegacySharedCover("/media/dir/poster.jpg") {
		t.Fatal("通用命名 poster.jpg 应判定为旧共享封面")
	}
	if !svc.IsLegacySharedCover("/media/dir/COVER.PNG") {
		t.Fatal("大小写不敏感")
	}
	if svc.IsLegacySharedCover("/media/dir/011921.jpg") {
		t.Fatal("同名规则命中的具体封面不应误判")
	}
	if svc.IsLegacySharedCover("") {
		t.Fatal("空路径应返回 false")
	}
}

func TestGetPosterPathNeverSharesGenericCover(t *testing.T) {
	db, repos, _, _, streamSvc, _, _ := newCoverTestStack(t)

	dir := t.TempDir()
	createTestVideo(t, filepath.Join(dir, "ep1.mp4"), 2)
	createTestVideo(t, filepath.Join(dir, "ep2.mp4"), 2)
	// 目录里放一张通用命名封面 —— 不允许被任何一集借用
	writeFakeJPEG(t, filepath.Join(dir, "cover.jpg"))

	library := &model.Library{ID: "lib-no-share", Name: "独立性", Path: dir, Type: "mixed"}
	if err := db.Create(library).Error; err != nil {
		t.Fatal(err)
	}

	ids := map[string]string{}
	for i, name := range []string{"ep1.mp4", "ep2.mp4"} {
		m := &model.Media{
			LibraryID:  library.ID,
			Title:      name,
			FilePath:   filepath.Join(dir, name),
			MediaType:  "episode",
			EpisodeNum: i + 1,
		}
		if err := repos.Media.Create(m); err != nil {
			t.Fatal(err)
		}
		ids[name] = m.ID
	}

	p1, err := streamSvc.GetPosterPath(ids["ep1.mp4"])
	if err != nil {
		t.Fatal(err)
	}
	p2, err := streamSvc.GetPosterPath(ids["ep2.mp4"])
	if err != nil {
		t.Fatal(err)
	}

	for label, p := range map[string]string{"ep1": p1, "ep2": p2} {
		if p == "" {
			t.Fatalf("%s 应至少得到首帧封面", label)
		}
		if strings.EqualFold(strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)), "cover") {
			t.Fatalf("%s 不得使用共享通用封面 cover.jpg，实际 %s", label, p)
		}
		if !svcIsFirstFrameOrUnderCache(t, streamSvc, p) {
			t.Fatalf("%s 无同名图时应走首帧兜底缓存，实际 %s", label, p)
		}
	}
	if p1 == p2 {
		t.Fatalf("两个视频应各自拥有独立海报，实际相同: %s", p1)
	}
}

func svcIsFirstFrameOrUnderCache(t *testing.T, streamSvc *StreamService, p string) bool {
	t.Helper()
	return streamSvc.nfoService.IsFirstFrameCachePath(p)
}

func writeFakeJPEG(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("\xFF\xD8\xFF\xe0fake"), 0644); err != nil {
		t.Fatal(err)
	}
}
