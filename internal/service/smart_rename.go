package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ==================== SmartRename 智能扫描重命名服务 ====================
//
// 模块职责：
//   P0 识别评分：基于规则的命名解析（复用 ParseMovieFilename）+ 文件落库匹配 -> 置信度
//   P1 AI Fallback：低置信度时调用 AIService.ChatCompletion 补全识别
//   P2 命名模板：Jellyfin/Emby `[tmdbid-xxx]` 默认；可切 Plex `{tmdb-xxx}`
//   P3 关联资源：同名 .nfo/.srt/.ass/.sub/-poster.jpg/-fanart.jpg/-thumb.jpg/.idx 等随主迁移
//   P4 安全检测：跨卷、目标已存在、磁盘空间、硬链接计数、相对路径越界
//   P5 事务执行：plan + journal，按条目串行原子，遇错回滚已成功部分
//   P6 默认 dry-run：仅 confirm=true 才真正动盘
//
// 该服务不修改 FileManagerService 现有 API，独立运转。

// ================================ Constants ================================

// 常见视频扩展名
var smartRenameVideoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true, ".wmv": true,
	".flv": true, ".webm": true, ".m4v": true, ".ts": true, ".m2ts": true,
	".mpg": true, ".mpeg": true, ".rmvb": true, ".rm": true, ".3gp": true,
	".vob": true, ".iso": true,
}

// 关联资源扩展名（不带前缀的尾缀） -> kind
var smartRenameRelatedExts = map[string]string{
	".nfo":  "nfo",
	".srt":  "subtitle",
	".ass":  "subtitle",
	".ssa":  "subtitle",
	".sub":  "subtitle",
	".idx":  "subtitle",
	".vtt":  "subtitle",
	".sup":  "subtitle",
	".lrc":  "subtitle",
	".chs":  "subtitle",
	".cht":  "subtitle",
	".chi":  "subtitle",
	".eng":  "subtitle",
	".jpg":  "image",
	".jpeg": "image",
	".png":  "image",
	".webp": "image",
	".tbn":  "image",
}

// 媒体伴生图片的命名后缀（去扩展名后的最末段）
var smartRenameImageSuffix = map[string]string{
	"poster":    "poster",
	"fanart":    "fanart",
	"thumb":     "thumb",
	"banner":    "banner",
	"clearlogo": "clearlogo",
	"landscape": "landscape",
	"disc":      "disc",
	"backdrop":  "fanart",
}

// 命名模板风格
const (
	NamingStyleJellyfin = "jellyfin" // Title (Year) [tmdbid-12345].ext
	NamingStylePlex     = "plex"     // Title (Year) {tmdb-12345}.ext
)

// 安全 / 标题字符清洗：去除 NTFS / ext4 禁用字符
var smartRenameUnsafeCharPattern = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// ================================ Types ====================================

// SmartRenameConfig 服务级配置（来自全局 config 注入）
type SmartRenameConfig struct {
	DefaultStyle   string   // jellyfin / plex
	MaxScanFiles   int      // 单次扫描最大文件数（防爆，默认 5000）
	SafeRoots      []string // 安全根目录白名单：若非空，所有改名必须在白名单内
	RequireConfirm bool     // 是否强制 confirm（即使前端传 false）
}

// DefaultSmartRenameConfig 默认配置
func DefaultSmartRenameConfig() SmartRenameConfig {
	return SmartRenameConfig{
		DefaultStyle:   NamingStyleJellyfin,
		MaxScanFiles:   5000,
		SafeRoots:      nil,
		RequireConfirm: true,
	}
}

// SmartRenameRelatedFile 单个关联资源
type SmartRenameRelatedFile struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"` // nfo / subtitle / poster / fanart / thumb / other
}

// SmartRenameSafetyReport 安全检测结果
type SmartRenameSafetyReport struct {
	OK              bool     `json:"ok"`
	CrossVolume     bool     `json:"cross_volume"`   // 跨卷
	TargetExists    bool     `json:"target_exists"`  // 目标已存在
	HardlinkCount   uint64   `json:"hardlink_count"` // 硬链接数（>1 警告）
	OutsideSafeRoot bool     `json:"outside_safe_root"`
	NotEnoughSpace  bool     `json:"not_enough_space"`
	Issues          []string `json:"issues"` // 人类可读问题列表
}

// ScanInput 扫描入参
type ScanInput struct {
	RootPath    string   // 待扫描根目录（绝对路径）
	LibraryID   string   // 可选：限定到媒体库
	NamingStyle string   // 可选：jellyfin / plex
	Template    string   // 可选：自定义模板（空则按 style 取默认）
	SafeRoots   []string // 可选：本次扫描覆盖的安全根
	CreatedBy   string   // 当前用户 ID
}

// ExecuteInput 执行入参
type ExecuteInput struct {
	PlanID       string   // 必填
	Confirm      bool     // 必须 true 才真正落盘
	ItemIDs      []string // 可选：仅执行指定条目（空表示全部 pending+safety_ok 条目）
	IgnoreSafety bool     // 可选：用户显式忽略安全警告（默认 false）
}

// SmartRenameService 智能扫描重命名服务
type SmartRenameService struct {
	repo       *repository.RenameRepo
	mediaRepo  *repository.MediaRepo
	seriesRepo *repository.SeriesRepo
	cfg        SmartRenameConfig
	logger     *zap.SugaredLogger

	// preloadedMedia 在单次 Scan 期间临时缓存的"路径→Media"映射；
	// buildItem 内部仅读，外部由 Scan 在每次调用前/后重新设置/清理。
	// 因 Scan 内部使用 errgroup 串行启动+等待，所以无需读写锁。
	preloadedMedia map[string]*model.Media
}

// NewSmartRenameService 构造服务
func NewSmartRenameService(
	repo *repository.RenameRepo,
	mediaRepo *repository.MediaRepo,
	seriesRepo *repository.SeriesRepo,
	cfg SmartRenameConfig,
	logger *zap.SugaredLogger,
) *SmartRenameService {
	if cfg.MaxScanFiles <= 0 {
		cfg.MaxScanFiles = 5000
	}
	if cfg.DefaultStyle == "" {
		cfg.DefaultStyle = NamingStyleJellyfin
	}
	return &SmartRenameService{
		repo:       repo,
		mediaRepo:  mediaRepo,
		seriesRepo: seriesRepo,
		cfg:        cfg,
		logger:     logger,
	}
}

// ================================ P0+P1: 扫描 + 规划 ==========================

// Scan 扫描目录、识别每个视频文件、生成规划任务（draft 状态）。
//
// 不会动磁盘，仅在 DB 中创建 RenamePlan + 一组 RenamePlanItem。
func (s *SmartRenameService) Scan(in ScanInput) (*model.RenamePlan, error) {
	if in.RootPath == "" {
		return nil, errors.New("root_path 必填")
	}
	absRoot, err := filepath.Abs(in.RootPath)
	if err != nil {
		return nil, fmt.Errorf("根目录非法: %w", err)
	}
	st, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("根目录不可访问: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("根目录不是目录: %s", absRoot)
	}

	// 合并配置
	style := strings.ToLower(strings.TrimSpace(in.NamingStyle))
	if style != NamingStyleJellyfin && style != NamingStylePlex {
		style = s.cfg.DefaultStyle
	}
	safeRoots := in.SafeRoots
	if len(safeRoots) == 0 {
		safeRoots = s.cfg.SafeRoots
	}

	// 1) 扫描视频文件
	videoFiles, err := s.collectVideoFiles(absRoot)
	if err != nil {
		return nil, fmt.Errorf("扫描失败: %w", err)
	}
	s.logger.Infof("[SmartRename] 扫描完成：发现 %d 个视频文件 root=%s", len(videoFiles), absRoot)

	// 2) 持久化 Plan
	planID := uuid.New().String()
	plan := &model.RenamePlan{
		ID:          planID,
		LibraryID:   in.LibraryID,
		RootPath:    absRoot,
		NamingStyle: style,
		Template:    in.Template,
		Status:      model.RenamePlanStatusDraft,
		DryRun:      true,
		TotalItems:  len(videoFiles),
		CreatedBy:   in.CreatedBy,
	}
	if err := s.repo.CreatePlan(plan); err != nil {
		return nil, fmt.Errorf("持久化规划失败: %w", err)
	}

	// 3) 预加载：一次 SQL 拉全部 file_path→Media 映射，避免循环内 N+1。
	mediaMap := s.preloadMediaMap(videoFiles)

	// 4) 并发识别 + 生成条目
	items := s.buildItemsParallel(planID, videoFiles, style, in.Template, safeRoots, mediaMap)

	// 5) 统计汇总
	stats := struct {
		need, skipped, unsafe int
	}{}
	for i := range items {
		it := &items[i]
		switch it.Status {
		case model.RenameItemStatusPending:
			stats.need++
		case model.RenameItemStatusSkipped:
			stats.skipped++
		case model.RenameItemStatusUnsafe:
			stats.unsafe++
		}
	}

	if err := s.repo.CreateItems(items); err != nil {
		return nil, fmt.Errorf("持久化条目失败: %w", err)
	}

	// 4) 更新统计
	plan.NeedRename = stats.need
	plan.SkippedItems = stats.skipped
	plan.UnsafeItems = stats.unsafe
	if err := s.repo.UpdatePlanFields(planID, map[string]interface{}{
		"need_rename":   stats.need,
		"skipped_items": stats.skipped,
		"unsafe_items":  stats.unsafe,
	}); err != nil {
		s.logger.Warnf("[SmartRename] 更新规划统计失败: %v", err)
	}

	// 重新加载（带 items 返回）
	return s.repo.GetPlanWithItems(planID)
}

// collectVideoFiles 递归扫描目录，仅收集视频文件
//
// 加速优化：
//   - 跳过小于 10MB 的文件：片头/广告/样片/损坏文件，避免浪费 AI 调用 + 后续磁盘 IO；
//   - .strm 远程流文件 大小不代表内容，豁免大小过滤。
func (s *SmartRenameService) collectVideoFiles(root string) ([]string, error) {
	var files []string
	maxFiles := s.cfg.MaxScanFiles
	const minVideoBytes = 10 * 1024 * 1024 // 10MB
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			s.logger.Warnf("[SmartRename] 跳过不可访问路径 %s: %v", p, err)
			return nil
		}
		if d.IsDir() {
			// 忽略以 . / @eaDir 开头的目录
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "@eaDir" || name == "$RECYCLE.BIN" || name == "System Volume Information" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if !smartRenameVideoExts[ext] {
			return nil
		}
		// 小文件过滤（.strm 远程流豁免）
		if ext != ".strm" {
			if info, e := d.Info(); e == nil && info.Size() < minVideoBytes {
				return nil
			}
		}
		files = append(files, p)
		if maxFiles > 0 && len(files) >= maxFiles {
			return errStopWalk
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return nil, err
	}
	return files, nil
}

var errStopWalk = errors.New("stop walk")

// buildItem 针对单个视频文件生成 RenamePlanItem
func (s *SmartRenameService) buildItem(
	planID, src, style, customTpl string,
	safeRoots []string,
	usedTargets map[string]bool,
) (*model.RenamePlanItem, error) {
	srcName := filepath.Base(src)
	item := &model.RenamePlanItem{
		ID:         uuid.New().String(),
		PlanID:     planID,
		SourcePath: src,
		SourceName: srcName,
		Status:     model.RenameItemStatusPending,
	}

	// === P0: 规则解析 + 置信度评分 ===
	parsed := ParseMovieFilename(srcName)
	conf := s.scoreConfidence(parsed)

	// === 关联落库的 Media (若有)：优先用 DB 信息覆盖 ===
	// 优先从预加载 map 取；未传则兑底 SQL。
	var mediaInfo *model.Media
	if s.preloadedMedia != nil {
		mediaInfo = s.preloadedMedia[src]
	} else {
		mediaInfo, _ = s.lookupMediaByPath(src)
	}
	if mediaInfo != nil {
		item.MediaID = mediaInfo.ID
		// 用 DB 中的精确字段强化识别
		if mediaInfo.Title != "" {
			parsed.Title = mediaInfo.Title
		}
		if mediaInfo.OrigTitle != "" && parsed.TitleAlt == "" {
			parsed.TitleAlt = mediaInfo.OrigTitle
		}
		if mediaInfo.Year > 0 {
			parsed.Year = mediaInfo.Year
		}
		if mediaInfo.TMDbID > 0 {
			parsed.TMDbID = mediaInfo.TMDbID
		}
		if mediaInfo.IMDbID != "" {
			parsed.IMDbID = mediaInfo.IMDbID
		}
		// DB 已有准确的 TMDb 识别 -> 置信度拉满
		if mediaInfo.TMDbID > 0 {
			conf = 0.99
		}
		item.MediaType = mediaInfo.MediaType
		item.SeasonNum = mediaInfo.SeasonNum
		item.EpisodeNum = mediaInfo.EpisodeNum
		item.IsPersonal = mediaInfo.IsPersonal
	}

	// 兜底：未识别 Title 时使用文件名主体
	if parsed.Title == "" {
		parsed.Title = strings.TrimSuffix(srcName, filepath.Ext(srcName))
	}
	if item.MediaType == "" {
		// 默认按电影；若文件名中检测出 SxxExx 则改为 episode（粗略再扫一次）
		if s, e := extractSxxExx(srcName); s > 0 && e > 0 {
			item.MediaType = "episode"
			item.SeasonNum = s
			item.EpisodeNum = e
		} else {
			item.MediaType = "movie"
		}
	}

	// === P0.5: 路径 ID 兜底 ===
	// 当文件名/解析结果中没有 tmdbid 但源路径目录上挂着 [tmdbid-X] 时，回收之。
	// 用例："逃学威龙2/逃学威龙2.mkv" 上层有 "逃学威龙 (1991) [tmdbid-10258]" 时回收 ID。
	if parsed.TMDbID == 0 || parsed.IMDbID == "" {
		tmdbFromPath, imdbFromPath := ExtractIDsFromPath(src)
		if parsed.TMDbID == 0 && tmdbFromPath > 0 {
			parsed.TMDbID = tmdbFromPath
			if conf < 0.9 {
				conf = 0.9 // 路径上有精确 ID，置信度抬升
			}
		}
		if parsed.IMDbID == "" && imdbFromPath != "" {
			parsed.IMDbID = imdbFromPath
		}
	}

	item.ParsedTitle = parsed.Title
	item.ParsedTitleAlt = parsed.TitleAlt
	item.ParsedYear = parsed.Year
	item.ParsedTMDbID = parsed.TMDbID
	item.ParsedIMDbID = parsed.IMDbID
	item.Confidence = conf

	// === P2: 渲染目标命名 ===
	targetName, err := s.renderTargetName(style, customTpl, parsed, item)
	if err != nil {
		return nil, err
	}
	targetPath := filepath.Join(filepath.Dir(src), targetName)
	item.TargetName = targetName
	item.TargetPath = targetPath

	// 如果目标名等于源名 -> 跳过（已是目标格式）
	if filepath.Base(src) == targetName {
		item.Status = model.RenameItemStatusSkipped
		item.SafetyOK = true
		item.SafetyNote = "已是目标命名"
		return item, nil
	}

	// === P3: 关联资源 ===
	relatedRaw, relatedTargets := s.collectRelatedFiles(src, targetPath)
	if buf, err := json.Marshal(relatedRaw); err == nil {
		item.RelatedFilesJSON = string(buf)
	}

	// === P4: 安全检测 ===
	allTargets := append([]string{targetPath}, relatedTargets...)
	safety := s.checkSafety(src, targetPath, allTargets, safeRoots, usedTargets)
	if buf, err := json.Marshal(safety); err == nil {
		item.SafetyJSON = string(buf)
	}
	item.SafetyOK = safety.OK
	if !safety.OK {
		item.SafetyNote = strings.Join(safety.Issues, "; ")
		item.Status = model.RenameItemStatusUnsafe
	} else {
		// 标记目标已占用（跨平台大小写策略由 pathKey 决定）
		for _, t := range allTargets {
			usedTargets[pathKey(t)] = true
		}
	}

	return item, nil
}

// scoreConfidence 基于解析结果计算 0~1 的置信度
//
// 评分模型（最高 1.0）：
//   - 有 TMDbID：+0.5（强证据）
//   - 有 IMDbID：+0.4
//   - Title 非空且非全 ASCII 噪声：+0.25
//   - Year > 0：+0.2
//   - TitleAlt 非空：+0.05
func (s *SmartRenameService) scoreConfidence(p ParsedFilename) float64 {
	score := 0.0
	if p.TMDbID > 0 {
		score += 0.5
	}
	if p.IMDbID != "" {
		score += 0.4
	}
	if p.Title != "" && len([]rune(strings.TrimSpace(p.Title))) >= 2 {
		score += 0.25
	}
	if p.Year > 0 {
		score += 0.2
	}
	if p.TitleAlt != "" {
		score += 0.05
	}
	if score > 1.0 {
		score = 1.0
	}
	return score
}

// preloadMediaMap 一次 SQL 把所有候选源路径对应的 Media 拉出来，回写到 s.preloadedMedia。
//
// 任何失败都不会阻断流程：返回 nil/空 map 时，buildItem 会自动回退到逐条 SQL 查询。
func (s *SmartRenameService) preloadMediaMap(paths []string) map[string]*model.Media {
	if s.mediaRepo == nil || len(paths) == 0 {
		return nil
	}
	m, err := s.mediaRepo.ListByFilePaths(paths)
	if err != nil {
		s.logger.Warnf("[SmartRename] 预加载 Media 失败（回退到逐条查询）: %v", err)
		return nil
	}
	return m
}

// buildItemsParallel 并发执行 buildItem。
//
// 关键点：
//   - 并发数 = min(8, NumCPU*2)；
//   - usedTargets 是"目标路径占用表"（防止两个源解析到相同目标），并发下用 mu 保护；
//   - 结果 items 与 paths 顺序一一对应（用 index 写回，避免 channel 乱序）。
func (s *SmartRenameService) buildItemsParallel(
	planID string,
	paths []string,
	style, customTpl string,
	safeRoots []string,
	preloaded map[string]*model.Media,
) []model.RenamePlanItem {
	// 临时把预加载 map 注入服务，buildItem 直接读
	s.preloadedMedia = preloaded
	defer func() { s.preloadedMedia = nil }()

	items := make([]model.RenamePlanItem, len(paths))

	// 并发参数
	workers := runtime.NumCPU() * 2
	if workers < 4 {
		workers = 4
	}
	if workers > 8 {
		workers = 8
	}

	var (
		mu          sync.Mutex
		usedTargets = map[string]bool{}
		wg          sync.WaitGroup
	)

	jobs := make(chan int, len(paths))

	worker := func() {
		defer wg.Done()
		// 每个 goroutine 用独立的 localUsed，避免每条都抢全局锁；
		// 完成后一次性合并到 usedTargets。
		// 但 SafetyCheck 里的 usedTargets 检测必须看到全局视图，
		// 所以这里仍要传共享 map + 加锁。简化：直接共享 + 锁。
		for i := range jobs {
			src := paths[i]
			item, err := s.buildItemSafe(planID, src, style, customTpl, safeRoots, &mu, usedTargets)
			if err != nil {
				items[i] = model.RenamePlanItem{
					ID:         uuid.New().String(),
					PlanID:     planID,
					SourcePath: src,
					SourceName: filepath.Base(src),
					Status:     model.RenameItemStatusFailed,
					ErrorMsg:   err.Error(),
				}
				continue
			}
			items[i] = *item
		}
	}

	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go worker()
	}
	for i := range paths {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return items
}

// buildItemSafe 是 buildItem 的并发安全包装：替换原先直接传 usedTargets map 的方式，
// 由调用方持有锁；内部 buildItem 仍假设 usedTargets 是它独占的临时副本视图。
//
// 实现：在持锁状态下做"读快照 + 调用 buildItem + 合并写回"，保证目标占用判定一致。
func (s *SmartRenameService) buildItemSafe(
	planID, src, style, customTpl string,
	safeRoots []string,
	mu *sync.Mutex,
	usedTargets map[string]bool,
) (*model.RenamePlanItem, error) {
	// 复制一份 used 快照供 buildItem 检测（buildItem 只在内部 markUsed 时往里塞）
	mu.Lock()
	snapshot := make(map[string]bool, len(usedTargets))
	for k, v := range usedTargets {
		snapshot[k] = v
	}
	mu.Unlock()

	item, err := s.buildItem(planID, src, style, customTpl, safeRoots, snapshot)
	if err != nil {
		return nil, err
	}

	// 把 snapshot 中新增的目标占用合并回全局
	mu.Lock()
	for k := range snapshot {
		usedTargets[k] = true
	}
	mu.Unlock()

	return item, nil
}

// lookupMediaByPath 用源路径反查 Media（不强求一定能查到）。
//
// 之前使用 ListFilesAdvanced(keyword=src) 进行 LIKE %src% 模糊查询：
//   - 路径中的 `_` / `%` 会被 LIKE 当作通配符，造成误匹配；
//   - LIMIT 1 + ORDER BY created_at DESC 不一定是精确匹配者，造成漏命中。
//
// 现改用 MediaRepo.FindByFilePath 进行 file_path = ? 精确查询。
func (s *SmartRenameService) lookupMediaByPath(src string) (*model.Media, error) {
	if s.mediaRepo == nil {
		return nil, nil
	}
	m, err := s.mediaRepo.FindByFilePath(src)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

// extractSxxExx 从字符串中找出 SxxExx 集数。
//
// 之前使用 \b（word boundary）作为边界，但 Go 正则中 `_` 是 word char，
// 造成 `something_S01E01_other` 这类常见命名不能匹配。
// 现改用"非字母数字/起始结束"作为显式边界，同时接受 _/-/. /空格 作为可选分隔。
func extractSxxExx(s string) (int, int) {
	re := regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])S(\d{1,3})[._\s\-]?E(\d{1,3})(?:[^A-Za-z0-9]|$)`)
	m := re.FindStringSubmatch(s)
	if len(m) < 3 {
		return 0, 0
	}
	se, _ := strconv.Atoi(m[1])
	ep, _ := strconv.Atoi(m[2])
	return se, ep
}

// extractEpisodeNumber 仅识别"独立集号"（无 SxxExx 时的备选）。
//
// 覆盖国内动漫资源常见命名：
//   - "[ANi] XXX - 02 [1080P]..."
//   - "XXX 第08话 / EP08 / [08]"
//   - "Some Show 12.mkv"
//
// 仅返回正整数集号；返回 0 表示未识别。
func extractEpisodeNumber(s string) int {
	// 去扩展名
	stem := strings.TrimSuffix(s, filepath.Ext(s))
	patterns := []*regexp.Regexp{
		// 显式中/英集号关键词
		regexp.MustCompile(`(?i)(?:^|[\s\-_\[【])(?:EP|E|第)\s*0*(\d{1,4})\s*(?:话|集|话\b|集\b|话|集|\b|[\]\s\-_）】])`),
		// "title - 02 [..." 这种空格-数字-空格段（动漫主流命名）
		regexp.MustCompile(`(?:^|\s|\-)\s*\-\s*0*(\d{1,4})\s*(?:[\[\(\s]|$)`),
		// "[12]" 单独括号集号
		regexp.MustCompile(`[\[【]\s*0*(\d{1,4})\s*[\]】]`),
	}
	for _, re := range patterns {
		m := re.FindStringSubmatch(stem)
		if len(m) >= 2 {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n < 2000 {
				return n
			}
		}
	}
	return 0
}

func (s *SmartRenameService) renderTargetName(style, customTpl string, p ParsedFilename, item *model.RenamePlanItem) (string, error) {
	names := BuildStandardNames(StandardNameInput{
		SourcePath: item.SourcePath,
		SourceName: item.SourceName,
		MediaType:  item.MediaType,
		Title:      p.Title,
		Year:       p.Year,
		TMDbID:     p.TMDbID,
		IMDbID:     p.IMDbID,
		SeasonNum:  item.SeasonNum,
		EpisodeNum: item.EpisodeNum,
		IsPersonal: item.IsPersonal,
		Style:      style,
		CustomTpl:  customTpl,
	})

	// 同步回 item：剧集季尾缀剥离 / 季号兜底的结果反写，避免后续 deriveShowFolderName 再走一遍
	if strings.EqualFold(item.MediaType, "episode") {
		if names.EffectiveTitle != "" && names.EffectiveTitle != p.Title {
			item.ParsedTitle = names.EffectiveTitle
		}
		if names.EffectiveSeasonNum > 0 && item.SeasonNum != names.EffectiveSeasonNum {
			item.SeasonNum = names.EffectiveSeasonNum
		}
	}

	if names.FileName == "" {
		return "", fmt.Errorf("无法渲染目标文件名（mediaType=%s season=%d episode=%d title=%q）",
			item.MediaType, item.SeasonNum, item.EpisodeNum, p.Title)
	}
	return names.FileName, nil
}

// renderIDTag 按风格生成 ID 标签
func renderIDTag(style string, tmdbID int, imdbID string) string {
	if tmdbID == 0 && imdbID == "" {
		return ""
	}
	switch style {
	case NamingStylePlex:
		// Plex: {tmdb-12345} / {imdb-tt123}
		if tmdbID > 0 {
			return fmt.Sprintf(" {tmdb-%d}", tmdbID)
		}
		return fmt.Sprintf(" {imdb-%s}", imdbID)
	default:
		// Jellyfin/Emby: [tmdbid-12345] / [imdbid-tt123]
		if tmdbID > 0 {
			return fmt.Sprintf(" [tmdbid-%d]", tmdbID)
		}
		return fmt.Sprintf(" [imdbid-%s]", imdbID)
	}
}

// pathKey 返回一个能用于"同一规划内目标占用集合"的路径键。
//
// Windows / macOS 默认不区分大小写，这里走 ToLower；Linux 区分大小写，保留原值。
// 统一走 filepath.Clean，避免例如 `a//b` vs `a/b` 不一致。
func pathKey(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "linux" {
		return p
	}
	return strings.ToLower(p)
}

// pathEqual 判定两个路径是否指向同一位置（考虑平台大小写 + Clean）。
func pathEqual(a, b string) bool {
	return pathKey(a) == pathKey(b)
}

// sanitizeTitle 标题中的 NTFS/ext4 禁用字符替换为空格
func sanitizeTitle(s string) string {
	s = smartRenameUnsafeCharPattern.ReplaceAllString(s, " ")
	return collapseWhitespace(s)
}

// collapseWhitespace 把多空白合一并 trim
func collapseWhitespace(s string) string {
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
}

// ================================ P3: 关联资源 ===============================

// collectRelatedFiles 收集同名/同前缀的关联资源；返回明细 + 目标路径列表
func (s *SmartRenameService) collectRelatedFiles(srcVideo, targetVideo string) ([]SmartRenameRelatedFile, []string) {
	dir := filepath.Dir(srcVideo)
	srcBase := strings.TrimSuffix(filepath.Base(srcVideo), filepath.Ext(srcVideo))
	tgtBase := strings.TrimSuffix(filepath.Base(targetVideo), filepath.Ext(targetVideo))
	tgtDir := filepath.Dir(targetVideo)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}

	var related []SmartRenameRelatedFile
	var targets []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// 跳过自身
		if name == filepath.Base(srcVideo) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		stem := strings.TrimSuffix(name, filepath.Ext(name))

		// 1) 完全同名前缀（srcBase.ext / srcBase.zh.srt / srcBase-poster.jpg ...）
		if !strings.HasPrefix(stem, srcBase) {
			continue
		}
		suffix := stem[len(srcBase):] // 比如 "-poster" 或 ".zh"
		// 决定 kind
		kind, ok := smartRenameRelatedExts[ext]
		if !ok {
			continue
		}
		// 对于 image 类型，区分海报 / 背景 / 缩略
		if kind == "image" {
			// 去掉前导分隔符
			suffixCore := strings.TrimLeft(suffix, "-._")
			suffixCore = strings.ToLower(suffixCore)
			if k, ok2 := smartRenameImageSuffix[suffixCore]; ok2 {
				kind = k
			} else if suffixCore == "" {
				kind = "thumb" // 同名图（无后缀）
			} else {
				kind = "image"
			}
		}

		newName := tgtBase + suffix + ext
		newPath := filepath.Join(tgtDir, newName)
		related = append(related, SmartRenameRelatedFile{
			Source: filepath.Join(dir, name),
			Target: newPath,
			Kind:   kind,
		})
		targets = append(targets, newPath)
	}
	// 排序，便于前端展示稳定
	sort.SliceStable(related, func(i, j int) bool {
		return related[i].Source < related[j].Source
	})
	return related, targets
}

// ================================ P4: 安全检测 ===============================

// checkSafety 对源/目标进行安全审查
func (s *SmartRenameService) checkSafety(src, tgt string, allTargets, safeRoots []string, usedTargets map[string]bool) SmartRenameSafetyReport {
	report := SmartRenameSafetyReport{OK: true}

	// 1) 跨卷检测（Windows 看盘符；POSIX 看 device id）
	if isCrossVolume(src, tgt) {
		report.CrossVolume = true
		report.Issues = append(report.Issues, "源与目标位于不同卷/盘符")
	}

	// 2) 目标已存在
	for _, t := range allTargets {
		if pathEqual(t, src) {
			continue
		}
		if _, err := os.Stat(t); err == nil {
			report.TargetExists = true
			report.Issues = append(report.Issues, "目标已存在: "+filepath.Base(t))
			break
		}
		// 同一规划中目标已被占用
		if usedTargets[pathKey(t)] {
			report.TargetExists = true
			report.Issues = append(report.Issues, "目标与同一规划内其他条目冲突: "+filepath.Base(t))
			break
		}
	}

	// 3) 硬链接计数（POSIX）
	if hlc := getHardlinkCount(src); hlc > 1 {
		report.HardlinkCount = hlc
		report.Issues = append(report.Issues, fmt.Sprintf("源文件硬链接数=%d，重命名可能影响其他位置", hlc))
	}

	// 4) 安全根白名单
	if len(safeRoots) > 0 {
		ok := false
		for _, root := range safeRoots {
			absRoot, _ := filepath.Abs(root)
			absTgt, _ := filepath.Abs(tgt)
			if absRoot != "" && (strings.HasPrefix(strings.ToLower(absTgt), strings.ToLower(absRoot+string(os.PathSeparator))) ||
				strings.EqualFold(absRoot, absTgt)) {
				ok = true
				break
			}
		}
		if !ok {
			report.OutsideSafeRoot = true
			report.Issues = append(report.Issues, "目标位于安全根白名单之外")
		}
	}

	// 5) 磁盘空间（粗略：只在跨卷时检查；同卷重命名不消耗空间）
	if report.CrossVolume {
		if !hasEnoughSpace(filepath.Dir(tgt), getFileSize(src)) {
			report.NotEnoughSpace = true
			report.Issues = append(report.Issues, "目标卷可用空间不足")
		}
	}

	report.OK = len(report.Issues) == 0
	return report
}

// ================================ P5+P6: 执行（plan -> journal） ===============

// Execute 落盘执行（confirm=false 仅做 dry-run 校验，不真正动盘）
func (s *SmartRenameService) Execute(in ExecuteInput) (*model.RenamePlan, error) {
	plan, err := s.repo.GetPlanWithItems(in.PlanID)
	if err != nil {
		return nil, fmt.Errorf("规划不存在: %w", err)
	}
	if plan.Status != model.RenamePlanStatusDraft && plan.Status != model.RenamePlanStatusFailed {
		return nil, fmt.Errorf("规划状态不允许执行: %s", plan.Status)
	}

	// 强制 confirm
	if s.cfg.RequireConfirm && !in.Confirm {
		// dry-run：把 plan 状态保留 draft，但更新一次校验时间
		_ = s.repo.UpdatePlanFields(plan.ID, map[string]interface{}{
			"dry_run": true,
		})
		return plan, nil
	}

	// 标记执行中
	now := time.Now()
	_ = s.repo.UpdatePlanFields(plan.ID, map[string]interface{}{
		"status":      model.RenamePlanStatusExecuting,
		"dry_run":     false,
		"executed_at": &now,
	})

	// 过滤需要执行的条目
	itemFilter := map[string]bool{}
	for _, id := range in.ItemIDs {
		itemFilter[id] = true
	}

	executed := 0
	failed := 0
	executor := newRenameExecutor(s.repo, s.logger)

	for i := range plan.Items {
		it := &plan.Items[i]
		if len(itemFilter) > 0 && !itemFilter[it.ID] {
			continue
		}
		if it.Excluded {
			continue
		}
		if it.Status != model.RenameItemStatusPending && it.Status != model.RenameItemStatusFailed {
			continue
		}
		if !it.SafetyOK && !in.IgnoreSafety {
			continue
		}
		// 取 OverrideName 覆盖
		if it.OverrideName != "" {
			it.TargetName = it.OverrideName
			it.TargetPath = filepath.Join(filepath.Dir(it.SourcePath), it.OverrideName)
		}

		var related []SmartRenameRelatedFile
		if it.RelatedFilesJSON != "" {
			_ = json.Unmarshal([]byte(it.RelatedFilesJSON), &related)
		}

		if err := executor.executeItem(plan.ID, it, related); err != nil {
			failed++
			it.Status = model.RenameItemStatusFailed
			it.ErrorMsg = err.Error()
			_ = s.repo.UpdateItem(it)
			s.logger.Errorf("[SmartRename] 执行条目失败 plan=%s item=%s: %v", plan.ID, it.ID, err)
			continue
		}
		executed++
		it.Status = model.RenameItemStatusExecuted
		_ = s.repo.UpdateItem(it)
	}

	completedAt := time.Now()
	finalStatus := model.RenamePlanStatusCompleted
	if failed > 0 && executed == 0 {
		finalStatus = model.RenamePlanStatusFailed
	}
	_ = s.repo.UpdatePlanFields(plan.ID, map[string]interface{}{
		"status":         finalStatus,
		"executed_items": executed,
		"failed_items":   failed,
		"completed_at":   &completedAt,
	})
	s.logger.Infof("[SmartRename] 规划执行完成 plan=%s executed=%d failed=%d", plan.ID, executed, failed)

	return s.repo.GetPlanWithItems(plan.ID)
}

// Rollback 回滚一次规划（按 journal 倒序逆操作）
func (s *SmartRenameService) Rollback(planID string) (*model.RenamePlan, error) {
	plan, err := s.repo.GetPlanWithItems(planID)
	if err != nil {
		return nil, err
	}
	if plan.Status != model.RenamePlanStatusCompleted &&
		plan.Status != model.RenamePlanStatusFailed {
		return nil, fmt.Errorf("规划状态不可回滚: %s", plan.Status)
	}

	journals, err := s.repo.ListJournalByPlan(planID)
	if err != nil {
		return nil, err
	}
	executor := newRenameExecutor(s.repo, s.logger)
	if err := executor.rollback(journals); err != nil {
		return nil, err
	}

	// 把对应条目标记为 reverted
	for i := range plan.Items {
		it := &plan.Items[i]
		if it.Status == model.RenameItemStatusExecuted {
			it.Status = model.RenameItemStatusReverted
			_ = s.repo.UpdateItem(it)
		}
	}

	_ = s.repo.UpdatePlanFields(planID, map[string]interface{}{
		"status": model.RenamePlanStatusRolledBack,
	})
	return s.repo.GetPlanWithItems(planID)
}

// Cancel 取消（仅 draft 状态可取消）
func (s *SmartRenameService) Cancel(planID string) error {
	plan, err := s.repo.GetPlan(planID)
	if err != nil {
		return err
	}
	if plan.Status != model.RenamePlanStatusDraft {
		return fmt.Errorf("仅 draft 状态可取消，当前: %s", plan.Status)
	}
	return s.repo.UpdatePlanFields(planID, map[string]interface{}{
		"status": model.RenamePlanStatusCanceled,
	})
}

// UpdateItemOverride 用户修改单条目标名 / 排除标记
func (s *SmartRenameService) UpdateItemOverride(itemID, overrideName string, excluded *bool) (*model.RenamePlanItem, error) {
	it, err := s.repo.GetItem(itemID)
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{}
	if overrideName != "" {
		updates["override_name"] = overrideName
		updates["target_name"] = overrideName
		updates["target_path"] = filepath.Join(filepath.Dir(it.SourcePath), overrideName)
	}
	if excluded != nil {
		updates["excluded"] = *excluded
	}
	if len(updates) == 0 {
		return it, nil
	}
	if err := s.repo.UpdateItemFields(itemID, updates); err != nil {
		return nil, err
	}
	return s.repo.GetItem(itemID)
}

// ListPlans 列出规划（可按 LibraryID 过滤）
func (s *SmartRenameService) ListPlans(page, size int, libraryID string) ([]model.RenamePlan, int64, error) {
	return s.repo.ListPlansFiltered(libraryID, page, size)
}

// GetPlan 取详情
func (s *SmartRenameService) GetPlan(planID string) (*model.RenamePlan, error) {
	return s.repo.GetPlanWithItems(planID)
}

// DeletePlan 删除（仅非执行中）
func (s *SmartRenameService) DeletePlan(planID string) error {
	plan, err := s.repo.GetPlan(planID)
	if err != nil {
		return err
	}
	if plan.Status == model.RenamePlanStatusExecuting {
		return errors.New("执行中的规划不能删除")
	}
	return s.repo.DeletePlan(planID)
}
