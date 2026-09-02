package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fan-video/fan-video/internal/model"
)

func (s *ScannerService) scanMovieLibrary(library *model.Library) (int, error) {
	var count int
	var totalFiles int     // 遍历到的总文件数
	var videoFiles int     // 识别到的视频文件数
	var skippedExist int   // 已存在且未变更跳过的文件数
	var skippedUpdated int // 已存在但已更新的文件数

	// 增量扫描：获取上次扫描时间，仅处理新增/变更的文件
	lastScanTime := time.Time{}
	if library.LastScan != nil {
		lastScanTime = *library.LastScan
	}

	allPaths := library.AllPaths()
	s.logger.Infof("电影库扫描开始: %s, 路径: %v, 上次扫描: %v", library.Name, allPaths, lastScanTime)

	// P2: 文件路径预加载到内存 Set（避免 N+1 查询）
	existingPaths, err := s.mediaRepo.GetAllFilePathsByLibrary(library.ID)
	if err != nil {
		s.logger.Warnf("预加载文件路径失败，回退到逐个查询: %v", err)
		existingPaths = nil
	} else {
		s.logger.Infof("预加载 %d 个已有文件路径到内存", len(existingPaths))
	}

	// P2: 收集新发现的媒体文件，用于后续批量处理 FFprobe 和堆叠检测
	var pendingList []pendingMedia
	// 【火力全开 A】收集"已存在但需要更新"的文件，后续统一走 parallelProbe 并行探测
	// 避免 walkFn 内同步调用 FFprobe 拖慢整个遍历过程。
	var updateList []pendingMedia
	// 【火力全开 B】细粒度锁：walkFn 原先整体加锁串行，现在仅对共享容器写入加锁，
	// 把磁盘 IO（readdir）和 CPU 操作（标题解析/正则匹配）放在锁外并行。
	var collectMu sync.Mutex
	// existingPaths 并发读写也需要保护（来自多路径并行遍历场景）
	var existingMu sync.Mutex

	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			s.logger.Warnf("访问文件失败: %s, 错误: %v", path, err)
			return nil
		}
		if info.IsDir() {
			// 跳过 extras/trailers 等非正片目录（P0: 兼容 Emby 标准）
			if extrasExcludeDirs[strings.ToLower(filepath.Base(path))] {
				return filepath.SkipDir
			}
			return nil
		}
		// 计数类字段放到最后统一用细粒度锁保护，中间先做无锁过滤
		ext := strings.ToLower(filepath.Ext(path))
		if !supportedExts[ext] {
			collectMu.Lock()
			totalFiles++
			collectMu.Unlock()
			return nil
		}

		// P0: 文件大小过滤（启用 MinFileSize 配置）
		if s.skipUndersized(library, path, info.Size()) {
			collectMu.Lock()
			totalFiles++
			collectMu.Unlock()
			return nil
		}

		// P0: 排除 extras 路径和 Emby 特典后缀文件
		if isExtrasPath(path) || isExtrasFile(filepath.Base(path)) {
			s.logger.Debugf("跳过非正片内容: %s", path)
			collectMu.Lock()
			totalFiles++
			collectMu.Unlock()
			return nil
		}

		// P2: 内存查重（替代逐个 DB 查询）
		// 【火力全开 A】已存在文件的 probe 不再在 walkFn 里同步执行，
		// 而是收集到 updateList，待 walk 结束后与 pendingList 合并走 parallelProbe。
		if existingPaths != nil {
			existingMu.Lock()
			hit := existingPaths[path]
			if hit {
				delete(existingPaths, path)
			}
			existingMu.Unlock()
			if hit {
				// 文件已存在：增量扫描模式下，如果文件未修改则跳过
				if !lastScanTime.IsZero() && info.ModTime().Before(lastScanTime) {
					collectMu.Lock()
					totalFiles++
					videoFiles++
					skippedExist++
					collectMu.Unlock()
					return nil
				}
				// 文件已变更 → 先查 DB 记录，但不在这里 probe
				existing, findErr := s.mediaRepo.FindByFilePath(path)
				if findErr == nil && existing != nil {
					if s.deleteSourceDuplicateIfOrganized(library, existing, path, info) {
						collectMu.Lock()
						totalFiles++
						videoFiles++
						skippedExist++
						collectMu.Unlock()
						return nil
					}
					existing.FileSize = info.Size()
					collectMu.Lock()
					totalFiles++
					videoFiles++
					skippedUpdated++
					updateList = append(updateList, pendingMedia{media: existing, path: path, info: info})
					collectMu.Unlock()
				} else {
					collectMu.Lock()
					totalFiles++
					videoFiles++
					collectMu.Unlock()
				}
				return nil
			}
		} else {
			// 回退：逐个查询
			existing, findErr := s.mediaRepo.FindByFilePath(path)
			if findErr == nil && existing != nil {
				if s.deleteSourceDuplicateIfOrganized(library, existing, path, info) {
					collectMu.Lock()
					totalFiles++
					videoFiles++
					skippedExist++
					collectMu.Unlock()
					return nil
				}
				if !lastScanTime.IsZero() && info.ModTime().Before(lastScanTime) {
					collectMu.Lock()
					totalFiles++
					videoFiles++
					skippedExist++
					collectMu.Unlock()
					return nil
				}
				existing.FileSize = info.Size()
				collectMu.Lock()
				totalFiles++
				videoFiles++
				skippedUpdated++
				updateList = append(updateList, pendingMedia{media: existing, path: path, info: info})
				collectMu.Unlock()
				return nil
			}
		}

		if _, represented := s.findOrganizedHardlinkRecord(library, path, info); represented {
			collectMu.Lock()
			totalFiles++
			videoFiles++
			skippedExist++
			collectMu.Unlock()
			return nil
		}

		// P0: 增强的标题提取（含年份 + ID 标签解析）
		filename := filepath.Base(path)
		title, year, tmdbID := s.extractTitleEnhanced(filename)

		// 提取 IMDB ID 标签（如 [imdbid=tt1234567]）
		imdbID := ""
		idType, idValue := parseIDFromName(filepath.Base(path))
		if idType == "imdbid" || idType == "imdb" {
			imdbID = idValue
		}

		media := &model.Media{
			LibraryID:    library.ID,
			Title:        title,
			FilePath:     path,
			FileSize:     info.Size(),
			MediaType:    "movie",
			Year:         year,
			TMDbID:       tmdbID,
			IMDbID:       imdbID,
			ScrapeStatus: "pending",
		}

		// P2: 检测多 CD 堆叠
		stackBase, stackOrder := detectStacking(filename)
		if stackOrder > 0 {
			media.StackGroup = stackBase
			media.StackOrder = stackOrder
			s.logger.Debugf("检测到堆叠文件: %s (组=%s, 序号=%d)", filename, stackBase, stackOrder)
		}

		// P2: 检测多版本标识
		if versionTag := detectVersionTag(filename); versionTag != "" {
			media.VersionTag = versionTag
			s.logger.Debugf("检测到版本标识: %s -> %s", filename, versionTag)
		}

		// 收集到待处理列表（FFprobe 后续并行处理）
		collectMu.Lock()
		totalFiles++
		videoFiles++
		pendingList = append(pendingList, pendingMedia{media: media, path: path, info: info})
		collectMu.Unlock()
		return nil
	}

	// 【火力全开】多路径并行遍历：
	// 之前串行 for 导致多媒体库 / 多根目录只能一个个扫，
	// 遇到慢盘（网络挂载、WebDAV、外置 USB）会拖死整体进度。
	// 改为每条根路径一个 goroutine，IO 并行打满。
	if len(allPaths) <= 1 {
		for _, root := range allPaths {
			if walkErr := s.walkLibraryPath(root, walkFn); walkErr != nil {
				s.logger.Warnf("扫描路径失败: %s, 错误: %v", root, walkErr)
				err = walkErr
			}
		}
	} else {
		var (
			walkWg   sync.WaitGroup
			errMu    sync.Mutex // 仅保护 firstErr 写入（walkFn 自身已线程安全）
			firstErr error
		)
		// 【火力全开 B】walkFn 内部已使用细粒度锁（collectMu / existingMu）保护共享变量，
		// 这里不再用大锁包裹整个回调，磁盘 readdir 与文件处理在不同根路径间完全并行。
		for _, root := range allPaths {
			root := root
			walkWg.Add(1)
			go func() {
				defer walkWg.Done()
				if walkErr := s.walkLibraryPath(root, walkFn); walkErr != nil {
					s.logger.Warnf("扫描路径失败: %s, 错误: %v", root, walkErr)
					errMu.Lock()
					if firstErr == nil {
						firstErr = walkErr
					}
					errMu.Unlock()
				}
			}()
		}
		walkWg.Wait()
		if firstErr != nil {
			err = firstErr
		}
	}

	// P2: 并行 FFprobe 探测 + 批量入库
	// 【火力全开 A】已存在但需更新的文件(updateList) 也一并走并行 probe，
	// 与 pendingList 合并后只跑一次 Worker Pool，把 CPU 全部吃满。
	if len(pendingList) > 0 || len(updateList) > 0 {
		combined := make([]pendingMedia, 0, len(pendingList)+len(updateList))
		combined = append(combined, pendingList...)
		combined = append(combined, updateList...)
		s.logger.Infof("开始并行 FFprobe 探测 %d 个文件 (新增: %d, 更新: %d)",
			len(combined), len(pendingList), len(updateList))
		s.parallelProbe(combined)
	}

	// 【火力全开 A】处理已更新文件：probe 已在上面并行完成，这里只做字幕扫描 + DB 更新
	if len(updateList) > 0 {
		for _, pm := range updateList {
			s.scanExternalSubtitles(pm.media)

			// 回填缺失的海报/背景图：老库或此前匹配失败的记录，
			// 在重新扫描时补跑本地图片匹配（含视频首帧兜底）。
			// 旧版共享的通用命名封面也一并重算，保证每个视频独立海报。
			if pm.media.PosterPath == "" || s.nfoService.IsLegacySharedCover(pm.media.PosterPath) || pm.media.BackdropPath == "" {
				healPoster := pm.media.PosterPath != "" && s.nfoService.IsLegacySharedCover(pm.media.PosterPath)
				if poster, backdrop := s.nfoService.FindLocalImagesForMedia(pm.path); poster != "" || backdrop != "" {
					if poster != "" && (pm.media.PosterPath == "" || healPoster) {
						pm.media.PosterPath = poster
						s.logger.Debugf("回填本地海报: %s -> %s", pm.path, poster)
					}
					if backdrop != "" && pm.media.BackdropPath == "" {
						pm.media.BackdropPath = backdrop
						s.logger.Debugf("回填本地背景图: %s -> %s", pm.path, backdrop)
					}
				}
			}

			if err := s.mediaRepo.Update(pm.media); err != nil {
				s.logger.Warnf("更新媒体失败: %s, 错误: %v", pm.path, err)
				continue
			}
			s.logger.Debugf("更新已有媒体: %s", pm.path)
		}
	}

	if len(pendingList) > 0 {
		// 逐个入库（保留 NFO/图片扫描逻辑 + 事件广播）
		for _, pm := range pendingList {
			s.scanExternalSubtitles(pm.media)

			// 识别本地 NFO 信息文件并解析元数据
			if nfoPath := s.nfoService.FindNFOForMedia(pm.path); nfoPath != "" {
				if err := s.nfoService.ParseMovieNFO(nfoPath, pm.media); err != nil {
					s.logger.Debugf("解析NFO失败: %s, 错误: %v", nfoPath, err)
				} else {
					s.logger.Debugf("从NFO读取元数据: %s -> %s", nfoPath, pm.media.Title)
				}
			}

			// 识别本地海报封面图片（使用按文件名匹配的方法，避免同目录多视频共用封面）
			if poster, backdrop := s.nfoService.FindLocalImagesForMedia(pm.path); poster != "" || backdrop != "" {
				if poster != "" && pm.media.PosterPath == "" {
					pm.media.PosterPath = poster
					s.logger.Debugf("发现本地海报: %s", poster)
				}
				if backdrop != "" && pm.media.BackdropPath == "" {
					pm.media.BackdropPath = backdrop
					s.logger.Debugf("发现本地背景图: %s", backdrop)
				}
			}

			if err := s.mediaRepo.Create(pm.media); err != nil {
				s.logger.Warnf("保存媒体失败: %s, 错误: %v", pm.path, err)
				continue
			}
			count++
			s.logger.Infof("发现电影: %s [%s | %s | %s]", pm.media.Title, pm.media.Resolution, pm.media.VideoCodec, pm.media.AudioCodec)
			s.broadcastScanEvent(EventScanProgress, &ScanProgressData{
				LibraryID:   library.ID,
				LibraryName: library.Name,
				Phase:       "scanning",
				NewFound:    count,
				Message:     fmt.Sprintf("发现: %s [%s]", pm.media.Title, pm.media.Resolution),
			})
		}
	}

	// 清理失效记录：walk 正常完成后，existingPaths 里剩余的路径可能是磁盘已不存在的记录，
	// 也可能是 AI 整理后指向虚拟化/硬链接输出目录的记录。因此必须先 stat，不能直接删除。
	staleRemoved := 0
	if err == nil && existingPaths != nil && len(existingPaths) > 0 {
		for stalePath := range existingPaths {
			if _, statErr := s.statLibraryPath(stalePath); !os.IsNotExist(statErr) {
				continue
			}
			if m, findErr := s.mediaRepo.FindByFilePath(stalePath); findErr == nil && m != nil {
				if delErr := PurgeMediaCompletely(s.mediaRepo, s.cfg.Cache.CacheDir, s.logger, m, "扫描清理"); delErr != nil {
					continue
				}
				staleRemoved++
				s.logger.Infof("清理失效媒体记录及其关联数据（磁盘已不存在）: %s", stalePath)
			}
		}
	}

	s.logger.Infof("电影库扫描统计: %s — 遍历文件: %d, 视频文件: %d, 新增: %d, 已存在跳过: %d, 已更新: %d, 清理失效: %d (文件过滤: 开启=%v, 阈值=%dMB)",
		library.Name, totalFiles, videoFiles, count, skippedExist, skippedUpdated, staleRemoved, library.EnableFileFilter, library.MinFileSize)

	return count, err
}

// ==================== P2: 并行 FFprobe 探测 ====================

// pendingMedia 待处理的媒体文件信息（P2: 用于并行 FFprobe 和批量入库）
type pendingMedia struct {
	media *model.Media
	path  string
	info  os.FileInfo
}

// findOrganizedHardlinkRecord 判断当前源路径是否已经被 AI 整理后的硬链接记录代表。
// 当 Media.FilePath 已同步为 OrganizeOutputDir 下的路径时，后续扫描源目录不能再把源文件当新媒体入库。
func (s *ScannerService) deleteSourceDuplicateIfOrganized(library *model.Library, sourceMedia *model.Media, sourcePath string, sourceInfo os.FileInfo) bool {
	if sourceMedia == nil || sourceMedia.ID == "" {
		return false
	}
	represented, ok := s.findOrganizedHardlinkRecord(library, sourcePath, sourceInfo)
	if !ok || represented == nil || represented.ID == "" || represented.ID == sourceMedia.ID {
		return false
	}
	if err := s.mediaRepo.DeleteByID(sourceMedia.ID); err != nil {
		s.logger.Warnf("删除源目录重复媒体记录失败: %s, 错误: %v", sourcePath, err)
		return false
	}
	s.logger.Infof("源目录重复记录已由 AI 整理硬链接记录代表，已删除源记录: %s -> %s", sourcePath, represented.FilePath)
	return true
}

func (s *ScannerService) findOrganizedHardlinkRecord(library *model.Library, sourcePath string, sourceInfo os.FileInfo) (*model.Media, bool) {
	if library == nil || library.ID == "" || sourcePath == "" || sourceInfo == nil || sourceInfo.IsDir() || s.mediaRepo == nil {
		return nil, false
	}
	outputDir := strings.TrimSpace(library.OrganizeOutputDir)
	if outputDir == "" || IsWebDAVPath(sourcePath) || IsWebDAVPath(outputDir) {
		return nil, false
	}
	cleanSource := filepath.Clean(sourcePath)
	cleanOutput := filepath.Clean(outputDir)
	if isPathUnderRoot(cleanSource, cleanOutput) {
		return nil, false
	}

	candidates, err := s.mediaRepo.ListByLibraryAndFileSize(library.ID, sourceInfo.Size())
	if err != nil || len(candidates) == 0 {
		return nil, false
	}
	for i := range candidates {
		candidate := &candidates[i]
		candidatePath := strings.TrimSpace(candidate.FilePath)
		cleanCandidate := filepath.Clean(candidatePath)
		if candidatePath == "" || strings.EqualFold(cleanCandidate, cleanSource) || !isPathUnderRoot(cleanCandidate, cleanOutput) {
			continue
		}
		candidateInfo, statErr := os.Stat(candidatePath)
		if statErr != nil || candidateInfo.IsDir() {
			continue
		}
		if os.SameFile(sourceInfo, candidateInfo) {
			s.logger.Debugf("跳过源文件重复入库，已由 AI 整理硬链接记录代表: %s -> %s", sourcePath, candidatePath)
			return candidate, true
		}
	}
	return nil, false
}

// parallelProbe 使用 Worker Pool 并行执行 FFprobe 探测
func (s *ScannerService) parallelProbe(items []pendingMedia) {
	// 【火力全开】并发数 = NumCPU，让 FFprobe 用满所有 CPU 核心。
	// FFprobe 主要瓶颈是磁盘 IO 与容器解析，单实例占用很低，
	// 多开不会压爆 CPU，反而能把 NVMe/SSD 的 IO 并发打满。
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}

	type probeJob struct {
		index int
	}

	jobs := make(chan probeJob, len(items))
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				s.probeMediaInfo(items[job.index].media)
			}
		}()
	}

	for i := range items {
		jobs <- probeJob{index: i}
	}
	close(jobs)
	wg.Wait()
}

// ==================== P2: 多 CD 堆叠检测 ====================

// detectStacking 检测文件名中的多 CD/多分卷标识
// 返回: (去除堆叠后缀的基础名, 堆叠序号)，序号为 0 表示非堆叠文件
func detectStacking(filename string) (baseName string, order int) {
	nameWithoutExt := strings.TrimSuffix(filename, filepath.Ext(filename))
	for _, pattern := range stackingPatterns {
		if m := pattern.FindStringSubmatchIndex(nameWithoutExt); m != nil {
			// 提取序号
			orderStr := nameWithoutExt[m[4]:m[5]]
			// 字母序号转数字: a=1, b=2, c=3, d=4
			if len(orderStr) == 1 && orderStr[0] >= 'a' && orderStr[0] <= 'd' {
				order = int(orderStr[0]-'a') + 1
			} else {
				order, _ = strconv.Atoi(orderStr)
			}
			if order > 0 {
				// 基础名 = 去除堆叠标识的部分
				baseName = strings.TrimSpace(nameWithoutExt[:m[0]])
				return baseName, order
			}
		}
	}
	return "", 0
}

// detectVersionTag 检测文件名中的版本标识（Director's Cut, Extended 等）
func detectVersionTag(filename string) string {
	nameWithoutExt := strings.TrimSuffix(filename, filepath.Ext(filename))
	if m := versionPatterns[0].FindStringSubmatch(nameWithoutExt); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// scanMixedLibrary
// scanMixedLibrary 扫描混合媒体库（智能区分电影和电视剧）
// 策略：遍历根目录第一层，对每个子目录判断是电影还是电视剧文件夹
// - 如果子目录内包含多个视频文件，或文件名匹配剧集命名模式，则视为电视剧
// - 如果子目录内只有单个视频文件且不匹配剧集模式，则视为电影
// - 根目录下的散落视频文件按电影处理
