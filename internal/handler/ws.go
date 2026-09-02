package handler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/service"
	"go.uber.org/zap"
)

// WSHandler WebSocket处理器
type WSHandler struct {
	hub     *service.WSHub
	logger  *zap.SugaredLogger
	upgrader websocket.Upgrader
}

// wsAllowedOrigins 收集允许的 WS 来源（不含同源，同源在 CheckOrigin 内动态判定）。
func wsAllowedOrigins(cfg *config.Config) []string {
	return append([]string{
		"tauri://localhost",
		"http://tauri.localhost",
		"https://tauri.localhost",
	}, cfg.App.CORSOrigins...)
}

// NewWSHandler 创建 WebSocket 处理器。
// allowedOrigins 为额外的允许来源（除同源外）；原生客户端不发送 Origin 头，直接放行。
func NewWSHandler(hub *service.WSHub, logger *zap.SugaredLogger, allowedOrigins ...string) *WSHandler {
	allowed := map[string]struct{}{}
	for _, o := range allowedOrigins {
		if o = strings.TrimSpace(strings.ToLower(o)); o != "" {
			allowed[o] = struct{}{}
		}
	}
	return &WSHandler{
		hub:    hub,
		logger: logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				origin := strings.TrimSpace(strings.ToLower(r.Header.Get("Origin")))
				if origin == "" {
					// 非浏览器客户端（原生应用/工具）不发送 Origin 头，放行。
					return true
				}
				if _, ok := allowed[origin]; ok {
					return true
				}
				// 同源放行：浏览器页面由本服务端提供，其 Origin 的 host 与请求 Host 一致。
				return originHostMatches(origin, r.Host)
			},
		},
	}
}

// originHostMatches 判断 Origin URL 的 host 是否与请求 Host 一致（同源）。
func originHostMatches(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

// HandleWebSocket 处理WebSocket连接请求
func (h *WSHandler) HandleWebSocket(c *gin.Context) {
	// 获取用户信息（已通过JWT中间件验证）
	userID, _ := c.Get("user_id")

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Errorf("WebSocket升级失败: %v", err)
		return
	}

	uid := ""
	if userID != nil {
		uid = userID.(string)
	}

	h.hub.RegisterClient(conn, uid)
	h.logger.Debugf("新的WebSocket连接: user=%s", uid)
}
