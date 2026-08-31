package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fan-video/fan-video/internal/model"
	"gorm.io/gorm"
)

const (
	thumbnailWidth  = 240
	thumbnailFormat = "webp"
)

// thumbBaseDir 缩略图根目录（与数据卷分离，避免只读挂载问题）
const thumbBaseDir = "/cache/thumbs"

// GetThumbPath 生成缩略图文件路径，存放在 /cache/thumbs 下，保留原路径结构
func GetThumbPath(posterPath string) string {
	// 去掉前导 /，保留相对路径，替换扩展名为 .webp
	rel := strings.TrimPrefix(posterPath, "/")
	ext := filepath.Ext(rel)
	name := strings.TrimSuffix(rel, ext)
	return filepath.Join(thumbBaseDir, name+"."+thumbnailFormat)
}

// posterImageExts 常见海报图片扩展名（含大写变体）。用于在 DB 记录的
// poster_path 扩展名与实际磁盘文件不一致时（如记录为 .jpeg 但文件为 .webp），
// 兜底定位真实存在的海报文件，保证缩略图照常生成。
var posterImageExts = []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".avif", ".heic"}

// resolvePosterFile 返回实际存在的海报源文件路径。
// 若 posterPath 本身存在则直接返回；否则尝试同目录下、同名不同扩展名的文件来修正
// 扩展名漂移。找不到任何候选时，返回原路径（交由调用方报错）。
func resolvePosterFile(posterPath string) string {
	if posterPath == "" {
		return posterPath
	}
	if _, err := os.Stat(posterPath); err == nil {
		return posterPath
	}
	dir := filepath.Dir(posterPath)
	base := strings.TrimSuffix(filepath.Base(posterPath), filepath.Ext(posterPath))
	for _, ext := range posterImageExts {
		// 先试小写扩展名，再试大写变体（磁盘上文件可能为 .JPG/.PNG 等）。
		for _, candidateExt := range []string{ext, strings.ToUpper(ext)} {
			candidate := filepath.Join(dir, base+candidateExt)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return posterPath
}

// EnsureThumbnail 确保指定海报有对应的缩略图。如果已存在则直接返回，否则使用 FFmpeg 生成。
func EnsureThumbnail(posterPath string) (string, error) {
	// 记录的真实路径可能与磁盘文件扩展名不一致（如 .jpeg vs .webp），先定位真实源文件，
	// 这样缩略图路径、审计、海报展示都基于真实文件保持一致。
	posterPath = resolvePosterFile(posterPath)
	thumbPath := GetThumbPath(posterPath)

	// 检查是否已存在
	if _, err := os.Stat(thumbPath); err == nil {
		return thumbPath, nil
	}

	// 确保父目录存在
	parentDir := filepath.Dir(thumbPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return "", fmt.Errorf("创建缩略图目录失败: %w", err)
	}

	// 使用 FFmpeg 生成固定宽度缩略图
	// -vf "scale=128:-2"：按宽度128缩放，高度自动保持比例
	// -frames:v 1：只处理第一帧（对于图片输入是生成缩略图）
	// -c:v libwebp：输出 WebP 格式，体积小
	// -quality 75：质量平衡
	args := []string{
		"-i", posterPath,
		"-vf", fmt.Sprintf("scale=%d:-2", thumbnailWidth),
		"-frames:v", "1",
		"-c:v", "libwebp",
		"-quality", "95",
		"-lossless", "0",
		thumbPath,
	}

	cmd := exec.CommandContext(context.Background(), "ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 清理可能的部分文件
		os.Remove(thumbPath)
		return "", fmt.Errorf("FFmpeg 生成缩略图失败: %s", string(output))
	}

	// 验证文件是否已创建
	if _, err := os.Stat(thumbPath); err != nil {
		return "", fmt.Errorf("缩略图文件未生成: %w", err)
	}

	return thumbPath, nil
}

// ThumbnailStats 缩略图统计信息
type ThumbnailStats struct {
	Total     int `json:"total"`
	Generated int `json:"generated"`
	Missing   int `json:"missing"`
}

// ThumbnailAuditItem 缩略图完整性检查中的单条问题
type ThumbnailAuditItem struct {
	MediaID    string `json:"media_id"`
	Title      string `json:"title"`
	PosterPath string `json:"poster_path"`
	ThumbPath  string `json:"thumb_path"`
	Detail     string `json:"detail"`
}

// ThumbnailAuditReport 缩略图完整性检查报告（三分类）
type ThumbnailAuditReport struct {
	Total         int                  `json:"total"`
	Generated     int                  `json:"generated"`
	PosterDeleted []ThumbnailAuditItem `json:"poster_deleted"` // 缩略图存在但源海报已删除
	ThumbMissing  []ThumbnailAuditItem `json:"thumb_missing"`  // 海报存在但缩略图缺失
	OrphanThumbs  []ThumbnailAuditItem `json:"orphan_thumbs"`  // 缩略图文件无对应媒体
}

// DeleteThumbnail 删除指定媒体的缩略图文件
func DeleteThumbnail(posterPath string) error {
	thumbPath := GetThumbPath(posterPath)
	if err := os.Remove(thumbPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除缩略图失败: %w", err)
	}
	return nil
}

// PurgeThumbnails 一键清空数据/清理缓存时删除全部海报缩略图（含其目录结构）。
// 缩略图属可再生缓存，直接删除整个 /cache/thumbs 目录树（父目录一起移除，
// 后续 EnsureThumbnail 会自动重建）；目录不存在时返回 0 而非报错。
func PurgeThumbnails() (int, error) {
	if _, err := os.Stat(thumbBaseDir); os.IsNotExist(err) {
		return 0, nil
	}
	count := 0
	_ = filepath.Walk(thumbBaseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if filepath.Ext(path) == "."+thumbnailFormat {
			count++
		}
		return nil
	})
	if err := os.RemoveAll(thumbBaseDir); err != nil {
		return count, fmt.Errorf("清理海报缩略图目录失败: %w", err)
	}
	return count, nil
}

// GetThumbnailStats 统计全库缩略图覆盖情况
func GetThumbnailStats(db *gorm.DB) (ThumbnailStats, error) {
	var medias []model.Media
	if err := db.Model(&model.Media{}).Find(&medias).Error; err != nil {
		return ThumbnailStats{}, fmt.Errorf("获取媒体列表失败: %w", err)
	}
	stats := ThumbnailStats{Total: len(medias)}
	for _, media := range medias {
		if media.PosterPath == "" {
			stats.Missing++
			continue
		}
		if _, err := os.Stat(GetThumbPath(media.PosterPath)); err == nil {
			stats.Generated++
		} else {
			stats.Missing++
		}
	}
	return stats, nil
}

// thumbPathToPosterKey 从缩略图路径反推海报键（用于与 DB 记录比对）
func thumbPathToPosterKey(thumbPath string) string {
	rel := strings.TrimPrefix(thumbPath, thumbBaseDir+"/")
	rel = strings.TrimPrefix(rel, thumbBaseDir)
	rel = strings.TrimPrefix(rel, "/")
	ext := filepath.Ext(rel)
	return strings.TrimSuffix(rel, ext) // e.g. "data/movies/poster"
}

// GetThumbnailAudit 完整性检查：三分类报告
func GetThumbnailAudit(db *gorm.DB) (ThumbnailAuditReport, error) {
	var medias []model.Media
	if err := db.Model(&model.Media{}).Find(&medias).Error; err != nil {
		return ThumbnailAuditReport{}, fmt.Errorf("获取媒体列表失败: %w", err)
	}

	// 构建 thumb_key → media 的映射，用于孤儿检测
	thumbToMedia := make(map[string]*model.Media)
	for i := range medias {
		if medias[i].PosterPath == "" {
			continue
		}
		key := thumbPathToPosterKey(GetThumbPath(medias[i].PosterPath))
		thumbToMedia[key] = &medias[i]
	}

	report := ThumbnailAuditReport{
		Total:         len(medias),
		PosterDeleted: make([]ThumbnailAuditItem, 0),
		ThumbMissing:  make([]ThumbnailAuditItem, 0),
		OrphanThumbs:  make([]ThumbnailAuditItem, 0),
	}

	// 1. 遍历 DB 媒体：检测 poster_deleted 和 thumb_missing
	for _, media := range medias {
		if media.PosterPath == "" {
			// 无海报：缩略图缺失（海报本身不存在）
			report.ThumbMissing = append(report.ThumbMissing, ThumbnailAuditItem{
				MediaID: media.ID,
				Title:   media.Title,
				Detail:  "无海报图，无法生成缩略图",
			})
			continue
		}

		posterExists := false
		if _, err := os.Stat(media.PosterPath); err == nil {
			posterExists = true
		}
		thumbExists := false
		thumbPath := GetThumbPath(media.PosterPath)
		if _, err := os.Stat(thumbPath); err == nil {
			thumbExists = true
		}

		if posterExists {
			report.Generated++
		}

		if thumbExists && !posterExists {
			// 缩略图存在但源海报已删除
			report.PosterDeleted = append(report.PosterDeleted, ThumbnailAuditItem{
				MediaID:    media.ID,
				Title:      media.Title,
				PosterPath: media.PosterPath,
				ThumbPath:  thumbPath,
				Detail:     "源海报已删除，缩略图已过期",
			})
		} else if !thumbExists {
			// 海报存在但缩略图缺失
			detail := "缩略图文件缺失"
			if !posterExists {
				detail = "海报与缩略图均缺失"
			}
			report.ThumbMissing = append(report.ThumbMissing, ThumbnailAuditItem{
				MediaID:    media.ID,
				Title:      media.Title,
				PosterPath: media.PosterPath,
				ThumbPath:  thumbPath,
				Detail:     detail,
			})
		}
	}

	// 2. 扫描缩略图目录：检测孤儿文件
	_ = filepath.Walk(thumbBaseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != "."+thumbnailFormat {
			return nil
		}
		key := thumbPathToPosterKey(path)
		if _, found := thumbToMedia[key]; !found {
			report.OrphanThumbs = append(report.OrphanThumbs, ThumbnailAuditItem{
				ThumbPath: path,
				Detail:    "缩略图文件无对应媒体记录",
			})
		}
		return nil
	})

	return report, nil
}

// CleanThumbnailAuditIssues 清理缩略图完整性问题
func CleanThumbnailAuditIssues(db *gorm.DB, deleteOrphan, deleteStale bool) (int, error) {
	deleted := 0

	// 获取报告
	report, err := GetThumbnailAudit(db)
	if err != nil {
		return 0, err
	}

	// 删除孤儿缩略图文件
	if deleteOrphan {
		for _, item := range report.OrphanThumbs {
			if item.ThumbPath != "" {
				if err := os.Remove(item.ThumbPath); err == nil {
					deleted++
				}
			}
		}
	}

	// 删除源海报已删除的缩略图文件
	if deleteStale {
		for _, item := range report.PosterDeleted {
			if item.ThumbPath != "" {
				if err := os.Remove(item.ThumbPath); err == nil {
					deleted++
				}
			}
		}
	}

	return deleted, nil
}