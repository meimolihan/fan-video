package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fan-video/fan-video/internal/model"
)

// 本地精彩片段生成（项目内置，替代脚本 fan-video_highlight.sh 的 OUTPUT=timeline 模式）。
//
// 目录规则：
//   - 一个视频独占一个目录（该目录内仅此一个视频）→ 触发生成；
//   - 目录下存在多个视频 → 报错（含目录与文件清单）跳过，继续处理后续目录。
//
// 每部符合条件视频在其目录 .highlights/ 下生成：
//   - <视频茎>_<NN>_<标题>.json          时间线侧车（title/start_time/end_time/score）
//   - <视频茎>_<NN>_<标题>.thumb.webp|.jpg  从原视频对应时间点截取的缩略图
//
// 生成完成后立即把侧车导入数据库（Source=manual），媒体详情页直接可见、可播放。
// 删除动作会一并清理磁盘文件与对应 manual 记录，回到可重新生成的状态。

const (
	localHLMaxClips     = 8  // 每部视频最多分段数
	localHLSecPerClip   = 30 // 每段基准秒数
	localHLThumbWidth   = 720
	localHLThumbQuality = 80
	localHLThumbTime    = 3 // 缩略图相对片段起点的偏移秒数
	localHLScore        = 8.5
	localHLThumbTimeout = 30 * time.Second
	localHLRetryCooldown = 24 * time.Hour // 生成失败后的自动重试冷却期
)

// ErrLocalHighlightGenInProgress 表示本地精彩片段生成任务正在进行中。
var ErrLocalHighlightGenInProgress = errors.New("本地精彩片段生成任务已在进行中")

// ErrNoLocalHighlightJob 表示当前没有正在运行的本地精彩片段生成任务。
var ErrNoLocalHighlightJob = errors.New("当前没有正在进行的本地精彩片段生成任务")

// localHLTitle 依据片段在整片中的相对位置命名（与脚本一致）。
func localHLTitle(i, n int) string {
	r := (float64(i) + 0.5) * 100 / float64(n)
	switch {
	case r < 12:
		return "开场高能"
	case r > 85:
		return "结局高潮"
	case r > 62:
		return "后期转折"
	case r >= 50:
		return "中点高潮"
	case r < 32:
		return "前期精彩"
	default:
		return "精彩片段"
	}
}

// localHighlightSegment 单个计划片段。
type localHighlightSegment struct {
	Index    int
	Title    string
	Start    float64
	End      float64 // Start + 段长（秒）
	ThumbSec float64
}

// planLocalHighlightSegments 与脚本一致的切分算法。
func planLocalHighlightSegments(duration float64) []localHighlightSegment {
	dur := math.Floor(duration)
	n := 1
	clip := duration
	if dur >= 15 {
		n = int(dur)/localHLSecPerClip + 1
		if n > localHLMaxClips {
			n = localHLMaxClips
		}
		clip = math.Floor(dur / float64(n))
		if clip > localHLSecPerClip {
			clip = float64(localHLSecPerClip)
		}
	}
	segments := make([]localHighlightSegment, 0, n)
	for i := 0; i < n; i++ {
		title := localHLTitle(i, n)
		var start float64
		if dur < 15 {
			title = "完整片段"
		} else {
			start = (float64(i)+0.5)/float64(n)*duration - clip/2
			if start < 0 {
				start = 0
			}
			if m := duration - clip; start > m {
				start = math.Max(m, 0)
			}
		}
		startR := math.Round(start)
		thumb := startR + localHLThumbTime
		if max := duration - 1; thumb > max {
			thumb = math.Max(max, 0)
		}
		segments = append(segments, localHighlightSegment{
			Index:    i + 1,
			Title:    title,
			Start:    startR,
			End:      startR + clip,
			ThumbSec: thumb,
		})
	}
	return segments
}

// ==================== 目录扫描（刷新） ====================

// LocalHighlightDirItem 符合条件（单视频目录）的目录项。
type LocalHighlightDirItem struct {
	Dir       string `json:"dir"`
	VideoFile string `json:"video_file"`
	Title     string `json:"title"`
	Duration  int    `json:"duration"` // 秒
	Clips     int    `json:"clips"`    // 计划片段数
	Generated bool   `json:"generated"`
	Status    string `json:"status"` // pending / failed
	MediaID   string `json:"media_id"`
}

// LocalHighlightSkipItem 跳过的目录及其原因。
type LocalHighlightSkipItem struct {
	Dir    string   `json:"dir"`
	Videos []string `json:"videos"`
	Reason string   `json:"reason"`
}

// LocalHighlightScanReport 目录扫描报告。
type LocalHighlightScanReport struct {
	TotalDirs    int                      `json:"total_dirs"`    // 含至少一个视频的目录总数
	EligibleDirs int                      `json:"eligible_dirs"` // 单视频目录
	AlreadyDirs  int                      `json:"already_dirs"`  // 已生成完整时间线侧车
	SkippedDirs  int                      `json:"skipped_dirs"`  // 多视频/不可用目录
	FullScan     bool                     `json:"full_scan"`     // 是否为全量（逐片 ffprobe）模式
	Eligible     []LocalHighlightDirItem  `json:"eligible,omitempty"`
	Skipped      []LocalHighlightSkipItem `json:"skipped,omitempty"`
}

// partitionLocalHighlightDirs 按父目录把视频分组，返回「单视频目录（可生成）」与
// 「多视频目录（应跳过并附原因）」两部分。
func partitionLocalHighlightDirs(videos []model.Media) (eligible []model.Media, skipped []LocalHighlightSkipItem) {
	byDir := make(map[string][]model.Media)
	for _, m := range videos {
		dir := filepath.Dir(m.FilePath)
		byDir[dir] = append(byDir[dir], m)
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		list := byDir[dir]
		if len(list) > 1 {
			files := make([]string, 0, len(list))
			for _, m := range list {
				files = append(files, filepath.Base(m.FilePath))
			}
			sort.Strings(files)
			skipped = append(skipped, LocalHighlightSkipItem{
				Dir: dir, Videos: files,
				Reason: fmt.Sprintf("目录下存在 %d 个视频文件，请将每个视频放在独立目录：%s", len(files), strings.Join(files, "、")),
			})
			continue
		}
		eligible = append(eligible, list[0])
	}
	return eligible, skipped
}

// localHighlightMediaUsable 判断媒体是否可参与本地生成：本地文件、受支持格式、文件存在。
func localHighlightMediaUsable(m model.Media) bool {
	if m.StreamURL != "" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(m.FilePath))
	if ext == ".strm" || !supportedExts[ext] {
		return false
	}
	info, err := os.Stat(m.FilePath)
	return err == nil && !info.IsDir()
}

// localHLGenerated 判断某视频的 .highlights/ 下是否已存在以 <视频茎>_ 命名的完整侧车集合。
func localHLGenerated(dir, stem string, segments []localHighlightSegment) bool {
	hlDir := filepath.Join(dir, ".highlights")
	for _, seg := range segments {
		base := fmt.Sprintf("%s_%02d_%s", stem, seg.Index, seg.Title)
		jsonOK := fileExists(filepath.Join(hlDir, base+".json"))
		if !jsonOK {
			return false
		}
		thumbOK := false
		for _, suffix := range []string{".thumb.webp", ".thumb.jpg"} {
			if fileExists(filepath.Join(hlDir, base+suffix)) {
				thumbOK = true
				break
			}
		}
		if !thumbOK {
			return false
		}
	}
	return true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ScanLocalHighlightDirs 扫描目录归属。full=true 走全量模式（对每个单视频目录逐片
// ffprobe 校验生成状态，最准确但最慢）；full=false（默认，增量）仅用数据库状态聚合
// 计数：明细只列出待生成/失败的目录，已生成的影片不再逐片探测。
func (s *MediaAnalysisService) ScanLocalHighlightDirs(full bool) (*LocalHighlightScanReport, error) {
	if full {
		return s.scanLocalHighlightDirsFull()
	}
	return s.scanLocalHighlightDirsIncremental()
}

// scanLocalHighlightDirsIncremental 增量扫描：不触发任何 ffprobe。
//   - 计数来自数据库状态聚合 + 单视频目录分组（一次轻量 SELECT，无子进程）；
//   - 「已生成」且带时长缓存的影片做纯 os.Stat 校验，产物缺失自动重置为 pending
//     （修复旧版「删除片段文件后仍报已生成、无法重新生成」的缺陷）；
//   - 「待生成/失败」列出明细（时长优先取缓存，未缓存则不探测、显示 0）。
func (s *MediaAnalysisService) scanLocalHighlightDirsIncremental() (*LocalHighlightScanReport, error) {
	videos, err := s.mediaRepo.ListAllLocalVideos()
	if err != nil {
		return nil, err
	}
	report := &LocalHighlightScanReport{FullScan: false}
	eligible, skipped := partitionLocalHighlightDirs(videos)
	report.Skipped = skipped

	usable := make([]model.Media, 0, len(eligible))
	for _, m := range eligible {
		if localHighlightMediaUsable(m) {
			usable = append(usable, m)
			continue
		}
		reason := "视频文件不存在或不可访问"
		ext := strings.ToLower(filepath.Ext(m.FilePath))
		if m.StreamURL != "" || ext == ".strm" || !supportedExts[ext] {
			reason = "远程流（strm）或不支持的格式，无法本地生成"
		}
		report.Skipped = append(report.Skipped, LocalHighlightSkipItem{
			Dir: filepath.Dir(m.FilePath), Videos: []string{filepath.Base(m.FilePath)}, Reason: reason,
		})
	}

	report.Eligible = make([]LocalHighlightDirItem, 0, len(usable))
	for _, m := range usable {
		dir := filepath.Dir(m.FilePath)
		title := m.Title
		if title == "" {
			title = filepath.Base(m.FilePath)
		}
		duration := m.LocalHLDuration
		item := LocalHighlightDirItem{
			Dir: dir, VideoFile: filepath.Base(m.FilePath), Title: title, MediaID: m.ID,
			Status: m.LocalHLStatus,
		}
		if duration > 0 {
			item.Duration = int(math.Floor(duration))
			item.Clips = len(planLocalHighlightSegments(duration))
		}
		switch m.LocalHLStatus {
		case model.LocalHLStatusGenerated:
			// 时长缓存缺失（如迁移回填）时信任状态，不触发探测，避免升级后全库慢扫描。
			if duration <= 0 || localHLGenerated(dir, mediaStem(m.FilePath), planLocalHighlightSegments(duration)) {
				report.AlreadyDirs++
				continue
			}
			// 侧车/缩略图被手动删除 → 回写 pending，下一次生成即补齐
			if err := s.mediaRepo.MarkLocalHLPending(m.ID); err != nil {
				s.logger.Warnf("local highlight scan 重置状态 %s: %v", m.ID, err)
			}
			report.Eligible = append(report.Eligible, item)
		default: // pending / failed
			report.Eligible = append(report.Eligible, item)
		}
	}
	report.EligibleDirs = len(report.Eligible) + report.AlreadyDirs
	report.SkippedDirs = len(report.Skipped)
	report.TotalDirs = report.EligibleDirs + report.SkippedDirs
	return report, nil
}

// scanLocalHighlightDirsFull 全量扫描：对每个单视频目录 ffprobe 探测时长并校验产物。
// 仅显式全量校验/升级排障时使用，999 部影片可能出现分钟级耗时。
func (s *MediaAnalysisService) scanLocalHighlightDirsFull() (*LocalHighlightScanReport, error) {
	videos, err := s.mediaRepo.ListAllLocalVideos()
	if err != nil {
		return nil, err
	}
	report := &LocalHighlightScanReport{FullScan: true}
	eligible, skipped := partitionLocalHighlightDirs(videos)
	report.Skipped = skipped

	usable := make([]model.Media, 0, len(eligible))
	for _, m := range eligible {
		if localHighlightMediaUsable(m) {
			usable = append(usable, m)
			continue
		}
		reason := "视频文件不存在或不可访问"
		ext := strings.ToLower(filepath.Ext(m.FilePath))
		if m.StreamURL != "" || ext == ".strm" || !supportedExts[ext] {
			reason = "远程流（strm）或不支持的格式，无法本地生成"
		}
		report.Skipped = append(report.Skipped, LocalHighlightSkipItem{
			Dir: filepath.Dir(m.FilePath), Videos: []string{filepath.Base(m.FilePath)}, Reason: reason,
		})
	}

	report.Eligible = make([]LocalHighlightDirItem, 0, len(usable))
	for _, m := range usable {
		dir := filepath.Dir(m.FilePath)
		title := m.Title
		if title == "" {
			title = filepath.Base(m.FilePath)
		}
		duration, probeErr := probeFileDuration(s.cfg.App.FFprobePath, m.FilePath)
		item := LocalHighlightDirItem{
			Dir: dir, VideoFile: filepath.Base(m.FilePath), Title: title, MediaID: m.ID,
			Status: m.LocalHLStatus,
		}
		if probeErr != nil || duration <= 0 {
			continue
		}
		item.Duration = int(math.Floor(duration))
		segments := planLocalHighlightSegments(duration)
		item.Clips = len(segments)
		item.Generated = localHLGenerated(dir, mediaStem(m.FilePath), segments)
		if item.Generated {
			report.AlreadyDirs++
		}
		report.Eligible = append(report.Eligible, item)
	}
	report.EligibleDirs = len(report.Eligible)
	report.SkippedDirs = len(report.Skipped)
	report.TotalDirs = report.EligibleDirs + report.SkippedDirs
	return report, nil
}

// ==================== 后台生成任务 ====================

// LocalHighlightGenStatus 本地精彩片段生成任务的进度快照。
type LocalHighlightGenStatus struct {
	Running       bool       `json:"running"`
	StopRequested bool       `json:"stop_requested"`
	Total         int        `json:"total"`     // 本轮应处理的单视频目录数
	Processed     int        `json:"processed"` // 生成并导入成功（含补齐）
	Already       int        `json:"already"`   // 已存在完整侧车，无变动
	Skipped       int        `json:"skipped"`   // 目录跳过（多视频/不可用），本轮不处理
	Failed        int        `json:"failed"`
	LastError     string     `json:"last_error"` // 最近一次失败原因
	Remaining     int        `json:"remaining"`
	CurrentDir    string     `json:"current_dir"`
	CurrentVideo  string     `json:"current_video"`
	CurrentLike   int        `json:"current_like"` // 当前视频的计划片段数
	CurrentDone   int        `json:"current_done"` // 当前视频已完成的片段/缩略图数
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
}

// localHighlightGenState 服务内保存的本地生成任务可变状态（localHL.mu 保护）。
type localHighlightGenState struct {
	mu            sync.Mutex
	running       bool
	stopRequested bool
	total         int
	processed     int
	already       int
	skipped       int
	failed        int
	lastError     string
	currentDir    string
	currentVideo  string
	currentLike   int
	currentDone   int
	startedAt     time.Time
	finishedAt    *time.Time
}

func (s *MediaAnalysisService) snapshotLocalHL() LocalHighlightGenStatus {
	st := &s.localHL
	st.mu.Lock()
	defer st.mu.Unlock()
	done := st.processed + st.already + st.skipped + st.failed
	remaining := st.total - done
	if remaining < 0 {
		remaining = 0
	}
	return LocalHighlightGenStatus{
		Running:       st.running,
		StopRequested: st.stopRequested,
		Total:         st.total,
		Processed:     st.processed,
		Already:       st.already,
		Skipped:       st.skipped,
		Failed:        st.failed,
		LastError:     st.lastError,
		Remaining:     remaining,
		CurrentDir:    st.currentDir,
		CurrentVideo:  st.currentVideo,
		CurrentLike:   st.currentLike,
		CurrentDone:   st.currentDone,
		StartedAt:     st.startedAt,
		FinishedAt:    st.finishedAt,
	}
}

// SnapshotLocalHighlightGeneration 返回本地精彩片段生成任务当前进度快照。
func (s *MediaAnalysisService) SnapshotLocalHighlightGeneration() LocalHighlightGenStatus {
	return s.snapshotLocalHL()
}

// StartLocalHighlightGeneration 启动本地精彩片段生成任务（增量模式，后台执行，可查询/停止）。
// mediaIDs 为要处理的多媒体 ID 列表；空列表表示「只处理待生成/失败的影片」，绝不
// 遍历已生成过缩略图的旧影片。与全库批量生成互斥，避免 ffmpeg 争抢。
// 返回启动瞬间的快照。
func (s *MediaAnalysisService) StartLocalHighlightGeneration(mediaIDs []string) (LocalHighlightGenStatus, error) {
	// 与批量生成互斥（先查 batch，再取 localHL 锁，统一加锁顺序避免死锁）
	s.batch.mu.Lock()
	batchRunning := s.batch.running
	s.batch.mu.Unlock()
	if batchRunning {
		return s.snapshotLocalHL(), errors.New("精彩片段批量任务运行中，请先停止或等待完成")
	}

	s.localHL.mu.Lock()
	if s.localHL.running {
		s.localHL.mu.Unlock()
		return s.snapshotLocalHL(), ErrLocalHighlightGenInProgress
	}

	// 增量候选：只读取 pending / failed 两类，不遍历全部影片
	videos, err := s.mediaRepo.ListPendingLocalHighlights()
	if err != nil {
		s.localHL.mu.Unlock()
		return s.snapshotLocalHL(), err
	}
	eligible, skippedDirs := partitionLocalHighlightDirs(videos)

	// 目录归属二次校验：仅按候选分组无法发现「目录内还有其它已生成视频」的混排
	// 目录（例如目录里第 2 个视频后补入库、第 1 个已生成）。整目录计数 >1 即跳过。
	if len(eligible) > 0 {
		dirs := make([]string, 0, len(eligible))
		for _, m := range eligible {
			dirs = append(dirs, filepath.Dir(m.FilePath))
		}
		counts, err := s.mediaRepo.CountLocalByParentDirs(dirs)
		if err != nil {
			s.localHL.mu.Unlock()
			return s.snapshotLocalHL(), err
		}
		kept := eligible[:0]
		for _, m := range eligible {
			dir := filepath.Dir(m.FilePath)
			if c := counts[dir]; c > 1 {
				skippedDirs = append(skippedDirs, LocalHighlightSkipItem{
					Dir: dir, Videos: []string{filepath.Base(m.FilePath)},
					Reason: fmt.Sprintf("目录下存在 %d 个视频文件，请将每个视频放在独立目录", c),
				})
				continue
			}
			kept = append(kept, m)
		}
		eligible = kept
	}

	// 自动模式（未显式指定 mediaIDs）：跳过正在失败冷却期的影片，避免每次点击都重试。
	if len(mediaIDs) == 0 {
		kept := eligible[:0]
		for _, m := range eligible {
			if m.LocalHLStatus == model.LocalHLStatusFailed && m.LocalHLFailedAt != nil &&
				time.Since(*m.LocalHLFailedAt) < localHLRetryCooldown {
				continue
			}
			kept = append(kept, m)
		}
		eligible = kept
	}

	// 若调用方指定了 mediaIDs，则只保留这些 ID 对应的目录（精确处理，无视冷却）
	if len(mediaIDs) > 0 {
		set := make(map[string]bool, len(mediaIDs))
		for _, id := range mediaIDs {
			set[id] = true
		}
		filtered := make([]model.Media, 0, len(eligible))
		for _, m := range eligible {
			if set[m.ID] {
				filtered = append(filtered, m)
			}
		}
		eligible = filtered
	}

	now := time.Now()
	s.localHL.running = true
	s.localHL.stopRequested = false
	s.localHL.total = len(eligible)
	s.localHL.processed = 0
	s.localHL.already = 0
	s.localHL.skipped = 0
	s.localHL.failed = 0
	s.localHL.currentDir = ""
	s.localHL.currentVideo = ""
	s.localHL.currentLike = 0
	s.localHL.currentDone = 0
	s.localHL.startedAt = now
	s.localHL.finishedAt = nil
	s.localHL.mu.Unlock()

	go s.runLocalHighlightGeneration(eligible, skippedDirs)
	return s.snapshotLocalHL(), nil
}

// StopLocalHighlightGeneration 请求停止本地生成任务。已处理的目录保留结果，
// 半成品目录会在下次运行时自动补齐。
func (s *MediaAnalysisService) StopLocalHighlightGeneration() (LocalHighlightGenStatus, error) {
	s.localHL.mu.Lock()
	if !s.localHL.running {
		s.localHL.mu.Unlock()
		return s.snapshotLocalHL(), ErrNoLocalHighlightJob
	}
	s.localHL.stopRequested = true
	s.localHL.mu.Unlock()
	return s.snapshotLocalHL(), nil
}

func (s *MediaAnalysisService) localHLStopped() bool {
	s.localHL.mu.Lock()
	defer s.localHL.mu.Unlock()
	return s.localHL.stopRequested
}

func (s *MediaAnalysisService) finishLocalHL() {
	s.localHL.mu.Lock()
	now := time.Now()
	s.localHL.running = false
	s.localHL.stopRequested = false
	s.localHL.currentDir = ""
	s.localHL.currentVideo = ""
	s.localHL.currentLike = 0
	s.localHL.currentDone = 0
	s.localHL.finishedAt = &now
	s.localHL.mu.Unlock()
}

func (s *MediaAnalysisService) runLocalHighlightGeneration(eligible []model.Media, skippedDirs []LocalHighlightSkipItem) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Errorf("local highlight generation panic: %v", r)
			s.finishLocalHL()
		}
	}()

	s.localHL.mu.Lock()
	s.localHL.skipped += len(skippedDirs)
	s.localHL.mu.Unlock()

	for _, m := range eligible {
		if s.localHLStopped() {
			break
		}
		s.localHL.mu.Lock()
		s.localHL.currentDir = filepath.Dir(m.FilePath)
		s.localHL.currentVideo = m.Title
		if s.localHL.currentVideo == "" {
			s.localHL.currentVideo = filepath.Base(m.FilePath)
		}
		s.localHL.currentLike = 0
		s.localHL.currentDone = 0
		s.localHL.mu.Unlock()

		status, err := s.generateOneLocalMedia(m)
		s.localHL.mu.Lock()
		if err != nil {
			s.localHL.lastError = err.Error()
		}
		switch status {
		case "generated":
			s.localHL.processed++
		case "already":
			s.localHL.already++
		default:
			s.localHL.failed++
		}
		s.localHL.currentDone = s.localHL.currentLike
		s.localHL.mu.Unlock()
	}

	s.finishLocalHL()
	snap := s.snapshotLocalHL()
	s.logger.Infof("local highlight generation finished total=%d processed=%d already=%d skipped=%d failed=%d",
		snap.Total, snap.Processed, snap.Already, snap.Skipped, snap.Failed)
}

// generateOneLocalMedia 为一部视频补全 .highlights/ 侧车（json+缩略图）并自动导入数据库。
// 返回 "generated"/"already"/"failed" 及失败原因（err 非空时表示失败）。
// 每次调用都会把结果持久化到 media.local_hl_* 状态列（增量模式的正确性基础）。
func (s *MediaAnalysisService) generateOneLocalMedia(m model.Media) (string, error) {
	if !localHighlightMediaUsable(m) {
		err := errors.New("视频文件不存在或不受支持的格式")
		_ = s.mediaRepo.MarkLocalHLFailed(m.ID, err.Error())
		return "failed", err
	}
	dir := filepath.Dir(m.FilePath)
	stem := mediaStem(m.FilePath)

	duration, err := probeFileDuration(s.cfg.App.FFprobePath, m.FilePath)
	if err != nil || duration <= 0 {
		msg := "ffprobe 读取时长失败"
		if err != nil {
			msg = fmt.Sprintf("ffprobe 读取时长失败: %v", err)
		}
		s.logger.Warnf("local highlight probe failed media=%s: %v", m.ID, err)
		_ = s.mediaRepo.MarkLocalHLFailed(m.ID, msg)
		return "failed", errors.New(msg)
	}
	// 缓存探测时长：后续增量扫描/校验无需重复 ffprobe
	if m.LocalHLDuration != duration {
		_ = s.mediaRepo.CacheLocalHLDuration(m.ID, duration)
	}
	segments := planLocalHighlightSegments(duration)

	// 已生成完整侧车且数据库中有对应 manual 片段 → 无变动
	if localHLGenerated(dir, stem, segments) {
		imported, _, _, importErr := s.importHighlightDir(filepath.Join(dir, ".highlights"), []model.Media{m})
		if importErr == nil && imported > 0 {
			_ = s.mediaRepo.MarkLocalHLGenerated(m.ID, duration)
			return "already", nil
		}
		if importErr == nil {
			importErr = errors.New("已存在侧车文件但数据库无对应 manual 片段")
		}
		_ = s.mediaRepo.MarkLocalHLFailed(m.ID, importErr.Error())
		return "failed", importErr
	}

	hlDir := filepath.Join(dir, ".highlights")
	if err := os.MkdirAll(hlDir, 0o755); err != nil {
		s.logger.Warnf("local highlight mkdir %s: %v", hlDir, err)
		msg := fmt.Sprintf("创建 .highlights 目录失败: %v", err)
		_ = s.mediaRepo.MarkLocalHLFailed(m.ID, msg)
		return "failed", errors.New(msg)
	}
	webp := s.ffmpegSupportsEncoder("libwebp")

	s.localHL.mu.Lock()
	s.localHL.currentLike = len(segments)
	s.localHL.mu.Unlock()

	wrote := 0
	for _, seg := range segments {
		if s.localHLStopped() {
			break
		}
		base := filepath.Join(hlDir, fmt.Sprintf("%s_%02d_%s", stem, seg.Index, seg.Title))
		if !fileExists(base + ".json") {
			if err := writeLocalHighlightSidecar(base, seg); err != nil {
				s.logger.Warnf("local highlight sidecar %s: %v", base, err)
				continue
			}
		}
		thumb := thumbsForBase(base)
		if len(thumb) == 0 {
			if err := s.writeLocalHighlightThumb(m.FilePath, base, seg.ThumbSec, webp); err != nil {
				s.logger.Warnf("local highlight thumb %s: %v", base, err)
				continue
			}
		}
		wrote++
		s.localHL.mu.Lock()
		s.localHL.currentDone = wrote
		s.localHL.mu.Unlock()
	}

	if wrote == 0 {
		err := errors.New("未写入任何片段文件（缩略图生成失败）")
		_ = s.mediaRepo.MarkLocalHLFailed(m.ID, err.Error())
		return "failed", err
	}

	imported, _, _, importErr := s.importHighlightDir(hlDir, []model.Media{m})
	if importErr != nil {
		s.logger.Warnf("local highlight import %s: %v", hlDir, importErr)
		_ = s.mediaRepo.MarkLocalHLFailed(m.ID, importErr.Error())
		return "failed", importErr
	}
	if imported == 0 {
		err := errors.New("导入数据库失败（无片段写入）")
		_ = s.mediaRepo.MarkLocalHLFailed(m.ID, err.Error())
		return "failed", err
	}
	s.logger.Infof("local highlight generated media=%s clips=%d", m.ID, imported)
	_ = s.mediaRepo.MarkLocalHLGenerated(m.ID, duration)
	return "generated", nil
}

func thumbsForBase(base string) []string {
	var found []string
	for _, suffix := range []string{".thumb.webp", ".thumb.jpg"} {
		if fileExists(base + suffix) {
			found = append(found, base+suffix)
		}
	}
	return found
}

// writeLocalHighlightSidecar 写入 <base>.json 时间线侧车。
func writeLocalHighlightSidecar(base string, seg localHighlightSegment) error {
	sidecar := map[string]interface{}{
		"title":      seg.Title,
		"start_time": seg.Start,
		"end_time":   seg.End,
		"score":      localHLScore,
	}
	data, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(base+".json", data, 0o644)
}

// writeLocalHighlightThumb 从原视频对应时间点截取 720 宽缩略图（webp，不支持则 jpg）。
func (s *MediaAnalysisService) writeLocalHighlightThumb(source, base string, sec float64, webp bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), localHLThumbTimeout)
	defer cancel()
	output := base + ".thumb.jpg"
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.3f", sec), "-i", source,
		"-frames:v", "1", "-vf", fmt.Sprintf("scale=%d:-2", localHLThumbWidth),
	}
	if webp {
		output = base + ".thumb.webp"
		args = append(args, "-c:v", "libwebp", "-quality", fmt.Sprint(localHLThumbQuality))
	} else {
		args = append(args, "-q:v", "3")
	}
	args = append(args, "-y", output)
	cmd := exec.CommandContext(ctx, s.cfg.App.FFmpegPath, args...)
	if data, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("thumbnail ffmpeg: %w: %s", err, strings.TrimSpace(string(data)))
	}
	return nil
}

// ==================== 删除（清理） ====================

// LocalHighlightCleanupReport 本地精彩片段清理结果。
type LocalHighlightCleanupReport struct {
	MediaAffected int `json:"media_affected"` // 清理了生成物并删除 manual 记录的视频数
	FilesDeleted  int `json:"files_deleted"`  // 删除的本地生成文件数
}

// CleanupLocalHighlights 删除「本地生成」产生的 .highlights 文件（<视频茎>_ 前缀的
// json / 缩略图 / 旧切割 mp4）并清除对应媒体的 manual 精彩片段记录。
// 只处理单视频目录；多视频目录因归属不清不在本次范围内。
func (s *MediaAnalysisService) CleanupLocalHighlights() (*LocalHighlightCleanupReport, error) {
	videos, err := s.mediaRepo.ListAllLocalVideos()
	if err != nil {
		return nil, err
	}
	eligible, _ := partitionLocalHighlightDirs(videos)

	report := &LocalHighlightCleanupReport{}
	for _, m := range eligible {
		stem := mediaStem(m.FilePath)
		hlDir := filepath.Join(filepath.Dir(m.FilePath), ".highlights")
		entries, readErr := os.ReadDir(hlDir)
		if readErr != nil {
			continue
		}
		prefix := stem + "_"
		deleted := 0
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasPrefix(name, prefix) {
				continue
			}
			ext := strings.ToLower(filepath.Ext(name))
			isSidecar := strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".thumb.webp") || strings.HasSuffix(name, ".thumb.jpg")
			if !isSidecar && !manualClipExts[ext] {
				continue
			}
			if rmErr := os.Remove(filepath.Join(hlDir, name)); rmErr == nil {
				deleted++
			}
		}
		if deleted == 0 {
			continue
		}
		if err := s.highlightRepo.DeleteManualByMediaID(m.ID); err != nil {
			s.logger.Warnf("cleanup local highlight db %s: %v", m.ID, err)
		}
		// 清理后重置为待生成，下一次增量生成即自动补齐
		if err := s.mediaRepo.MarkLocalHLPending(m.ID); err != nil {
			s.logger.Warnf("cleanup local highlight status %s: %v", m.ID, err)
		}
		report.MediaAffected++
		report.FilesDeleted += deleted
		// 目录已空则一并移除
		if after, err := os.ReadDir(hlDir); err == nil && len(after) == 0 {
			_ = os.Remove(hlDir)
		}
	}
	s.logger.Infof("cleanup local highlights: media=%d files=%d", report.MediaAffected, report.FilesDeleted)
	return report, nil
}

// ==================== 一致性校验 ====================

// LocalHighlightVerifyReport 本地精彩片段一致性校验结果。
type LocalHighlightVerifyReport struct {
	TotalChecked int `json:"total_checked"` // 参与校验的影片数
	Repaired     int `json:"repaired"`      // 已生成但产物缺失，重置回待生成
	Completed    int `json:"completed"`     // 待生成但产物已齐全，补记为已生成
	Errors       int `json:"errors"`        // 校验过程中的数据库错误数
}

// VerifyLocalHighlights 校验「已生成」影片的 .highlights 产物是否仍在磁盘上。
// 仅用缓存的时长做 os.Stat 校验，不触发 ffprobe；产物缺失（如被手动删除）的
// 影片重置回 pending，下一次增量生成即补齐。
func (s *MediaAnalysisService) VerifyLocalHighlights() (*LocalHighlightVerifyReport, error) {
	videos, err := s.mediaRepo.ListAllLocalVideos()
	if err != nil {
		return nil, err
	}
	eligible, _ := partitionLocalHighlightDirs(videos)
	report := &LocalHighlightVerifyReport{}
	for _, m := range eligible {
		if !localHighlightMediaUsable(m) {
			continue
		}
		if m.LocalHLDuration <= 0 {
			// 无时长缓存（如迁移回填的 generated）：信任状态，避免升级后大规模探测，
			// 由下一次增量生成探测后回填时长。
			continue
		}
		dir := filepath.Dir(m.FilePath)
		filesOK := localHLGenerated(dir, mediaStem(m.FilePath), planLocalHighlightSegments(m.LocalHLDuration))
		switch {
		case m.LocalHLStatus == model.LocalHLStatusGenerated && !filesOK:
			if err := s.mediaRepo.MarkLocalHLPending(m.ID); err != nil {
				report.Errors++
				s.logger.Warnf("verify local highlight reset %s: %v", m.ID, err)
				continue
			}
			report.Repaired++
			report.TotalChecked++
		case m.LocalHLStatus == model.LocalHLStatusPending && filesOK:
			if err := s.mediaRepo.MarkLocalHLGenerated(m.ID, m.LocalHLDuration); err != nil {
				report.Errors++
				s.logger.Warnf("verify local highlight complete %s: %v", m.ID, err)
				continue
			}
			report.Completed++
			report.TotalChecked++
		case m.LocalHLStatus == model.LocalHLStatusGenerated:
			report.TotalChecked++
		}
	}
	return report, nil
}
