package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fan-video/fan-video/internal/model"
)

// batchHighlightPollInterval 批处理轮询单媒体任务状态的间隔。
const batchHighlightPollInterval = 800 * time.Millisecond

// batchHighlightPerMediaTimeout 单个视频的分析超时上限。
const batchHighlightPerMediaTimeout = 20 * time.Minute

// ErrBatchNotFound 表示当前没有批量任务。
var ErrBatchNotFound = errors.New("当前没有正在进行的批量任务")

// BatchHighlightStatus 批量生成精彩片段的全局进度快照。
type BatchHighlightStatus struct {
	Running       bool       `json:"running"`
	StopRequested bool       `json:"stop_requested"`
	Total         int        `json:"total"`
	Processed     int        `json:"processed"` // 本轮成功生成
	Skipped       int        `json:"skipped"`   // 已有片段/不支持/文件缺失
	Discarded     int        `json:"discarded"` // 停止时被放弃并删除结果的视频
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
	stopRequested   bool
	total           int
	processed       int
	skipped         int
	discarded       int
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
	done := st.processed + st.skipped + st.discarded + st.failed
	return BatchHighlightStatus{
		Running:         st.running,
		StopRequested:   st.stopRequested,
		Total:           st.total,
		Processed:       st.processed,
		Skipped:         st.skipped,
		Discarded:       st.discarded,
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
// 已有精彩片段的视频自动跳过；如需全部重新生成请先调用 ClearAllHighlights。
func (s *MediaAnalysisService) StartBatchHighlights() (BatchHighlightStatus, error) {
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
	s.batch.stopRequested = false
	s.batch.total = len(ids)
	s.batch.processed = 0
	s.batch.skipped = 0
	s.batch.discarded = 0
	s.batch.failed = 0
	s.batch.currentMediaID = ""
	s.batch.currentTitle = ""
	s.batch.currentProgress = 0
	s.batch.startedAt = now
	s.batch.finishedAt = nil
	s.batch.mu.Unlock()

	go s.runBatchHighlights(ids)
	return s.snapshotBatch(), nil
}

func (s *MediaAnalysisService) runBatchHighlights(videos []model.Media) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Errorf("batch highlights panic: %v", r)
			s.finishBatch()
		}
	}()
	for _, m := range videos {
		s.batch.mu.Lock()
		if s.batch.stopRequested {
			s.batch.mu.Unlock()
			break
		}
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
		case "discarded":
			s.batch.discarded++
		default:
			s.batch.failed++
		}
		s.batch.currentProgress = 100
		s.batch.mu.Unlock()
	}
	s.finishBatch()
	snap := s.snapshotBatch()
	s.logger.Infof("batch highlights finished total=%d processed=%d skipped=%d discarded=%d failed=%d",
		snap.Total, snap.Processed, snap.Skipped, snap.Discarded, snap.Failed)
}

func (s *MediaAnalysisService) finishBatch() {
	s.batch.mu.Lock()
	defer s.batch.mu.Unlock()
	now := time.Now()
	s.batch.running = false
	s.batch.stopRequested = false
	s.batch.currentProgress = 0
	s.batch.finishedAt = &now
}

// isStopRequested 返回是否已请求停止批量任务。
func (s *MediaAnalysisService) isStopRequested() bool {
	s.batch.mu.Lock()
	defer s.batch.mu.Unlock()
	return s.batch.stopRequested
}

// processOneBatchMedia 调度单个视频的精彩片段分析并等待其终态。
// 返回 "completed" / "skipped" / "discarded" / "failed"。
// 等待期间收到停止请求时视为「放弃当前视频」：等任务到终态后删除该视频
// 已持久化的片段记录与产物文件（用户要求：停止后不保留未生成完毕的结果），
// 返回 "discarded" 单独计数，不计入已生成。
func (s *MediaAnalysisService) processOneBatchMedia(mediaID string) string {
	abandoned := false
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
		if s.isStopRequested() {
			abandoned = true
		}

		current, err := s.taskRepo.FindByID(task.ID)
		if err != nil {
			return "failed"
		}
		s.batch.mu.Lock()
		s.batch.currentProgress = current.Progress
		s.batch.mu.Unlock()

		// 停止时不立刻返回：必须等当前视频任务到达终态，
		// 否则其分析协程在删除之后仍可能把结果写回数据库。
		switch current.Status {
		case "completed":
			if abandoned {
				// 用户要求：确认停止后，未生成完毕（含刚完成但未及保留）的结果一律丢弃
				if delErr := s.DeleteHighlights(mediaID); delErr != nil {
					s.logger.Warnf("discard highlights for stopped media %s: %v", mediaID, delErr)
				}
				return "discarded"
			}
			return "completed"
		case "failed", "interrupted":
			if abandoned {
				return "discarded"
			}
			return "failed"
		}
		if time.Now().After(deadline) {
			return "failed"
		}
		time.Sleep(batchHighlightPollInterval)
	}
}

// StopBatchHighlights 请求停止批量任务（当前视频会继续完成到安全点）。
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
