package service

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/fan-video/fan-video/internal/model"
)

func (s *ScannerService) GetSubtitleTracks(filePath string) ([]SubtitleTrack, error) {
	cmd := exec.Command(s.cfg.App.FFprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-select_streams", "s", filePath,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("FFprobe获取字幕失败: %w", err)
	}

	var result struct {
		Streams []FFprobeStream `json:"streams"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("解析字幕信息失败: %w", err)
	}

	var tracks []SubtitleTrack
	for _, stream := range result.Streams {
		track := SubtitleTrack{
			Index:   stream.Index,
			Codec:   stream.CodecName,
			Default: stream.Disposition.Default == 1,
			Forced:  stream.Disposition.Forced == 1,
			Bitmap:  isBitmapSubtitle(stream.CodecName),
		}
		if lang, ok := stream.Tags["language"]; ok {
			track.Language = lang
		}
		if title, ok := stream.Tags["title"]; ok {
			track.Title = title
		}
		tracks = append(tracks, track)
	}

	return tracks, nil
}

// ExtractSubtitle 提取内嵌字幕到文件
func (s *ScannerService) ExtractSubtitle(filePath string, streamIndex int, outputFormat string) (string, error) {
	// 确定输出文件路径
	cacheDir := filepath.Join(s.cfg.Cache.CacheDir, "subtitles")
	os.MkdirAll(cacheDir, 0755)

	baseName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	outputPath := filepath.Join(cacheDir, fmt.Sprintf("%s_%d.%s", baseName, streamIndex, outputFormat))

	// 检查缓存
	if _, err := os.Stat(outputPath); err == nil {
		return outputPath, nil
	}

	cmd := exec.Command(s.cfg.App.FFmpegPath,
		"-y",
		"-i", filePath,
		"-map", fmt.Sprintf("0:%d", streamIndex),
		"-c:s", s.getSubtitleCodec(outputFormat),
		outputPath,
	)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("提取字幕失败: %w", err)
	}

	return outputPath, nil
}

// scanExternalSubtitles 扫描外挂字幕文件
func (s *ScannerService) scanExternalSubtitles(media *model.Media) {
	dir := filepath.Dir(media.FilePath)
	baseName := strings.TrimSuffix(filepath.Base(media.FilePath), filepath.Ext(media.FilePath))

	subtitleExts := []string{".srt", ".ass", ".ssa", ".vtt", ".sub", ".idx"}

	var found []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))

		// 检查是否为字幕文件且与视频同名前缀
		isSubtitle := false
		for _, subExt := range subtitleExts {
			if ext == subExt {
				isSubtitle = true
				break
			}
		}
		if !isSubtitle {
			continue
		}

		// 检查文件名前缀匹配
		nameWithoutExt := strings.TrimSuffix(name, ext)
		if strings.HasPrefix(strings.ToLower(nameWithoutExt), strings.ToLower(baseName)) {
			found = append(found, filepath.Join(dir, name))
		}
	}

	if len(found) > 0 {
		media.SubtitlePaths = strings.Join(found, "|")
		s.logger.Debugf("发现外挂字幕: %s -> %d 个", baseName, len(found))
	}
}

// getSubtitleCodec 根据输出格式获取字幕编解码器
func (s *ScannerService) getSubtitleCodec(format string) string {
	switch format {
	case "srt":
		return "srt"
	case "ass", "ssa":
		return "ass"
	case "vtt", "webvtt":
		return "webvtt"
	default:
		return "srt"
	}
}

// classifyResolution 根据分辨率分类
func (s *ScannerService) GetExternalSubtitles(filePath string) []ExternalSubtitle {
	dir := filepath.Dir(filePath)
	baseName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))

	subtitleExts := []string{".srt", ".ass", ".ssa", ".vtt", ".sub"}

	var subs []ExternalSubtitle
	entries, err := os.ReadDir(dir)
	if err != nil {
		return subs
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))

		isSubtitle := false
		for _, subExt := range subtitleExts {
			if ext == subExt {
				isSubtitle = true
				break
			}
		}
		if !isSubtitle {
			continue
		}

		nameWithoutExt := strings.TrimSuffix(name, ext)
		if strings.HasPrefix(strings.ToLower(nameWithoutExt), strings.ToLower(baseName)) {
			// 尝试从文件名提取语言信息，如 movie.zh.srt, movie.eng.srt
			langs := strings.TrimPrefix(strings.ToLower(nameWithoutExt), strings.ToLower(baseName))
			langs = strings.Trim(langs, "._ ")
			lang := s.detectSubtitleLanguage(langs)

			subs = append(subs, ExternalSubtitle{
				Path:     filepath.Join(dir, name),
				Filename: name,
				Format:   strings.TrimPrefix(ext, "."),
				Language: lang,
			})
		}
	}

	return subs
}

// ExternalSubtitle 外挂字幕信息
type ExternalSubtitle struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
	Format   string `json:"format"`   // srt, ass, vtt等
	Language string `json:"language"` // 语言编码
}

// detectSubtitleLanguage 从文件名中检测字幕语言
func (s *ScannerService) detectSubtitleLanguage(namePart string) string {
	// 按优先级排序的语言映射（长匹配优先，避免短码误匹配）
	type langEntry struct {
		code string
		lang string
	}
	langEntries := []langEntry{
		// 长匹配优先
		{"chinese", "中文"},
		{"english", "English"},
		{"japanese", "日本語"},
		{"korean", "한국어"},
		{"简体", "简体中文"},
		{"繁体", "繁体中文"},
		{"简中", "简体中文"},
		{"繁中", "繁体中文"},
		// 三字母ISO 639-2
		{"chi", "中文"},
		{"chs", "简体中文"},
		{"cht", "繁体中文"},
		{"eng", "English"},
		{"jpn", "日本語"},
		{"kor", "한국어"},
		// 两字母ISO 639-1（使用分隔符精确匹配）
		{"zh", "中文"},
		{"en", "English"},
		{"ja", "日本語"},
		{"jp", "日本語"},
		{"ko", "한국어"},
		{"sc", "简体中文"},
		{"tc", "繁体中文"},
	}

	namePart = strings.ToLower(namePart)
	// 将分隔符统一为点号，方便精确匹配
	normalized := strings.NewReplacer("_", ".", "-", ".", " ", ".").Replace(namePart)
	parts := strings.Split(normalized, ".")

	// 先尝试精确匹配各段
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		for _, entry := range langEntries {
			if part == entry.code {
				return entry.lang
			}
		}
	}

	// 再尝试包含匹配（仅对长码，避免短码误匹配）
	for _, entry := range langEntries {
		if len(entry.code) >= 3 && strings.Contains(namePart, entry.code) {
			return entry.lang
		}
	}

	if namePart != "" {
		return namePart
	}
	return "未知"
}

// ConvertSubtitleToVTT 将外挂字幕文件转换为WebVTT格式（浏览器原生支持）
func (s *ScannerService) ConvertSubtitleToVTT(subtitlePath string) (string, error) {
	// 确定输出文件路径
	cacheDir := filepath.Join(s.cfg.Cache.CacheDir, "subtitles")
	os.MkdirAll(cacheDir, 0755)

	// 使用原始文件名+哈希避免冲突
	baseName := strings.TrimSuffix(filepath.Base(subtitlePath), filepath.Ext(subtitlePath))
	outputPath := filepath.Join(cacheDir, fmt.Sprintf("%s_ext.vtt", baseName))

	// 检查缓存：如果转换后的文件已存在且比源文件新，直接返回
	if outInfo, err := os.Stat(outputPath); err == nil {
		if srcInfo, err := os.Stat(subtitlePath); err == nil {
			if outInfo.ModTime().After(srcInfo.ModTime()) {
				return outputPath, nil
			}
		}
	}

	// 使用FFmpeg将字幕转换为WebVTT
	cmd := exec.Command(s.cfg.App.FFmpegPath,
		"-y",
		"-i", subtitlePath,
		"-c:s", "webvtt",
		outputPath,
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("FFmpeg字幕转换失败: %w, 输出: %s", err, string(output))
	}

	s.logger.Debugf("字幕转换完成: %s -> %s", subtitlePath, outputPath)
	return outputPath, nil
}

// GetFileExt 获取文件扩展名（小写）
type ExtractedSubtitleFile struct {
	TrackIndex int    `json:"track_index"`
	Language   string `json:"language"`
	Title      string `json:"title"`
	Codec      string `json:"codec"`
	Format     string `json:"format"`
	Path       string `json:"path"`
	Bitmap     bool   `json:"bitmap"`
	Error      string `json:"error,omitempty"`
}

// ExtractAllSubtitles 批量提取视频中所有文本字幕轨道
// format: 输出格式 "srt" | "vtt"
// trackIndices: 指定轨道索引列表，为空则提取所有文本字幕
func (s *ScannerService) ExtractAllSubtitles(filePath string, format string, trackIndices []int) ([]ExtractedSubtitleFile, error) {
	if format == "" {
		format = "srt"
	}

	// 获取所有字幕轨道
	tracks, err := s.GetSubtitleTracks(filePath)
	if err != nil {
		return nil, fmt.Errorf("获取字幕轨道失败: %w", err)
	}

	if len(tracks) == 0 {
		return nil, fmt.Errorf("该视频文件不包含任何字幕轨道")
	}

	// 过滤要提取的轨道
	var targetTracks []SubtitleTrack
	if len(trackIndices) > 0 {
		indexSet := make(map[int]bool)
		for _, idx := range trackIndices {
			indexSet[idx] = true
		}
		for _, t := range tracks {
			if indexSet[t.Index] {
				targetTracks = append(targetTracks, t)
			}
		}
	} else {
		targetTracks = tracks
	}

	var results []ExtractedSubtitleFile

	for _, track := range targetTracks {
		result := ExtractedSubtitleFile{
			TrackIndex: track.Index,
			Language:   track.Language,
			Title:      track.Title,
			Codec:      track.Codec,
			Bitmap:     track.Bitmap,
			Format:     format,
		}

		if track.Bitmap {
			result.Error = "图形字幕（" + track.Codec + "）无法提取为文本格式"
			results = append(results, result)
			continue
		}

		outputPath, err := s.ExtractSubtitle(filePath, track.Index, format)
		if err != nil {
			result.Error = err.Error()
			s.logger.Warnf("提取字幕轨道 #%d 失败: %v", track.Index, err)
		} else {
			result.Path = outputPath
		}

		results = append(results, result)
	}

	return results, nil
}

// ==================== P1: 编码自动检测与转换 ====================

// ConvertSubtitleToVTTWithEncoding 带编码检测的字幕转换
// 先检测文件编码，如果非 UTF-8 则先转码为 UTF-8 临时文件，再交给 FFmpeg 转换
func (s *ScannerService) ConvertSubtitleToVTTWithEncoding(subtitlePath string) (string, error) {
	// 确定输出文件路径
	cacheDir := filepath.Join(s.cfg.Cache.CacheDir, "subtitles")
	os.MkdirAll(cacheDir, 0755)

	baseName := strings.TrimSuffix(filepath.Base(subtitlePath), filepath.Ext(subtitlePath))
	outputPath := filepath.Join(cacheDir, fmt.Sprintf("%s_ext.vtt", baseName))

	// 检查缓存
	if outInfo, err := os.Stat(outputPath); err == nil {
		if srcInfo, err := os.Stat(subtitlePath); err == nil {
			if outInfo.ModTime().After(srcInfo.ModTime()) {
				return outputPath, nil
			}
		}
	}

	// 编码检测：读取原始文件
	raw, err := os.ReadFile(subtitlePath)
	if err != nil {
		return "", fmt.Errorf("读取字幕文件失败: %w", err)
	}

	// 检测是否为 UTF-8
	inputPath := subtitlePath
	needCleanup := false

	if !isValidUTF8(raw) {
		// 非 UTF-8，使用 SubtitleCleaner 的编码检测逻辑
		cleaner := NewSubtitleCleaner(SubtitleCleanConfig{AutoDetectEncoding: true}, s.logger)
		content, encoding, converted := cleaner.detectAndConvertEncoding(subtitlePath)

		if converted && content != "" {
			s.logger.Infof("字幕编码转换: %s -> UTF-8 (检测到: %s)", filepath.Base(subtitlePath), encoding)

			// 写入 UTF-8 临时文件
			tmpPath := filepath.Join(cacheDir, fmt.Sprintf("%s_utf8%s", baseName, filepath.Ext(subtitlePath)))
			if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
				return "", fmt.Errorf("写入 UTF-8 临时文件失败: %w", err)
			}
			inputPath = tmpPath
			needCleanup = true
		}
	}

	// 使用 FFmpeg 转换为 WebVTT
	cmd := exec.Command(s.cfg.App.FFmpegPath,
		"-y",
		"-i", inputPath,
		"-c:s", "webvtt",
		outputPath,
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		// 清理临时文件
		if needCleanup {
			os.Remove(inputPath)
		}
		return "", fmt.Errorf("FFmpeg字幕转换失败: %w, 输出: %s", err, string(output))
	}

	// 清理临时文件
	if needCleanup {
		os.Remove(inputPath)
	}

	s.logger.Debugf("字幕转换完成（含编码检测）: %s -> %s", subtitlePath, outputPath)
	return outputPath, nil
}

// isValidUTF8 检查字节数据是否为有效的 UTF-8 编码
func isValidUTF8(data []byte) bool {
	// 跳过 BOM
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	return utf8.Valid(data)
}

// EnsureUTF8Subtitle 确保字幕文件为 UTF-8 编码（保持原始格式不变）
// 用于 Android 端 ExoPlayer 直接解析 ASS/SRT 等格式时，确保编码正确
func (s *ScannerService) EnsureUTF8Subtitle(subtitlePath string) (string, error) {
	// 读取原始文件
	raw, err := os.ReadFile(subtitlePath)
	if err != nil {
		return "", fmt.Errorf("读取字幕文件失败: %w", err)
	}

	// 如果已经是 UTF-8，直接返回原始路径
	if isValidUTF8(raw) {
		return subtitlePath, nil
	}

	// 非 UTF-8，进行编码转换
	cacheDir := filepath.Join(s.cfg.Cache.CacheDir, "subtitles")
	os.MkdirAll(cacheDir, 0755)

	baseName := strings.TrimSuffix(filepath.Base(subtitlePath), filepath.Ext(subtitlePath))
	ext := filepath.Ext(subtitlePath)
	outputPath := filepath.Join(cacheDir, fmt.Sprintf("%s_utf8%s", baseName, ext))

	// 检查缓存
	if outInfo, err := os.Stat(outputPath); err == nil {
		if srcInfo, err := os.Stat(subtitlePath); err == nil {
			if outInfo.ModTime().After(srcInfo.ModTime()) {
				return outputPath, nil
			}
		}
	}

	// 使用编码检测逻辑转换
	cleaner := NewSubtitleCleaner(SubtitleCleanConfig{AutoDetectEncoding: true}, s.logger)
	content, encoding, converted := cleaner.detectAndConvertEncoding(subtitlePath)

	if converted && content != "" {
		s.logger.Infof("字幕编码转换（保持原格式）: %s -> UTF-8 (检测到: %s)", filepath.Base(subtitlePath), encoding)
		if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
			return "", fmt.Errorf("写入 UTF-8 字幕文件失败: %w", err)
		}
		return outputPath, nil
	}

	// 转换失败，返回原始文件
	return subtitlePath, nil
}

// ==================== P2: 异步字幕提取 + 进度反馈 ====================

// 字幕提取事件常量
const (
	EventSubExtractStarted   = "sub_extract_started"
	EventSubExtractProgress  = "sub_extract_progress"
	EventSubExtractCompleted = "sub_extract_completed"
	EventSubExtractFailed    = "sub_extract_failed"
)

// SubExtractProgressData 字幕提取进度事件数据
type SubExtractProgressData struct {
	MediaID    string                  `json:"media_id"`
	MediaTitle string                  `json:"media_title"`
	Format     string                  `json:"format"`
	Total      int                     `json:"total"`
	Current    int                     `json:"current"`
	Progress   float64                 `json:"progress"`
	Message    string                  `json:"message"`
	Results    []ExtractedSubtitleFile `json:"results,omitempty"`
	Error      string                  `json:"error,omitempty"`
}

// ExtractAllSubtitlesAsync 异步批量提取字幕（适用于大文件，通过 WebSocket 推送进度）
func (s *ScannerService) ExtractAllSubtitlesAsync(mediaID, mediaTitle, filePath, format string, trackIndices []int) {
	go func() {
		if format == "" {
			format = "srt"
		}

		// 广播开始事件
		s.broadcastSubExtractEvent(EventSubExtractStarted, &SubExtractProgressData{
			MediaID:    mediaID,
			MediaTitle: mediaTitle,
			Format:     format,
			Message:    "开始提取字幕...",
		})

		// 获取所有字幕轨道
		tracks, err := s.GetSubtitleTracks(filePath)
		if err != nil {
			s.broadcastSubExtractEvent(EventSubExtractFailed, &SubExtractProgressData{
				MediaID:    mediaID,
				MediaTitle: mediaTitle,
				Error:      fmt.Sprintf("获取字幕轨道失败: %v", err),
				Message:    "提取失败",
			})
			return
		}

		// 过滤目标轨道
		var targetTracks []SubtitleTrack
		if len(trackIndices) > 0 {
			indexSet := make(map[int]bool)
			for _, idx := range trackIndices {
				indexSet[idx] = true
			}
			for _, t := range tracks {
				if indexSet[t.Index] {
					targetTracks = append(targetTracks, t)
				}
			}
		} else {
			targetTracks = tracks
		}

		total := len(targetTracks)
		var results []ExtractedSubtitleFile

		for i, track := range targetTracks {
			result := ExtractedSubtitleFile{
				TrackIndex: track.Index,
				Language:   track.Language,
				Title:      track.Title,
				Codec:      track.Codec,
				Bitmap:     track.Bitmap,
				Format:     format,
			}

			// 广播进度
			progress := float64(i) / float64(total) * 100
			s.broadcastSubExtractEvent(EventSubExtractProgress, &SubExtractProgressData{
				MediaID:    mediaID,
				MediaTitle: mediaTitle,
				Format:     format,
				Total:      total,
				Current:    i + 1,
				Progress:   progress,
				Message:    fmt.Sprintf("正在提取轨道 #%d (%d/%d)...", track.Index, i+1, total),
			})

			if track.Bitmap {
				result.Error = "图形字幕无法提取为文本格式"
			} else {
				outputPath, err := s.ExtractSubtitle(filePath, track.Index, format)
				if err != nil {
					result.Error = err.Error()
					s.logger.Warnf("异步提取字幕轨道 #%d 失败: %v", track.Index, err)
				} else {
					result.Path = outputPath
				}
			}

			results = append(results, result)
		}

		// 广播完成事件
		successCount := 0
		for _, r := range results {
			if r.Error == "" {
				successCount++
			}
		}

		s.broadcastSubExtractEvent(EventSubExtractCompleted, &SubExtractProgressData{
			MediaID:    mediaID,
			MediaTitle: mediaTitle,
			Format:     format,
			Total:      total,
			Current:    total,
			Progress:   100,
			Message:    fmt.Sprintf("提取完成: %d/%d 个轨道成功", successCount, total),
			Results:    results,
		})

		s.logger.Infof("异步字幕提取完成: %s, %d/%d 成功", mediaTitle, successCount, total)
	}()
}

// broadcastSubExtractEvent 广播字幕提取事件
func (s *ScannerService) broadcastSubExtractEvent(eventType string, data *SubExtractProgressData) {
	if s.wsHub != nil {
		s.wsHub.BroadcastEvent(eventType, data)
	}
}
