package middleware

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// 只在这类 Content-Type 上启用传输层 gzip 压缩。图片/音频/视频/字体等
// 本身已是压缩格式，再压只会空耗 CPU；text/event-stream(SSE) 需流式即时
// 交付，压缩会引入缓冲并破坏部分客户端，故一并排除。
var compressibleContentTypes = map[string]bool{
	"text/css":                  true,
	"text/html":                 true,
	"text/javascript":           true,
	"text/plain":                true,
	"text/xml":                  true,
	"text/vtt":                  true,
	"application/javascript":    true,
	"application/x-javascript":  true,
	"application/json":          true,
	"application/ld+json":       true,
	"application/xml":           true,
	"application/manifest+json": true,
	"image/svg+xml":             true,
}

// minCompressSize 显式声明 Content-Length 的响应低于该字节数不值得压缩
// （gzip 头 + 字典开销抵消省下的字节）。未声明长度的流式响应不在此限制内。
const minCompressSize = 1024

// acceptsGzip 判断请求是否接受 gzip（正确处理 q=0 的显式拒绝）。
func acceptsGzip(c *gin.Context) bool {
	for _, part := range strings.Split(c.GetHeader("Accept-Encoding"), ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "" || !strings.HasPrefix(part, "gzip") {
			continue
		}
		// gzip;q=0 表示明确不接受该编码
		if eq := strings.Index(part, "q="); eq >= 0 {
			q, err := strconv.ParseFloat(part[eq+2:], 64)
			if err == nil && q <= 0 {
				continue
			}
		}
		return true
	}
	return false
}

func compressibleContentType(ct string) bool {
	if ct == "" {
		return false
	}
	if semi := strings.IndexByte(ct, ';'); semi >= 0 {
		ct = ct[:semi]
	}
	return compressibleContentTypes[strings.ToLower(strings.TrimSpace(ct))]
}

// gzipResponseWriter 在首次写入时依据 Content-Type / 状态码 / Content-Length
// 延迟决定是否压缩，避免为图片、视频流与极小响应套一层 gzip。
type gzipResponseWriter struct {
	gin.ResponseWriter
	decided  bool
	compress bool
	gzw      *gzip.Writer
}

// resolve 做压缩决策并初始化 gzip 流；仅在第一次 Write 时调用一次。
// gin 默认 status 为 0（等价 200，直到 WriteHeaderNow 才置位），Content-Type
// 通常由 render 在写首块前才写入 Header，故把首块数据传进来做嗅探兜底。
func (w *gzipResponseWriter) resolve(data []byte) {
	if w.decided {
		return
	}
	w.decided = true
	h := w.Header()
	if status := w.Status(); status != 0 && (status < 200 || status >= 300) { // 仅压缩成功响应
		return
	}
	if h.Get("Content-Encoding") != "" { // 上游可能已预压缩
		return
	}
	ct := h.Get("Content-Type")
	if ct == "" && len(data) > 0 {
		ct = http.DetectContentType(data)
	}
	if !compressibleContentType(ct) {
		return
	}
	if cl := h.Get("Content-Length"); cl != "" {
		if n, err := strconv.Atoi(cl); err == nil && n < minCompressSize {
			return
		}
	}
	w.compress = true
	// 移除与原始字节强绑定的头：长度会被 gzip 重写；ETag 与未压缩字节绑定
	// （immutable 资源不走条件请求，删除无副作用，避免用到错误的 validator）
	h.Del("Content-Length")
	h.Del("ETag")
	h.Add("Vary", "Accept-Encoding")
	h.Set("Content-Encoding", "gzip")
	w.gzw = gzip.NewWriter(w.ResponseWriter)
}

func (w *gzipResponseWriter) Write(data []byte) (int, error) {
	w.resolve(data)
	if !w.compress {
		return w.ResponseWriter.Write(data)
	}
	return w.gzw.Write(data)
}

func (w *gzipResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *gzipResponseWriter) Flush() {
	if w.gzw != nil {
		w.gzw.Flush()
	}
	w.ResponseWriter.Flush()
}

func (w *gzipResponseWriter) Close() {
	if w.gzw != nil {
		w.gzw.Close()
	}
}

// Gzip 内容压缩中间件。
//
// 必须注册在 gin.Recovery() 之前（外层）：这样当内层 handler panic 时，
// Recovery 写入的错误响应仍经由本包装器压缩，且 gzip 尾块在本中间件的
// defer 中最后关闭，保证流向客户端的是完整合法的 gzip 流。
func Gzip() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet || !acceptsGzip(c) {
			c.Next()
			return
		}
		// Range 请求（视频/音频流式拉流）绝不压缩：Content-Encoding:gzip
		// 会破坏 Range 定位、续传与 seek。
		if c.GetHeader("Range") != "" {
			c.Next()
			return
		}
		// WebSocket 升级请求走 Hijack，禁止包装 ResponseWriter。
		if strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
			c.Next()
			return
		}
		w := &gzipResponseWriter{ResponseWriter: c.Writer}
		c.Writer = w
		defer w.Close()
		c.Next()
	}
}
