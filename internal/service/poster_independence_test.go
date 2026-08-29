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
	library.EnableFileFilter = false // 测试假文件过小：Create 后显式关闭大小过滤

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

// 规则：视频目录同时匹配到「真实海报」与「首帧封面」时，
// 扫描后的 healFirstFramePosters 应删除首帧封面并把海报更新为目录海报。
func TestHealFirstFramePosterPreferDirectoryPoster(t *testing.T) {
	_, repos, _, nfoSvc, _, _, scanner := newCoverTestStack(t)

	dir := t.TempDir()
	videoPath := filepath.Join(dir, "movie.mp4")
	createTestVideo(t, videoPath, 2)

	// 模拟：媒体当前以「首帧封面」作为海报（首帧文件真实存在于缓存目录）
	firstFrame := filepath.Join(nfoSvc.firstFrameCacheDir(), "simframe.jpg")
	writeFakeJPEG(t, firstFrame)

	library := &model.Library{ID: "lib-heal", Name: "修复库", Path: dir}
	m := &model.Media{
		LibraryID:  library.ID,
		Title:      "movie",
		FilePath:   videoPath,
		MediaType:  "movie",
		PosterPath: firstFrame,
	}
	if err := repos.Media.Create(m); err != nil {
		t.Fatal(err)
	}

	// 用户随后在视频目录新增了真实海报（同名图片）
	writeFakeJPEG(t, filepath.Join(dir, "movie.jpg"))

	scanner.healFirstFramePosters(library)

	updated, err := repos.Media.FindByID(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PosterPath != filepath.Join(dir, "movie.jpg") {
		t.Fatalf("应改用目录海报 movie.jpg，实际 %s", updated.PosterPath)
	}
	if _, err := os.Stat(firstFrame); !os.IsNotExist(err) {
		t.Fatalf("首帧封面文件应被删除，实际仍存在: %v", err)
	}
}

// 规则：即使数据库海报已不是首帧（例如用户手动上传海报后），
// 只要该视频目录存在真实海报，扫描后也应清理其遗留的首帧缓存文件（孤儿文件）。
func TestHealFirstFramePosterRemovesOrphanWhenPosterAlreadyReplaced(t *testing.T) {
	_, repos, _, nfoSvc, _, _, scanner := newCoverTestStack(t)

	dir := t.TempDir()
	videoPath := filepath.Join(dir, "movie.mp4")
	createTestVideo(t, videoPath, 2)

	// 目录写真海报
	writeFakeJPEG(t, filepath.Join(dir, "movie.jpg"))
	// 数据库已指向目录海报（此前被替换）
	library := &model.Library{ID: "lib-heal3", Name: "修复库3", Path: dir}
	m := &model.Media{
		LibraryID:  library.ID,
		Title:      "movie",
		FilePath:   videoPath,
		MediaType:  "movie",
		PosterPath: filepath.Join(dir, "movie.jpg"),
	}
	if err := repos.Media.Create(m); err != nil {
		t.Fatal(err)
	}

	// 但磁盘上仍残留该视频对应的首帧缓存文件（孤儿）
	info, err := os.Stat(videoPath)
	if err != nil {
		t.Fatal(err)
	}
	key := firstFrameCacheKey(videoPath, info)
	orphan := filepath.Join(nfoSvc.firstFrameCacheDir(), key+".jpg")
	writeFakeJPEG(t, orphan)

	scanner.healFirstFramePosters(library)

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("遗留首帧缓存文件应被删除，实际仍存在: %v", err)
	}
	updated, err := repos.Media.FindByID(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PosterPath != filepath.Join(dir, "movie.jpg") {
		t.Fatalf("海报不应被改写，实际 %s", updated.PosterPath)
	}
}

// 规则：目录中没有真实海报时，应保留首帧封面、不删除也不改写。
func TestHealFirstFramePosterKeepsFallbackWhenNoDirectoryPoster(t *testing.T) {
	_, repos, _, nfoSvc, _, _, scanner := newCoverTestStack(t)

	dir := t.TempDir()
	videoPath := filepath.Join(dir, "movie.mp4")
	createTestVideo(t, videoPath, 2)

	firstFrame := filepath.Join(nfoSvc.firstFrameCacheDir(), "simframe2.jpg")
	writeFakeJPEG(t, firstFrame)

	library := &model.Library{ID: "lib-heal2", Name: "修复库2", Path: dir}
	m := &model.Media{
		LibraryID:  library.ID,
		Title:      "movie",
		FilePath:   videoPath,
		MediaType:  "movie",
		PosterPath: firstFrame,
	}
	if err := repos.Media.Create(m); err != nil {
		t.Fatal(err)
	}

	scanner.healFirstFramePosters(library)

	updated, err := repos.Media.FindByID(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PosterPath != firstFrame {
		t.Fatalf("无目录海报时应保留首帧封面，实际 %s", updated.PosterPath)
	}
	if _, err := os.Stat(firstFrame); err != nil {
		t.Fatalf("首帧封面文件不应被删除: %v", err)
	}
}

// 规则：被任一媒体引用的首帧缓存应保留，未被引用的孤儿首帧缓存应被删除，
// 临时写入文件（.tmp.）应被忽略。
func TestFirstFrameCacheGCRemovesOrphans(t *testing.T) {
	_, repos, _, nfoSvc, _, _, scanner := newCoverTestStack(t)

	cacheDir := nfoSvc.firstFrameCacheDir()

	referenced := filepath.Join(cacheDir, "refabc.jpg")
	writeFakeJPEG(t, referenced)
	orphan := filepath.Join(cacheDir, "orphan123.jpg")
	writeFakeJPEG(t, orphan)
	tmp := filepath.Join(cacheDir, ".tmpkeep.tmp.jpg")
	writeFakeJPEG(t, tmp)

	media := &model.Media{
		LibraryID:  "lib-gc",
		Title:      "gc",
		FilePath:   filepath.Join(t.TempDir(), "gc.mp4"),
		MediaType:  "movie",
		PosterPath: referenced,
	}
	if err := repos.Media.Create(media); err != nil {
		t.Fatal(err)
	}

	scanner.firstFrameCacheGC()

	if _, err := os.Stat(referenced); err != nil {
		t.Fatalf("被引用的首帧不应被删除: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("孤儿首帧应被删除，实际仍存在: %v", err)
	}
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("临时文件不应被删除: %v", err)
	}
}
