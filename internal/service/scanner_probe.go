package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/fan-video/fan-video/internal/model"
)

type FFprobeResult struct {
	Streams []FFprobeStream `json:"streams"`
	Format  FFprobeFormat   `json:"format"`
}

// FFprobeStream 流信息
type FFprobeStream struct {
	Index         int    `json:"index"`
	CodecType     string `json:"codec_type"` // video, audio, subtitle
	CodecName     string `json:"codec_name"` // h264, hevc, aac, srt, ass
	CodecLongName string `json:"codec_long_name"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	Duration      string `json:"duration"`
	BitRate       string `json:"bit_rate"`
	// 字幕相关
	Tags        map[string]string  `json:"tags"`
	Disposition FFprobeDisposition `json:"disposition"`
}

// FFprobeDisposition 流标志
type FFprobeDisposition struct {
	Default int `json:"default"`
	Forced  int `json:"forced"`
}

// FFprobeFormat 格式信息
type FFprobeFormat struct {
	Filename       string `json:"filename"`
	Duration       string `json:"duration"`
	Size           string `json:"size"`
	BitRate        string `json:"bit_rate"`
	FormatName     string `json:"format_name"`
	FormatLongName string `json:"format_long_name"`
}

// SubtitleTrack 字幕轨道信息
type SubtitleTrack struct {
	Index    int    `json:"index"`
	Codec    string `json:"codec"`    // srt, ass, subrip, hdmv_pgs_subtitle
	Language string `json:"language"` // chi, eng, jpn等
	Title    string `json:"title"`    // 字幕标题
	Default  bool   `json:"default"`  // 是否默认
	Forced   bool   `json:"forced"`   // 是否强制
	Bitmap   bool   `json:"bitmap"`   // 是否为图形字幕（PGS/VobSub等，不可提取为文本）
}

// isBitmapSubtitle 判断字幕编解码器是否为图形字幕
func isBitmapSubtitle(codec string) bool {
	switch strings.ToLower(codec) {
	case "hdmv_pgs_subtitle", "pgssub", "dvd_subtitle", "dvdsub", "dvb_subtitle", "xsub":
		return true
	default:
		return false
	}
}

// ScannerService 媒体文件扫描服务
func (s *ScannerService) ProbeMediaInfo(media *model.Media) {
	s.probeMediaInfo(media)
}

// parseSTRMFile 解析 .strm 文件，提取远程流 URL
// .strm 文件格式：纯文本文件，第一行为可播放的远程 URL
func (s *ScannerService) parseSTRMFile(filePath string) (string, error) {
	meta, err := ParseSTRMFileEnhanced(filePath)
	if err != nil {
		return "", err
	}
	return meta.URL, nil
}

// parseSTRMFileMeta 返回 .strm 的完整元数据（URL + 自定义 Header 等）
func (s *ScannerService) parseSTRMFileMeta(filePath string) (*STRMMeta, error) {
	return ParseSTRMFileEnhanced(filePath)
}

// isSTRMFile 判断是否为 .strm 文件
func isSTRMFile(filePath string) bool {
	return strings.ToLower(filepath.Ext(filePath)) == ".strm"
}

// probeSTRMMedia 处理 .strm 文件的媒体信息
// 对于 .strm 文件，不使用 FFprobe 探测（远程 URL 可能很慢或不支持），
// 而是设置默认值，后续播放时由前端/后端动态处理
func (s *ScannerService) probeSTRMMedia(media *model.Media, streamURL string) {
	media.StreamURL = streamURL
	// 根据远程 URL 的扩展名推断基本信息
	urlLower := strings.ToLower(streamURL)
	if strings.Contains(urlLower, ".m3u8") {
		media.VideoCodec = "strm_hls"
	} else if strings.HasSuffix(urlLower, ".mp4") || strings.Contains(urlLower, ".mp4?") {
		media.VideoCodec = "strm_mp4"
	} else if strings.HasSuffix(urlLower, ".mkv") || strings.Contains(urlLower, ".mkv?") {
		media.VideoCodec = "strm_mkv"
	} else {
		media.VideoCodec = "strm_unknown"
	}
	s.logger.Debugf("STRM 文件: %s -> %s", media.FilePath, streamURL)
}

// probeMediaInfo 使用FFprobe提取视频元数据（.strm 文件走特殊逻辑）
func (s *ScannerService) probeMediaInfo(media *model.Media) {
	// .strm 文件：解析远程 URL，不使用 FFprobe
	if isSTRMFile(media.FilePath) {
		meta, err := s.parseSTRMFileMeta(media.FilePath)
		if err != nil {
			s.logger.Warnf("解析 STRM 文件失败: %s, 错误: %v", media.FilePath, err)
			return
		}
		ApplySTRMMetaToMedia(media, meta)
		s.probeSTRMMedia(media, meta.URL)

		// 可选：对直链型 STRM 进行远程 FFprobe 探测（慢但能拿到真实时长/编码/分辨率）
		// 仅对 http(s) + 明显的视频直链启用（排除 HLS/磁力/rtmp 等）
		if s.cfg != nil && s.cfg.STRM.RemoteProbe && isDirectVideoLink(meta.URL) {
			// 构造请求头（FFprobe 需要透传 UA / Referer / Cookie）
			ua, referer, cookie, extra := ResolveSTRMHeaders(&s.cfg.STRM, media)
			hdr := http.Header{}
			if ua != "" {
				hdr.Set("User-Agent", ua)
			}
			if referer != "" {
				hdr.Set("Referer", referer)
			}
			if cookie != "" {
				hdr.Set("Cookie", cookie)
			}
			for k, v := range extra {
				hdr.Set(k, v)
			}
			timeout := s.cfg.STRM.RemoteProbeTimeout
			if timeout <= 0 {
				timeout = 8
			}
			if ok := RemoteProbeSTRM(context.Background(), s.cfg.App.FFprobePath, media, hdr, timeout); ok {
				s.logger.Debugf("STRM 远程 probe 成功: %s", media.FilePath)
			}
		}
		return
	}

	cmd := exec.Command(s.cfg.App.FFprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		media.FilePath,
	)

	output, err := cmd.Output()
	if err != nil {
		s.logger.Warnf("FFprobe分析失败: %s, 错误: %v", media.FilePath, err)
		return
	}

	var result FFprobeResult
	if err := json.Unmarshal(output, &result); err != nil {
		s.logger.Warnf("解析FFprobe输出失败: %s, 错误: %v", media.FilePath, err)
		return
	}

	// 提取视频流信息
	for _, stream := range result.Streams {
		switch stream.CodecType {
		case "video":
			media.VideoCodec = stream.CodecName
			if stream.Width > 0 && stream.Height > 0 {
				media.Resolution = s.classifyResolution(stream.Width, stream.Height)
			}
		case "audio":
			if media.AudioCodec == "" {
				media.AudioCodec = stream.CodecName
			}
		}
	}

	// 提取时长
	if result.Format.Duration != "" {
		if dur, err := strconv.ParseFloat(result.Format.Duration, 64); err == nil {
			media.Duration = dur
		}
	}
}

// GetSubtitleTracks 获取媒体文件的内嵌字幕轨道列表
func (s *ScannerService) classifyResolution(width, height int) string {
	// 以高度为主要判断标准
	maxDim := height
	if width > height {
		// 正常横向视频
		maxDim = height
	} else {
		// 竖向视频
		maxDim = width
	}

	switch {
	case maxDim >= 2160:
		return "4K"
	case maxDim >= 1440:
		return "2K"
	case maxDim >= 1080:
		return "1080p"
	case maxDim >= 720:
		return "720p"
	case maxDim >= 480:
		return "480p"
	default:
		return fmt.Sprintf("%dp", maxDim)
	}
}

// extractTitle 从文件名提取标题（保持向后兼容的简单版本）
func (s *ScannerService) extractTitle(filename string) string {
	title, _, _ := s.extractTitleEnhanced(filename)
	return title
}

// extractTitleEnhanced 从文件名增强提取标题、年份和 TMDb ID
// 支持 Emby 标准命名格式：Title (Year) [tmdbid=xxx]
// 以及国内资源站的脏命名：
//   - "01届.《翼》-《Wings》-1927-1929。【十万度Q裙 319940383】.mkv"
//   - "[yyh3d.com]采花和尚.Satyr Monks.1994.LD_D9.x264.AAC.480P.YYH3D.xt.mkv"
func (s *ScannerService) extractTitleEnhanced(filename string) (title string, year int, tmdbID int) {
	// 优先走统一增强解析器：它能处理《》、【广告】、[站点]、XX届、115chrome 等脏命名
	if parsed := ParseMovieFilename(filename); parsed.Title != "" {
		title = parsed.Title
		year = parsed.Year
		tmdbID = parsed.TMDbID
		return
	}

	// —— 兜底：保留原本的简单清洗逻辑，避免极端边界下丢名 ——
	// 去掉扩展名
	name := strings.TrimSuffix(filename, filepath.Ext(filename))

	// 步骤1：提取 ID 标签 [tmdbid=xxx]、{imdb-ttxxx} 等
	idType, idValue := parseIDFromName(name)
	if idType == "tmdbid" || idType == "tmdb" {
		tmdbID, _ = strconv.Atoi(idValue)
	}
	// 注意：IMDB ID (imdbid/imdb) 标签在此处仅被识别和移除，
	// 实际的 IMDB ID → TMDb ID 转换在刮削阶段（ScrapeMedia）中通过网络请求完成
	// 从名称中移除 ID 标签
	for _, pattern := range idTagPatterns {
		name = pattern.ReplaceAllString(name, "")
	}

	// 步骤2：提取年份 (2009) 或 [2009]
	year = extractYearFromName(name)
	// 移除年份标记
	name = yearInNamePattern.ReplaceAllString(name, "")

	// 步骤3：清理常见编码/来源/分辨率标记
	cleanPatterns := []string{
		`(?i)\b(BluRay|BDRip|HDRip|WEB-?DL|WEBRip|DVDRip|HDTV|HDCam|REMUX)\b`,
		`(?i)\b(x264|x265|h\.?264|h\.?265|HEVC|AVC|AAC|DTS|AC3|FLAC|OPUS)\b`,
		`(?i)\b(1080p|720p|480p|2160p|4K|UHD)\b`,
		`(?i)\b(PROPER|REPACK|EXTENDED|UNRATED|DIRECTORS\.?CUT|REMASTERED)\b`,
	}
	for _, p := range cleanPatterns {
		re := regexp.MustCompile(p)
		name = re.ReplaceAllString(name, " ")
	}

	// 步骤4：替换常见分隔符为空格
	replacer := strings.NewReplacer(
		".", " ",
		"_", " ",
	)
	name = replacer.Replace(name)

	// 步骤5：清理多余空格和首尾的分隔符
	name = regexp.MustCompile(`\s+`).ReplaceAllString(name, " ")
	name = strings.Trim(name, " -")

	title = strings.TrimSpace(name)
	return
}

// GetExternalSubtitles 获取媒体文件的外挂字幕列表
