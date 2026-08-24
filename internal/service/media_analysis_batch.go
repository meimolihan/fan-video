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

// HighlightPendingVideo 尚未拥有精彩片段的本地视频明细。
type HighlightPendingVideo struct {
	MediaID string `json:"media_id"`
	Title   string `json:"title"` // 视频标题；为空时回退为文件名
	File    string `json:"file"`  // 文件名（不含路径）
}

// GetPendingHighlightVideos 返回库内尚未生成任何精彩片段的本地视频清单，
// 口径与 GetHighlightStorageStats 的 LocalVideos-HighlightMedia 一致：
// 包含从未分析、分析失败/超时，以及启动时被过滤（不支持格式、文件缺失、
// strm 远程流）的视频。数据实时取自数据库，服务重启后依然准确。
func (s *MediaAnalysisService) GetPendingHighlightVideos() ([]HighlightPendingVideo, error) {
	videos, err := s.mediaRepo.ListAllLocalVideos()
	if err != nil {
		return nil, err
	}
	highlighted, err := s.highlightRepo.ListAllMediaIDs()
	if err != nil {
		return nil, err
	}
	hasHighlight := make(map[string]bool, len(highlighted))
	for _, id := range highlighted {
		hasHighlight[id] = true
	}
	pending := make([]HighlightPendingVideo, 0)
	for _, m := range videos {
		if hasHighlight[m.ID] {
			continue
		}
		file := filepath.Base(m.FilePath)
		title := m.Title
		if title == "" {
			title = file
		}
		pending = append(pending, HighlightPendingVideo{MediaID: m.ID, Title: title, File: file})
	}
	return pending, nil
}

// HighlightAuditItem 完整性检查发现的单个问题媒体。
type HighlightAuditItem struct {
	MediaID    string `json:"media_id"`
	Title      string `json:"title"`
	File       string `json:"file"`       // 源视频文件名（不含路径）；媒体记录缺失时为空
	Highlights int    `json:"highlights"` // 该媒体的片段条数
	Detail     string `json:"detail"`     // 具体说明（缺失的文件等）
}

// HighlightAuditReport 全库片段完整性检查报告。
type HighlightAuditReport struct {
	TotalVideos    int                  `json:"total_videos"`    // 库内本地视频总数（含尚未生成片段的）
	WithHighlights int                  `json:"with_highlights"` // 已生成片段、纳入本次完整性检查的媒体数
	SourceMissing  []HighlightAuditItem `json:"source_missing"`  // 源视频已删除/媒体记录不存在
	AssetsMissing  []HighlightAuditItem `json:"assets_missing"` // 片段缩略图/预览文件缺失
	OrphanCaches   []HighlightAuditItem `json:"orphan_caches"`  // 磁盘缓存目录无对应片段记录（失败残留/媒体已删除）
}

// GetHighlightAudit 检查全库已生成片段的完整性：
// 1) 源视频文件是否仍存在（strm/远程流跳过本地检查）；
// 2) 每条片段引用的缩略图/预览产物文件是否仍在磁盘上；
// 3) 反向扫描磁盘缓存目录，找出没有任何片段记录的孤儿目录。
// 检查对象是"拥有片段"的媒体；尚未生成片段的视频不在范围内，
// 通过 TotalVideos-WithHighlights 可得缺口数（与覆盖统计口径一致）。
// 只读不改库；修复请调用 CleanBrokenHighlights。
func (s *MediaAnalysisService) GetHighlightAudit() (*HighlightAuditReport, error) {
	report := &HighlightAuditReport{
		SourceMissing: make([]HighlightAuditItem, 0),
		AssetsMissing: make([]HighlightAuditItem, 0),
		OrphanCaches:  make([]HighlightAuditItem, 0),
	}
	videos, err := s.mediaRepo.ListAllLocalVideos()
	if err != nil {
		return nil, err
	}
	report.TotalVideos = len(videos)
	mediaIDs, err := s.highlightRepo.ListAllMediaIDs()
	if err != nil {
		return nil, err
	}
	report.WithHighlights = len(mediaIDs)

	for _, id := range mediaIDs {
		media, mediaErr := s.mediaRepo.FindByID(id)
		highlights, hlErr := s.highlightRepo.ListByMediaID(id)
		if hlErr != nil {
			// 读取片段列表失败：本轮跳过该媒体，避免误报
			continue
		}
		count := len(highlights)

		if mediaErr != nil {
			// 片段还在但媒体行已被删除：孤儿片段，归入源缺失
			report.SourceMissing = append(report.SourceMissing, HighlightAuditItem{
				MediaID: id, Highlights: count,
				Detail: "媒体记录不存在（孤儿片段）",
			})
			continue
		}

		file := filepath.Base(media.FilePath)
		title := media.Title
		if title == "" {
			title = file
		}

		// 本地视频才检查源文件；strm/远程流没有本地路径概念
		if media.StreamURL == "" && !strings.EqualFold(filepath.Ext(media.FilePath), ".strm") {
			if info, statErr := os.Stat(media.FilePath); statErr != nil || info.IsDir() {
				detail := "源视频文件不存在或不可访问"
				if statErr != nil {
					detail = "源视频文件不存在或不可访问: " + statErr.Error()
				}
				report.SourceMissing = append(report.SourceMissing, HighlightAuditItem{
					MediaID: id, Title: title, File: file, Highlights: count, Detail: detail,
				})
				continue
			}
		}

		// 产物完整性：缩略图/动态预览任一引用路径缺失即视为不完整
		missing := make([]string, 0)
		for _, h := range highlights {
			for _, p := range []string{h.Thumbnail, h.PreviewPath, h.GifPath} {
				if p == "" {
					continue
				}
				if _, statErr := os.Stat(p); statErr != nil {
					missing = append(missing, filepath.Base(p))
				}
			}
		}
		if len(missing) > 0 {
			detail := fmt.Sprintf("缺失 %d 个产物文件: %s", len(missing), strings.Join(missing, ", "))
			report.AssetsMissing = append(report.AssetsMissing, HighlightAuditItem{
				MediaID: id, Title: title, File: file, Highlights: count, Detail: detail,
			})
		}
	}

	// 反向扫描：磁盘缓存目录里存在、但数据库没有任何片段记录的孤儿目录。
	// 典型成因：分析失败/超时中断后的残留产物、媒体已删除但缓存未清。
	cacheRoot := filepath.Join(s.cfg.Cache.CacheDir, "media-analysis")
	entries, dirErr := os.ReadDir(cacheRoot)
	if dirErr != nil {
		if !errors.Is(dirErr, os.ErrNotExist) {
			s.logger.Warnf("scan highlight cache dir %s: %v", cacheRoot, dirErr)
		}
	} else {
		highlightSet := make(map[string]bool, len(mediaIDs))
		for _, id := range mediaIDs {
			highlightSet[id] = true
		}
		localSet := make(map[string]bool, len(videos))
		for _, m := range videos {
			localSet[m.ID] = true
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			id := entry.Name()
			if highlightSet[id] {
				continue
			}
			item := HighlightAuditItem{MediaID: id}
			if localSet[id] {
				item.Detail = "缓存目录残留：该视频在库中但没有片段记录（分析失败/中断）"
			} else {
				item.Detail = "缓存目录残留：对应媒体已不在库中"
			}
			report.OrphanCaches = append(report.OrphanCaches, item)
		}
	}
	return report, nil
}

// CleanBrokenHighlights 删除完整性检查发现的问题片段记录与产物：
// 源视频已丢失的、孤儿缓存目录的始终清理；includeAssetIssues 为 true 时
// 同时清理产物缺失的，下次「一键生成」会对其中仍有源文件的媒体自动补齐。
// 有正在进行的分析任务的媒体会被跳过，避免误删写到一半的产物。
// 返回成功清理的媒体数。
func (s *MediaAnalysisService) CleanBrokenHighlights(includeAssetIssues bool) (int, error) {
	report, err := s.GetHighlightAudit()
	if err != nil {
		return 0, err
	}
	targets := make([]HighlightAuditItem, 0, len(report.SourceMissing)+len(report.AssetsMissing)+len(report.OrphanCaches))
	targets = append(targets, report.SourceMissing...)
	targets = append(targets, report.OrphanCaches...)
	if includeAssetIssues {
		targets = append(targets, report.AssetsMissing...)
	}
	cleaned := 0
	for _, item := range targets {
		// 安全护栏：该媒体仍有进行中的分析任务时跳过，避免删除在写一半的产物
		if active, activeErr := s.taskRepo.FindActiveByMediaAndType(item.MediaID, mediaHighlightTaskType); activeErr == nil && active != nil {
			s.logger.Infof("clean broken highlights: skip media=%s (analysis task active)", item.MediaID)
			continue
		}
		if err := s.DeleteHighlights(item.MediaID); err != nil {
			s.logger.Warnf("clean broken highlights media=%s: %v", item.MediaID, err)
			continue
		}
		cleaned++
	}
	s.logger.Infof("clean broken highlights: total=%d with_highlights=%d cleaned=%d source_missing=%d orphan_caches=%d assets_missing=%d",
		report.TotalVideos, report.WithHighlights, cleaned, len(report.SourceMissing), len(report.OrphanCaches), len(report.AssetsMissing))
	return cleaned, nil
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
