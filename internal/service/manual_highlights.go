package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/fan-video/fan-video/internal/model"
)

// 手动精彩片段导入。
//
// 目录约定（与工具链保持一致，见脚本）：
//
//	<视频目录>/.highlights/                       平铺：目录内只有一部视频，或由侧车 json 的 source 消歧
//	<视频目录>/.highlights/<视频茎>/              子目录模式：子目录名 = 源视频文件名（不含扩展名）
//	片段：<序号>_<标题>.mp4（可选）
//	缩略图：<片段名>.thumb.webp / .thumb.jpg（可选）
//	侧车：<片段名>.json（可选）：title / score / tags / start_time / end_time / source
//
// 时间线模式（推荐）：省空间，只写 <片段名>.json + <片段名>.thumb.*，
// 不切割 mp4。系统播放/预览/导出始终定位原视频时间线，从不读取片段文件本身。
//
// 导入为 Source=manual 的行；重复导入时按媒体幂等替换 manual 片段，绝不触碰
// 自动生成（ffmpeg / ai / client_*）的片段。

const manualHighlightSource = "manual"

var (
	manualClipSequenceRe = regexp.MustCompile(`^\d+[_\-.、\s]+(.+)$`)
	manualClipExts       = map[string]bool{".mp4": true, ".mkv": true, ".webm": true, ".mov": true}
	manualThumbSuffixes  = []string{".thumb.webp", ".thumb.jpg"}
)

// ManualHighlightReport 手动片段导入结果。
type ManualHighlightReport struct {
	DirsFound     int                   `json:"dirs_found"`     // 扫描到的 .highlights 目录数
	MediaScanned  int                   `json:"media_scanned"`  // 归属到来源视频并已写入的媒体数
	ClipsImported int                   `json:"clips_imported"` // 成功导入片段条数
	ClipsSkipped  int                   `json:"clips_skipped"`  // 跳过片段条数
	Skipped       []ManualHighlightSkip `json:"skipped,omitempty"`
}

// ManualHighlightSkip 被跳过的片段及其原因。
type ManualHighlightSkip struct {
	Path   string `json:"path"` // 片段文件或子目录路径
	Reason string `json:"reason"`
}

// manualHighlightMeta 片段侧车 json 的字段（全部可选）。
type manualHighlightMeta struct {
	Title     string  `json:"title"`
	Score     float64 `json:"score"`
	Tags      string  `json:"tags"`
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
	Source    string  `json:"source"` // 所属视频文件名；平铺且同目录多视频时用于消歧
}

// ImportManualHighlights 全库扫描：为每个本地视频，检查同目录的 .highlights/，
// 将其中的手工片段导入为 Source=manual 的精彩片段。
// 幂等：同一 .highlights/ 目录重复导入会整体替换该媒体的 manual 片段。
func (s *MediaAnalysisService) ImportManualHighlights() (*ManualHighlightReport, error) {
	videos, err := s.mediaRepo.ListAllLocalVideos()
	if err != nil {
		return nil, err
	}

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

	report := &ManualHighlightReport{Skipped: make([]ManualHighlightSkip, 0)}
	for _, dir := range dirs {
		hlDir := filepath.Join(dir, ".highlights")
		info, err := os.Stat(hlDir)
		if err != nil || !info.IsDir() {
			continue
		}
		report.DirsFound++

		imported, skipItems, mediaScanned, err := s.importHighlightDir(hlDir, byDir[dir])
		if err != nil {
			return nil, err
		}
		report.ClipsImported += imported
		report.ClipsSkipped += len(skipItems)
		report.MediaScanned += mediaScanned
		report.Skipped = append(report.Skipped, skipItems...)
	}
	return report, nil
}

// importHighlightDir 将单个 .highlights/ 目录解析出的片段按媒体分组导入数据库
// （幂等替换该媒体全部 manual 片段，不触碰自动生成结果）。
// 返回导入条数、跳过的条目与涉及的媒体数。
func (s *MediaAnalysisService) importHighlightDir(hlDir string, mediaInDir []model.Media) (int, []ManualHighlightSkip, int, error) {
	highlights, skipped := s.collectHighlightsDir(hlDir, mediaInDir)
	byMedia := make(map[string][]model.VideoHighlight)
	mediaOrder := make([]string, 0)
	for _, h := range highlights {
		if _, seen := byMedia[h.MediaID]; !seen {
			mediaOrder = append(mediaOrder, h.MediaID)
		}
		byMedia[h.MediaID] = append(byMedia[h.MediaID], h)
	}
	for _, mediaID := range mediaOrder {
		if err := s.highlightRepo.ReplaceManualByMediaID(mediaID, byMedia[mediaID]); err != nil {
			return 0, nil, 0, err
		}
	}
	return len(highlights), skipped, len(mediaOrder), nil
}

// collectHighlightsDir 解析单个 .highlights/ 目录下的全部片段并归属到来源媒体。
func (s *MediaAnalysisService) collectHighlightsDir(hlDir string, mediaInDir []model.Media) ([]model.VideoHighlight, []ManualHighlightSkip) {
	stems := make(map[string]model.Media, len(mediaInDir))
	byBasename := make(map[string]model.Media, len(mediaInDir))
	for _, m := range mediaInDir {
		stems[mediaStem(m.FilePath)] = m
		byBasename[filepath.Base(m.FilePath)] = m
	}

	entries, err := os.ReadDir(hlDir)
	if err != nil {
		return nil, []ManualHighlightSkip{{Path: hlDir, Reason: "读取失败: " + err.Error()}}
	}

	highlights := make([]model.VideoHighlight, 0, len(entries))
	skipped := make([]ManualHighlightSkip, 0)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsDir() {
			// 子目录模式：子目录名 = 视频茎
			media, ok := stems[name]
			if !ok {
				skipped = append(skipped, ManualHighlightSkip{Path: filepath.Join(hlDir, name), Reason: "未找到同名源视频（可建 .json 侧车指定 source）"})
				continue
			}
			sub, subSkipped := s.collectClips(filepath.Join(hlDir, name), media, false)
			highlights = append(highlights, sub...)
			skipped = append(skipped, subSkipped...)
			continue
		}

		ext := filepath.Ext(name)
		lowerExt := strings.ToLower(ext)
		if lowerExt == ".json" && !siblingClipExists(hlDir, strings.TrimSuffix(name, ext)) {
			// 时间线模式：只有 sidecar json，没有对应片段视频文件。
			// start_time/end_time 来自侧车，缩略图匹配 <片段名>.thumb.*。
			clipPath := filepath.Join(hlDir, name)
			base := strings.TrimSuffix(name, ext)
			meta := readManualClipMeta(clipPath)
			media, stripStem := resolveFlatClipMedia(base, mediaInDir, stems, meta.Source)
			if media == nil {
				skipped = append(skipped, ManualHighlightSkip{Path: clipPath, Reason: "无法确定所属视频（多个候选；请放入同名子目录或在侧车 json 指定 source）"})
				continue
			}
			h := s.buildManualHighlight(*media, clipPath, base, mediaStem(media.FilePath), stripStem, meta)
			if h == nil {
				continue
			}
			// 时间线片段必须有明确的结束点（end > start）。start 允许为 0：
			// 短片（完整片段）从 0 起到全片时长是合法时间线。
			if h.EndTime <= h.StartTime {
				skipped = append(skipped, ManualHighlightSkip{Path: clipPath, Reason: "时间线片段缺少有效的 end_time（需大于 start_time）"})
				continue
			}
			highlights = append(highlights, *h)
			continue
		}
		if !manualClipExts[lowerExt] {
			continue
		}
		base := strings.TrimSuffix(name, ext)
		clipPath := filepath.Join(hlDir, name)
		meta := readManualClipMeta(filepath.Join(hlDir, base+".json"))
		media, stripStem := resolveFlatClipMedia(base, mediaInDir, stems, meta.Source)
		if media == nil {
			skipped = append(skipped, ManualHighlightSkip{Path: clipPath, Reason: "无法确定所属视频（多个候选；请放入同名子目录或在侧车 json 指定 source）"})
			continue
		}
		h := s.buildManualHighlight(*media, clipPath, base, mediaStem(media.FilePath), stripStem, meta)
		if h != nil {
			highlights = append(highlights, *h)
		}
	}
	return highlights, skipped
}

// collectClips 解析子目录模式下的全部片段（子目录名即为视频茎）。
func (s *MediaAnalysisService) collectClips(dir string, media model.Media, onlyOne bool) ([]model.VideoHighlight, []ManualHighlightSkip) {
	_ = onlyOne
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []ManualHighlightSkip{{Path: dir, Reason: "读取失败: " + err.Error()}}
	}
	highlights := make([]model.VideoHighlight, 0)
	skipped := make([]ManualHighlightSkip, 0)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		ext := filepath.Ext(name)
		lowerExt := strings.ToLower(ext)
		base := strings.TrimSuffix(name, ext)
		if lowerExt == ".json" && !siblingClipExists(dir, base) {
			// 时间线模式：只有 sidecar json，没有对应片段视频文件。
			clipPath := filepath.Join(dir, name)
			meta := readManualClipMeta(clipPath)
			h := s.buildManualHighlight(media, clipPath, base, "", false, meta)
			if h == nil {
				continue
			}
			if h.EndTime <= h.StartTime {
				skipped = append(skipped, ManualHighlightSkip{Path: clipPath, Reason: "时间线片段缺少有效的 end_time（需大于 start_time）"})
				continue
			}
			highlights = append(highlights, *h)
			continue
		}
		if !manualClipExts[lowerExt] {
			continue
		}
		clipPath := filepath.Join(dir, name)
		meta := readManualClipMeta(filepath.Join(dir, base+".json"))
		h := s.buildManualHighlight(media, clipPath, base, "", false, meta)
		if h != nil {
			highlights = append(highlights, *h)
		}
	}
	return highlights, skipped
}

// resolveFlatClipMedia 为平铺目录下的片段寻找所属媒体。
func resolveFlatClipMedia(clipBase string, mediaInDir []model.Media, stems map[string]model.Media, metaSource string) (*model.Media, bool) {
	if len(mediaInDir) == 1 {
		return &mediaInDir[0], false
	}
	if metaSource != "" {
		for i := range mediaInDir {
			if mediaInDir[i].FilePath == metaSource || filepath.Base(mediaInDir[i].FilePath) == metaSource {
				return &mediaInDir[i], false
			}
		}
	}
	// 兼容工具链平铺命名 <视频茎>_<序号>_<标题>。
	for stem, media := range stems {
		if strings.HasPrefix(clipBase, stem+"_") {
			return &media, true
		}
	}
	return nil, false
}

// siblingClipExists 判断 <dir>/<base> 下是否存在已支持的片段视频文件。
// 存在时 sidecar json 只是附属元数据；不存在时该 json 本身即时间线片段定义。
func siblingClipExists(dir, base string) bool {
	for ext := range manualClipExts {
		if info, err := os.Stat(filepath.Join(dir, base+ext)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// buildManualHighlight 根据片段文件与可选侧车数据构造一条 manual 高亮。
// stripStem=true 时去掉文件名里的 <视频茎>_ 前缀后解析标题。
func (s *MediaAnalysisService) buildManualHighlight(media model.Media, clipPath, base, stem string, stripStem bool, meta manualHighlightMeta) *model.VideoHighlight {
	titleBase := base
	if stripStem && stem != "" && strings.HasPrefix(titleBase, stem+"_") {
		titleBase = strings.TrimPrefix(titleBase, stem+"_")
	}
	title := parseManualClipTitle(titleBase)
	if meta.Title != "" {
		title = meta.Title
	}

	highlight := &model.VideoHighlight{
		MediaID:        media.ID,
		Title:          title,
		StartTime:      meta.StartTime,
		EndTime:        meta.EndTime,
		Score:          meta.Score,
		Tags:           meta.Tags,
		Source:         manualHighlightSource,
		AnalysisMethod: manualHighlightSource,
		Version:        2,
	}

	dir := filepath.Dir(clipPath)
	for _, suffix := range manualThumbSuffixes {
		thumb := filepath.Join(dir, base+suffix)
		if info, err := os.Stat(thumb); err == nil && !info.IsDir() {
			highlight.Thumbnail = thumb
			break
		}
	}

	// 侧车未给出起止时间时，回退到片段自身时长（仅填充 EndTime，StartTime 保持 0，
	// 不臆造源视频时间点）。探测失败不阻塞导入。
	if meta.StartTime <= 0 && meta.EndTime <= 0 {
		if duration, err := probeFileDuration(s.cfg.App.FFprobePath, clipPath); err == nil && duration > 0 {
			highlight.EndTime = duration
		}
	}

	// Score 缺省值：手动片段是用户精选，给一个高于多数自动片段的中等偏高分数，
	// 使排序（score DESC）靠前但不至于掩盖 9.0+ 的高分段。
	if highlight.Score <= 0 {
		if meta.Score > 0 {
			highlight.Score = meta.Score
		} else {
			highlight.Score = 8.5
		}
	}
	return highlight
}

// readManualClipMeta 读取片段同名侧车 json；不存在或非法时返回空信息。
func readManualClipMeta(path string) manualHighlightMeta {
	data, err := os.ReadFile(path)
	if err != nil {
		return manualHighlightMeta{}
	}
	var meta manualHighlightMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return manualHighlightMeta{}
	}
	return meta
}

func mediaStem(filePath string) string {
	base := filepath.Base(filePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// parseManualClipTitle 解析 <序号>_<标题> 形式的片段标题。
func parseManualClipTitle(base string) string {
	if m := manualClipSequenceRe.FindStringSubmatch(base); m != nil {
		return strings.TrimSpace(m[1])
	}
	return base
}
