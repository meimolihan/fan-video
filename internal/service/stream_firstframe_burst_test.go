package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fan-video/fan-video/internal/model"
)

// TestValidFirstFrameFile 校验首帧缓存文件的可用性判定：
// 纯白/半截/损坏文件（虽存在且非空）必须被判为无效，触发重新提取。
func TestValidFirstFrameFile(t *testing.T) {
	dir := t.TempDir()

	big := make([]byte, 4096)
	big[0], big[1], big[2] = 0xFF, 0xD8, 0xFF // JPEG 魔数
	goodJPEG := filepath.Join(dir, "good.jpg")
	if err := os.WriteFile(goodJPEG, big, 0644); err != nil {
		t.Fatal(err)
	}
	if !validFirstFrameFile(goodJPEG) {
		t.Fatal(">2KB 且带 JPEG 魔数的文件应判为有效")
	}

	badMagic := make([]byte, 4096)
	for i := range badMagic {
		badMagic[i] = 0x55
	}
	garbage := filepath.Join(dir, "garbage.jpg")
	if err := os.WriteFile(garbage, badMagic, 0644); err != nil {
		t.Fatal(err)
	}
	if validFirstFrameFile(garbage) {
		t.Fatal("无图片魔数的文件应判为无效")
	}

	small := []byte{0xFF, 0xD8, 0xFF, 0xDB}
	tiny := filepath.Join(dir, "tiny.jpg")
	if err := os.WriteFile(tiny, small, 0644); err != nil {
		t.Fatal(err)
	}
	if validFirstFrameFile(tiny) {
		t.Fatal("大小不达标（<2048B）的 JPEG 应判为无效")
	}

	if validFirstFrameFile(filepath.Join(dir, "missing.jpg")) {
		t.Fatal("不存在的文件应判为无效")
	}
}

// TestGetPosterPathBurstDeferredToBackground 回归测试：批量无封面媒体同时请求
// 海报时，同步首帧生成被限制在少量槽位内，超出槽位的请求必须快速返回
// （不卡死在 ffmpeg 上），由后台队列补齐封面并回写数据库。
func TestGetPosterPathBurstDeferredToBackground(t *testing.T) {
	stream, repos, _ := setupTestStreamService(t)

	const n = 24
	dir := t.TempDir()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		video := filepath.Join(dir, fmt.Sprintf("burst_%d.mp4", i))
		createTestVideo(t, video, 2)
		id := fmt.Sprintf("m-burst-%d", i)
		if err := repos.Media.Create(&model.Media{ID: id, Title: "Burst" + fmt.Sprint(i), FilePath: video, MediaType: "movie"}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	// 预占全部同步槽位：突发请求只能走后台队列路径
	if !stream.tryAcquireFirstFrameSlot() || !stream.tryAcquireFirstFrameSlot() {
		t.Fatal("应能占用两个同步槽位")
	}
	defer func() {
		stream.releaseFirstFrameSlot()
		stream.releaseFirstFrameSlot()
	}()

	type result struct {
		poster string
		err    error
	}
	burstStart := time.Now()
	var wg sync.WaitGroup
	results := make([]result, n)
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			poster, err := stream.GetPosterPath(id)
			results[i] = result{poster: poster, err: err}
		}(i, id)
	}
	wg.Wait()
	burstElapsed := time.Since(burstStart)

	// 槽位占满时，请求必须快速返回空（占位图），不能排队等待 ffmpeg
	if burstElapsed > 10*time.Second {
		t.Fatalf("批量海报请求耗时过长（疑似卡死在同步 ffmpeg 上）: %v", burstElapsed)
	}
	for i, res := range results {
		if res.err != nil {
			t.Fatalf("槽位占满时请求不应报错 idx=%d: %v", i, res.err)
		}
		if res.poster != "" {
			t.Fatalf("槽位占满时请求应返回空占位，实际 idx=%d %q", i, res.poster)
		}
	}

	// 后台队列应逐步补齐全部媒体的封面（含数据库回写）
	deadline := time.Now().Add(90 * time.Second)
	for {
		allDone := true
		for _, id := range ids {
			media, err := repos.Media.FindByID(id)
			if err != nil || media.PosterPath == "" {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("后台首帧队列未在超时内补齐所有媒体封面")
		}
		time.Sleep(300 * time.Millisecond)
	}

	// 后台补齐后：所有媒体都能立刻拿到有效封面
	for _, id := range ids {
		poster, err := stream.GetPosterPath(id)
		if err != nil || poster == "" || !isJPEG(poster) {
			t.Fatalf("后台补齐后仍无法返回有效封面 id=%s poster=%q err=%v", id, poster, err)
		}
	}
}
