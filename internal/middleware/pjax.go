package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/fan-video/fan-video/internal/embedded"
	"github.com/gin-gonic/gin"
)

// PJAX 中间件：识别前端发出的 X-PJAX 请求头，实现最小侵入的页面局部刷新。
//
// 本项目前端是 React SPA，页面内容已由 react-router 在客户端做片段级切换，
// 后端不渲染业务模板。因此这里对 PJAX 请求返回“页面主体片段”——即
// index.html 中 <body> 内（含 <div id="root"> 挂载点）的部分；普通请求返回
// 完整 HTML 文档。二者都复用同一份 index.html，不引入额外模板，不重写业务。
// 说明：构建产物的模块脚本位于 <head>，PJAX 片段通常不含脚本；前端收到片段后
// 由已在 <head> 中运行的入口重新挂载 #root，等价于一次干净的同文档刷新。
func PJAX(webDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 非 PJAX 请求（或请求头缺失）走完整页面/正常逻辑。
		if c.GetHeader("X-PJAX") == "" && c.GetHeader("X-Requested-With") != "XMLHttpRequest" {
			c.Next()
			return
		}

		// 标记该响应确实经 PJAX 路径处理过，便于前端/调试识别。
		c.Writer.Header().Set("X-PJAX", "true")

		// API 返回 JSON，不存在“页面主体片段”语义，原样放行。
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Next()
			return
		}

		// 非 GET（如 HEAD 探活）或非 SPA 页面路由，原样放行。
		if c.Request.Method != http.MethodGet || c.Request.URL.Path == "/favicon.ico" {
			c.Next()
			return
		}

		// 读取同一份 index.html（优先磁盘 webDir，缺失时回退二进制内嵌副本），
		// 抽取 <body>...</body> 之间的主体片段返回。
		html, err := embedded.IndexHTML(webDir)
		if err != nil {
			// 读取失败时降级为完整页面，保证任何情况下页面都可用。
			c.Next()
			return
		}

		fragment := extractBodyFragment(string(html))
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.String(200, fragment)
		c.Abort()
	}
}

// extractBodyFragment 从完整 HTML 文档中抽取 <body> ... </body> 之间的主体片段。
// 返回的片段保留 body 内的挂载点(root)与模块脚本，React 挂载后即可完成路由渲染，
// 是 SPA 场景下可作为“页面主体片段”交付的最小单元。
func extractBodyFragment(html string) string {
	start := strings.Index(html, "<body")
	if start < 0 {
		return html
	}
	// 找到 <body ...> 的闭合 '>'，片段从该处之后开始。
	gt := strings.IndexByte(html[start:], '>')
	if gt < 0 {
		return html
	}
	contentStart := start + gt + 1

	end := strings.LastIndex(html, "</body>")
	if end < 0 || end < contentStart {
		return html
	}

	return strings.TrimSpace(html[contentStart:end])
}

// readBodyFragment 读取 index.html，抽取 <body> ... </body> 之间的主体片段。
func readBodyFragment(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	html := string(data)

	start := strings.Index(html, "<body")
	if start < 0 {
		return html, nil
	}
	// 找到 <body ...> 的闭合 '>'，片段从该处之后开始。
	gt := strings.IndexByte(html[start:], '>')
	if gt < 0 {
		return html, nil
	}
	contentStart := start + gt + 1

	end := strings.LastIndex(html, "</body>")
	if end < 0 || end < contentStart {
		return html, nil
	}

	return strings.TrimSpace(html[contentStart:end]), nil
}
