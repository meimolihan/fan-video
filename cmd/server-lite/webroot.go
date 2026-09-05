package main

import (
	"net/http"
	"path"

	"github.com/gin-gonic/gin"
)

// serveFrontendFile 从 root（磁盘 webDir 或内嵌副本，见 internal/embedded.Resolve）
// 提供一个静态文件。经 http.ServeContent 处理，天然支持 Range/HEAD、自动设置
// Content-Type，行为与 gin 的 c.File 一致。
func serveFrontendFile(c *gin.Context, root http.FileSystem, name, cacheControl string) {
	clean := path.Clean("/" + name)
	f, err := root.Open(clean)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		c.Status(http.StatusNotFound)
		return
	}
	if cacheControl != "" {
		c.Header("Cache-Control", cacheControl)
	}
	http.ServeContent(c.Writer, c.Request, path.Base(clean), info.ModTime(), f)
}