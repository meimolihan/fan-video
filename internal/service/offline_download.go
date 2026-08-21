package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// OfflineDownloadService 离线下载服务
// 支持将媒体内容下载到本地设备，实现无网络环境下的离线观看
type OfflineDownloadService struct {
	db            *gorm.DB
	logger        *zap.SugaredLogger
	wsHub         *WSHub
	mu            sync.RWMutex
	downloadDir   string
	maxConcurrent int
	activeJobs    map[string]*DownloadJob
	jobQueue      chan *DownloadJob
}

// DownloadTask 下载任务模型
type DownloadTask struct {
	ID         string     `json:"id" gorm:"primaryKey;type:text"`
	UserID     string     `json:"user_id" gorm:"index;type:text;not null"`
	MediaID    string     `json:"media_id" gorm:"index;type:text;not null"`
	Title      string     `json:"title" gorm:"type:text"`
	Quality    string     `json:"quality" gorm:"type:text;default:original"` // original / 1080p / 720p / 480p
	Status     string     `json:"status" gorm:"type:text;default:queued"`    // queued / downloading / completed / failed / cancelled / paused
	Progress   float64    `json:"progress"`                                  // 0-100
	FileSize   int64      `json:"file_size"`                                 // 原始文件大小
	Downloaded int64      `json:"downloaded"`                                // 已下载字节数
	OutputPath string     `json:"output_path" gorm:"type:text"`              // 下载输出路径
	Speed      int64      `json:"speed"`                                     // 下载速度（字节/秒）
	ETA        int        `json:"eta"`                                       // 预计剩余时间（秒）
	Error      string     `json:"error" gorm:"type:text"`
	Priority   int        `json:"priority" gorm:"default:0"` // 优先级（越大越优先）
	ExpiresAt  *time.Time `json:"expires_at"`                // 过期时间（离线内容有效期）
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// DownloadJob 下载任务执行体
type DownloadJob struct {
	Task    *DownloadTask
	Cancel  chan struct{}
	Paused  bool
	pauseMu sync.Mutex
}

// DownloadQueueInfo 下载队列信息
type DownloadQueueInfo struct {
	Active    int            `json:"active"`
	Queued    int            `json:"queued"`
	Completed int            `json:"completed"`
	Failed    int            `json:"failed"`
	TotalSize int64          `json:"total_size"`
	Tasks     []DownloadTask `json:"tasks"`
}

// BatchDownloadRequest 批量下载请求
type BatchDownloadRequest struct {
	MediaIDs []string `json:"media_ids"`
	Quality  string   `json:"quality"`
	Priority int      `json:"priority"`
}

func NewOfflineDownloadService(db *gorm.DB, downloadDir string, logger *zap.SugaredLogger) *OfflineDownloadService {
	db.AutoMigrate(&DownloadTask{})

	if downloadDir == "" {
		downloadDir = "./data/downloads"
	}
	os.MkdirAll(downloadDir, 0755)

	svc := &OfflineDownloadService{
		db:            db,
		logger:        logger,
		downloadDir:   downloadDir,
		maxConcurrent: 3,
		activeJobs:    make(map[string]*DownloadJob),
		jobQueue:      make(chan *DownloadJob, 100),
	}

	// 启动下载工作协程
	for i := 0; i < svc.maxConcurrent; i++ {
		go svc.downloadWorker(i)
	}

	// 恢复未完成的下载任务
	go svc.resumePendingTasks()

	return svc
}

// SetWSHub 设置WebSocket Hub
func (s *OfflineDownloadService) SetWSHub(hub *WSHub) {
	s.wsHub = hub
}

// CreateDownload 创建下载任务
func (s *OfflineDownloadService) CreateDownload(userID, mediaID, title string, fileSize int64, filePath, quality string) (*DownloadTask, error) {
	// 检查是否已有相同的下载任务
	var existing DownloadTask
	result := s.db.Where("user_id = ? AND media_id = ? AND status IN ?",
		userID, mediaID, []string{"queued", "downloading"}).First(&existing)
	if result.Error == nil {
		return &existing, fmt.Errorf("该媒体已在下载队列中")
	}

	// 确定输出路径
	userDir := filepath.Join(s.downloadDir, userID)
	os.MkdirAll(userDir, 0755)
	ext := filepath.Ext(filePath)
	outputPath := filepath.Join(userDir, fmt.Sprintf("%s_%s%s", mediaID, quality, ext))

	task := &DownloadTask{
		ID:         fmt.Sprintf("dl_%d", time.Now().UnixNano()),
		UserID:     userID,
		MediaID:    mediaID,
		Title:      title,
		Quality:    quality,
		Status:     "queued",
		FileSize:   fileSize,
		OutputPath: outputPath,
	}

	if err := s.db.Create(task).Error; err != nil {
		return nil, fmt.Errorf("创建下载任务失败: %w", err)
	}

	// 加入下载队列
	job := &DownloadJob{
		Task:   task,
		Cancel: make(chan struct{}),
	}

	s.mu.Lock()
	s.activeJobs[task.ID] = job
	s.mu.Unlock()

	s.jobQueue <- job

	s.logger.Infof("下载任务已创建: %s (%s)", title, quality)
	return task, nil
}

// BatchCreateDownloads 批量创建下载任务
func (s *OfflineDownloadService) BatchCreateDownloads(userID string, req BatchDownloadRequest) ([]DownloadTask, []string) {
	var tasks []DownloadTask
	var errors []string

	for _, mediaID := range req.MediaIDs {
		// 查询媒体信息
		var media struct {
			ID       string
			Title    string
			FileSize int64
			FilePath string
		}
		if err := s.db.Table("media").Select("id, title, file_size, file_path").
			Where("id = ?", mediaID).First(&media).Error; err != nil {
			errors = append(errors, fmt.Sprintf("媒体 %s 不存在", mediaID))
			continue
		}

		task, err := s.CreateDownload(userID, media.ID, media.Title, media.FileSize, media.FilePath, req.Quality)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", media.Title, err))
			continue
		}
		tasks = append(tasks, *task)
	}

	return tasks, errors
}

// CancelDownload 取消下载任务
func (s *OfflineDownloadService) CancelDownload(taskID, userID string) error {
	var task DownloadTask
	if err := s.db.Where("id = ? AND user_id = ?", taskID, userID).First(&task).Error; err != nil {
		return fmt.Errorf("下载任务不存在")
	}

	s.mu.RLock()
	job, exists := s.activeJobs[taskID]
	s.mu.RUnlock()

	if exists {
		close(job.Cancel)
	}

	task.Status = "cancelled"
	s.db.Save(&task)

	// 清理已下载的文件
	if task.OutputPath != "" {
		os.Remove(task.OutputPath)
	}

	s.mu.Lock()
	delete(s.activeJobs, taskID)
	s.mu.Unlock()

	return nil
}

// PauseDownload 暂停下载
func (s *OfflineDownloadService) PauseDownload(taskID, userID string) error {
	s.mu.RLock()
	job, exists := s.activeJobs[taskID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("下载任务不存在或未在执行")
	}

	job.pauseMu.Lock()
	job.Paused = true
	job.pauseMu.Unlock()

	s.db.Model(&DownloadTask{}).Where("id = ?", taskID).Update("status", "paused")
	return nil
}

// ResumeDownload 恢复下载
func (s *OfflineDownloadService) ResumeDownload(taskID, userID string) error {
	var task DownloadTask
	if err := s.db.Where("id = ? AND user_id = ? AND status = ?", taskID, userID, "paused").First(&task).Error; err != nil {
		return fmt.Errorf("下载任务不存在或未暂停")
	}

	task.Status = "queued"
	s.db.Save(&task)

	job := &DownloadJob{
		Task:   &task,
		Cancel: make(chan struct{}),
	}

	s.mu.Lock()
	s.activeJobs[task.ID] = job
	s.mu.Unlock()

	s.jobQueue <- job
	return nil
}

// GetUserDownloads 获取用户的下载列表
func (s *OfflineDownloadService) GetUserDownloads(userID string, status string) ([]DownloadTask, error) {
	var tasks []DownloadTask
	query := s.db.Where("user_id = ?", userID)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	err := query.Order("priority DESC, created_at DESC").Find(&tasks).Error
	return tasks, err
}

// GetQueueInfo 获取下载队列信息
func (s *OfflineDownloadService) GetQueueInfo(userID string) (*DownloadQueueInfo, error) {
	info := &DownloadQueueInfo{}

	var active, queued, completed, failed int64
	s.db.Model(&DownloadTask{}).Where("user_id = ? AND status = ?", userID, "downloading").Count(&active)
	s.db.Model(&DownloadTask{}).Where("user_id = ? AND status = ?", userID, "queued").Count(&queued)
	s.db.Model(&DownloadTask{}).Where("user_id = ? AND status = ?", userID, "completed").Count(&completed)
	s.db.Model(&DownloadTask{}).Where("user_id = ? AND status = ?", userID, "failed").Count(&failed)
	info.Active = int(active)
	info.Queued = int(queued)
	info.Completed = int(completed)
	info.Failed = int(failed)

	var totalSize int64
	s.db.Model(&DownloadTask{}).Where("user_id = ? AND status = ?", userID, "completed").
		Select("COALESCE(SUM(file_size), 0)").Scan(&totalSize)
	info.TotalSize = totalSize

	s.db.Where("user_id = ?", userID).Order("priority DESC, created_at DESC").Limit(50).Find(&info.Tasks)

	return info, nil
}

// DeleteDownload 删除已完成的下载
func (s *OfflineDownloadService) DeleteDownload(taskID, userID string) error {
	var task DownloadTask
	if err := s.db.Where("id = ? AND user_id = ?", taskID, userID).First(&task).Error; err != nil {
		return fmt.Errorf("下载任务不存在")
	}

	// 删除文件
	if task.OutputPath != "" {
		os.Remove(task.OutputPath)
	}

	return s.db.Delete(&task).Error
}

// CleanExpired 清理过期的离线内容
func (s *OfflineDownloadService) CleanExpired() (int, error) {
	var expired []DownloadTask
	s.db.Where("expires_at IS NOT NULL AND expires_at < ? AND status = ?",
		time.Now(), "completed").Find(&expired)

	for _, task := range expired {
		if task.OutputPath != "" {
			os.Remove(task.OutputPath)
		}
	}

	result := s.db.Where("expires_at IS NOT NULL AND expires_at < ? AND status = ?",
		time.Now(), "completed").Delete(&DownloadTask{})

	return int(result.RowsAffected), result.Error
}

// ==================== 内部方法 ====================

// downloadWorker 下载工作协程
func (s *OfflineDownloadService) downloadWorker(id int) {
	s.logger.Infof("下载工作协程 #%d 启动", id)
	for job := range s.jobQueue {
		s.processDownload(job)
	}
}

// processDownload 处理下载任务（文件复制方式）
func (s *OfflineDownloadService) processDownload(job *DownloadJob) {
	task := job.Task
	task.Status = "downloading"
	s.db.Save(task)

	s.logger.Infof("开始下载: %s", task.Title)

	// 广播下载开始事件
	s.broadcastDownloadEvent("download_started", task)

	// 查询源文件路径
	var filePath string
	s.db.Table("media").Select("file_path").Where("id = ?", task.MediaID).Scan(&filePath)
	if filePath == "" {
		task.Status = "failed"
		task.Error = "源文件不存在"
		s.db.Save(task)
		s.broadcastDownloadEvent("download_failed", task)
		return
	}

	// 打开源文件
	src, err := os.Open(filePath)
	if err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("打开源文件失败: %v", err)
		s.db.Save(task)
		s.broadcastDownloadEvent("download_failed", task)
		return
	}
	defer src.Close()

	// 获取文件大小
	fi, _ := src.Stat()
	task.FileSize = fi.Size()

	// 创建目标文件
	dst, err := os.Create(task.OutputPath)
	if err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("创建目标文件失败: %v", err)
		s.db.Save(task)
		s.broadcastDownloadEvent("download_failed", task)
		return
	}
	defer dst.Close()

	// 分块复制，支持进度报告和取消
	buf := make([]byte, 1024*1024) // 1MB 缓冲
	var copied int64
	startTime := time.Now()
	lastReport := time.Now()

	for {
		select {
		case <-job.Cancel:
			task.Status = "cancelled"
			s.db.Save(task)
			os.Remove(task.OutputPath)
			s.broadcastDownloadEvent("download_cancelled", task)
			return
		default:
		}

		// 检查暂停
		job.pauseMu.Lock()
		paused := job.Paused
		job.pauseMu.Unlock()
		if paused {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		n, err := src.Read(buf)
		if n > 0 {
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				task.Status = "failed"
				task.Error = fmt.Sprintf("写入失败: %v", writeErr)
				s.db.Save(task)
				s.broadcastDownloadEvent("download_failed", task)
				return
			}
			copied += int64(n)
			task.Downloaded = copied

			// 每秒报告一次进度
			if time.Since(lastReport) > time.Second {
				elapsed := time.Since(startTime).Seconds()
				if elapsed > 0 {
					task.Speed = int64(float64(copied) / elapsed)
					if task.Speed > 0 {
						task.ETA = int(float64(task.FileSize-copied) / float64(task.Speed))
					}
				}
				if task.FileSize > 0 {
					task.Progress = float64(copied) / float64(task.FileSize) * 100
				}
				s.db.Save(task)
				s.broadcastDownloadEvent("download_progress", task)
				lastReport = time.Now()
			}
		}

		if err != nil {
			break
		}
	}

	// 下载完成
	task.Status = "completed"
	task.Progress = 100
	task.Downloaded = task.FileSize
	s.db.Save(task)

	s.mu.Lock()
	delete(s.activeJobs, task.ID)
	s.mu.Unlock()

	s.logger.Infof("下载完成: %s", task.Title)
	s.broadcastDownloadEvent("download_completed", task)
}

// resumePendingTasks 恢复未完成的下载任务
func (s *OfflineDownloadService) resumePendingTasks() {
	time.Sleep(3 * time.Second) // 等待服务完全启动

	var tasks []DownloadTask
	s.db.Where("status IN ?", []string{"queued", "downloading"}).
		Order("priority DESC, created_at ASC").Find(&tasks)

	for i := range tasks {
		tasks[i].Status = "queued"
		s.db.Save(&tasks[i])

		job := &DownloadJob{
			Task:   &tasks[i],
			Cancel: make(chan struct{}),
		}

		s.mu.Lock()
		s.activeJobs[tasks[i].ID] = job
		s.mu.Unlock()

		s.jobQueue <- job
	}

	if len(tasks) > 0 {
		s.logger.Infof("恢复了 %d 个未完成的下载任务", len(tasks))
	}
}

// broadcastDownloadEvent 广播下载事件
func (s *OfflineDownloadService) broadcastDownloadEvent(eventType string, task *DownloadTask) {
	if s.wsHub != nil {
		s.wsHub.BroadcastEvent(eventType, task)
	}
}
