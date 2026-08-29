// Package pwa 内嵌 PWA 资源（sw.js / manifest.json），使其编译进二进制，
// 从而彻底摆脱对运行时构建目录、文件权限与反向代理对顶层路径拦截的依赖。
package pwa

import _ "embed"

//go:embed sw.js
var swJS []byte

//go:embed manifest.json
var manifestJSON []byte

// SWJS 返回内嵌的 Service Worker 内容。
func SWJS() []byte {
	return swJS
}

// ManifestJSON 返回内嵌的 Web App Manifest 内容。
func ManifestJSON() []byte {
	return manifestJSON
}
