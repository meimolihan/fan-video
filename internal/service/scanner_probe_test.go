package service

import (
	"testing"

	"github.com/fan-video/fan-video/internal/model"
)

func TestApplyFFprobeStreamsSkipsImageStreams(t *testing.T) {
	scanner := &ScannerService{}
	media := &model.Media{}

	// 海报图片（webp/mjpeg）排在 0 号流，真实 h264 视频在 1 号流：
	// 图片流的尺寸（960×540 → "480p"）绝不能污染分辨率。
	result := &FFprobeResult{
		Streams: []FFprobeStream{
			{
				CodecType: "video",
				CodecName: "webp",
				Width:     960,
				Height:    540,
			},
			{
				CodecType: "video",
				CodecName: "h264",
				Width:     1920,
				Height:    1080,
			},
			{
				CodecType: "audio",
				CodecName: "aac",
			},
		},
		Format: FFprobeFormat{Duration: "2712.37"},
	}

	scanner.applyFFprobeStreams(media, result)

	if media.VideoCodec != "h264" {
		t.Fatalf("image stream must be skipped, got codec %q", media.VideoCodec)
	}
	if media.Resolution != "1080p" {
		t.Fatalf("resolution must come from the real video, got %q", media.Resolution)
	}
	if media.AudioCodec != "aac" {
		t.Fatalf("audio codec missing: %q", media.AudioCodec)
	}
	if media.Duration != 2712.37 {
		t.Fatalf("duration not applied: %v", media.Duration)
	}
}

func TestApplyFFprobeStreamsSkipsAttachedPicCover(t *testing.T) {
	scanner := &ScannerService{}
	media := &model.Media{}

	// 内嵌封面（attached_pic, mjpeg 640×480）即使编解码本身合法也不算视频轨。
	result := &FFprobeResult{
		Streams: []FFprobeStream{
			{
				CodecType:   "video",
				CodecName:   "mjpeg",
				Width:       640,
				Height:      480,
				Disposition: FFprobeDisposition{AttachedPic: 1},
			},
			{
				CodecType: "video",
				CodecName: "hevc",
				Width:     3840,
				Height:    2160,
			},
		},
	}

	scanner.applyFFprobeStreams(media, result)

	if media.VideoCodec != "hevc" || media.Resolution != "4K" {
		t.Fatalf("attached_pic cover wrongly treated as main video: %s / %s", media.VideoCodec, media.Resolution)
	}
}

func TestApplyFFprobeStreamsLeavesPureImageFileUnpolluted(t *testing.T) {
	scanner := &ScannerService{}
	// 模拟一个"媒体行"原本已有正确技术字段，但内存里的 media 被一张图片探测结果填充。
	// applyFFprobeStreams 遇到纯图片流（如海报）时不应写入任何视频编码/分辨率。
	media := &model.Media{VideoCodec: "h264", Resolution: "1080p"}

	result := &FFprobeResult{
		Streams: []FFprobeStream{
			{CodecType: "video", CodecName: "png", Width: 141, Height: 141},
			{CodecType: "video", CodecName: "mjpeg", Width: 320, Height: 240},
		},
	}
	scanner.applyFFprobeStreams(media, result)

	if media.VideoCodec != "h264" || media.Resolution != "1080p" {
		t.Fatalf("pure image streams must not overwrite video metadata: %s / %s", media.VideoCodec, media.Resolution)
	}
}
