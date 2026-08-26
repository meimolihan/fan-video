package handler

import (
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/service"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type ThumbnailHandler struct {
	db     *gorm.DB
	logger *zap.SugaredLogger
}

func NewThumbnailHandler(db *gorm.DB, logger *zap.SugaredLogger) *ThumbnailHandler {
	return &ThumbnailHandler{db: db, logger: logger}
}

// ThumbnailBatchStatus 缩略图批量生成状态
type ThumbnailBatchStatus struct {
	Running        bool   `json:"running"`
	StopRequested  bool   `json:"stop_requested"`
	Total          int    `json:"total"`
	Generated      int    `json:"generated"`
	Skipped        int    `json:"skipped"`
	Failed         int    `json:"failed"`
	Remaining      int    `json:"remaining"`
	CurrentTitle   string `json:"current_title"`
	CurrentPercent int    `json:"current_percent"`
	StartedAt      string `json:"started_at"`
	FinishedAt     string `json:"finished_at,omitempty"`
}

var (
	thumbBatchMu   sync.Mutex
	thumbBatchState ThumbnailBatchStatus
)

func getThumbBatchState() ThumbnailBatchStatus {
	thumbBatchMu.Lock()
	defer thumbBatchMu.Unlock()
	return thumbBatchState
}

func setThumbBatchState(s ThumbnailBatchStatus) {
	thumbBatchMu.Lock()
	defer thumbBatchMu.Unlock()
	thumbBatchState = s
}

// Generate 为指定媒体生成缩略图
func (h *ThumbnailHandler) Generate(c *gin.Context) {
	id := c.Param("mediaId")
	var media model.Media
	if err := h.db.First(&media, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "媒体不存在"})
		return
	}
	if media.PosterPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "该媒体无海报图"})
		return
	}
	thumbPath, err := service.EnsureThumbnail(media.PosterPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   "缩略图生成成功",
		"thumbPath": thumbPath,
		"width":     240,
	})
}

// BatchGenerate 启动批量缩略图生成（异步后台执行）
func (h *ThumbnailHandler) BatchGenerate(c *gin.Context) {
	state := getThumbBatchState()
	if state.Running {
		c.JSON(http.StatusConflict, gin.H{"error": "批量生成任务正在进行中"})
		return
	}

	// 初始化状态
	now := time.Now().Format(time.RFC3339)
	setThumbBatchState(ThumbnailBatchStatus{
		Running:   true,
		StartedAt: now,
	})

	// 后台执行
	go h.runBatchGenerate()

	c.JSON(http.StatusOK, gin.H{
		"data":    getThumbBatchState(),
		"message": "批量缩略图生成已启动",
	})
}

func (h *ThumbnailHandler) runBatchGenerate() {
	var medias []model.Media
	if err := h.db.Model(&model.Media{}).Find(&medias).Error; err != nil {
		h.logger.Errorf("批量缩略图：获取媒体列表失败: %v", err)
		s := getThumbBatchState()
		s.Running = false
		s.FinishedAt = time.Now().Format(time.RFC3339)
		setThumbBatchState(s)
		return
	}

	total := len(medias)
	state := ThumbnailBatchStatus{
		Running:   true,
		Total:     total,
		Remaining: total,
		StartedAt: time.Now().Format(time.RFC3339),
	}
	setThumbBatchState(state)

	for _, media := range medias {
		// 检查停止请求
		cur := getThumbBatchState()
		if cur.StopRequested {
			cur.Running = false
			cur.StopRequested = false
			cur.FinishedAt = time.Now().Format(time.RFC3339)
			setThumbBatchState(cur)
			return
		}

		title := media.Title
		if title == "" {
			title = media.FilePath
		}

		// 更新当前处理
		cur.CurrentTitle = title
		cur.CurrentPercent = 0
		setThumbBatchState(cur)

		if media.PosterPath == "" {
			cur.Failed++
			cur.Remaining--
			cur.CurrentPercent = 100
			setThumbBatchState(cur)
			continue
		}

		// 已有缩略图则跳过
		thumbPath := service.GetThumbPath(media.PosterPath)
		if _, err := os.Stat(thumbPath); err == nil {
			cur.Skipped++
			cur.Remaining--
			cur.CurrentPercent = 100
			setThumbBatchState(cur)
			continue
		}

		// 生成缩略图
		if _, err := service.EnsureThumbnail(media.PosterPath); err != nil {
			h.logger.Warnf("缩略图生成失败 [%s]: %v", media.ID, err)
			cur.Failed++
		} else {
			cur.Generated++
		}
		cur.Remaining--
		cur.CurrentPercent = 100
		setThumbBatchState(cur)
	}

	final := getThumbBatchState()
	final.Running = false
	final.StopRequested = false
	final.CurrentTitle = ""
	final.CurrentPercent = 0
	final.FinishedAt = time.Now().Format(time.RFC3339)
	setThumbBatchState(final)

	h.logger.Infof("批量缩略图生成完成：新增 %d，跳过 %d，失败 %d", final.Generated, final.Skipped, final.Failed)
}

// BatchStatus 查询批量缩略图生成进度
func (h *ThumbnailHandler) BatchStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"data": getThumbBatchState(),
	})
}

// BatchStop 停止批量缩略图生成
func (h *ThumbnailHandler) BatchStop(c *gin.Context) {
	state := getThumbBatchState()
	if !state.Running {
		c.JSON(http.StatusNotFound, gin.H{"error": "没有正在进行的批量任务"})
		return
	}
	state.StopRequested = true
	setThumbBatchState(state)
	c.JSON(http.StatusOK, gin.H{
		"data":    getThumbBatchState(),
		"message": "已请求停止",
	})
}

// Delete 删除指定媒体的缩略图
func (h *ThumbnailHandler) Delete(c *gin.Context) {
	id := c.Param("mediaId")
	var media model.Media
	if err := h.db.First(&media, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "媒体不存在"})
		return
	}
	if media.PosterPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "该媒体无海报图"})
		return
	}
	if err := service.DeleteThumbnail(media.PosterPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "缩略图已删除"})
}

// BatchDelete 删除全部缩略图
func (h *ThumbnailHandler) BatchDelete(c *gin.Context) {
	state := getThumbBatchState()
	if state.Running {
		c.JSON(http.StatusConflict, gin.H{"error": "批量生成进行中，无法删除"})
		return
	}

	var medias []model.Media
	if err := h.db.Model(&model.Media{}).Find(&medias).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取媒体列表失败"})
		return
	}
	var deleted int
	for _, media := range medias {
		if media.PosterPath == "" {
			continue
		}
		thumbPath := service.GetThumbPath(media.PosterPath)
		if _, err := os.Stat(thumbPath); err == nil {
			os.Remove(thumbPath)
			deleted++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"deleted": deleted,
		},
		"message": "已删除全部缩略图",
	})
}

// Stats 缩略图统计
func (h *ThumbnailHandler) Stats(c *gin.Context) {
	stats, err := service.GetThumbnailStats(h.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"total":     stats.Total,
			"generated": stats.Generated,
			"missing":   stats.Missing,
		},
	})
}

// Audit 缩略图完整性检查
func (h *ThumbnailHandler) Audit(c *gin.Context) {
	report, err := service.GetThumbnailAudit(h.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": report,
	})
}

// Clean 清理缩略图完整性问题（孤儿 + 源图已删除）
func (h *ThumbnailHandler) Clean(c *gin.Context) {
	state := getThumbBatchState()
	if state.Running {
		c.JSON(http.StatusConflict, gin.H{"error": "批量生成进行中，无法清理"})
		return
	}

	var req struct {
		DeleteOrphan bool `json:"delete_orphan"`
		DeleteStale  bool `json:"delete_stale"`
	}
	// 默认两项都清理
	req.DeleteOrphan = true
	req.DeleteStale = true
	_ = c.ShouldBindJSON(&req)

	deleted, err := service.CleanThumbnailAuditIssues(h.db, req.DeleteOrphan, req.DeleteStale)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted": deleted,
		"message": fmt.Sprintf("已清理 %d 个失效缩略图", deleted),
	})
}
