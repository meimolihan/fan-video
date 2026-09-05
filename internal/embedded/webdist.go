// Package embedded 将前端构建产物（web/dist）内嵌进二进制。
//
// server-lite 默认从磁盘目录（app.web_dir / NOWEN_APP_WEB_DIR）读取前端资源，
// 便于部署时直接替换 content-hashed 构建产物并复用 immutable 缓存。但对于
// 未经前端同步的部署（例如 install.sh 无本地产物、仅下载 Release 二进制），
// 磁盘目录可能不存在，此时回退到此处内嵌的构建产物，保证任何安装形态都能
// 直接打开网页，不再出现 404。
package embedded

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

// distFS 由 Makefile 的 sync-webdist 目标从 web/dist 同步而来（编译期固定）。
// 注意：`all:` 前缀会连同隐藏/点前缀文件一并内嵌，避免 Vite 产物遗漏。
//
//go:embed all:dist
var distFS embed.FS

// Resolve 返回前端静态资源服务的 http.FileSystem。
// 优先使用磁盘版 webDir（与现有行为一致），仅当磁盘上不存在 index.html 时
// 回退到二进制内嵌副本。
func Resolve(webDir string) http.FileSystem {
	if webDir != "" {
		if _, err := os.Stat(filepath.Join(webDir, "index.html")); err == nil {
			return http.Dir(webDir)
		}
	}
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // dist 已由同步目标保证存在；构建失败应直接暴露
	}
	return http.FS(sub)
}

// IndexHTML 返回 index.html 内容，优先磁盘 webDir，缺失时回退内嵌副本。
func IndexHTML(webDir string) ([]byte, error) {
	if webDir != "" {
		if b, err := os.ReadFile(filepath.Join(webDir, "index.html")); err == nil {
			return b, nil
		}
	}
	return distFS.ReadFile("dist/index.html")
}
