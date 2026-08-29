package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gunzip(t *testing.T, body []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(body))
	require.NoError(t, err)
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	require.NoError(t, err)
	return string(raw)
}

func setupCompressionRouter(handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Gzip())
	r.GET("/test", handler)
	r.GET("/asset.css", func(c *gin.Context) {
		c.File("/dev/null") // placeholder; tests override via custom handler if needed
	})
	return r
}

func doGET(t *testing.T, r *gin.Engine, path string, extra func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	if extra != nil {
		extra(req)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGzipCompressesJSON(t *testing.T) {
	body := `{"data":"` + string(bytes.Repeat([]byte("x"), 4096)) + `"}`
	r := setupCompressionRouter(func(c *gin.Context) {
		c.Header("Content-Type", "application/json")
		c.Writer.Write([]byte(body))
	})

	w := doGET(t, r, "/test", nil)
	assert.Equal(t, "application/json", cHeaderCT(w))
	// 请求 Accept-Encoding:gzip 且响应体大于 min 阈值 → 应压缩
	assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", w.Header().Get("Vary"))
	assert.Equal(t, body, gunzip(t, w.Body.Bytes()))
}

func TestGzipSkipsWithoutAcceptEncoding(t *testing.T) {
	r := setupCompressionRouter(func(c *gin.Context) { c.JSON(http.StatusOK, map[string]string{"ok": "1"}) })
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Empty(t, w.Header().Get("Content-Encoding"))
	assert.Contains(t, w.Body.String(), `"ok"`)
}

func TestGzipSkipsTinyPayloadWithLength(t *testing.T) {
	r := setupCompressionRouter(func(c *gin.Context) {
		c.Header("Content-Type", "application/json")
		c.Header("Content-Length", "5")
		c.Writer.WriteHeader(200)
		c.Writer.Write([]byte("12345"))
	})
	w := doGET(t, r, "/test", nil)
	assert.Empty(t, w.Header().Get("Content-Encoding"))
	assert.Equal(t, "12345", w.Body.String())
}

func TestGzipSkipsNon2xx(t *testing.T) {
	r := setupCompressionRouter(func(c *gin.Context) {
		c.Status(http.StatusNotFound)
		c.Header("Content-Type", "application/json")
		c.Writer.Write([]byte(`{"error":"nope"}`))
	})
	w := doGET(t, r, "/test", nil)
	assert.Empty(t, w.Header().Get("Content-Encoding"))
	assert.Contains(t, w.Body.String(), `"error"`)
}

func TestGzipSkipsRangeRequest(t *testing.T) {
	r := setupCompressionRouter(func(c *gin.Context) { c.JSON(http.StatusOK, map[string]string{"bytes": "x"}) })
	w := doGET(t, r, "/test", func(req *http.Request) { req.Header.Set("Range", "bytes=0-1023") })
	assert.Empty(t, w.Header().Get("Content-Encoding"))
	assert.Contains(t, w.Body.String(), `"bytes"`)
}

func TestGzipSkipsWebSocketUpgrade(t *testing.T) {
	r := setupCompressionRouter(func(c *gin.Context) { c.JSON(http.StatusOK, map[string]string{"ws": "1"}) })
	w := doGET(t, r, "/test", func(req *http.Request) { req.Header.Set("Upgrade", "websocket") })
	assert.Empty(t, w.Header().Get("Content-Encoding"))
}

func TestGzipRejectsQZero(t *testing.T) {
	r := setupCompressionRouter(func(c *gin.Context) { c.JSON(http.StatusOK, map[string]string{"q": "0"}) })
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip;q=0")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Empty(t, w.Header().Get("Content-Encoding"))
}

func cHeaderCT(w *httptest.ResponseRecorder) string {
	return w.Result().Header.Get("Content-Type")
}
