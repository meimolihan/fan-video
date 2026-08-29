package main

import (
	"net/http"
	"path"
	"strings"

	"github.com/fan-video/fan-video/internal/pwa"
	"github.com/gin-gonic/gin"
)

// registerPWAAndAssets serves the /assets/* subtree with a single custom handler
// instead of gin's r.Static/StaticFS.
//
// 为什么不用 r.Static：
//   - r.Static 注册 /assets/*filepath 通配路由。若再为本项目新增 /assets/sw.js 等
//     精确路由，会和通配路由冲突导致 Gin 启动 panic。
//   - PWA 资源（sw.js / manifest.json）必须从二进制内嵌提供（internal/pwa），才能彻底
//     摆脱对运行时构建目录、文件权限、以及反向代理对顶层路径拦截的依赖：无论在何种
//     部署形态下都稳定可用。
//
// 所有非 PWA 的 /assets/* 仍回落到 webDir/assets 磁盘目录（内容哈希、可 immutable 缓存）。
func registerPWAAndAssets(r *gin.Engine, webDir string) {
	r.GET("/assets/*filepath", func(c *gin.Context) {
		reqPath := c.Param("filepath")
		switch reqPath {
		case "/sw.js":
			c.Header("Service-Worker-Allowed", "/")
			c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
			c.Data(http.StatusOK, "text/javascript", pwa.SWJS())
			return
		case "/manifest.json":
			c.Header("Cache-Control", "no-cache")
			c.Data(http.StatusOK, "application/manifest+json", pwa.ManifestJSON())
			return
		}

		// 其余 /assets/*（构建产物、图标）：磁盘文件 + immutable 缓存。
		filePath := webDir + "/assets" + path.Clean("/"+reqPath)
		if !strings.HasPrefix(filePath, webDir+"/assets/") && filePath != webDir+"/assets" {
			c.Status(http.StatusForbidden)
			return
		}
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.File(filePath)
	})
}
