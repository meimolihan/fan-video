package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

type ScannerService struct {
	mediaRepo      *repository.MediaRepo
	seriesRepo     *repository.SeriesRepo
	cfg            *config.Config
	logger         *zap.SugaredLogger
	wsHub          *WSHub                 // WebSocket事件广播
	nfoService     *NFOService            // NFO 本地元数据解析服务
	onScanComplete func(libraryID string) // 扫描完成回调（用于触发预处理）
	vfsMgr         *VFSManager            // V2.1: VFS 管理器（支持 webdav:// 路径）
}

func NewScannerService(mediaRepo *repository.MediaRepo, seriesRepo *repository.SeriesRepo, cfg *config.Config, logger *zap.SugaredLogger) *ScannerService {
	return &ScannerService{
		mediaRepo:  mediaRepo,
		seriesRepo: seriesRepo,
		cfg:        cfg,
		logger:     logger,
		nfoService: NewNFOService(logger, cfg),
	}
}

// SetWSHub 设置WebSocket Hub（延迟注入，避免循环依赖）
func (s *ScannerService) SetWSHub(hub *WSHub) {
	s.wsHub = hub
}

// isFileTooSmall 判断文件是否命中媒体库「最小文件大小」过滤规则。
// .strm 是纯文本远程流索引，大小与内容无关，始终豁免。
func isFileTooSmall(library *model.Library, path string, size int64) bool {
	if library == nil || !library.EnableFileFilter || library.MinFileSize <= 0 {
		return false
	}
	if strings.EqualFold(filepath.Ext(path), ".strm") {
		return false
	}
	return size < int64(library.MinFileSize)*1024*1024
}

// skipUndersized 命中大小过滤时记录日志并返回 true（调用方应跳过该文件）。
func (s *ScannerService) skipUndersized(library *model.Library, path string, size int64) bool {
	if !isFileTooSmall(library, path, size) {
		return false
	}
	s.logger.Infof("文件过滤: 跳过过小文件(%.2fMB < %dMB): %s",
		float64(size)/(1024*1024), library.MinFileSize, path)
	return true
}

func isFileFilterActive(library *model.Library) bool {
	return library != nil && library.EnableFileFilter && library.MinFileSize > 0
}

// purgeUndersizedMedia 清除库内低于大小阈值的存量媒体记录及其关联数据。
// 覆盖所有入库管线（电影/混合/电视剧/批量导入等）的历史遗留数据；
// FileSize 未知(0)的记录不动。返回清除数量。
func (s *ScannerService) purgeUndersizedMedia(library *model.Library) int {
	if !isFileFilterActive(library) {
		return 0
	}
	minBytes := int64(library.MinFileSize) * 1024 * 1024
	medias, err := s.mediaRepo.ListByLibraryID(library.ID)
	if err != nil {
		s.logger.Warnf("过小清理: 读取媒体库记录失败: %v", err)
		return 0
	}
	removed := 0
	for i := range medias {
		m := &medias[i]
		if m.FileSize <= 0 || isSTRMFile(m.FilePath) || m.FileSize >= minBytes {
			continue
		}
		if delErr := PurgeMediaCompletely(s.mediaRepo, s.cfg.Cache.CacheDir, s.logger, m, "过小文件过滤"); delErr != nil {
			s.logger.Warnf("过小清理: 清除记录失败 %s: %v", m.FilePath, delErr)
			continue
		}
		s.logger.Infof("文件过滤: 清除存量过小记录(%.2fMB < %dMB): %s",
			float64(m.FileSize)/(1024*1024), library.MinFileSize, m.FilePath)
		removed++
	}
	return removed
}

// SetVFSManager 设置 VFS 管理器（V2.1: 用于 webdav:// 路径支持）
func (s *ScannerService) SetVFSManager(vfsMgr *VFSManager) {
	s.vfsMgr = vfsMgr
}

// walkLibraryPath 根据媒体库路径前缀自动选择 VFS 遍历
// 返回的 path 是完整路径（LocalFS 返回原生路径；WebDAVFS 返回 webdav:// 前缀路径）
func (s *ScannerService) walkLibraryPath(root string, fn filepath.WalkFunc) error {
	if s.vfsMgr != nil && IsWebDAVPath(root) {
		return s.vfsMgr.Walk(root, fn)
	}
	return filepath.Walk(root, fn)
}

// statLibraryPath 根据路径前缀选择合适的 Stat 实现
func (s *ScannerService) statLibraryPath(p string) (os.FileInfo, error) {
	if s.vfsMgr != nil && IsWebDAVPath(p) {
		return s.vfsMgr.Stat(p)
	}
	return os.Stat(p)
}

// readDirLibraryPath 根据路径前缀选择合适的 ReadDir 实现
func (s *ScannerService) readDirLibraryPath(p string) ([]os.DirEntry, error) {
	if s.vfsMgr != nil && IsWebDAVPath(p) {
		entries, err := s.vfsMgr.ReadDir(p)
		if err != nil {
			return nil, err
		}
		// fs.DirEntry 与 os.DirEntry 在 Go 1.16+ 其实是同一接口别名
		result := make([]os.DirEntry, len(entries))
		copy(result, entries)
		return result, nil
	}
	return os.ReadDir(p)
}

// vfsJoin 拼接路径：对 webdav:// 前缀的路径使用正斜杠，避免 filepath.Join 在 Windows 下把前缀破坏为 webdav:\
func vfsJoin(base, name string) string {
	if IsWebDAVPath(base) {
		base = strings.TrimRight(base, "/")
		return base + "/" + strings.TrimLeft(name, "/")
	}
	return filepath.Join(base, name)
}

// collectMediaRoots 将媒体库根路径展开为一个或多个"真实媒体根目录"列表
//
// 适配 xiaoya/小雅 这种多级分类结构（如 xiaoya/115/电视剧/xxx）：
//   - 若 root 本身或其子目录是已知分类目录（xiaoyaCategoryDirs）且不包含直接视频文件，
//     则递归穿透；
//   - 若目录名命中 xiaoyaSkipDirs 或 extrasExcludeDirs，则直接跳过；
//   - 最多穿透 maxDepth 层，防止无限递归；
//   - 如果没有任何分类目录命中，直接返回 [root]（完全向后兼容）。
//
// kind 仅影响日志打印，不影响展开规则。
// expandedMediaRoot 展开后的真实媒体根目录及其来源深度
type expandedMediaRoot struct {
	Path string // 完整路径
	// Depth 为穿透深度：0 表示用户配置的库根自身；>0 表示由分类目录穿透展开而来。
	// 穿透而来的内容目录（depth>0）语义上更接近"真实内容文件夹"，
	// 可用于更激进的归类策略（如同目录多视频归组为一部剧集）。
	Depth int
}

func (s *ScannerService) collectMediaRoots(root string, kind string) []string {
	infos := s.collectMediaRootInfos(root, kind)
	paths := make([]string, len(infos))
	for i := range infos {
		paths[i] = infos[i].Path
	}
	return paths
}

// collectMediaRootInfos 与 collectMediaRoots 相同，但额外返回每个媒体根的穿透深度
func (s *ScannerService) collectMediaRootInfos(root string, kind string) []expandedMediaRoot {
	const maxDepth = 4
	var results []expandedMediaRoot
	seen := make(map[string]bool)
	tvOnly := kind == "tvshow"

	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		if seen[path] {
			return
		}
		seen[path] = true

		base := filepath.Base(path)
		if isXiaoyaSkipDir(base) || extrasExcludeDirs[strings.ToLower(base)] {
			s.logger.Debugf("[xiaoya] 跳过特殊目录: %s", path)
			return
		}
		// [tvshow 扫描] 直接过滤掉"综艺/演唱会/音乐/MV/每日更新"等非剧集分类
		// depth>0 时才跳过；depth==0 即用户把库根直接指到了这种目录，维持原行为（返回自身），由上层语义决定
		if tvOnly && depth > 0 && isNonTVCategoryDirName(base) {
			s.logger.Infof("[xiaoya][tvshow] 跳过非剧集分类目录: %s", path)
			return
		}

		// depth==0 即 root 本身，不论是否命中分类名都要尝试穿透；
		// depth>0 时只有命中分类白名单 或 目录内没视频才穿透
		entries, err := s.readDirLibraryPath(path)
		if err != nil {
			// 无法读取的目录保守当做普通媒体根加入
			results = append(results, expandedMediaRoot{Path: path, Depth: depth})
			return
		}

		var hasVideoFile bool
		var subDirs []os.DirEntry
		for _, e := range entries {
			if e.IsDir() {
				subDirs = append(subDirs, e)
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if supportedExts[ext] {
				hasVideoFile = true
			}
			// 已有 NFO 也视为"当前目录已是媒体根"
			lower := strings.ToLower(e.Name())
			if lower == "tvshow.nfo" || lower == "movie.nfo" {
				hasVideoFile = true
			}
		}

		// 已是真实媒体目录 —— 不再下钻
		if hasVideoFile {
			results = append(results, expandedMediaRoot{Path: path, Depth: depth})
			return
		}

		// 分类目录识别：
		// - depth == 0：默认尝试穿透（用户把库根设在 xiaoya 上层也能工作）；
		//   但若子目录已经是"标准剧集合集结构"（含 Season XX 子目录或 tvshow.nfo 或视频），
		//   则 root 本身就是真实媒体根，绝不下钻——否则会把每个"剧集名目录"当作 root，
		//   再把 Season XX 当成系列名，造成重大归类错乱（已发生过 bug）。
		// - depth >  0：仅当目录名命中分类白名单 或 子目录数 >= 3（无视频+多子目录特征）
		shouldExpand := depth == 0
		if depth == 0 && len(subDirs) > 0 {
			// [关键防御] 如果存在任何"已知分类目录"子目录（如 Movies / TV Shows / 电影 / 电视剧 / _unsorted），
			// 必须下钻。这种结构下，root 自身绝不能被当作媒体根，否则会把分类目录当成"一部剧"。
			hasCategorySub := false
			for _, sd := range subDirs {
				if isCategoryDirName(sd.Name()) {
					hasCategorySub = true
					break
				}
			}
			if hasCategorySub {
				s.logger.Infof("[xiaoya] 检测到 %s 下存在标准分类目录子项（如 Movies/TV Shows/电影/电视剧），强制下钻", path)
				// 跳过下面的"抽样判断"，直接进入展开流程
			} else {
				// 抽样检查前若干个子目录，只要有一个看起来像剧集合集，就把 root 自身当媒体根
				sampleN := len(subDirs)
				if sampleN > 8 {
					sampleN = 8
				}
				for i := 0; i < sampleN; i++ {
					sd := subDirs[i]
					if isXiaoyaSkipDir(sd.Name()) || extrasExcludeDirs[strings.ToLower(sd.Name())] {
						continue
					}
					childPath := vfsJoin(path, sd.Name())
					if s.looksLikeSeriesFolder(childPath) {
						s.logger.Infof("[xiaoya][tvshow] 检测到 %s 是标准剧集库根（子目录 %s 含季号/视频/NFO），不下钻", path, sd.Name())
						results = append(results, expandedMediaRoot{Path: path, Depth: depth})
						return
					}
				}
			}
		}
		if !shouldExpand {
			if isCategoryDirName(base) {
				shouldExpand = true
			} else if len(subDirs) >= 3 && !hasVideoFile {
				// 兜底启发式：无视频 + 多子目录，很可能也是分类目录
				shouldExpand = true
			}
		}

		// [嵌套单视频包装层] 目录自身无视频、且各内容子目录各自只含一个视频
		// （如 MD/Nina/Nina-01/Nina-01.mp4）。这不是分类层——继续下钻会把每个叶子
		// 拆成散落的独立电影；应把当前目录整体作为一个媒体根，
		// 由扫描阶段的同目录归组逻辑收编为一部剧集。
		if !hasVideoFile && depth > 0 && len(subDirs) >= 2 && s.isNestedSingleVideoCollection(path, subDirs) {
			s.logger.Debugf("[xiaoya] %s 为嵌套单视频集合，不再下钻", path)
			results = append(results, expandedMediaRoot{Path: path, Depth: depth})
			return
		}

		if !shouldExpand || depth >= maxDepth {
			results = append(results, expandedMediaRoot{Path: path, Depth: depth})
			return
		}

		// 穿透：把每个子目录递归展开
		expandedCount := 0
		for _, sd := range subDirs {
			if isXiaoyaSkipDir(sd.Name()) || extrasExcludeDirs[strings.ToLower(sd.Name())] {
				continue
			}
			// [tvshow 扫描] 子目录层级过滤非剧集分类（综艺/演唱会/音乐/MV/每日更新）
			if tvOnly && isNonTVCategoryDirName(sd.Name()) {
				s.logger.Infof("[xiaoya][tvshow] 跳过非剧集分类子目录: %s/%s", path, sd.Name())
				continue
			}
			childPath := vfsJoin(path, sd.Name())
			before := len(results)
			walk(childPath, depth+1)
			if len(results) > before {
				expandedCount++
			}
		}

		// 如果穿透后一个结果都没有（极端情况），保底把当前目录加入
		if expandedCount == 0 {
			results = append(results, expandedMediaRoot{Path: path, Depth: depth})
		}
	}

	walk(root, 0)

	// 去重并保持顺序
	uniq := make([]expandedMediaRoot, 0, len(results))
	dedup := make(map[string]bool)
	for _, r := range results {
		if dedup[r.Path] {
			continue
		}
		dedup[r.Path] = true
		uniq = append(uniq, r)
	}

	if len(uniq) > 1 {
		s.logger.Infof("[xiaoya] %s 库多级分类展开: %s → 共 %d 个媒体根目录", kind, root, len(uniq))
	}
	return uniq
}

// SetOnScanComplete 设置扫描完成回调（用于触发视频预处理）
func (s *ScannerService) SetOnScanComplete(fn func(libraryID string)) {
	s.onScanComplete = fn
}

type ScanLibraryOptions struct {
	// SuppressCompletedEvent 用于多阶段扫描链路，避免文件扫描完成时提前通知前端“全部完成”。
	SuppressCompletedEvent bool
	// SuppressCompletionCallback 用于把 onScanComplete 延后到多阶段流程真正结束后执行。
	SuppressCompletionCallback bool
}

// ScanLibrary 扫描媒体库目录
func (s *ScannerService) ScanLibrary(library *model.Library) (int, error) {
	return s.ScanLibraryWithOptions(library, ScanLibraryOptions{})
}

// NotifyScanComplete 触发扫描完成回调。多阶段流程可在真正完成后手动调用。
func (s *ScannerService) NotifyScanComplete(libraryID string) {
	if s.onScanComplete != nil {
		go s.onScanComplete(libraryID)
	}
}

// ScanLibraryWithOptions 扫描媒体库目录，可由多阶段调用方控制是否广播最终扫描完成事件。
func (s *ScannerService) ScanLibraryWithOptions(library *model.Library, opts ScanLibraryOptions) (int, error) {
	s.logger.Infof("开始扫描媒体库: %s (路径数: %d)", library.Name, len(library.AllPaths()))

	// 发送扫描开始事件
	s.broadcastScanEvent(EventScanStarted, &ScanProgressData{
		LibraryID:   library.ID,
		LibraryName: library.Name,
		Phase:       "scanning",
		Message:     fmt.Sprintf("开始扫描媒体库: %s", library.Name),
	})

	// 根据媒体库类型采用不同的扫描策略
	var count int
	var err error

	switch library.Type {
	case "tvshow":
		count, err = s.scanTVShowLibrary(library)
	case "mixed":
		count, err = s.scanMixedLibrary(library)
	default:
		count, err = s.scanMovieLibrary(library)
	}

	// 全局存量清扫：清除所有入库管线（电影/混合/电视剧等）遗留的过小媒体记录
	sizeRemoved := 0
	if err == nil {
		sizeRemoved = s.purgeUndersizedMedia(library)
	}

	// 首帧封面修复：对当前仍以「首帧图片」作为海报的视频，检查视频目录是否已新增
	// 真实海报；若两者同时匹配，则删除首帧封面并把海报更新为目录海报。
	// 该修复在扫描完成后统一执行，确保增量扫描跳过未改动文件时也能生效。
	if err == nil {
		s.healFirstFramePosters(library)
	}
	// 首帧缓存垃圾回收：清理已被真实海报替换后遗留的孤儿首帧文件。
	// 与 healFirstFramePosters 不同，这里按全体媒体/剧集引用做全局比对，
	// 不依赖键匹配或目录海报匹配规则，能可靠删除任何残留首帧缓存。
	if err == nil {
		s.firstFrameCacheGC()
	}

	if err != nil {
		s.broadcastScanEvent(EventScanFailed, &ScanProgressData{
			LibraryID:   library.ID,
			LibraryName: library.Name,
			Phase:       "scanning",
			NewFound:    count,
			Message:     fmt.Sprintf("扫描出错: %v", err),
		})
	} else if !opts.SuppressCompletedEvent {
		s.broadcastScanEvent(EventScanCompleted, &ScanProgressData{
			LibraryID:   library.ID,
			LibraryName: library.Name,
			Phase:       "scanning",
			NewFound:    count,
			Message:     fmt.Sprintf("扫描完成: %s, 新增 %d 个媒体", library.Name, count),
		})
	}

	s.logger.Infof("扫描完成: %s, 新增 %d 个媒体, 过小清除 %d", library.Name, count, sizeRemoved)

	// 触发预处理回调（如果已配置）
	if !opts.SuppressCompletionCallback {
		s.NotifyScanComplete(library.ID)
	}

	return count, err
}

// healFirstFramePosters 在扫描完成后统一修复「首帧封面」：
// 对当前仍以首帧图片作为海报的本地视频，重新匹配视频目录海报。
// 一旦目录中匹配到真实海报（非首帧），就删除首帧封面文件并把海报更新为目录海报。
// 该遍历仅覆盖仍持有首帧封面的媒体（数量有限），不影响其余海报。
func (s *ScannerService) healFirstFramePosters(library *model.Library) {
	if s.nfoService == nil || library == nil {
		return
	}
	mediaList, err := s.mediaRepo.ListByLibraryID(library.ID)
	if err != nil {
		s.logger.Warnf("加载媒体库海报修复列表失败: %v", err)
		return
	}

	healed := 0
	deletedFiles := 0
	for i := range mediaList {
		m := &mediaList[i]
		// 仅处理本地视频
		if m.FilePath == "" || m.StreamURL != "" || IsWebDAVPath(m.FilePath) {
			continue
		}
		// 目录海报匹配（无目录海报时函数内部以「首帧兜底」返回）
		poster, backdrop := s.nfoService.FindLocalImagesForMedia(m.FilePath)
		// 仅当目录中匹配到真实海报（非首帧）时才处理；否则首帧仍是合法兜底
		if poster == "" || s.nfoService.IsFirstFrameCachePath(poster) {
			continue
		}
		oldPoster := m.PosterPath
		if oldPoster != poster && s.nfoService.IsFirstFrameCachePath(oldPoster) {
			m.PosterPath = poster
			if backdrop != "" && m.BackdropPath == "" {
				m.BackdropPath = backdrop
			}
			if err := s.mediaRepo.Update(m); err != nil {
				s.logger.Warnf("更新目录海报失败 media=%s: %v", m.ID, err)
			} else {
				healed++
				s.logger.Debugf("首帧封面替换为目录海报 media=%s: %s", m.ID, poster)
			}
		}
		// 无论数据库是否仍指向首帧，都清理该视频遗留的首帧缓存文件（含孤儿文件）
		deletedFiles += s.deleteVideoFirstFrame(m, oldPoster)
	}
	if healed > 0 {
		s.logger.Infof("扫描后首帧封面修复: %d 个媒体已改用目录海报", healed)
	}
	if deletedFiles > 0 {
		s.logger.Infof("扫描后清理首帧封面缓存: 删除 %d 个文件", deletedFiles)
	}
}

// deleteVideoFirstFrame 删除视频的首帧封面缓存文件。
// 同时清理数据库中记录的首帧路径与按当前视频身份推导出的缓存路径，
// 以覆盖数据库路径过期/不一致（孤儿首帧被替换后残留）的情况。
// 返回实际删除的文件数。
func (s *ScannerService) deleteVideoFirstFrame(m *model.Media, storedPath string) int {
	targets := map[string]struct{}{}
	if storedPath != "" {
		targets[storedPath] = struct{}{}
	}
	if m != nil && m.FilePath != "" && !IsWebDAVPath(m.FilePath) {
		if info, err := os.Stat(m.FilePath); err == nil {
			key := firstFrameCacheKey(m.FilePath, info)
			targets[filepath.Join(s.nfoService.firstFrameCacheDir(), key+".jpg")] = struct{}{}
		}
	}
	deleted := 0
	for path := range targets {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil {
			if !os.IsNotExist(err) {
				s.logger.Warnf("删除首帧封面失败: %s: %v", path, err)
			}
			continue
		}
		deleted++
		s.logger.Debugf("已删除首帧封面: %s", path)
	}
	return deleted
}

// firstFrameCacheGC 首帧封面缓存垃圾回收：删除未被任何媒体/剧集引用的孤儿首帧缓存文件。
// 以「全体媒体与剧集当前海报/背景图引用」为准做全局比对，不依赖键匹配或目录海报匹配规则，
// 因此无论首帧因何被替换（扫描、手动上传、剧集借用等）都能可靠清理残留文件。
func (s *ScannerService) firstFrameCacheGC() {
	if s.nfoService == nil {
		return
	}
	cacheDir := s.nfoService.firstFrameCacheDir()
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return // 目录不存在或不可读时无需处理
	}

	referenced := map[string]bool{}
	collect := func(paths ...string) {
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p == "" || !s.nfoService.IsFirstFrameCachePath(p) {
				continue
			}
			if abs, aerr := filepath.Abs(p); aerr == nil {
				referenced[abs] = true
			}
		}
	}
	if media, merr := s.mediaRepo.ListAllImagePaths(); merr == nil {
		for i := range media {
			collect(media[i].PosterPath, media[i].BackdropPath)
		}
	}
	if series, serr := s.seriesRepo.ListAllImagePaths(); serr == nil {
		for i := range series {
			collect(series[i].PosterPath, series[i].BackdropPath)
		}
	}

	deleted := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".jpg") || strings.Contains(name, ".tmp.") {
			continue
		}
		full := filepath.Join(cacheDir, name)
		ref, aerr := filepath.Abs(full)
		if aerr != nil {
			continue
		}
		if referenced[ref] {
			continue
		}
		if err := os.Remove(full); err != nil {
			if !os.IsNotExist(err) {
				s.logger.Warnf("清理孤儿首帧缓存失败: %s: %v", full, err)
			}
			continue
		}
		deleted++
		s.logger.Debugf("已清理孤儿首帧缓存: %s", full)
	}
	if deleted > 0 {
		s.logger.Infof("首帧缓存清理完成: 删除 %d 个孤儿文件", deleted)
	}
}

// scanMovieLibrary 扫描电影库（支持增量扫描 + P2 性能优化）
func (s *ScannerService) broadcastScanEvent(eventType string, data *ScanProgressData) {
	if s.wsHub != nil {
		s.wsHub.BroadcastEvent(eventType, data)
	}
}

// ProbeMediaInfo 公开的 FFprobe 媒体信息探测方法（供外部服务调用）
func GetFileExt(path string) string {
	return strings.ToLower(filepath.Ext(path))
}

// ==================== P0: 批量字幕提取导出 ====================

// ExtractedSubtitleFile 提取后的字幕文件信息
