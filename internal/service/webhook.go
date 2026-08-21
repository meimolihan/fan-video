package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// WebhookService Webhook 事件通知服务
// 支持在特定事件发生时（扫描完成、新增媒体、播放开始等）向外部 URL 发送 HTTP 回调
type WebhookService struct {
	logger     *zap.SugaredLogger
	client     *http.Client
	mu         sync.RWMutex
	hooks      []WebhookConfig
	eventQueue chan WebhookEvent
}

// WebhookConfig Webhook 配置
type WebhookConfig struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	URL     string   `json:"url"`              // 回调 URL
	Events  []string `json:"events"`           // 监听的事件类型
	Secret  string   `json:"secret,omitempty"` // HMAC 签名密钥
	Enabled bool     `json:"enabled"`
}

// WebhookEvent Webhook 事件
type WebhookEvent struct {
	Type      string      `json:"type"` // 事件类型
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// Webhook 事件类型常量
const (
	WebhookEventLibraryScanComplete = "library.scan.complete"
	WebhookEventMediaAdded          = "media.added"
	WebhookEventMediaScraped        = "media.scraped"
	WebhookEventPlaybackStart       = "playback.start"
	WebhookEventPlaybackStop        = "playback.stop"
	WebhookEventTranscodeComplete   = "transcode.complete"
	WebhookEventUserLogin           = "user.login"
)

func NewWebhookService(logger *zap.SugaredLogger) *WebhookService {
	ws := &WebhookService{
		logger: logger,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		hooks:      make([]WebhookConfig, 0),
		eventQueue: make(chan WebhookEvent, 100),
	}

	// 启动事件分发协程
	go ws.dispatchLoop()

	return ws
}

// AddHook 添加 Webhook
func (ws *WebhookService) AddHook(hook WebhookConfig) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.hooks = append(ws.hooks, hook)
	ws.logger.Infof("添加 Webhook: %s -> %s", hook.Name, hook.URL)
}

// RemoveHook 移除 Webhook
func (ws *WebhookService) RemoveHook(id string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	for i, h := range ws.hooks {
		if h.ID == id {
			ws.hooks = append(ws.hooks[:i], ws.hooks[i+1:]...)
			ws.logger.Infof("移除 Webhook: %s", h.Name)
			return
		}
	}
}

// ListHooks 获取所有 Webhook 配置
func (ws *WebhookService) ListHooks() []WebhookConfig {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	result := make([]WebhookConfig, len(ws.hooks))
	copy(result, ws.hooks)
	return result
}

// Emit 发送事件（异步，不阻塞调用方）
func (ws *WebhookService) Emit(eventType string, data interface{}) {
	select {
	case ws.eventQueue <- WebhookEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}:
	default:
		ws.logger.Warnf("Webhook 事件队列已满，丢弃事件: %s", eventType)
	}
}

// dispatchLoop 事件分发循环
func (ws *WebhookService) dispatchLoop() {
	for event := range ws.eventQueue {
		ws.mu.RLock()
		hooks := make([]WebhookConfig, len(ws.hooks))
		copy(hooks, ws.hooks)
		ws.mu.RUnlock()

		for _, hook := range hooks {
			if !hook.Enabled {
				continue
			}
			if !ws.matchEvent(hook.Events, event.Type) {
				continue
			}
			go ws.sendWebhook(hook, event)
		}
	}
}

// matchEvent 检查 hook 是否监听此事件
func (ws *WebhookService) matchEvent(hookEvents []string, eventType string) bool {
	if len(hookEvents) == 0 {
		return true // 空列表表示监听所有事件
	}
	for _, e := range hookEvents {
		if e == eventType || e == "*" {
			return true
		}
	}
	return false
}

// sendWebhook 发送 Webhook 请求
func (ws *WebhookService) sendWebhook(hook WebhookConfig, event WebhookEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		ws.logger.Errorf("Webhook 序列化失败: %v", err)
		return
	}

	req, err := http.NewRequest("POST", hook.URL, bytes.NewReader(payload))
	if err != nil {
		ws.logger.Errorf("Webhook 创建请求失败: %s -> %v", hook.URL, err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "fan-video-webhook/1.0")
	req.Header.Set("X-Webhook-Event", event.Type)

	// 带重试的发送（最多重试2次）
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}

		resp, err := ws.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			ws.logger.Debugf("Webhook 发送成功: %s -> %s [%d]", event.Type, hook.URL, resp.StatusCode)
			return
		}
		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	ws.logger.Warnf("Webhook 发送失败（已重试）: %s -> %s: %v", event.Type, hook.URL, lastErr)
}
