package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// FederationService 多服务器联邦架构服务
// 实现多台 NAS 设备间的媒体资源共享、数据同步和负载均衡
type FederationService struct {
	db     *gorm.DB
	logger *zap.SugaredLogger
	client *http.Client
	mu     sync.RWMutex
	nodeID string // 当前节点ID
	wsHub  *WSHub
}

// ServerNode 服务器节点
type ServerNode struct {
	ID      string `json:"id" gorm:"primaryKey;type:text"`
	Name    string `json:"name" gorm:"type:text;not null"`
	URL     string `json:"url" gorm:"type:text;not null;uniqueIndex"` // 节点地址
	APIKey  string `json:"api_key" gorm:"type:text"`                  // 认证密钥
	Status  string `json:"status" gorm:"type:text;default:offline"`   // online / offline / syncing / error
	Role    string `json:"role" gorm:"type:text;default:peer"`        // primary / peer / mirror
	Version string `json:"version" gorm:"type:text"`
	// 节点能力
	MediaCount   int     `json:"media_count"`
	StorageUsed  int64   `json:"storage_used"`  // 已用存储（字节）
	StorageTotal int64   `json:"storage_total"` // 总存储（字节）
	CPUUsage     float64 `json:"cpu_usage"`
	MemUsage     float64 `json:"mem_usage"`
	// 同步信息
	LastSync   *time.Time `json:"last_sync"`
	SyncStatus string     `json:"sync_status" gorm:"type:text"` // idle / syncing / error
	SyncError  string     `json:"sync_error" gorm:"type:text"`
	// 网络信息
	Latency   int       `json:"latency"`                       // 延迟（毫秒）
	Bandwidth int64     `json:"bandwidth"`                     // 带宽（字节/秒）
	IsLocal   bool      `json:"is_local" gorm:"default:false"` // 是否为本地节点
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SharedMedia 共享媒体索引（来自其他节点的媒体）
type SharedMedia struct {
	ID         string    `json:"id" gorm:"primaryKey;type:text"`
	NodeID     string    `json:"node_id" gorm:"index;type:text;not null"`
	RemoteID   string    `json:"remote_id" gorm:"type:text;not null"` // 远程节点上的媒体ID
	Title      string    `json:"title" gorm:"type:text"`
	OrigTitle  string    `json:"orig_title" gorm:"type:text"`
	Year       int       `json:"year"`
	Overview   string    `json:"overview" gorm:"type:text"`
	PosterPath string    `json:"poster_path" gorm:"type:text"`
	Rating     float64   `json:"rating"`
	Genres     string    `json:"genres" gorm:"type:text"`
	MediaType  string    `json:"media_type" gorm:"type:text"`
	Duration   float64   `json:"duration"`
	Resolution string    `json:"resolution" gorm:"type:text"`
	StreamURL  string    `json:"stream_url" gorm:"type:text"` // 远程流媒体地址
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// SyncTask 同步任务
type SyncTask struct {
	ID          string     `json:"id" gorm:"primaryKey;type:text"`
	NodeID      string     `json:"node_id" gorm:"index;type:text;not null"`
	Type        string     `json:"type" gorm:"type:text"`                   // full / incremental / metadata_only
	Status      string     `json:"status" gorm:"type:text;default:pending"` // pending / running / completed / failed
	Progress    float64    `json:"progress"`
	Total       int        `json:"total"`
	Synced      int        `json:"synced"`
	Failed      int        `json:"failed"`
	Error       string     `json:"error" gorm:"type:text"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// NodeHealthCheck 节点健康检查响应
type NodeHealthCheck struct {
	NodeID       string  `json:"node_id"`
	Version      string  `json:"version"`
	Status       string  `json:"status"`
	MediaCount   int     `json:"media_count"`
	StorageUsed  int64   `json:"storage_used"`
	StorageTotal int64   `json:"storage_total"`
	CPUUsage     float64 `json:"cpu_usage"`
	MemUsage     float64 `json:"mem_usage"`
	Uptime       int64   `json:"uptime"`
}

// FederationStats 联邦统计
type FederationStats struct {
	TotalNodes   int   `json:"total_nodes"`
	OnlineNodes  int   `json:"online_nodes"`
	TotalMedia   int   `json:"total_media"`
	SharedMedia  int   `json:"shared_media"`
	TotalStorage int64 `json:"total_storage"`
	UsedStorage  int64 `json:"used_storage"`
}

func NewFederationService(db *gorm.DB, nodeID string, logger *zap.SugaredLogger) *FederationService {
	db.AutoMigrate(&ServerNode{}, &SharedMedia{}, &SyncTask{})

	svc := &FederationService{
		db:     db,
		logger: logger,
		client: &http.Client{Timeout: 15 * time.Second},
		nodeID: nodeID,
	}

	// 启动定期健康检查
	go svc.healthCheckLoop()

	return svc
}

// SetWSHub 设置WebSocket Hub
func (s *FederationService) SetWSHub(hub *WSHub) {
	s.wsHub = hub
}

// RegisterNode 注册新节点
func (s *FederationService) RegisterNode(name, url, apiKey, role string) (*ServerNode, error) {
	// 检查是否已存在
	var existing ServerNode
	if err := s.db.Where("url = ?", url).First(&existing).Error; err == nil {
		return nil, fmt.Errorf("节点 %s 已注册", url)
	}

	node := &ServerNode{
		ID:     fmt.Sprintf("node_%d", time.Now().UnixNano()),
		Name:   name,
		URL:    url,
		APIKey: apiKey,
		Role:   role,
		Status: "offline",
	}

	// 立即进行健康检查
	if err := s.checkNodeHealth(node); err != nil {
		s.logger.Warnf("节点健康检查失败: %s -> %v", url, err)
		node.Status = "error"
		node.SyncError = err.Error()
	}

	if err := s.db.Create(node).Error; err != nil {
		return nil, fmt.Errorf("注册节点失败: %w", err)
	}

	s.logger.Infof("节点已注册: %s (%s)", name, url)
	return node, nil
}

// RemoveNode 移除节点
func (s *FederationService) RemoveNode(nodeID string) error {
	// 删除共享媒体索引
	s.db.Where("node_id = ?", nodeID).Delete(&SharedMedia{})
	// 删除同步任务
	s.db.Where("node_id = ?", nodeID).Delete(&SyncTask{})
	// 删除节点
	return s.db.Delete(&ServerNode{}, "id = ?", nodeID).Error
}

// ListNodes 获取所有节点
func (s *FederationService) ListNodes() ([]ServerNode, error) {
	var nodes []ServerNode
	err := s.db.Order("is_local DESC, name ASC").Find(&nodes).Error
	return nodes, err
}

// GetNode 获取节点详情
func (s *FederationService) GetNode(nodeID string) (*ServerNode, error) {
	var node ServerNode
	err := s.db.First(&node, "id = ?", nodeID).Error
	return &node, err
}

// SyncFromNode 从指定节点同步媒体索引
func (s *FederationService) SyncFromNode(nodeID string, syncType string) (*SyncTask, error) {
	var node ServerNode
	if err := s.db.First(&node, "id = ?", nodeID).Error; err != nil {
		return nil, fmt.Errorf("节点不存在")
	}

	if node.Status != "online" {
		return nil, fmt.Errorf("节点不在线")
	}

	task := &SyncTask{
		ID:     fmt.Sprintf("sync_%d", time.Now().UnixNano()),
		NodeID: nodeID,
		Type:   syncType,
		Status: "pending",
	}
	s.db.Create(task)

	// 异步执行同步
	go s.executeSyncTask(task, &node)

	return task, nil
}

// SearchSharedMedia 搜索共享媒体
func (s *FederationService) SearchSharedMedia(query string, page, size int) ([]SharedMedia, int64, error) {
	var media []SharedMedia
	var total int64

	searchQuery := "%" + query + "%"
	q := s.db.Model(&SharedMedia{}).Where("title LIKE ? OR orig_title LIKE ?", searchQuery, searchQuery)
	q.Count(&total)
	err := q.Offset((page - 1) * size).Limit(size).Find(&media).Error
	return media, total, err
}

// GetSharedMediaStream 获取共享媒体的流媒体地址
func (s *FederationService) GetSharedMediaStream(sharedMediaID string) (string, error) {
	var sm SharedMedia
	if err := s.db.First(&sm, "id = ?", sharedMediaID).Error; err != nil {
		return "", fmt.Errorf("共享媒体不存在")
	}

	var node ServerNode
	if err := s.db.First(&node, "id = ?", sm.NodeID).Error; err != nil {
		return "", fmt.Errorf("源节点不存在")
	}

	// 构建远程流媒体URL
	streamURL := fmt.Sprintf("%s/api/stream/%s/direct?token=%s", node.URL, sm.RemoteID, node.APIKey)
	return streamURL, nil
}

// GetStats 获取联邦统计
func (s *FederationService) GetStats() (*FederationStats, error) {
	stats := &FederationStats{}

	var totalNodes, onlineNodes, sharedMedia int64
	s.db.Model(&ServerNode{}).Count(&totalNodes)
	s.db.Model(&ServerNode{}).Where("status = ?", "online").Count(&onlineNodes)
	s.db.Model(&SharedMedia{}).Count(&sharedMedia)
	stats.TotalNodes = int(totalNodes)
	stats.OnlineNodes = int(onlineNodes)
	stats.SharedMedia = int(sharedMedia)

	// 统计本地媒体数
	var localMedia int64
	s.db.Table("media").Count(&localMedia)
	stats.TotalMedia = int(localMedia) + stats.SharedMedia

	// 统计存储
	var storageStats struct {
		TotalStorage int64
		UsedStorage  int64
	}
	s.db.Model(&ServerNode{}).
		Select("COALESCE(SUM(storage_total), 0) as total_storage, COALESCE(SUM(storage_used), 0) as used_storage").
		Scan(&storageStats)
	stats.TotalStorage = storageStats.TotalStorage
	stats.UsedStorage = storageStats.UsedStorage

	return stats, nil
}

// GetSyncTasks 获取同步任务列表
func (s *FederationService) GetSyncTasks(nodeID string) ([]SyncTask, error) {
	var tasks []SyncTask
	query := s.db.Model(&SyncTask{})
	if nodeID != "" {
		query = query.Where("node_id = ?", nodeID)
	}
	err := query.Order("created_at DESC").Limit(50).Find(&tasks).Error
	return tasks, err
}

// ==================== 内部方法 ====================

// healthCheckLoop 定期健康检查
func (s *FederationService) healthCheckLoop() {
	time.Sleep(10 * time.Second) // 等待服务启动

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		var nodes []ServerNode
		s.db.Where("is_local = ?", false).Find(&nodes)

		for i := range nodes {
			if err := s.checkNodeHealth(&nodes[i]); err != nil {
				nodes[i].Status = "offline"
				nodes[i].SyncError = err.Error()
			} else {
				nodes[i].Status = "online"
				nodes[i].SyncError = ""
			}
			s.db.Save(&nodes[i])
		}
	}
}

// checkNodeHealth 检查节点健康状态
func (s *FederationService) checkNodeHealth(node *ServerNode) error {
	url := fmt.Sprintf("%s/api/federation/health", node.URL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	if node.APIKey != "" {
		req.Header.Set("X-Federation-Key", node.APIKey)
	}

	start := time.Now()
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()

	node.Latency = int(time.Since(start).Milliseconds())

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var health NodeHealthCheck
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	node.Version = health.Version
	node.MediaCount = health.MediaCount
	node.StorageUsed = health.StorageUsed
	node.StorageTotal = health.StorageTotal
	node.CPUUsage = health.CPUUsage
	node.MemUsage = health.MemUsage

	return nil
}

// executeSyncTask 执行同步任务
func (s *FederationService) executeSyncTask(task *SyncTask, node *ServerNode) {
	now := time.Now()
	task.Status = "running"
	task.StartedAt = &now
	s.db.Save(task)

	s.logger.Infof("开始同步: %s (%s)", node.Name, task.Type)

	// 获取远程媒体列表
	url := fmt.Sprintf("%s/api/federation/media?type=%s", node.URL, task.Type)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		s.failSyncTask(task, err)
		return
	}

	if node.APIKey != "" {
		req.Header.Set("X-Federation-Key", node.APIKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		s.failSyncTask(task, err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		s.failSyncTask(task, err)
		return
	}

	var remoteMedia struct {
		Data []struct {
			ID         string  `json:"id"`
			Title      string  `json:"title"`
			OrigTitle  string  `json:"orig_title"`
			Year       int     `json:"year"`
			Overview   string  `json:"overview"`
			PosterPath string  `json:"poster_path"`
			Rating     float64 `json:"rating"`
			Genres     string  `json:"genres"`
			MediaType  string  `json:"media_type"`
			Duration   float64 `json:"duration"`
			Resolution string  `json:"resolution"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &remoteMedia); err != nil {
		s.failSyncTask(task, err)
		return
	}

	task.Total = len(remoteMedia.Data)

	// 同步每个媒体
	for _, rm := range remoteMedia.Data {
		// 检查是否已存在
		var count int64
		s.db.Model(&SharedMedia{}).Where("node_id = ? AND remote_id = ?", node.ID, rm.ID).Count(&count)

		shared := SharedMedia{
			NodeID:     node.ID,
			RemoteID:   rm.ID,
			Title:      rm.Title,
			OrigTitle:  rm.OrigTitle,
			Year:       rm.Year,
			Overview:   rm.Overview,
			PosterPath: rm.PosterPath,
			Rating:     rm.Rating,
			Genres:     rm.Genres,
			MediaType:  rm.MediaType,
			Duration:   rm.Duration,
			Resolution: rm.Resolution,
			StreamURL:  fmt.Sprintf("%s/api/stream/%s/direct", node.URL, rm.ID),
		}

		if count == 0 {
			shared.ID = fmt.Sprintf("sm_%d", time.Now().UnixNano())
			s.db.Create(&shared)
		} else {
			s.db.Model(&SharedMedia{}).Where("node_id = ? AND remote_id = ?", node.ID, rm.ID).Updates(&shared)
		}

		task.Synced++
		task.Progress = float64(task.Synced) / float64(task.Total) * 100
		s.db.Save(task)

		time.Sleep(time.Millisecond) // 避免ID冲突
	}

	// 完成
	completedAt := time.Now()
	task.Status = "completed"
	task.Progress = 100
	task.CompletedAt = &completedAt
	s.db.Save(task)

	// 更新节点同步时间
	node.LastSync = &completedAt
	node.SyncStatus = "idle"
	s.db.Save(node)

	s.logger.Infof("同步完成: %s, 共 %d 个媒体", node.Name, task.Synced)
}

func (s *FederationService) failSyncTask(task *SyncTask, err error) {
	task.Status = "failed"
	task.Error = err.Error()
	s.db.Save(task)
	s.logger.Errorf("同步失败: %v", err)
}

// ==================== 联邦 API 端点（供其他节点调用） ====================

// GetLocalHealth 获取本地节点健康信息
func (s *FederationService) GetLocalHealth() *NodeHealthCheck {
	var mediaCount int64
	s.db.Table("media").Count(&mediaCount)

	return &NodeHealthCheck{
		NodeID:     s.nodeID,
		Version:    "2.0.0",
		Status:     "online",
		MediaCount: int(mediaCount),
	}
}

// GetLocalMediaList 获取本地媒体列表（供其他节点同步）
func (s *FederationService) GetLocalMediaList() ([]map[string]interface{}, error) {
	var media []struct {
		ID         string
		Title      string
		OrigTitle  string `gorm:"column:orig_title"`
		Year       int
		Overview   string
		PosterPath string `gorm:"column:poster_path"`
		Rating     float64
		Genres     string
		MediaType  string `gorm:"column:media_type"`
		Duration   float64
		Resolution string
	}

	err := s.db.Table("media").
		Select("id, title, orig_title, year, overview, poster_path, rating, genres, media_type, duration, resolution").
		Find(&media).Error
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, len(media))
	for i, m := range media {
		result[i] = map[string]interface{}{
			"id":          m.ID,
			"title":       m.Title,
			"orig_title":  m.OrigTitle,
			"year":        m.Year,
			"overview":    m.Overview,
			"poster_path": m.PosterPath,
			"rating":      m.Rating,
			"genres":      m.Genres,
			"media_type":  m.MediaType,
			"duration":    m.Duration,
			"resolution":  m.Resolution,
		}
	}

	return result, nil
}

// VerifyFederationKey 验证联邦密钥
func (s *FederationService) VerifyFederationKey(key string) bool {
	// 检查是否有节点使用此密钥
	var count int64
	s.db.Model(&ServerNode{}).Where("api_key = ?", key).Count(&count)
	return count > 0
}

// 确保 bytes 包被使用（用于未来的请求体构建）
// 确保编译通过
