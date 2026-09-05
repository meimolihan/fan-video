package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fan-video/fan-video/internal/service"
	"github.com/gin-gonic/gin"
)

// ==================== 全量备份 / 还原 ====================

// ListBackups 列出全部备份文件
func (h *AdminHandler) ListBackups(c *gin.Context) {
	backups, err := h.backupService.List()
	if err != nil {
		h.logger.Errorf("列出备份失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取备份列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"backups":    backups,
			"backup_dir": h.backupService.BackupDir(),
		},
	})
}

// CreateBackup 创建一份全量备份
func (h *AdminHandler) CreateBackup(c *gin.Context) {
	entry, err := h.backupService.Create()
	if err != nil {
		h.logger.Errorf("创建备份失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建备份失败：" + err.Error()})
		return
	}
	h.auditFromContext(c, "backup.create", "backup", entry.Name, "size="+fmt.Sprintf("%d", entry.Size))
	c.JSON(http.StatusOK, gin.H{"message": "备份创建成功", "data": entry})
}

// DownloadBackup 下载指定备份文件
func (h *AdminHandler) DownloadBackup(c *gin.Context) {
	name := c.Param("name")
	path, err := h.backupService.DownloadPath(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.FileAttachment(path, filepath.Base(name))
}

// DeleteBackup 删除指定备份文件
func (h *AdminHandler) DeleteBackup(c *gin.Context) {
	name := c.Param("name")
	if err := h.backupService.Delete(name); err != nil {
		h.logger.Errorf("删除备份失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.auditFromContext(c, "backup.delete", "backup", name, "")
	c.JSON(http.StatusOK, gin.H{"message": "备份已删除", "data": gin.H{"name": name}})
}

// maybeRestartAfterRestore 还原暂存成功后，若配置了自动重启则安排进程退出，
// 由 systemd/docker 等进程管理器以非零退出码重启，使暂存的数据库立即生效。
// 延迟退出以先让 HTTP 响应送达客户端。
func (h *AdminHandler) maybeRestartAfterRestore() {
	if h.cfg == nil || !h.cfg.App.RestartAfterRestore {
		return
	}
	h.logger.Warn("还原已暂存，1.5 秒后自动重启服务以应用数据库（配置 app.restart_after_restore=true）")
	go func() {
		time.Sleep(1500 * time.Millisecond)
		os.Exit(1)
	}()
}

// RestoreBackup 上传备份并还原。
// multipart 字段： file（zip 文件）+ confirm（必须为 CONFIRM_RESTORE）
func (h *AdminHandler) RestoreBackup(c *gin.Context) {
	confirm := c.PostForm("confirm")
	if confirm != service.RestoreConfirmToken {
		c.JSON(http.StatusBadRequest, gin.H{"error": "确认标识不正确，请传入 " + service.RestoreConfirmToken + " 以确认还原操作"})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少备份文件（字段名 file）"})
		return
	}
	if !strings.HasSuffix(strings.ToLower(fileHeader.Filename), ".zip") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 .zip 格式的备份文件"})
		return
	}
	src, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取上传文件失败"})
		return
	}
	defer src.Close()

	h.logger.Warn(fmt.Sprintf("⚠️  管理员发起全量还原，来源文件: %s", fileHeader.Filename))
	result, err := h.backupService.Restore(src, fileHeader.Filename)
	if err != nil {
		h.logger.Errorf("还原失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "还原失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"message": "还原成功，数据库将在下次重启服务后生效",
		"data":    result,
	})
	h.maybeRestartAfterRestore()
}

// RestoreBackupLocal 直接还原服务器上已存在的备份文件
func (h *AdminHandler) RestoreBackupLocal(c *gin.Context) {
	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效，需要提供确认标识"})
		return
	}
	if req.Confirm != service.RestoreConfirmToken {
		c.JSON(http.StatusBadRequest, gin.H{"error": "确认标识不正确，请传入 " + service.RestoreConfirmToken + " 以确认还原操作"})
		return
	}

	name := c.Param("name")
	path, err := h.backupService.DownloadPath(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	src, err := os.Open(path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "打开备份文件失败"})
		return
	}
	defer src.Close()

	h.logger.Warn(fmt.Sprintf("⚠️  管理员发起全量还原（服务器备份），来源文件: %s", name))
	result, err := h.backupService.Restore(src, name)
	if err != nil {
		h.logger.Errorf("还原失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "还原失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"message": "还原成功，数据库将在下次重启服务后生效",
		"data":    result,
	})
	h.maybeRestartAfterRestore()
}