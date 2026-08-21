package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// NotificationService 智能通知服务
// 支持多渠道通知：Webhook、邮件、Telegram
type NotificationService struct {
	logger     *zap.SugaredLogger
	client     *http.Client
	mu         sync.RWMutex
	config     NotificationConfig
	eventQueue chan NotificationEvent
}

// NotificationConfig 通知配置
type NotificationConfig struct {
	// 全局开关
	Enabled bool `json:"enabled"`

	// Webhook 配置
	Webhooks []WebhookNotifyConfig `json:"webhooks"`

	// 邮件配置
	Email EmailConfig `json:"email"`

	// Telegram 配置
	Telegram TelegramConfig `json:"telegram"`

	// 事件订阅（哪些事件触发通知）
	Events NotificationEvents `json:"events"`
}

// WebhookNotifyConfig Webhook 通知配置
type WebhookNotifyConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Secret  string `json:"secret,omitempty"`
	Enabled bool   `json:"enabled"`
}

// EmailConfig 邮件通知配置
type EmailConfig struct {
	Enabled    bool     `json:"enabled"`
	SMTPHost   string   `json:"smtp_host"`
	SMTPPort   int      `json:"smtp_port"`
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	FromAddr   string   `json:"from_addr"`
	FromName   string   `json:"from_name"`
	Recipients []string `json:"recipients"`
	UseTLS     bool     `json:"use_tls"`
}

// TelegramConfig Telegram 通知配置
type TelegramConfig struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// NotificationEvents 事件订阅配置
type NotificationEvents struct {
	MediaAdded        bool `json:"media_added"`
	ScanComplete      bool `json:"scan_complete"`
	ScrapeComplete    bool `json:"scrape_complete"`
	TranscodeComplete bool `json:"transcode_complete"`
	UserLogin         bool `json:"user_login"`
	SystemError       bool `json:"system_error"`
}

// NotificationEvent 通知事件
type NotificationEvent struct {
	Type      string      `json:"type"`
	Title     string      `json:"title"`
	Message   string      `json:"message"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data,omitempty"`
}

// 通知事件类型常量
const (
	NotifyEventMediaAdded        = "media.added"
	NotifyEventScanComplete      = "scan.complete"
	NotifyEventScrapeComplete    = "scrape.complete"
	NotifyEventTranscodeComplete = "transcode.complete"
	NotifyEventUserLogin         = "user.login"
	NotifyEventSystemError       = "system.error"
)

func NewNotificationService(logger *zap.SugaredLogger) *NotificationService {
	ns := &NotificationService{
		logger: logger,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		config: NotificationConfig{
			Enabled: false,
			Events: NotificationEvents{
				MediaAdded:        true,
				ScanComplete:      true,
				ScrapeComplete:    false,
				TranscodeComplete: true,
				UserLogin:         false,
				SystemError:       true,
			},
		},
		eventQueue: make(chan NotificationEvent, 200),
	}

	// 启动事件分发协程
	go ns.dispatchLoop()

	return ns
}

// UpdateConfig 更新通知配置
func (ns *NotificationService) UpdateConfig(cfg NotificationConfig) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.config = cfg
	ns.logger.Infof("通知配置已更新，启用状态: %v", cfg.Enabled)
}

// GetConfig 获取当前通知配置
func (ns *NotificationService) GetConfig() NotificationConfig {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	return ns.config
}

// Notify 发送通知事件（异步，不阻塞调用方）
func (ns *NotificationService) Notify(eventType, title, message string, data interface{}) {
	ns.mu.RLock()
	enabled := ns.config.Enabled
	ns.mu.RUnlock()

	if !enabled {
		return
	}

	select {
	case ns.eventQueue <- NotificationEvent{
		Type:      eventType,
		Title:     title,
		Message:   message,
		Timestamp: time.Now(),
		Data:      data,
	}:
	default:
		ns.logger.Warnf("通知事件队列已满，丢弃事件: %s", eventType)
	}
}

// dispatchLoop 事件分发循环
func (ns *NotificationService) dispatchLoop() {
	for event := range ns.eventQueue {
		ns.mu.RLock()
		cfg := ns.config
		ns.mu.RUnlock()

		if !cfg.Enabled || !ns.shouldNotify(cfg.Events, event.Type) {
			continue
		}

		// 并行发送到各渠道
		var wg sync.WaitGroup

		// Webhook 通知
		for _, hook := range cfg.Webhooks {
			if hook.Enabled {
				wg.Add(1)
				go func(h WebhookNotifyConfig) {
					defer wg.Done()
					ns.sendWebhook(h, event)
				}(hook)
			}
		}

		// 邮件通知
		if cfg.Email.Enabled {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ns.sendEmail(cfg.Email, event)
			}()
		}

		// Telegram 通知
		if cfg.Telegram.Enabled {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ns.sendTelegram(cfg.Telegram, event)
			}()
		}

		wg.Wait()
	}
}

// shouldNotify 检查是否应该发送此类事件的通知
func (ns *NotificationService) shouldNotify(events NotificationEvents, eventType string) bool {
	switch eventType {
	case NotifyEventMediaAdded:
		return events.MediaAdded
	case NotifyEventScanComplete:
		return events.ScanComplete
	case NotifyEventScrapeComplete:
		return events.ScrapeComplete
	case NotifyEventTranscodeComplete:
		return events.TranscodeComplete
	case NotifyEventUserLogin:
		return events.UserLogin
	case NotifyEventSystemError:
		return events.SystemError
	default:
		return true
	}
}

// sendWebhook 发送 Webhook 通知
func (ns *NotificationService) sendWebhook(hook WebhookNotifyConfig, event NotificationEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		ns.logger.Errorf("Webhook 序列化失败: %v", err)
		return
	}

	req, err := http.NewRequest("POST", hook.URL, bytes.NewReader(payload))
	if err != nil {
		ns.logger.Errorf("Webhook 创建请求失败: %s -> %v", hook.URL, err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "fan-video-notification/2.0")
	req.Header.Set("X-Event-Type", event.Type)

	// 带重试的发送
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}

		resp, err := ns.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			ns.logger.Debugf("Webhook 发送成功: %s -> %s", event.Type, hook.URL)
			return
		}
		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	ns.logger.Warnf("Webhook 发送失败: %s -> %s: %v", event.Type, hook.URL, lastErr)
}

// sendEmail 发送邮件通知
func (ns *NotificationService) sendEmail(cfg EmailConfig, event NotificationEvent) {
	if cfg.SMTPHost == "" || len(cfg.Recipients) == 0 {
		return
	}

	fromName := cfg.FromName
	if fromName == "" {
		fromName = "Nowen Video"
	}

	// 构建邮件内容
	subject := fmt.Sprintf("[Nowen Video] %s", event.Title)
	body := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; padding: 20px; background: #f5f5f5;">
  <div style="max-width: 600px; margin: 0 auto; background: #fff; border-radius: 8px; padding: 30px; box-shadow: 0 2px 8px rgba(0,0,0,0.1);">
    <h2 style="color: #333; margin-top: 0;">%s</h2>
    <p style="color: #666; line-height: 1.6;">%s</p>
    <hr style="border: none; border-top: 1px solid #eee; margin: 20px 0;">
    <p style="color: #999; font-size: 12px;">
      事件类型: %s<br>
      时间: %s
    </p>
  </div>
</body>
</html>`, event.Title, event.Message, event.Type, event.Timestamp.Format("2006-01-02 15:04:05"))

	// 构建邮件头
	headers := make(map[string]string)
	headers["From"] = fmt.Sprintf("%s <%s>", fromName, cfg.FromAddr)
	headers["To"] = strings.Join(cfg.Recipients, ",")
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/html; charset=utf-8"

	var msg strings.Builder
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msg.WriteString("\r\n")
	msg.WriteString(body)

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPHost)

	err := smtp.SendMail(addr, auth, cfg.FromAddr, cfg.Recipients, []byte(msg.String()))
	if err != nil {
		ns.logger.Errorf("邮件发送失败: %v", err)
		return
	}

	ns.logger.Debugf("邮件通知发送成功: %s -> %v", event.Title, cfg.Recipients)
}

// sendTelegram 发送 Telegram 通知
func (ns *NotificationService) sendTelegram(cfg TelegramConfig, event NotificationEvent) {
	if cfg.BotToken == "" || cfg.ChatID == "" {
		return
	}

	// 构建消息文本（Markdown 格式）
	text := fmt.Sprintf("*%s*\n\n%s\n\n_事件: %s_\n_时间: %s_",
		escapeMarkdown(event.Title),
		escapeMarkdown(event.Message),
		event.Type,
		event.Timestamp.Format("2006-01-02 15:04:05"),
	)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)
	payload := map[string]interface{}{
		"chat_id":    cfg.ChatID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		ns.logger.Errorf("Telegram 消息序列化失败: %v", err)
		return
	}

	resp, err := ns.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		ns.logger.Errorf("Telegram 发送失败: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		ns.logger.Warnf("Telegram 发送异常: HTTP %d", resp.StatusCode)
		return
	}

	ns.logger.Debugf("Telegram 通知发送成功: %s", event.Title)
}

// TestNotification 测试通知渠道
func (ns *NotificationService) TestNotification(channel string) error {
	event := NotificationEvent{
		Type:      "test",
		Title:     "Nowen Video 通知测试",
		Message:   "如果您收到此消息，说明通知配置正确！",
		Timestamp: time.Now(),
	}

	ns.mu.RLock()
	cfg := ns.config
	ns.mu.RUnlock()

	switch channel {
	case "email":
		if !cfg.Email.Enabled {
			return fmt.Errorf("邮件通知未启用")
		}
		ns.sendEmail(cfg.Email, event)
	case "telegram":
		if !cfg.Telegram.Enabled {
			return fmt.Errorf("Telegram 通知未启用")
		}
		ns.sendTelegram(cfg.Telegram, event)
	case "webhook":
		for _, hook := range cfg.Webhooks {
			if hook.Enabled {
				ns.sendWebhook(hook, event)
			}
		}
	default:
		return fmt.Errorf("未知的通知渠道: %s", channel)
	}

	return nil
}

// escapeMarkdown 转义 Telegram Markdown 特殊字符
func escapeMarkdown(s string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(s)
}
