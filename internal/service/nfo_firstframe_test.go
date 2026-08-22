package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

// requireFFmpeg 跳过没有 ffmpeg 的环境（CI 精简镜像等）
func requireFFmpeg(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg 不可用，跳过首帧提取测试")
	}
	return path
}

// createTestVideo 用 ffmpeg lavfi 信号源生成测试视频
func createTestVideo(t *testing.T, path string, durationSeconds float64) {
	t.Helper()
	args := []string{
		"-y",
		"-f", "lavfi",
		"-i", "testsrc=duration=" + trimFloat(durationSeconds) + ":size=320x240:rate=10",
		"-pix_fmt", "yuv420p",
		path,
	}
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("生成测试视频失败 %s: %v\n%s", path, err, string(out))
	}
}

func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func newTestNFOService(t *testing.T) *NFOService {
	t.Helper()
	cfg := &config.Config{}
	cfg.App.FFmpegPath = requireFFmpeg(t)
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	return NewNFOService(zap.NewNop().Sugar(), cfg)
}

func isJPEG(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 4 {
		return false
	}
	return data[0] == 0xFF && data[1] == 0xD8
}

func TestEnsureFirstFramePosterExtractsFrame(t *testing.T) {
	svc := newTestNFOService(t)
	dir := t.TempDir()
	video := filepath.Join(dir, "movie.mp4")
	createTestVideo(t, video, 3)

	poster, err := svc.EnsureFirstFramePoster(video)
	if err != nil {
		t.Fatalf("提取首帧失败: %v", err)
	}
	if !isJPEG(poster) {
		t.Fatalf("输出不是有效 JPEG: %s", poster)
	}
	// 缓存必须落在持久化缓存目录内（而非系统临时目录）
	cacheDir := svc.cfg.Cache.CacheDir
	if !strings.HasPrefix(filepath.Clean(poster), filepath.Clean(cacheDir)) {
		t.Fatalf("缓存应位于应用缓存目录 %s 下，实际: %s", cacheDir, poster)
	}

	// 第二次调用应直接命中缓存（同一路径）
	again, err := svc.EnsureFirstFramePoster(video)
	if err != nil || again != poster {
		t.Fatalf("缓存命中失败: again=%s err=%v", again, err)
	}
}

func TestEnsureFirstFramePosterSameNameNoCollision(t *testing.T) {
	svc := newTestNFOService(t)
	dirA := t.TempDir()
	dirB := t.TempDir()
	videoA := filepath.Join(dirA, "S01E01.mp4")
	videoB := filepath.Join(dirB, "S01E01.mp4")
	createTestVideo(t, videoA, 3)
	time.Sleep(10 * time.Millisecond) // 确保 mtime 不同
	createTestVideo(t, videoB, 3)

	posterA, errA := svc.EnsureFirstFramePoster(videoA)
	posterB, errB := svc.EnsureFirstFramePoster(videoB)
	if errA != nil || errB != nil {
		t.Fatalf("提取失败: %v / %v", errA, errB)
	}
	if posterA == posterB {
		t.Fatal("不同目录下的同名视频不应共用同一张封面（串图）")
	}
	if !isJPEG(posterA) || !isJPEG(posterB) {
		t.Fatal("封面文件无效")
	}
}

func TestEnsureFirstFramePosterShortVideoFallsBackToFirstFrame(t *testing.T) {
	svc := newTestNFOService(t)
	video := filepath.Join(t.TempDir(), "short.mp4")
	createTestVideo(t, video, 0.5) // 时长不足 1 秒，seek(1s) 必然失败

	poster, err := svc.EnsureFirstFramePoster(video)
	if err != nil {
		t.Fatalf("短视频首帧回退失败: %v", err)
	}
	if !isJPEG(poster) {
		t.Fatalf("输出不是有效 JPEG: %s", poster)
	}
}

func TestEnsureFirstFramePosterCacheInvalidatesOnFileChange(t *testing.T) {
	svc := newTestNFOService(t)
	video := filepath.Join(t.TempDir(), "changed.mp4")
	createTestVideo(t, video, 3)

	first, err := svc.EnsureFirstFramePoster(video)
	if err != nil {
		t.Fatalf("首次提取失败: %v", err)
	}

	// 模拟视频被替换：修改 mtime
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(video, future, future); err != nil {
		t.Fatal(err)
	}

	second, err := svc.EnsureFirstFramePoster(video)
	if err != nil {
		t.Fatalf("变更后再提取失败: %v", err)
	}
	if first == second {
		t.Fatal("视频文件变化后应重新提取（旧实现永远返回过期缓存）")
	}
}

func TestEnsureFirstFramePosterRejectsRemoteAndStrm(t *testing.T) {
	svc := newTestNFOService(t)
	if _, err := svc.EnsureFirstFramePoster("webdav://nas/video.mp4"); err == nil {
		t.Fatal("webdav 路径应被拒绝")
	}
	strm := filepath.Join(t.TempDir(), "remote.strm")
	os.WriteFile(strm, []byte("http://example.com/v.mp4"), 0644)
	if _, err := svc.EnsureFirstFramePoster(strm); err == nil {
		t.Fatal("strm 文件应被拒绝")
	}
}

func TestFindLocalImagesForMediaFallsBackToFirstFrame(t *testing.T) {
	svc := newTestNFOService(t)
	dir := t.TempDir()
	video := filepath.Join(dir, "流浪地球.mp4")
	createTestVideo(t, video, 3)
	// 目录下没有任何图片

	poster, backdrop := svc.FindLocalImagesForMedia(video)
	if poster == "" {
		t.Fatal("本地图片缺失时应自动提取视频第一帧作为海报")
	}
	if !isJPEG(poster) {
		t.Fatalf("兜底海报不是有效 JPEG: %s", poster)
	}
	_ = backdrop

	// 同名本地图片存在时优先使用图片，不触发首帧提取
	imgDir := t.TempDir()
	imgVideo := filepath.Join(imgDir, "流浪地球2.mp4")
	createTestVideo(t, imgVideo, 3)
	localImg := filepath.Join(imgDir, "流浪地球2.jpg")
	if err := os.WriteFile(localImg, []byte{0xFF, 0xD8, 0xFF, 0xDB, 0x00, 0x01}, 0644); err != nil {
		t.Fatal(err)
	}
	poster2, _ := svc.FindLocalImagesForMedia(imgVideo)
	if poster2 != localImg {
		t.Fatalf("同名本地图片应优先于首帧兜底: want=%s got=%s", localImg, poster2)
	}
}

// setupTestStreamService 构建带 DB 的 StreamService（用于海报端到端流程测试）
func setupTestStreamService(t *testing.T) (*StreamService, *repository.Repositories, *config.Config) {
	t.Helper()
	requireFFmpeg(t)
	db := setupTestDB(t)
	repos := repository.NewRepositories(db)
	cfg := &config.Config{}
	cfg.App.FFmpegPath = "ffmpeg"
	cfg.Cache.CacheDir = filepath.Join(t.TempDir(), "cache")
	logger := zap.NewNop().Sugar()

	execution, err := NewMediaExecutionService(db, cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	stream := NewStreamService(repos.Media, repos.Series, execution, cfg, logger)
	stream.SetNFOService(NewNFOService(logger, cfg))
	return stream, repos, cfg
}

func TestGetPosterPathGeneratesFirstFrameWhenMissing(t *testing.T) {
	stream, repos, cfg := setupTestStreamService(t)

	video := filepath.Join(t.TempDir(), "nolocalart.mp4")
	createTestVideo(t, video, 3)

	media := &model.Media{ID: "m-frame", Title: "无图影片", FilePath: video, MediaType: "movie"}
	if err := repos.Media.Create(media); err != nil {
		t.Fatal(err)
	}

	poster, err := stream.GetPosterPath("m-frame")
	if err != nil {
		t.Fatal(err)
	}
	if poster == "" || !isJPEG(poster) {
		t.Fatalf("本地海报缺失时应返回首帧封面: %q", poster)
	}
	if !strings.HasPrefix(filepath.Clean(poster), filepath.Clean(cfg.Cache.CacheDir)) {
		t.Fatalf("首帧封面应位于缓存目录下: %s", poster)
	}

	// 封面路径必须回写数据库，列表页 poster_path 才能直接生效
	updated, err := repos.Media.FindByID("m-frame")
	if err != nil {
		t.Fatal(err)
	}
	if updated.PosterPath != poster {
		t.Fatalf("首帧封面未回写数据库: want=%s got=%s", poster, updated.PosterPath)
	}
}

func TestGetPosterPathHealsStalePosterWithFirstFrame(t *testing.T) {
	stream, repos, _ := setupTestStreamService(t)

	video := filepath.Join(t.TempDir(), "stale.mp4")
	createTestVideo(t, video, 3)

	media := &model.Media{
		ID:         "m-stale",
		Title:      "脏数据影片",
		FilePath:   video,
		MediaType:  "movie",
		PosterPath: filepath.Join(os.TempDir(), "fan-video", "frames", "gone_poster.jpg"),
	}
	if err := repos.Media.Create(media); err != nil {
		t.Fatal(err)
	}

	poster, err := stream.GetPosterPath("m-stale")
	if err != nil {
		t.Fatal(err)
	}
	if poster == "" || !isJPEG(poster) {
		t.Fatalf("失效的海报路径应通过首帧重新生成: %q", poster)
	}
	if poster == media.PosterPath {
		t.Fatal("应生成新的封面文件而非继续返回失效路径")
	}
}

func TestGetPosterPathPrefersLocalSameNameImage(t *testing.T) {
	stream, repos, _ := setupTestStreamService(t)

	dir := t.TempDir()
	video := filepath.Join(dir, "localart.mp4")
	createTestVideo(t, video, 3)
	img := filepath.Join(dir, "localart.jpg")
	if err := os.WriteFile(img, []byte{0xFF, 0xD8, 0xFF, 0xDB, 0x00, 0x01}, 0644); err != nil {
		t.Fatal(err)
	}

	media := &model.Media{ID: "m-local", Title: "有图影片", FilePath: video, MediaType: "movie"}
	if err := repos.Media.Create(media); err != nil {
		t.Fatal(err)
	}

	poster, err := stream.GetPosterPath("m-local")
	if err != nil {
		t.Fatal(err)
	}
	if poster != img {
		t.Fatalf("同名本地图片应最优先: want=%s got=%s", img, poster)
	}
}
