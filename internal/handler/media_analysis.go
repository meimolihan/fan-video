package handler

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MediaAnalysisHandler exposes local/distributed media analysis.
// It intentionally does not accept or inspect AI configuration.
type MediaAnalysisHandler struct {
	analysis *service.MediaAnalysisService
	logger   *zap.SugaredLogger
}

func NewMediaAnalysisHandler(analysis *service.MediaAnalysisService, logger *zap.SugaredLogger) *MediaAnalysisHandler {
	return &MediaAnalysisHandler{analysis: analysis, logger: logger}
}

type mediaHighlightView struct {
	ID             string  `json:"id"`
	MediaID        string  `json:"media_id"`
	Title          string  `json:"title"`
	StartTime      float64 `json:"start_time"`
	EndTime        float64 `json:"end_time"`
	Score          float64 `json:"score"`
	Tags           string  `json:"tags"`
	Source         string  `json:"source"`
	AnalysisMethod string  `json:"analysis_method"`
	ThumbnailURL   string  `json:"thumbnail_url,omitempty"`
	PreviewURL     string  `json:"preview_url,omitempty"`
	Version        int     `json:"version"`
}

func highlightView(mediaID string, h model.VideoHighlight) mediaHighlightView {
	view := mediaHighlightView{
		ID: h.ID, MediaID: h.MediaID, Title: h.Title,
		StartTime: h.StartTime, EndTime: h.EndTime, Score: h.Score,
		Tags: h.Tags, Source: h.Source, AnalysisMethod: h.AnalysisMethod, Version: h.Version,
	}
	// 即使分析阶段因为宿主 FFmpeg 缺少 libwebp 没有成功写入 Thumbnail，
	// 也始终暴露稳定 URL。首次图片请求会走 portable fallback 生成 JPEG 并持久化，
	// 避免 macOS / 精简 FFmpeg 环境出现“有精彩片段记录但整排空白”的状态。
	view.ThumbnailURL = "/api/media/" + mediaID + "/highlights/" + h.ID + "/thumbnail"
	// preview_thumbnail_v1 已把客户端生成的精彩片段也接入同一 lazy preview URL。
	// 首次请求时由 Media Compute Node V2 尝试 Desktop -> Android；若服务器缺少
	// libwebp，则 portable fallback 会直接生成浏览器可播放的 GIF，避免 hover 404。
	view.PreviewURL = "/api/media/" + mediaID + "/highlights/" + h.ID + "/preview"
	return view
}

func (h *MediaAnalysisHandler) ListHighlights(c *gin.Context) {
	mediaID := c.Param("id")
	result, err := h.analysis.ListHighlights(mediaID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrMediaNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	items := make([]mediaHighlightView, 0, len(result.Highlights))
	for _, item := range result.Highlights {
		items = append(items, highlightView(mediaID, item))
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"highlights": items,
		"stale":      result.Stale,
	}})
}

func (h *MediaAnalysisHandler) AnalyzeHighlights(c *gin.Context) {
	mediaID := c.Param("id")
	task, err := h.analysis.AnalyzeHighlights(mediaID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrMediaAnalysisInProgress):
			c.JSON(http.StatusAccepted, gin.H{"data": task, "message": "精彩片段分析已在进行中"})
			return
		case errors.Is(err, service.ErrMediaAnalysisUnsupported):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "当前媒体源暂不支持本地精彩片段分析"})
			return
		case errors.Is(err, service.ErrMediaNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "媒体不存在"})
			return
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "启动精彩片段分析失败: " + err.Error()})
			return
		}
	}
	c.JSON(http.StatusAccepted, gin.H{"data": task, "message": "精彩片段分析任务已启动"})
}

func (h *MediaAnalysisHandler) Status(c *gin.Context) {
	mediaID := c.Param("id")
	task, err := h.analysis.LatestTask(mediaID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusOK, gin.H{"data": nil})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取分析状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": task})
}

func (h *MediaAnalysisHandler) DeleteHighlights(c *gin.Context) {
	mediaID := c.Param("id")
	if err := h.analysis.DeleteHighlights(mediaID); err != nil && !errors.Is(err, os.ErrNotExist) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除精彩片段失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "精彩片段已删除"})
}

func (h *MediaAnalysisHandler) Thumbnail(c *gin.Context) {
	mediaID := c.Param("id")
	highlightID := c.Param("highlightId")
	path, err := h.analysis.EnsureHighlightThumbnailPortable(mediaID, highlightID)
	if err != nil {
		h.logger.Debugf("highlight thumbnail unavailable media=%s highlight=%s: %v", mediaID, highlightID, err)
		c.Status(http.StatusNotFound)
		return
	}
	h.serveFile(c, path)
}

func (h *MediaAnalysisHandler) Preview(c *gin.Context) {
	mediaID := c.Param("id")
	highlightID := c.Param("highlightId")
	path, err := h.analysis.EnsureHighlightPreviewPortable(mediaID, highlightID)
	if err != nil {
		h.logger.Debugf("lazy highlight preview unavailable media=%s highlight=%s: %v", mediaID, highlightID, err)
		c.Status(http.StatusNotFound)
		return
	}
	h.serveFile(c, path)
}

func (h *MediaAnalysisHandler) serveFile(c *gin.Context, path string) {
	c.Header("Cache-Control", "private, max-age=86400")
	c.File(path)
}

// ==================== 精彩片段导出（独立 mp4） ====================

// ExportHighlight 将一个精彩片段切片导出为独立 mp4（同步执行，短片段秒级完成）。
// POST /api/media/:id/highlights/:highlightId/export
func (h *MediaAnalysisHandler) ExportHighlight(c *gin.Context) {
	mediaID := c.Param("id")
	highlightID := c.Param("highlightId")
	export, err := h.analysis.ExportHighlightClip(mediaID, highlightID)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, service.ErrMediaNotFound):
			status = http.StatusNotFound
		case errors.Is(err, service.ErrHighlightNotFound):
			status = http.StatusNotFound
		case strings.Contains(err.Error(), "无效") || strings.Contains(err.Error(), "超过"):
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": export, "message": "导出成功"})
}

// ListHighlightExports 列出某媒体已导出的片段文件。
// GET /api/media/:id/highlights/exports
func (h *MediaAnalysisHandler) ListHighlightExports(c *gin.Context) {
	exports, err := h.analysis.ListHighlightExports(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取导出列表失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": exports})
}

// DownloadHighlightExport 以下载附件形式返回导出的 mp4。
// GET /api/media/:id/highlights/:highlightId/export
func (h *MediaAnalysisHandler) DownloadHighlightExport(c *gin.Context) {
	mediaID := c.Param("id")
	highlightID := c.Param("highlightId")
	path, err := h.analysis.FindHighlightExportPath(mediaID, highlightID)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.FileAttachment(path, filepath.Base(path))
}

// DeleteHighlightExport 删除导出的片段文件（不影响源视频与精彩片段记录）。
// DELETE /api/media/:id/highlights/:highlightId/export
func (h *MediaAnalysisHandler) DeleteHighlightExport(c *gin.Context) {
	if err := h.analysis.DeleteHighlightExport(c.Param("id"), c.Param("highlightId")); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrHighlightNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": "删除导出文件失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "导出文件已删除"})
}

// ==================== 精彩片段批量处理（媒体库管理） ====================

// StartBatchHighlightsRequest 批量生成启动参数。
type StartBatchHighlightsRequest struct {
	// Mode balanced=均衡（一次一部，默认）；performance=性能（多部并行）。
	Mode string `json:"mode"`
}

// StartBatchHighlights 一键为全部本地视频批量生成精彩片段。
// POST /api/admin/media-analysis/batch
func (h *MediaAnalysisHandler) StartBatchHighlights(c *gin.Context) {
	var req StartBatchHighlightsRequest
	// 兼容空请求体：缺省按均衡模式处理
	_ = c.ShouldBindJSON(&req)
	status, err := h.analysis.StartBatchHighlights(req.Mode)
	if err != nil {
		if errors.Is(err, service.ErrMediaAnalysisInProgress) {
			c.JSON(http.StatusAccepted, gin.H{"data": status, "message": "批量任务已在进行中"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "启动批量任务失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": status, "message": "批量任务已启动"})
}

// BatchHighlightsStatus 查询批量任务进度。
// GET /api/admin/media-analysis/batch/status
func (h *MediaAnalysisHandler) BatchHighlightsStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": h.analysis.SnapshotBatchHighlights()})
}

// StopBatchHighlights 请求停止批量任务。
// DELETE /api/admin/media-analysis/batch
func (h *MediaAnalysisHandler) StopBatchHighlights(c *gin.Context) {
	status, err := h.analysis.StopBatchHighlights()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": status, "message": "已请求停止：剩余视频不再处理，当前视频会正常完成并保留结果"})
}

// ClearAllHighlights 清空全库所有精彩片段。
// DELETE /api/admin/media-analysis/highlights-all
func (h *MediaAnalysisHandler) ClearAllHighlights(c *gin.Context) {
	mediaCount, highlightCount, err := h.analysis.ClearAllHighlights()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "清空精彩片段失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已清空全部精彩片段", "media_count": mediaCount, "highlight_count": highlightCount})
}

// HighlightStorageStats 返回片段相关表的行数统计（诊断用）。
func (h *MediaAnalysisHandler) HighlightStorageStats(c *gin.Context) {
	stats, err := h.analysis.GetHighlightStorageStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取统计失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// PendingHighlightVideos 返回尚未生成精彩片段的本地视频清单（覆盖缺口明细）。
// GET /api/admin/media-analysis/highlights-pending
func (h *MediaAnalysisHandler) PendingHighlightVideos(c *gin.Context) {
	list, err := h.analysis.GetPendingHighlightVideos()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取未处理视频清单失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// HighlightAudit 返回全库片段完整性检查报告（源视频缺失/产物文件缺失）。
// GET /api/admin/media-analysis/highlights-audit
func (h *MediaAnalysisHandler) HighlightAudit(c *gin.Context) {
	report, err := h.analysis.GetHighlightAudit()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取完整性报告失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": report})
}

// ImportManualHighlights 扫描全库本地视频同目录的 .highlights/ 隐藏目录，
// 将手工 ffmpeg 剪辑的片段导入为 Source=manual 的精彩片段（幂等，不覆盖自动结果）。
// POST /api/admin/media-analysis/highlights-import-manual
func (h *MediaAnalysisHandler) ImportManualHighlights(c *gin.Context) {
	report, err := h.analysis.ImportManualHighlights()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "导入手动精彩片段失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":    report,
		"message": fmt.Sprintf("已导入 %d 个手动精彩片段（涉及 %d 个视频）", report.ClipsImported, report.MediaScanned),
	})
}

// ==================== 本地精彩片段生成（媒体库管理） ====================

// GenerateLocalHighlights 启动本地精彩片段生成任务（后台执行：单视频目录 →
// 时间线侧车 json + 缩略图 → 自动导入数据库）。
// POST /api/admin/media-analysis/highlights-local/generate
func (h *MediaAnalysisHandler) GenerateLocalHighlights(c *gin.Context) {
	var req struct {
		MediaIDs []string `json:"media_ids"`
	}
	_ = c.ShouldBindJSON(&req)
	status, err := h.analysis.StartLocalHighlightGeneration(req.MediaIDs)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrLocalHighlightGenInProgress):
			c.JSON(http.StatusConflict, gin.H{"data": status, "error": err.Error()})
			return
		case strings.Contains(err.Error(), "批量任务"):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "启动本地精彩片段生成失败: " + err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": status, "message": "本地精彩片段生成任务已启动"})
}

// LocalHighlightGenerationStatus 查询本地精彩片段生成任务进度。
// GET /api/admin/media-analysis/highlights-local/status
func (h *MediaAnalysisHandler) LocalHighlightGenerationStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": h.analysis.SnapshotLocalHighlightGeneration()})
}

// StopLocalHighlightGeneration 请求停止本地精彩片段生成任务。
// DELETE /api/admin/media-analysis/highlights-local
func (h *MediaAnalysisHandler) StopLocalHighlightGeneration(c *gin.Context) {
	status, err := h.analysis.StopLocalHighlightGeneration()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": status, "message": "已请求停止：剩余目录不再处理，已生成的保留结果"})
}

// ScanLocalHighlightDirs 扫描目录归属：单视频目录列入可生成集合，
// 多视频目录报错并附原因（提示将视频放入独立目录）。
// GET /api/admin/media-analysis/highlights-local/scan?full=true
// full=false（默认）增量模式：DB 状态聚合计数、不探测已生成影片；full=true 全量逐片校验。
func (h *MediaAnalysisHandler) ScanLocalHighlightDirs(c *gin.Context) {
	full := c.Query("full") == "true"
	report, err := h.analysis.ScanLocalHighlightDirs(full)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "扫描本地生成目录失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": report})
}

// VerifyLocalHighlights 校验已生成影片的产物是否仍在磁盘，缺失者重置回待生成。
// POST /api/admin/media-analysis/highlights-local/verify
func (h *MediaAnalysisHandler) VerifyLocalHighlights(c *gin.Context) {
	report, err := h.analysis.VerifyLocalHighlights()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "校验本地精彩片段失败: " + err.Error()})
		return
	}
	msg := "校验完成"
	if report.Repaired > 0 {
		msg = fmt.Sprintf("校验中发现 %d 个已删除产物被重置为待生成", report.Repaired)
	}
	c.JSON(http.StatusOK, gin.H{"data": report, "message": msg})
}

// CleanupLocalHighlights 删除本地生成的 .highlights 文件与对应 manual 精彩片段记录。
// POST /api/admin/media-analysis/highlights-local/cleanup
func (h *MediaAnalysisHandler) CleanupLocalHighlights(c *gin.Context) {
	report, err := h.analysis.CleanupLocalHighlights()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "清理本地精彩片段失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":    report,
		"message": fmt.Sprintf("已删除 %d 个视频的 %d 个本地片段文件", report.MediaAffected, report.FilesDeleted),
	})
}

// CleanBrokenHighlights 删除完整性检查发现的问题片段。
// POST /api/admin/media-analysis/highlights-audit/clean  body: {"include_asset_issues": true}
func (h *MediaAnalysisHandler) CleanBrokenHighlights(c *gin.Context) {
	var req struct {
		IncludeAssetIssues bool `json:"include_asset_issues"`
	}
	_ = c.ShouldBindJSON(&req) // 空 body 时按 false 处理
	cleaned, err := h.analysis.CleanBrokenHighlights(req.IncludeAssetIssues)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "清理失效精彩片段失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"cleaned": cleaned,
		"message": fmt.Sprintf("已清理 %d 个视频的失效精彩片段", cleaned),
	})
}
