package probe

import (
	"strings"
	"testing"
)

func TestParseFFprobeOutputDoesNotTreatSDRHEVCAsHDR(t *testing.T) {
	record, err := parseFFprobeOutput([]byte(`{
		"streams": [
			{
				"index": 0,
				"codec_type": "video",
				"codec_name": "hevc",
				"width": 3840,
				"height": 2160,
				"pix_fmt": "yuv420p10le",
				"avg_frame_rate": "24000/1001",
				"color_transfer": "bt709",
				"color_primaries": "bt709",
				"color_space": "bt709"
			},
			{
				"index": 1,
				"codec_type": "audio",
				"codec_name": "truehd",
				"channels": 8,
				"channel_layout": "7.1",
				"sample_rate": "48000",
				"tags": {"language": "eng"},
				"disposition": {"default": 1}
			}
		],
		"format": {"format_name": "matroska,webm", "duration": "120.125"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if record.HDR {
		t.Fatal("ordinary SDR HEVC must not be classified as HDR")
	}
	if record.FrameRateNum != 24000 || record.FrameRateDen != 1001 || record.GOPSize(2) != 48 {
		t.Fatalf("unexpected frame-rate normalization: %+v", record)
	}
	if record.BitDepth != 10 {
		t.Fatalf("expected 10-bit pixel format, got %d", record.BitDepth)
	}
	if record.DurationMS != 120125 {
		t.Fatalf("unexpected duration: %d", record.DurationMS)
	}
	streams := record.AudioStreams()
	if len(streams) != 1 || streams[0].Codec != "truehd" || streams[0].Channels != 8 || !streams[0].Default {
		t.Fatalf("unexpected audio stream normalization: %+v", streams)
	}
}

func TestParseFFprobeOutputDetectsPQHLGAndMetadataHDR(t *testing.T) {
	cases := []struct {
		name     string
		transfer string
		sideData string
	}{
		{name: "pq", transfer: "smpte2084"},
		{name: "hlg", transfer: "arib-std-b67"},
		{name: "mastering", transfer: "bt709", sideData: `,"side_data_list":[{"side_data_type":"Mastering display metadata"}]`},
		{name: "dolby-vision", transfer: "bt709", sideData: `,"side_data_list":[{"side_data_type":"DOVI configuration record"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := `{"streams":[{"index":0,"codec_type":"video","codec_name":"hevc","width":1920,"height":1080,"pix_fmt":"yuv420p10le","avg_frame_rate":"25/1","color_transfer":"` + tc.transfer + `"` + tc.sideData + `}],"format":{"duration":"1"}}`
			record, err := parseFFprobeOutput([]byte(payload))
			if err != nil {
				t.Fatal(err)
			}
			if !record.HDR {
				t.Fatalf("expected HDR for %s: %+v", tc.name, record)
			}
		})
	}
}

func TestParseFFprobeOutputRequiresVideo(t *testing.T) {
	_, err := parseFFprobeOutput([]byte(`{"streams":[{"codec_type":"audio","codec_name":"aac"}]}`))
	if err == nil || !strings.Contains(err.Error(), "no video stream") {
		t.Fatalf("expected missing-video error, got %v", err)
	}
}

func TestParseFFprobeOutputSkipsImageAndCoverStreams(t *testing.T) {
	// 真实场景：媒体文件内嵌 mjpeg 封面（attached_pic）位于 0 号流，
	// 真实视频位于 1 号流。图片/封面流的尺寸（如海报 960×540）不能
	// 用来推导视频分辨率。
	record, err := parseFFprobeOutput([]byte(`{
		"streams": [
			{
				"index": 0,
				"codec_type": "video",
				"codec_name": "mjpeg",
				"width": 960,
				"height": 540,
				"disposition": {"attached_pic": 1}
			},
			{
				"index": 1,
				"codec_type": "video",
				"codec_name": "h264",
				"width": 1920,
				"height": 1080,
				"pix_fmt": "yuv420p"
			},
			{
				"index": 2,
				"codec_type": "audio",
				"codec_name": "aac",
				"channels": 2,
				"sample_rate": "48000"
			}
		],
		"format": {"format_name": "mov,mp4,m4a,3gp,3g2,mj2", "duration": "60"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if record.VideoCodec != "h264" {
		t.Fatalf("cover/attached_pic must be skipped, got video codec %q", record.VideoCodec)
	}
	if record.Width != 1920 || record.Height != 1080 {
		t.Fatalf("resolution must come from the real video stream, got %dx%d", record.Width, record.Height)
	}
}

func TestParseFFprobeOutputRejectsPureImageFile(t *testing.T) {
	// 把一张 jpg/png/webp 海报当作"视频"探测时，唯一的 video 流是图片流，
	// 不能据此伪造出视频技术信息。
	_, err := parseFFprobeOutput([]byte(`{
		"streams": [
			{"index": 0, "codec_type": "video", "codec_name": "webp", "width": 800, "height": 450}
		],
		"format": {"format_name": "webp"}
	}`))
	if err == nil || !strings.Contains(err.Error(), "no video stream") {
		t.Fatalf("pure image file must not be treated as video, got %v", err)
	}
}

func TestParseFrameRateReducesFraction(t *testing.T) {
	numerator, denominator := parseFrameRate("60000/2002")
	if numerator != 30000 || denominator != 1001 {
		t.Fatalf("fraction not reduced: %d/%d", numerator, denominator)
	}
	if numerator, denominator := parseFrameRate("0/0"); numerator != 0 || denominator != 0 {
		t.Fatalf("invalid rate accepted: %d/%d", numerator, denominator)
	}
}
