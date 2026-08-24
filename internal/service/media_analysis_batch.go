package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fan-video/fan-video/internal/model"
)

// 批量生成模式。均衡模式完全保持旧行为（一次一部）；
// 性能模式允许多部影片并行分析，吞吐更高但资源占用更大。
const (
	BatchHighlightModeBalanced    = "balanced"
	BatchHighlightModePerformance = "performance"

	// batchHighlightMaxPerformanceWorkers 性能模式的最大并行影片数，
	// 适配 4 核级 NAS 配额，避免把转码/播放在线体验挤死。
	batchHighlightMaxPerformanceWorkers = 3
)

// NormalizeBatchHighlightMode 归一化模式参数：空值/未知值一律回退均衡模式。
func NormalizeBatchHighlightMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case BatchHighlightModePerformance:
		return BatchHighlightModePerformance
	default:
		return BatchHighlightModeBalanced
	}
}

// batchHighlightWorkers 计算指定模式的并行影片数（同时分析的电影上限）。
func batchHighlightWorkers(mode string) int {
	if mode != BatchHighlightModePerformance {
		return 1
	}
	n := runtime.NumCPU() / 2
	if n < 2 {
		n = 2
	}
	if n > batchHighlightMaxPerformanceWorkers {
		n = batchHighlightMaxPerformanceWorkers
	}
	return n
}

// batchHighlightPollInterval 批处理轮询单媒体任务状态的间隔。
const batchHighlightPollInterval = 800 * time.Millisecond

// batchHighlightPerMediaTimeout 单个视频的分析超时上限。
const batchHighlightPerMediaTimeout = 20 * time.Minute

// ErrBatchNotFound 表示当前没有批量任务。
var ErrBatchNotFound = errors.New("当前没有正在进行的批量任务")

// BatchHighlightStatus 批量生成精彩片段的全局进度快照。
type BatchHighlightStatus struct {
	Running       bool       `json:"running"`
	Mode          string     `json:"mode"`        // balanced / performance（最近一次启动所用模式）
	Parallelism   int        `json:"parallelism"` // 本模式的并行影片数
	StopRequested bool       `json:"stop_requested"`
	Total         int        `json:"total"`
	Processed     int        `json:"processed"` // 本轮成功生成（含停止时保存的当前任务）
	Skipped       int        `json:"skipped"`   // 已有片段/不支持/文件缺失
	Failed        int        `json:"failed"`
	Remaining     int        `json:"remaining"`
	CurrentMediaID  string     `json:"current_media_id"`
	CurrentTitle    string     `json:"current_title"`
	CurrentProgress float64    `json:"current_progress"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
}

// batchHighlightState 服务内保存的批量任务可变状态（batchMu 保护）。
type batchHighlightState struct {
	mu              sync.Mutex
	running         bool
	mode            string
	parallelism     int
	stopRequested   bool
	total           int
	processed       int
	skipped         int
	failed          int
	currentMediaID  string
	currentTitle    string
	currentProgress float64
	startedAt       time.Time
	finishedAt      *time.Time
}

func (s *MediaAnalysisService) snapshotBatch() BatchHighlightStatus {
	st := &s.batch
	st.mu.Lock()
	defer st.mu.Unlock()
	done := st.processed + st.skipped + st.failed
	return BatchHighlightStatus{
		Running:         st.running,
		Mode:            st.mode,
		Parallelism:     st.parallelism,
		StopRequested:   st.stopRequested,
		Total:           st.total,
		Processed:       st.processed,
		Skipped:         st.skipped,
		Failed:          st.failed,
		Remaining:       st.total - done,
		CurrentMediaID:  st.currentMediaID,
		CurrentTitle:    st.currentTitle,
		CurrentProgress: st.currentProgress,
		StartedAt:       st.startedAt,
		FinishedAt:      st.finishedAt,
	}
}

// SnapshotBatchHighlights 返回批量任务当前进度快照（供 handler 使用）。
func (s *MediaAnalysisService) SnapshotBatchHighlights() BatchHighlightStatus {
	return s.snapshotBatch()
}

// StartBatchHighlights 启动「全库视频批量生成精彩片段」。
// mode 为 balanced（均衡：一次一部，旧行为）或 performance（性能：多部并行）。
// 已有精彩片段的视频自动跳过；如需全部重新生成请先调用 ClearAllHighlights。
func (s *MediaAnalysisService) StartBatchHighlights(mode string) (BatchHighlightStatus, error) {
	mode = NormalizeBatchHighlightMode(mode)
	workers := batchHighlightWorkers(mode)

	s.batch.mu.Lock()
	if s.batch.running {
		s.batch.mu.Unlock()
		return s.snapshotBatch(), ErrMediaAnalysisInProgress
	}

	videos, err := s.mediaRepo.ListAllLocalVideos()
	if err != nil {
		s.batch.mu.Unlock()
		return s.snapshotBatch(), err
	}
	ids := make([]model.Media, 0, len(videos))
	for _, m := range videos {
		if m.StreamURL != "" {
			continue
		}
		if _, err := os.Stat(m.FilePath); err != nil {
			continue
		}
		ext := filepath.Ext(m.FilePath)
		if !supportedExts[ext] {
			continue
		}
		ids = append(ids, m)
	}

	now := time.Now()
	s.batch.running = true
	s.batch.mode = mode
	s.batch.parallelism = workers
	s.batch.stopRequested = false
	s.batch.total = len(ids)
	s.batch.processed = 0
	s.batch.skipped = 0
	s.batch.failed = 0
	s.batch.currentMediaID = ""
	s.batch.currentTitle = ""
	s.batch.currentProgress = 0
	s.batch.startedAt = now
	s.batch.finishedAt = nil
	s.batch.mu.Unlock()

	// 并行度与全局分析信号量同步放大；结束时 finishBatch 恢复为 1。
	// 必须在派发任务前设置，保证 worker 池能真正并发拿到槽位。
	s.setAnalysisCapacity(workers)

	go s.runBatchHighlights(ids, workers)
	return s.snapshotBatch(), nil
}

func (s *MediaAnalysisService) runBatchHighlights(videos []model.Media, workers int) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Errorf("batch highlights panic: %v", r)
			s.finishBatch()
		}
	}()

	var wg sync.WaitGroup
	jobs := make(chan model.Media)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range jobs {
				// 收到停止请求后不再领取新视频；已在处理中的视频会
				// 照常分析到完成并保留结果（processOneBatchMedia 不感知停止）。
				if s.isStopRequested() {
					continue
				}

				s.batch.mu.Lock()
				s.batch.currentMediaID = m.ID
				s.batch.currentTitle = m.Title
				s.batch.currentProgress = 0
				s.batch.mu.Unlock()

				done := s.processOneBatchMedia(m.ID)

				s.batch.mu.Lock()
				switch done {
				case "completed":
					s.batch.processed++
				case "skipped":
					s.batch.skipped++
				default:
					s.batch.failed++
				}
				if s.batch.currentMediaID == m.ID {
					s.batch.currentProgress = 100
				}
				s.batch.mu.Unlock()
			}
		}()
	}
	for _, m := range videos {
		if s.isStopRequested() {
			break
		}
		jobs <- m
	}
	close(jobs)
	wg.Wait()

	s.finishBatch()
	snap := s.snapshotBatch()
	s.logger.Infof("batch highlights finished mode=%s workers=%d total=%d processed=%d skipped=%d failed=%d",
		snap.Mode, workers, snap.Total, snap.Processed, snap.Skipped, snap.Failed)
}

func (s *MediaAnalysisService) finishBatch() {
	s.batch.mu.Lock()
	now := time.Now()
	s.batch.running = false
	s.batch.stopRequested = false
	s.batch.currentProgress = 0
	s.batch.finishedAt = &now
	s.batch.mu.Unlock()
	// 恢复 NAS 安全默认值：一次一部。在途任务按原 channel 归还槽位，安全。
	s.setAnalysisCapacity(1)
}

// isStopRequested 返回是否已请求停止批量任务。
func (s *MediaAnalysisService) isStopRequested() bool {
	s.batch.mu.Lock()
	defer s.batch.mu.Unlock()
	return s.batch.stopRequested
}

// processOneBatchMedia 调度单个视频的精彩片段分析并等待其终态。
// 返回 "completed" / "skipped" / "failed"。
// 停止请求不改变单个视频的处理结果：正在分析的视频会照常完成并保留
// 片段（用户要求：停止时保存当前任务），只是不再领取后续任务。
func (s *MediaAnalysisService) processOneBatchMedia(mediaID string) string {
	// 已有片段 → 跳过（重新生成需先清空）
	if existing, err := s.highlightRepo.ListByMediaID(mediaID); err == nil && len(existing) > 0 {
		return "skipped"
	}

	task, err := s.AnalyzeHighlights(mediaID)
	if err != nil {
		if errors.Is(err, ErrMediaAnalysisInProgress) {
			// 已有活动任务：接管等待它结束
			active, findErr := s.taskRepo.FindActiveByMediaAndType(mediaID, mediaHighlightTaskType)
			if findErr != nil {
				return "failed"
			}
			task = active
		} else if errors.Is(err, ErrMediaNotFound) || errors.Is(err, ErrMediaAnalysisUnsupported) {
			return "skipped"
		} else {
			return "failed"
		}
	}

	deadline := time.Now().Add(batchHighlightPerMediaTimeout)
	for {
		current, err := s.taskRepo.FindByID(task.ID)
		if err != nil {
			return "failed"
		}
		s.batch.mu.Lock()
		s.batch.currentProgress = current.Progress
		s.batch.mu.Unlock()

		switch current.Status {
		case "completed":
			return "completed"
		case "failed", "interrupted":
			return "failed"
		}
		if time.Now().After(deadline) {
			return "failed"
		}
		time.Sleep(batchHighlightPollInterval)
	}
}

// StopBatchHighlights 请求停止批量任务。
// 停止语义：剩余视频不再处理；正在分析的视频照常完成并保留结果。
func (s *MediaAnalysisService) StopBatchHighlights() (BatchHighlightStatus, error) {
	s.batch.mu.Lock()
	if !s.batch.running {
		s.batch.mu.Unlock()
		return s.snapshotBatch(), ErrBatchNotFound
	}
	s.batch.stopRequested = true
	s.batch.mu.Unlock()
	return s.snapshotBatch(), nil
}

// HighlightStorageStats 暴露精彩片段相关的数据库真值，用于前端诊断显示。
type HighlightStorageStats struct {
	HighlightRows  int64 `json:"highlight_rows"`
	HighlightMedia int64 `json:"highlight_media"`
	LocalVideos    int64 `json:"local_videos"`
	HighlightTasks int64 `json:"highlight_tasks"`
}

// GetHighlightStorageStats 返回片段存储统计（video_highlights 表行数/媒体数、本地视频数、分析任务数）。
func (s *MediaAnalysisService) GetHighlightStorageStats() (*HighlightStorageStats, error) {
	stats := &HighlightStorageStats{}
	var err error
	if stats.HighlightRows, err = s.highlightRepo.CountAll(); err != nil {
		return nil, err
	}
	mediaIDs, err := s.highlightRepo.ListAllMediaIDs()
	if err != nil {
		return nil, err
	}
	stats.HighlightMedia = int64(len(mediaIDs))
	videos, err := s.mediaRepo.ListAllLocalVideos()
	if err != nil {
		return nil, err
	}
	stats.LocalVideos = int64(len(videos))
	if stats.HighlightTasks, err = s.taskRepo.CountByType(mediaHighlightTaskType); err != nil {
		return nil, err
	}
	return stats, nil
}

// ClearAllHighlights 清空全库所有精彩片段记录与产物文件。返回受影响的媒体数与删除的片段数。
func (s *MediaAnalysisService) ClearAllHighlights() (mediaCount int, highlightCount int64, err error) {
	count, err := s.highlightRepo.CountAll()
	if err != nil {
		return 0, 0, err
	}
	mediaIDs, err := s.highlightRepo.ListAllMediaIDs()
	if err != nil {
		return 0, count, err
	}
	failed := 0
	for _, mediaID := range mediaIDs {
		if delErr := s.DeleteHighlights(mediaID); delErr != nil {
			failed++
			s.logger.Warnf("clear highlights for media %s: %v", mediaID, delErr)
		}
	}
	s.logger.Infof("clear all highlights: media=%d highlights=%d failed=%d", len(mediaIDs), count, failed)
	// 同步清掉已导出的片段 mp4 文件（<DataDir>/exports/highlights 整目录）
	if exportRoot := filepath.Join(s.cfg.App.DataDir, "exports", "highlights"); exportRoot != "" {
		if rmErr := os.RemoveAll(exportRoot); rmErr != nil {
			s.logger.Warnf("remove exported highlight clips: %v", rmErr)
		}
	}
	if failed > 0 {
		return len(mediaIDs), count, fmt.Errorf("%d 个视频的片段清理失败，请查看服务端日志", failed)
	}
	return len(mediaIDs), count, nil
}
