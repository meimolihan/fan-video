package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

// ==================== 字幕预处理事件常量 ====================

const (
	EventSubPreStarted   = "sub_preprocess_started"
	EventSubPreProgress  = "sub_preprocess_progress"
	EventSubPreCompleted = "sub_preprocess_completed"
	EventSubPreFailed    = "sub_preprocess_failed"
	EventSubPreSkipped   = "sub_preprocess_skipped"
)

// SubPreProgressData 字幕预处理进度事件数据
type SubPreProgressData struct {
	TaskID     string  `json:"task_id"`
	MediaID    string  `json:"media_id"`
	MediaTitle string  `json:"media_title"`
	Status     string  `json:"status"`
	Phase      string  `json:"phase"`
	Progress   float64 `json:"progress"`
	Message    string  `json:"message"`
	Error      string  `json:"error,omitempty"`
}

// SubtitlePreprocessService 字幕预处理服务
type SubtitlePreprocessService struct {
	cfg        *config.Config
	repo       *repository.SubtitlePreprocessRepo
	mediaRepo  *repository.MediaRepo
	scanner    *ScannerService
	logger     *zap.SugaredLogger
	wsHub      *WSHub

	// 工作协程控制
	workerCount int32
	maxWorkers  int32
	jobQueue    chan string // 任务 ID 队列
	cancelJobs  sync.Map    // 取消的任务 ID 集合
	inQueueIDs  sync.Map    // 已在队列或处理中的任务 ID，用于 reconciler 去重

	// 进度广播节流
	lastBroadcast sync.Map // taskID -> time.Time

}

// NewSubtitlePreprocessService 创建字幕预处理服务
func NewSubtitlePreprocessService(
	cfg *config.Config,
	repo *repository.SubtitlePreprocessRepo,
	mediaRepo *repository.MediaRepo,
	scanner *ScannerService,
	logger *zap.SugaredLogger,
) *SubtitlePreprocessService {
	// 字幕预处理默认 1 个 worker（避免与视频预处理争抢资源）
	maxWorkers := int32(cfg.Subtitle.Workers)
	if maxWorkers <= 0 {
		maxWorkers = 1
	}

	s := &SubtitlePreprocessService{
		cfg:        cfg,
		repo:       repo,
		mediaRepo:  mediaRepo,
		scanner:    scanner,
		logger:     logger,
		maxWorkers: maxWorkers,
		jobQueue:   make(chan string, 200),
	}

	// 启动工作协程池
	for i := int32(0); i < maxWorkers; i++ {
		go s.worker(int(i))
	}

	// 恢复未完成的任务
	go s.recoverPendingTasks()

	// TODO P2: 启动 pending reconciler，防止任务因队列满而永远被丢掉（方法待实现）
	// go s.pendingReconciler()

	return s
}

// SetWSHub 设置 WebSocket Hub
func (s *SubtitlePreprocessService) SetWSHub(hub *WSHub) {
	s.wsHub = hub
}

// ==================== 公开 API ====================

// enqueueTask 将任务入队，如队列满则仅记录 inQueueIDs、等待 reconciler 后续拉起
func (s *SubtitlePreprocessService) enqueueTask(taskID string) {
	select {
	case s.jobQueue <- taskID:
		s.inQueueIDs.Store(taskID, true)
	default:
		s.logger.Warnf("字幕预处理队列已满，任务 %s 将在下次调度时处理", taskID)
	}
}

// RetryAllFailed 一键重试所有失败任务
func (s *SubtitlePreprocessService) RetryAllFailed() (int, error) {
	tasks, err := s.repo.RetryAllFailed()
	if err != nil {
		return 0, fmt.Errorf("重试所有失败任务失败: %w", err)
	}

	// 将任务重新入队
	for _, task := range tasks {
		s.enqueueTask(task.ID)
	}

	return len(tasks), nil
}

// DeleteByStatus 按状态批量删除任务
func (s *SubtitlePreprocessService) DeleteByStatus(status string) (int64, error) {
	return s.repo.DeleteByStatus(status)
}

// SubmitMedia 提交单个媒体进行字幕预处理
func (s *SubtitlePreprocessService) SubmitMedia(mediaID string, targetLangs []string, forceRegenerate bool) (*model.SubtitlePreprocessTask, error) {
	// 检查是否已有活跃任务
	existing, err := s.repo.FindActiveByMediaID(mediaID)
	if err == nil && existing != nil {
		return existing, nil
	}

	media, err := s.mediaRepo.FindByID(mediaID)
	if err != nil {
		return nil, fmt.Errorf("媒体不存在: %w", err)
	}

	// STRM 远程流也支持（ASR 服务已支持远程流）
	// 但本地文件需要检查是否存在
	if media.StreamURL == "" {
		if _, err := os.Stat(media.FilePath); os.IsNotExist(err) {
			return nil, fmt.Errorf("媒体文件不存在: %s", media.FilePath)
		}
	}

	// 序列化目标语言列表
	targetLangsStr := ""
	if len(targetLangs) > 0 {
		targetLangsStr = strings.Join(targetLangs, ",")
	}

	task := &model.SubtitlePreprocessTask{
		MediaID:         mediaID,
		Status:          "pending",
		Phase:           "check",
		Message:         "等待处理...",
		SourceLang:      "auto",
		TargetLangs:     targetLangsStr,
		ForceRegenerate: forceRegenerate,
		MediaTitle:      media.DisplayTitle(),
	}

	// P0: 如果没有内嵌/外挂字幕且 ASR 不可用，预检并给出明确提示
	// 但仍然创建任务（可能有内嵌字幕可以提取）

	if err := s.repo.Create(task); err != nil {
		return nil, fmt.Errorf("创建字幕预处理任务失败: %w", err)
	}

	// 入队
	s.enqueueTask(task.ID)

	return task, nil
}

// BatchSubmit 批量提交字幕预处理任务
func (s *SubtitlePreprocessService) BatchSubmit(mediaIDs []string, targetLangs []string, forceRegenerate bool) ([]*model.SubtitlePreprocessTask, error) {
	var tasks []*model.SubtitlePreprocessTask
	for _, id := range mediaIDs {
		task, err := s.SubmitMedia(id, targetLangs, forceRegenerate)
		if err != nil {
			s.logger.Warnf("批量提交字幕预处理跳过 %s: %v", id, err)
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// SubmitLibrary 提交整个媒体库的所有视频进行字幕预处理
func (s *SubtitlePreprocessService) SubmitLibrary(libraryID string, targetLangs []string, forceRegenerate bool) (int, error) {
	medias, err := s.mediaRepo.ListByLibraryID(libraryID)
	if err != nil {
		return 0, fmt.Errorf("查询媒体库失败: %w", err)
	}

	count := 0
	for _, media := range medias {
		// 跳过已有活跃任务的
		if _, err := s.repo.FindActiveByMediaID(media.ID); err == nil {
			continue
		}
		// 跳过已完成且非强制重新生成的
		if !forceRegenerate {
			if existing, err := s.repo.FindByMediaID(media.ID); err == nil && existing.Status == "completed" {
				continue
			}
		}

		if _, err := s.SubmitMedia(media.ID, targetLangs, forceRegenerate); err == nil {
			count++
		}
	}

	return count, nil
}

// CancelTask 取消任务
func (s *SubtitlePreprocessService) CancelTask(taskID string) error {
	task, err := s.repo.FindByID(taskID)
	if err != nil {
		return fmt.Errorf("任务不存在: %w", err)
	}

	if task.Status == "completed" || task.Status == "cancelled" || task.Status == "skipped" {
		return fmt.Errorf("任务状态 %s 不可取消", task.Status)
	}

	s.cancelJobs.Store(taskID, true)
	task.Status = "cancelled"
	task.Message = "已取消"
	s.repo.Update(task)

	return nil
}

// RetryTask 重试失败的任务
func (s *SubtitlePreprocessService) RetryTask(taskID string) error {
	task, err := s.repo.FindByID(taskID)
	if err != nil {
		return fmt.Errorf("任务不存在: %w", err)
	}

	if task.Status != "failed" {
		return fmt.Errorf("只有失败的任务可以重试")
	}

	task.Status = "pending"
	task.Error = ""
	task.Message = "重试中..."
	s.repo.Update(task)

	s.enqueueTask(task.ID)

	return nil
}

// GetTask 获取任务详情
func (s *SubtitlePreprocessService) GetTask(taskID string) (*model.SubtitlePreprocessTask, error) {
	return s.repo.FindByID(taskID)
}

// GetMediaTask 获取媒体的字幕预处理任务
func (s *SubtitlePreprocessService) GetMediaTask(mediaID string) (*model.SubtitlePreprocessTask, error) {
	return s.repo.FindByMediaID(mediaID)
}

// ListTasks 分页获取任务列表
func (s *SubtitlePreprocessService) ListTasks(page, pageSize int, status string) ([]model.SubtitlePreprocessTask, int64, error) {
	tasks, total, err := s.repo.ListAll(page, pageSize, status)
	if err != nil {
		return tasks, total, err
	}
	// 用关联的 Media 信息补充/修正 media_title（兼容旧任务缺少集数信息的情况）
	for i := range tasks {
		if tasks[i].Media.ID != "" {
			tasks[i].MediaTitle = tasks[i].Media.DisplayTitle()
		}
	}
	return tasks, total, err
}

// GetStatistics 获取字幕预处理统计
func (s *SubtitlePreprocessService) GetStatistics() map[string]interface{} {
	counts, _ := s.repo.CountByStatus()

	return map[string]interface{}{
		"status_counts":  counts,
		"max_workers":    s.maxWorkers,
		"active_workers": atomic.LoadInt32(&s.workerCount),
		"queue_size":     len(s.jobQueue),
	}
}

// DeleteTask 删除任务（仅终态任务可删除）
func (s *SubtitlePreprocessService) DeleteTask(taskID string) error {
	task, err := s.repo.FindByID(taskID)
	if err != nil {
		return fmt.Errorf("任务不存在: %w", err)
	}

	if task.Status == "running" {
		return fmt.Errorf("运行中的任务不可删除，请先取消")
	}

	return s.repo.DeleteByID(taskID)
}

// ==================== 工作协程 ====================

func (s *SubtitlePreprocessService) worker(id int) {
	s.logger.Infof("字幕预处理工作协程 #%d 启动", id)
	for taskID := range s.jobQueue {
		// 出队时从 inQueueIDs 清除
		s.inQueueIDs.Delete(taskID)

		// 检查是否已取消
		if _, cancelled := s.cancelJobs.LoadAndDelete(taskID); cancelled {
			s.lastBroadcast.Delete(taskID)
			continue
		}

		atomic.AddInt32(&s.workerCount, 1)
		s.processTask(taskID)
		atomic.AddInt32(&s.workerCount, -1)

		// 统一清理那些【运行中被取消】而未在出队时清理的条目
		s.cancelJobs.Delete(taskID)
		s.lastBroadcast.Delete(taskID)
	}
}

func (s *SubtitlePreprocessService) processTask(taskID string) {
	task, err := s.repo.FindByID(taskID)
	if err != nil {
		s.logger.Warnf("字幕预处理任务不存在: %s", taskID)
		return
	}

	if task.Status == "cancelled" || task.Status == "completed" || task.Status == "skipped" {
		return
	}

	now := time.Now()
	task.Status = "running"
	task.StartedAt = &now
	task.Message = "开始字幕预处理..."
	s.repo.Update(task)

	s.broadcastEvent(EventSubPreStarted, task)
	s.logger.Infof("开始字幕预处理: %s (%s)", task.MediaTitle, task.MediaID)

	media, err := s.mediaRepo.FindByID(task.MediaID)
	if err != nil {
		s.failTask(task, fmt.Sprintf("媒体不存在: %v", err))
		return
	}

	// ========== Phase 1: 检查现有字幕 ==========
	if s.isCancelled(taskID) {
		return
	}
	s.updatePhase(task, "check", 5, "正在检查现有字幕...")

	existingVTTPath, subtitleSource := s.checkExistingSubtitles(media, task.ForceRegenerate)

	if existingVTTPath != "" && !task.ForceRegenerate {
		task.OriginalVTTPath = existingVTTPath
		task.SubtitleSource = subtitleSource
		task.CueCount = s.countVTTCues(existingVTTPath)
		s.logger.Infof("发现已有字幕: %s (来源: %s, %d 条)", task.MediaTitle, subtitleSource, task.CueCount)

		// 如果没有翻译需求，直接完成
		if task.TargetLangs == "" {
			s.completeTask(task, "已有字幕，无需处理")
			return
		}

		// 跳到翻译阶段
		s.updatePhase(task, "translate", 55, "已有字幕，开始翻译...")
	} else {
		// ========== Phase 2: 字幕格式标准化（提取/转换为 VTT） ==========
		if s.isCancelled(taskID) {
			return
		}

		// 检查是否有内嵌或外挂字幕可以提取
		extractedPath, extractSource := s.tryExtractSubtitle(media)
		if extractedPath != "" && !task.ForceRegenerate {
			task.OriginalVTTPath = extractedPath
			task.SubtitleSource = extractSource
			task.CueCount = s.countVTTCues(extractedPath)
			s.updatePhase(task, "extract", 35, fmt.Sprintf("已提取字幕（%d 条, %s）", task.CueCount, extractSource))
		} else {
			// 无可用字幕且无法提取，跳过
			task.Status = "skipped"
			task.Message = "未找到可用字幕（无内嵌文本字幕、外挂字幕或 VTT）"
			task.Phase = "done"
			completedAt := time.Now()
			task.CompletedAt = &completedAt
			s.repo.Update(task)
			s.broadcastEvent(EventSubPreSkipped, task)
			s.logger.Infof("字幕预处理跳过: %s (无可用字幕)", task.MediaTitle)
			return
		}
	}

	// ========== Phase 2.5: 字幕内容预处理（清洗/标准化） ==========
	if s.isCancelled(taskID) {
		return
	}

	if s.cfg.Subtitle.SubCleanEnabled && task.OriginalVTTPath != "" {
		s.updatePhase(task, "clean", 48, "正在清洗字幕内容...")

		cleaner := NewSubtitleCleaner(s.buildCleanConfig(), s.logger)
		report, err := cleaner.CleanVTT(task.OriginalVTTPath)
		if err != nil {
			s.logger.Warnf("字幕清洗失败（不影响后续流程）: %v", err)
			s.updatePhase(task, "clean", 52, fmt.Sprintf("字幕清洗失败: %v", err))
		} else {
			task.CueCount = report.ProcessedCueCount
			// 将详细报告持久化为 JSON，方便前端展示详情
			if reportBytes, jerr := json.Marshal(report); jerr == nil {
				task.CleanReportJSON = string(reportBytes)
			}
			msg := fmt.Sprintf("字幕清洗完成（%d→%d 条, 编码: %s",
				report.OriginalCueCount, report.ProcessedCueCount, report.DetectedEncoding)
			if report.RemovedAds > 0 {
				msg += fmt.Sprintf(", 去广告: %d", report.RemovedAds)
			}
			if report.RemovedSDH > 0 {
				msg += fmt.Sprintf(", 去SDH: %d", report.RemovedSDH)
			}
			if report.MergedCues > 0 {
				msg += fmt.Sprintf(", 合并: %d", report.MergedCues)
			}
			if report.SplitCues > 0 {
				msg += fmt.Sprintf(", 拆分: %d", report.SplitCues)
			}
			msg += "）"
			s.updatePhase(task, "clean", 55, msg)
		}
	}

	// ========== 完成 ==========
	s.completeTask(task, "")
}

// ==================== 内部方法 ====================

// checkExistingSubtitles 检查媒体是否已有可用字幕
func (s *SubtitlePreprocessService) checkExistingSubtitles(media *model.Media, forceRegenerate bool) (vttPath string, source string) {
	if forceRegenerate {
		return "", ""
	}

	// 1. 检查外挂 VTT 字幕
	if media.SubtitlePaths != "" {
		for _, subPath := range strings.Split(media.SubtitlePaths, "|") {
			subPath = strings.TrimSpace(subPath)
			if subPath == "" {
				continue
			}
			ext := strings.ToLower(filepath.Ext(subPath))
			if ext == ".vtt" {
				if _, err := os.Stat(subPath); err == nil {
					return subPath, "external_vtt"
				}
			}
		}
	}

	return "", ""
}

// tryExtractSubtitle 尝试从内嵌或外挂字幕提取 VTT
// 返回值: (vttPath, source) - source 为 "extracted"
func (s *SubtitlePreprocessService) tryExtractSubtitle(media *model.Media) (string, string) {
	// STRM 远程流不支持字幕提取
	if media.StreamURL != "" {
		return "", ""
	}

	filePath := media.FilePath

	// 1. 尝试提取内嵌文本字幕
	tracks, err := s.scanner.GetSubtitleTracks(filePath)
	if err == nil && len(tracks) > 0 {
		// 优先选择默认字幕轨道，其次选择第一个非图形字幕
		var bestTrack *SubtitleTrack
		for i := range tracks {
			if tracks[i].Bitmap {
				continue // 跳过图形字幕
			}
			if bestTrack == nil || tracks[i].Default {
				bestTrack = &tracks[i]
			}
		}

		if bestTrack != nil {
			vttPath, err := s.scanner.ExtractSubtitle(filePath, bestTrack.Index, "vtt")
			if err == nil {
				return vttPath, "extracted"
			}
			s.logger.Warnf("提取内嵌字幕失败: %v", err)
		}

	}

	// 2. 尝试转换外挂字幕为 VTT
	if media.SubtitlePaths != "" {
		for _, subPath := range strings.Split(media.SubtitlePaths, "|") {
			subPath = strings.TrimSpace(subPath)
			if subPath == "" {
				continue
			}
			ext := strings.ToLower(filepath.Ext(subPath))
			switch ext {
			case ".srt", ".ass", ".ssa":
				// P2: 使用带编码检测的转换函数，避免 GBK/Big5/SJIS 文件直接产生乱码
				vttPath, err := s.scanner.ConvertSubtitleToVTTWithEncoding(subPath)
				if err == nil {
					return vttPath, "extracted"
				}
				s.logger.Warnf("转换外挂字幕失败: %v", err)
			case ".vtt":
				if _, err := os.Stat(subPath); err == nil {
					return subPath, "extracted"
				}
			}
		}
	}

	return "", ""
}

// countVTTCues 统计 VTT 文件中的字幕条目数
func (s *SubtitlePreprocessService) countVTTCues(vttPath string) int {
	content, err := os.ReadFile(vttPath)
	if err != nil {
		return 0
	}
	cues := parseVTTCues(string(content))
	return len(cues)
}

// ==================== 辅助方法 ====================

func (s *SubtitlePreprocessService) isCancelled(taskID string) bool {
	_, cancelled := s.cancelJobs.Load(taskID)
	return cancelled
}

func (s *SubtitlePreprocessService) updatePhase(task *model.SubtitlePreprocessTask, phase string, progress float64, message string) {
	task.Phase = phase
	task.Progress = progress
	task.Message = message
	s.repo.Update(task)

	// 节流广播：每 3 秒最多广播一次
	now := time.Now()
	if lastTime, ok := s.lastBroadcast.Load(task.ID); ok {
		if now.Sub(lastTime.(time.Time)) < 3*time.Second {
			return
		}
	}
	s.lastBroadcast.Store(task.ID, now)
	s.broadcastEvent(EventSubPreProgress, task)
}

func (s *SubtitlePreprocessService) failTask(task *model.SubtitlePreprocessTask, errMsg string) {
	task.Status = "failed"
	task.Error = errMsg
	task.Message = errMsg
	completedAt := time.Now()
	task.CompletedAt = &completedAt
	if task.StartedAt != nil {
		task.ElapsedSec = completedAt.Sub(*task.StartedAt).Seconds()
	}
	s.repo.Update(task)

	s.broadcastEvent(EventSubPreFailed, task)
	s.logger.Warnf("字幕预处理失败: %s - %s", task.MediaTitle, errMsg)
}

func (s *SubtitlePreprocessService) completeTask(task *model.SubtitlePreprocessTask, customMsg string) {
	completedAt := time.Now()
	task.Status = "completed"
	task.Phase = "done"
	task.Progress = 100
	task.CompletedAt = &completedAt
	if task.StartedAt != nil {
		task.ElapsedSec = completedAt.Sub(*task.StartedAt).Seconds()
	}

	if customMsg != "" {
		task.Message = customMsg
	} else {
		parts := []string{fmt.Sprintf("字幕预处理完成（来源: %s", task.SubtitleSource)}
		if task.CueCount > 0 {
			parts = append(parts, fmt.Sprintf(", %d 条字幕", task.CueCount))
		}
		if task.TranslatedPaths != "" {
			translatedCount := len(strings.Split(task.TranslatedPaths, "|"))
			parts = append(parts, fmt.Sprintf(", 翻译 %d 种语言", translatedCount))
		}
		if task.FailedLangs != "" {
			parts = append(parts, fmt.Sprintf(", 失败语言: %s", task.FailedLangs))
		}
		task.Message = strings.Join(parts, "") + "）"
	}

	s.repo.Update(task)
	s.broadcastEvent(EventSubPreCompleted, task)
	s.logger.Infof("字幕预处理完成: %s, 来源: %s, 字幕数: %d, 耗时: %.1fs",
		task.MediaTitle, task.SubtitleSource, task.CueCount, task.ElapsedSec)
}

func (s *SubtitlePreprocessService) broadcastEvent(eventType string, task *model.SubtitlePreprocessTask) {
	if s.wsHub == nil {
		return
	}
	s.wsHub.BroadcastEvent(eventType, SubPreProgressData{
		TaskID:     task.ID,
		MediaID:    task.MediaID,
		MediaTitle: task.MediaTitle,
		Status:     task.Status,
		Phase:      task.Phase,
		Progress:   task.Progress,
		Message:    task.Message,
		Error:      task.Error,
	})
}

// BatchDeleteTasks 批量删除任务（跳过运行中的任务）
func (s *SubtitlePreprocessService) BatchDeleteTasks(taskIDs []string) (int64, error) {
	if len(taskIDs) == 0 {
		return 0, nil
	}
	return s.repo.DeleteByIDs(taskIDs)
}

// BatchCancelTasks 批量取消任务
func (s *SubtitlePreprocessService) BatchCancelTasks(taskIDs []string) (int, error) {
	if len(taskIDs) == 0 {
		return 0, nil
	}
	cancelled := 0
	for _, id := range taskIDs {
		if err := s.CancelTask(id); err == nil {
			cancelled++
		}
	}
	return cancelled, nil
}

// BatchRetryTasks 批量重试任务
func (s *SubtitlePreprocessService) BatchRetryTasks(taskIDs []string) (int, error) {
	if len(taskIDs) == 0 {
		return 0, nil
	}
	retried := 0
	for _, id := range taskIDs {
		if err := s.RetryTask(id); err == nil {
			retried++
		}
	}
	return retried, nil
}

// buildCleanConfig 从应用配置构建字幕清洗配置
func (s *SubtitlePreprocessService) buildCleanConfig() SubtitleCleanConfig {
	return SubtitleCleanConfig{
		AutoDetectEncoding:   true, // 始终启用编码检测
		FallbackEncoding:     s.cfg.Subtitle.SubCleanFallbackEnc,
		RemoveHTMLTags:       s.cfg.Subtitle.SubCleanRemoveHTML,
		RemoveASSStyles:      s.cfg.Subtitle.SubCleanRemoveASSStyle,
		NormalizePunctuation: s.cfg.Subtitle.SubCleanNormalizePunct,
		RemoveSDH:            s.cfg.Subtitle.SubCleanRemoveSDH,
		RemoveAds:            s.cfg.Subtitle.SubCleanRemoveAds,
		TimeOffsetMs:         s.cfg.Subtitle.SubCleanTimeOffsetMs,
		MinDurationMs:        s.cfg.Subtitle.SubCleanMinDurationMs,
		MaxDurationMs:        s.cfg.Subtitle.SubCleanMaxDurationMs,
		MinGapMs:             s.cfg.Subtitle.SubCleanMinGapMs,
		MergeShortCues:       s.cfg.Subtitle.SubCleanMergeShort,
		SplitLongCues:        s.cfg.Subtitle.SubCleanSplitLong,
		MaxCharsPerLine:      s.cfg.Subtitle.SubCleanMaxCharsPerLine,
		MaxLinesPerCue:       s.cfg.Subtitle.SubCleanMaxLinesPerCue,
		BackupOriginal:       s.cfg.Subtitle.SubCleanBackup,
	}
}

// recoverPendingTasks 恢复服务重启前未完成的任务
func (s *SubtitlePreprocessService) recoverPendingTasks() {
	time.Sleep(5 * time.Second) // 等待服务完全启动

	tasks, err := s.repo.ListPending(200)
	if err != nil {
		return
	}

	// 将之前 running 的任务重置为 pending
	running, _ := s.repo.ListRunning()
	for i := range running {
		task := &running[i]
		task.Status = "pending"
		task.Message = "服务重启后恢复..."
		s.repo.Update(task)
		tasks = append(tasks, *task)
	}

	for _, task := range tasks {
		s.enqueueTask(task.ID)
	}

	if len(tasks) > 0 {
		s.logger.Infof("恢复 %d 个未完成的字幕预处理任务", len(tasks))
	}
}

// pendingReconciler 定期扫描 pending 任务重新入队，防止因 jobQueue 容量满导致永久卡住
func (s *SubtitlePreprocessService) pendingReconciler() {
	time.Sleep(30 * time.Second)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// 队列较空时才推新任务
		if len(s.jobQueue) >= cap(s.jobQueue)/2 {
			continue
		}

		tasks, err := s.repo.ListPending(200)
		if err != nil {
			continue
		}

		enqueued := 0
		for _, task := range tasks {
			// 跳过已在队列中的
			if _, ok := s.inQueueIDs.Load(task.ID); ok {
				continue
			}
			select {
			case s.jobQueue <- task.ID:
				s.inQueueIDs.Store(task.ID, true)
				enqueued++
			default:
				return
			}
		}
		if enqueued > 0 {
			s.logger.Infof("pending reconciler 重新入队 %d 个字幕预处理任务", enqueued)
		}
	}
}
