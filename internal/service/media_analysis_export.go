package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrHighlightNotFound 表示指定的精彩片段不存在或不属于该媒体。
var ErrHighlightNotFound = errors.New("精彩片段不存在")

// highlightExportMaxSeconds 单个片段导出时长上限（秒），防止误操作整片导出。
const highlightExportMaxSeconds = 15 * 60

// HighlightExport 描述一个已导出的精彩片段视频文件。
type HighlightExport struct {
	HighlightID string    `json:"highlight_id"`
	MediaID     string    `json:"media_id"`
	Title       string    `json:"title"`
	FileName    string    `json:"file_name"`
	SizeBytes   int64     `json:"size_bytes"`
	Duration    float64   `json:"duration"`
	ExportedAt  time.Time `json:"exported_at"`
}

// highlightExportDir 导出文件保存目录：<DataDir>/exports/highlights/<mediaID>/。
// 放在 DataDir（持久卷）而不是 CacheDir：导出片段是用户主动生成的分享产物，
// 不应被缓存自动清理回收。
func (s *MediaAnalysisService) highlightExportDir(mediaID string) string {
	return filepath.Join(s.cfg.App.DataDir, "exports", "highlights", mediaID)
}

// sanitizeHighlightFileName 清理标题中的路径分隔符与控制字符，保留中日文等 Unicode 字符。
func sanitizeHighlightFileName(title string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_", "\n", "_", "\r", "_",
	)
	clean := strings.TrimSpace(replacer.Replace(title))
	if clean == "" {
		clean = "highlight"
	}
	const maxRunes = 60
	runes := []rune(clean)
	if len(runes) > maxRunes {
		clean = string(runes[:maxRunes])
	}
	return clean
}

// ExportHighlightClip 将一个精彩片段切片导出为独立 mp4 文件（精确重编码）。
func (s *MediaAnalysisService) ExportHighlightClip(mediaID, highlightID string) (*HighlightExport, error) {
	media, err := s.mediaRepo.FindByID(mediaID)
	if err != nil {
		return nil, ErrMediaNotFound
	}
	hl, err := s.highlightRepo.FindByID(highlightID)
	if err != nil || hl.MediaID != mediaID {
		return nil, ErrHighlightNotFound
	}

	duration := hl.EndTime - hl.StartTime
	if duration <= 0 || hl.StartTime < 0 {
		return nil, fmt.Errorf("片段时间区间无效")
	}
	if duration > highlightExportMaxSeconds {
		return nil, fmt.Errorf("片段超过 %d 分钟，无法导出", highlightExportMaxSeconds/60)
	}
	if _, err := os.Stat(media.FilePath); err != nil {
		return nil, fmt.Errorf("源视频文件不可访问: %w", err)
	}

	dir := s.highlightExportDir(mediaID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fileName := hl.ID + "_" + sanitizeHighlightFileName(hl.Title) + ".mp4"
	output := filepath.Join(dir, fileName)

	// 幂等：同片段重复导出直接覆盖（ffmpeg -y）
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(duration*8+120)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.cfg.App.FFmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.3f", hl.StartTime),
		"-i", media.FilePath,
		"-t", fmt.Sprintf("%.3f", duration),
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "22",
		"-c:a", "aac", "-b:a", "160k", "-ac", "2",
		"-movflags", "+faststart",
		"-y", output,
	)
	if data, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(output)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("导出超时")
		}
		return nil, fmt.Errorf("ffmpeg 切片失败: %s", strings.TrimSpace(string(data)))
	}

	info, err := os.Stat(output)
	if err != nil {
		return nil, fmt.Errorf("导出产物校验失败: %w", err)
	}
	return &HighlightExport{
		HighlightID: hl.ID,
		MediaID:     mediaID,
		Title:       hl.Title,
		FileName:    fileName,
		SizeBytes:   info.Size(),
		Duration:    duration,
		ExportedAt:  info.ModTime(),
	}, nil
}

// ListHighlightExports 列出某媒体已导出的片段文件。
func (s *MediaAnalysisService) ListHighlightExports(mediaID string) ([]HighlightExport, error) {
	dir := s.highlightExportDir(mediaID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []HighlightExport{}, nil
		}
		return nil, err
	}
	exports := make([]HighlightExport, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mp4") {
			continue
		}
		id, title, ok := strings.Cut(strings.TrimSuffix(e.Name(), ".mp4"), "_")
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		exports = append(exports, HighlightExport{
			HighlightID: id,
			MediaID:     mediaID,
			Title:       title,
			FileName:    e.Name(),
			SizeBytes:   info.Size(),
			ExportedAt:  info.ModTime(),
		})
	}
	return exports, nil
}

// FindHighlightExportPath 返回指定片段的导出文件完整路径。
func (s *MediaAnalysisService) FindHighlightExportPath(mediaID, highlightID string) (string, error) {
	dir := s.highlightExportDir(mediaID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", ErrHighlightNotFound
	}
	prefix := highlightID + "_"
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".mp4") {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", ErrHighlightNotFound
}

// DeleteHighlightExport 删除指定片段的导出文件。
func (s *MediaAnalysisService) DeleteHighlightExport(mediaID, highlightID string) error {
	path, err := s.FindHighlightExportPath(mediaID, highlightID)
	if err != nil {
		return err
	}
	return os.Remove(path)
}
