package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/fan-video/fan-video/internal/service"
	"go.uber.org/zap"
)

// SubtitleHandler 字幕处理器
type SubtitleHandler struct {
	scanner       *service.ScannerService
	streamService *service.StreamService
	logger        *zap.SugaredLogger
}

// ListTracks 获取媒体的字幕轨道列表（内嵌 + 外挂）
func (h *SubtitleHandler) ListTracks(c *gin.Context) {
	id := c.Param("id")

	// 获取媒体文件路径
	filePath, _, err := h.streamService.GetDirectStreamInfo(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "媒体不存在"})
		return
	}

	// STRM 远程流不支持字幕提取
	if filePath == "__strm__" {
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"embedded": []interface{}{},
				"external": []interface{}{},
			},
		})
		return
	}

	// 获取内嵌字幕
	embedded, err := h.scanner.GetSubtitleTracks(filePath)
	if err != nil {
		h.logger.Warnf("获取内嵌字幕失败: %v", err)
		embedded = nil
	}

	// 获取外挂字幕
	external := h.scanner.GetExternalSubtitles(filePath)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"embedded": embedded,
			"external": external,
		},
	})
}

// ExtractTrack 提取内嵌字幕为WebVTT格式
func (h *SubtitleHandler) ExtractTrack(c *gin.Context) {
	id := c.Param("id")
	indexStr := c.Param("index")

	streamIndex, err := strconv.Atoi(indexStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的字幕轨道索引"})
		return
	}

	// 获取媒体文件路径
	filePath, _, err := h.streamService.GetDirectStreamInfo(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "媒体不存在"})
		return
	}

	// STRM 远程流不支持字幕提取
	if filePath == "__strm__" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "STRM 远程流不支持字幕提取"})
		return
	}
	tracks, err := h.scanner.GetSubtitleTracks(filePath)
	if err == nil {
		for _, track := range tracks {
			if track.Index == streamIndex && track.Bitmap {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "该字幕轨道为图形字幕（" + track.Codec + "），无法提取为文本格式",
				})
				return
			}
		}
	}

	// 提取字幕为WebVTT格式（浏览器原生支持）
	vttPath, err := h.scanner.ExtractSubtitle(filePath, streamIndex, "vtt")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "字幕提取失败: " + err.Error()})
		return
	}

	c.Header("Content-Type", "text/vtt; charset=utf-8")
	c.Header("Cache-Control", "public, max-age=604800")
	c.File(vttPath)
}

// ServeExternal 提供外挂字幕文件（自动转换为WebVTT格式）
// 支持 format 参数：
//   - format=raw: 返回原始字幕文件（适用于 ExoPlayer 等原生支持 ASS/SRT 的播放器）
//   - 默认: 转换为 WebVTT 格式（适用于浏览器）
func (h *SubtitleHandler) ServeExternal(c *gin.Context) {
	// 外挂字幕路径通过query参数传入
	subPath := c.Query("path")
	if subPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少字幕路径"})
		return
	}

	// 是否返回原始格式（Android 端 ExoPlayer 原生支持 ASS/SRT）
	rawFormat := c.Query("format") == "raw"

	// 安全检查：确保是字幕文件
	ext := service.GetFileExt(subPath)
	switch ext {
	case ".vtt":
		// VTT 格式直接返回
		c.Header("Content-Type", "text/vtt; charset=utf-8")
		c.Header("Cache-Control", "public, max-age=604800")
		c.File(subPath)
		return
	case ".srt":
		if rawFormat {
			// 原始 SRT 格式直接返回（ExoPlayer 原生支持）
			// 先做编码检测转换为 UTF-8
			utf8Path, err := h.scanner.EnsureUTF8Subtitle(subPath)
			if err != nil {
				h.logger.Warnf("SRT 编码转换失败，直接返回原始文件: %v", err)
				c.Header("Content-Type", "application/x-subrip; charset=utf-8")
				c.Header("Cache-Control", "public, max-age=604800")
				c.File(subPath)
				return
			}
			c.Header("Content-Type", "application/x-subrip; charset=utf-8")
			c.Header("Cache-Control", "public, max-age=604800")
			c.File(utf8Path)
			return
		}
		// 默认转换为 WebVTT
		vttPath, err := h.scanner.ConvertSubtitleToVTTWithEncoding(subPath)
		if err != nil {
			h.logger.Warnf("字幕转换失败，尝试直接返回原始文件: %v", err)
			c.Header("Content-Type", "text/plain; charset=utf-8")
			c.Header("Cache-Control", "public, max-age=604800")
			c.File(subPath)
			return
		}
		c.Header("Content-Type", "text/vtt; charset=utf-8")
		c.Header("Cache-Control", "public, max-age=604800")
		c.File(vttPath)
		return
	case ".ass", ".ssa":
		if rawFormat {
			// 原始 ASS/SSA 格式直接返回（ExoPlayer 原生支持，保留样式信息）
			// 先做编码检测转换为 UTF-8
			utf8Path, err := h.scanner.EnsureUTF8Subtitle(subPath)
			if err != nil {
				h.logger.Warnf("ASS 编码转换失败，直接返回原始文件: %v", err)
				c.Header("Content-Type", "text/x-ssa; charset=utf-8")
				c.Header("Cache-Control", "public, max-age=604800")
				c.File(subPath)
				return
			}
			c.Header("Content-Type", "text/x-ssa; charset=utf-8")
			c.Header("Cache-Control", "public, max-age=604800")
			c.File(utf8Path)
			return
		}
		// 默认转换为 WebVTT（浏览器用）
		vttPath, err := h.scanner.ConvertSubtitleToVTTWithEncoding(subPath)
		if err != nil {
			h.logger.Warnf("字幕转换失败，尝试直接返回原始文件: %v", err)
			c.Header("Content-Type", "text/plain; charset=utf-8")
			c.Header("Cache-Control", "public, max-age=604800")
			c.File(subPath)
			return
		}
		c.Header("Content-Type", "text/vtt; charset=utf-8")
		c.Header("Cache-Control", "public, max-age=604800")
		c.File(vttPath)
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的字幕格式"})
		return
	}
}

// ==================== P0: 批量字幕提取导出 ====================

// ExtractAll 批量提取视频中所有（或指定）字幕轨道
func (h *SubtitleHandler) ExtractAll(c *gin.Context) {
	id := c.Param("id")

	// 获取媒体文件路径
	filePath, _, err := h.streamService.GetDirectStreamInfo(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "媒体不存在"})
		return
	}

	if filePath == "__strm__" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "STRM 远程流不支持字幕提取"})
		return
	}

	// 解析请求参数
	var req struct {
		Format string `json:"format"` // 输出格式: srt | vtt，默认 srt
		Tracks []int  `json:"tracks"` // 指定轨道索引，为空则提取所有
	}
	c.ShouldBindJSON(&req)

	if req.Format == "" {
		req.Format = "srt"
	}
	if req.Format != "srt" && req.Format != "vtt" && req.Format != "ass" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的输出格式，可选: srt, vtt, ass"})
		return
	}

	results, err := h.scanner.ExtractAllSubtitles(filePath, req.Format, req.Tracks)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 统计成功/失败数
	successCount := 0
	failCount := 0
	for _, r := range results {
		if r.Error == "" {
			successCount++
		} else {
			failCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("提取完成: %d 成功, %d 失败", successCount, failCount),
		"data": gin.H{
			"files":   results,
			"success": successCount,
			"failed":  failCount,
			"total":   len(results),
		},
	})
}

// ==================== P2: 异步字幕提取（大文件） ====================

// ExtractAllAsync 异步批量提取字幕（通过 WebSocket 推送进度）
func (h *SubtitleHandler) ExtractAllAsync(c *gin.Context) {
	id := c.Param("id")

	// 获取媒体文件路径
	filePath, _, err := h.streamService.GetDirectStreamInfo(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "媒体不存在"})
		return
	}

	if filePath == "__strm__" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "STRM 远程流不支持字幕提取"})
		return
	}

	var req struct {
		Format string `json:"format"`
		Tracks []int  `json:"tracks"`
		Title  string `json:"title"` // 媒体标题（用于进度消息）
	}
	c.ShouldBindJSON(&req)

	if req.Format == "" {
		req.Format = "srt"
	}

	// 启动异步提取
	h.scanner.ExtractAllSubtitlesAsync(id, req.Title, filePath, req.Format, req.Tracks)

	c.JSON(http.StatusOK, gin.H{
		"message": "异步字幕提取任务已启动，请通过 WebSocket 监听进度",
	})
}

// DownloadExtracted 下载已提取的字幕文件
func (h *SubtitleHandler) DownloadExtracted(c *gin.Context) {
	filePath := c.Query("path")
	if filePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少文件路径"})
		return
	}

	// 安全检查：确保文件在缓存目录下
	ext := service.GetFileExt(filePath)
	switch ext {
	case ".srt", ".vtt", ".ass", ".ssa":
		// 允许的字幕格式
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的文件格式"})
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(filePath)))
	c.Header("Content-Type", "application/octet-stream")
	c.File(filePath)
}
