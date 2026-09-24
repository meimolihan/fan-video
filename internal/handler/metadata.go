package handler

import (
	"errors"
	"net/http"

	"github.com/fan-video/fan-video/internal/service"
	probe "github.com/fan-video/fan-video/internal/transcode/probe"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// MetadataHandler 元数据处理器
type MetadataHandler struct {
	metadataService *service.MetadataService
	streamService   *service.StreamService
	logger          *zap.SugaredLogger
}

// ScrapeMedia 手动刮削单个媒体的元数据
func (h *MetadataHandler) ScrapeMedia(c *gin.Context) {
	id := c.Param("id")

	if err := h.metadataService.ScrapeMedia(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "元数据刮削失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "元数据刮削成功"})
}

// RefreshTechnicalMetadata 强制刷新单个媒体的技术元数据（重新探测源文件并回写
// 数据库编码/分辨率/音频/时长/文件大小）。适用于手动转码/替换文件后，DB 仍记录
// 旧编码（如 hevc）导致播放计划要求转码播放的场景。
func (h *MetadataHandler) RefreshTechnicalMetadata(c *gin.Context) {
	id := c.Param("id")

	if _, err := h.streamService.RefreshMediaTechnicalMetadata(id); err != nil {
		if errors.Is(err, probe.ErrUnsupportedSource) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该媒体源不支持直接探测（STRM / 远程流），无需刷新技术元数据"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "刷新技术元数据失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "技术元数据已刷新"})
}
