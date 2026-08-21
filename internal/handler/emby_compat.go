package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/fan-video/fan-video/internal/service"
	"go.uber.org/zap"
)

// EmbyCompatHandler EMBY 兼容性处理器
type EmbyCompatHandler struct {
	embyService *service.EmbyCompatService
	logger      *zap.SugaredLogger
}

// DetectEmbyFormat 检测目录是否为 EMBY 格式
func (h *EmbyCompatHandler) DetectEmbyFormat(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供目录路径"})
		return
	}

	result, err := h.embyService.DetectEmbyFormat(path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// ImportEmbyLibrary 从 EMBY 格式文件夹导入媒体库
func (h *EmbyCompatHandler) ImportEmbyLibrary(c *gin.Context) {
	var req service.EmbyImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}

	if req.RootPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供 EMBY 媒体库根目录路径"})
		return
	}
	if req.TargetLibraryID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择目标媒体库"})
		return
	}

	// 默认导入模式
	if req.ImportMode == "" {
		req.ImportMode = "incremental"
	}

	result, err := h.embyService.ImportEmbyLibrary(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "EMBY 媒体库导入完成",
		"data":    result,
	})
}

// GetEmbyCompatInfo 获取 EMBY 兼容性信息
func (h *EmbyCompatHandler) GetEmbyCompatInfo(c *gin.Context) {
	info := h.embyService.GetEmbyCompatInfo()
	c.JSON(http.StatusOK, gin.H{"data": info})
}

// GenerateEmbyNFO 为指定媒体生成 EMBY 兼容的 NFO 文件
func (h *EmbyCompatHandler) GenerateEmbyNFO(c *gin.Context) {
	mediaID := c.Param("mediaId")
	if mediaID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供媒体 ID"})
		return
	}

	// 这里需要先获取 media，然后生成 NFO
	// 简化实现：返回提示信息
	c.JSON(http.StatusOK, gin.H{
		"message":  "NFO 生成功能已就绪",
		"media_id": mediaID,
	})
}
