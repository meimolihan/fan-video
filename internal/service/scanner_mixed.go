package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/fan-video/fan-video/internal/model"
)

func (s *ScannerService) scanMixedLibrary(library *model.Library) (int, error) {
	allPaths := library.AllPaths()
	s.logger.Infof("混合媒体库扫描: %s (路径数: %d)", library.Name, len(allPaths))

	// [xiaoya 适配] 将所有媒体库根展开为平铺的真实媒体根集合（保留穿透深度）
	var mediaRoots []expandedMediaRoot
	for _, p := range allPaths {
		mediaRoots = append(mediaRoots, s.collectMediaRootInfos(p, "mixed")...)
	}

	var totalCount int
	// === 阶段一：收集子目录，按标准化系列名分组（用于多季合并检测） ===
	seriesDirGroups := make(map[string][]seriesFolder) // 标准化系列名 -> 目录列表
	type movieDirEntry struct {
		entry    os.DirEntry
		rootPath string
	}
	type looseEntry struct {
		entry    os.DirEntry
		rootPath string
	}
	var movieDirs []movieDirEntry    // 被判定为电影的目录
	var looseVideoFiles []looseEntry // 根目录散落的视频文件

	for _, mr := range mediaRoots {
		root := mr.Path
		entries, err := s.readDirLibraryPath(root)
		if err != nil {
			s.logger.Warnf("读取混合库根目录失败: %s, 错误: %v", root, err)
			continue
		}
		s.logger.Infof("混合库根 %s 包含 %d 个条目", root, len(entries))

		// [关键判断] mediaRoot 自身是否就是一个"剧集名目录"
		// 当 collectMediaRoots 把游标停在剧集名目录这一层（如 "TV Shows\\2.5次元的诱惑"）时，
		// 它的子目录全是 Season XX，不能再当作"分类目录"用 normalizeSeriesName 来分组——
		// 否则所有剧集都会因子目录名相同（"Season 01"）合并到一个空 key 里造成大灾难。
		// 这里直接把 mediaRoot 自身作为 series 目录处理，dirName 用 mediaRoot 的 basename。
		rootIsSeriesFolder := false
		seasonChildCount := 0
		nonSeasonChildCount := 0
		for _, e := range entries {
			if e.IsDir() {
				if isSeasonOnlyDirName(e.Name()) {
					seasonChildCount++
				} else if !isXiaoyaSkipDir(e.Name()) && !extrasExcludeDirs[strings.ToLower(e.Name())] {
					nonSeasonChildCount++
				}
			}
		}
		if seasonChildCount > 0 && nonSeasonChildCount == 0 {
			rootIsSeriesFolder = true
		}

		// [同目录归组] 穿透而来的内容根（depth>0）若包含多个视频，整体归组为一部剧集；
		// 库根自身（depth==0）的散落视频保持独立电影，避免整堆电影被误归为一部剧

		// [关键修正] isTVShowFolder 内部的 collectSeriesEvidence 会深入一层子目录统计视频，
		// 若直接用于库根，「影视库/剧名/SxxExx.mp4」这类经典结构会把整个媒体根误判为
		// 一部以库根目录名命名的剧集，所有剧的分集全部折叠进同一条记录。
		// 因此：
		//   - depth>0（穿透到剧集目录内部）：保留 isTVShowFolder 判定；
		//   - depth==0（用户配置的库根）：只有根目录【直属】视频命中剧集命名时才整体视为一部剧，
		//     子目录中的分集证据交给下方逐目录分类逻辑处理。
		rootHasSeriesEvidence := false
		if mr.Depth > 0 {
			rootHasSeriesEvidence = s.isTVShowFolder(root)
		} else {
			// depth==0：只有当根目录【没有】正常内容子目录、且直属视频命中剧集命名时，
			// 才整体视为一部剧（用户把库路径直接指向某部剧目录的情况）。
			// 否则哪怕存在一个日期命名的散落文件，也会把整库误折叠成一部剧。
			hasNormalSubDir := false
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				name := e.Name()
				if isSeasonOnlyDirName(name) || isXiaoyaSkipDir(name) || extrasExcludeDirs[strings.ToLower(name)] {
					continue
				}
				hasNormalSubDir = true
				break
			}
			if !hasNormalSubDir {
				for _, e := range entries {
					if !e.IsDir() && supportedExts[strings.ToLower(filepath.Ext(e.Name()))] && s.parseEpisodeInfo(e.Name()).EpisodeNum > 0 {
						rootHasSeriesEvidence = true
						break
					}
				}
			}
		}
		// [分类层保护] 媒体根自身有直属视频、且某个内容子目录的视频数量【超过】根直属视频数时
		// （如分类目录下既有少量散落电影又有大型剧集文件夹），不能把整根折叠成一部剧——
		// 否则子剧集的身份会被吞掉。此时改为逐子目录归类，直属视频按独立电影处理。
		// 注意：子目录视频数不超过直属视频数时不触发（如 剧名/压缩/ 变体目录应随主目录归组）；
		// 无直属视频的包装层（嵌套单视频集合）与季目录结构分别由其他分支处理，不受此保护影响。
		directVideoCount := 0
		for _, e := range entries {
			if !e.IsDir() && supportedExts[strings.ToLower(filepath.Ext(e.Name()))] {
				directVideoCount++
			}
		}
		hasDominantContentSubdir := false
		if directVideoCount > 0 {
			for _, e := range entries {
				if !e.IsDir() || isXiaoyaSkipDir(e.Name()) || extrasExcludeDirs[strings.ToLower(e.Name())] {
					continue
				}
				subVideos, _ := s.collectSeriesEvidence(vfsJoin(root, e.Name()))
				if len(subVideos) > directVideoCount {
					hasDominantContentSubdir = true
					break
				}
			}
		}

		sameDirGrouped := mr.Depth > 0 && !rootIsSeriesFolder && !rootHasSeriesEvidence &&
			!hasDominantContentSubdir && s.isSameDirOrDatedGroup(root)

		if rootIsSeriesFolder || rootHasSeriesEvidence || sameDirGrouped {
			rootBase := filepath.Base(root)
			normalizedName := s.normalizeSeriesName(rootBase)
			if normalizedName == "" {
				// 极端兜底：用原始名作 key 防止空键碰撞
				normalizedName = "__series_" + rootBase
			}
			seasonNum := s.extractSeasonFromDirName(rootBase)
			if sameDirGrouped {
				s.logger.Infof("[mixed] 媒体根 %s 为穿透内容目录且含多个视频，按同目录归组为剧集，序列名=%s", root, normalizedName)
			} else if rootIsSeriesFolder {
				s.logger.Infof("[mixed] 媒体根 %s 自身识别为剧集目录（%d 个季子目录），序列名=%s", root, seasonChildCount, normalizedName)
			} else {
				s.logger.Infof("[mixed] 媒体根 %s 自身识别为剧集目录（视频文件命中剧集命名），序列名=%s", root, normalizedName)
			}
			seriesDirGroups[normalizedName] = append(seriesDirGroups[normalizedName], seriesFolder{
				path:      root,
				dirName:   rootBase,
				seasonNum: seasonNum,
			})
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				// 根目录下的散落视频文件
				ext := strings.ToLower(filepath.Ext(entry.Name()))
				if supportedExts[ext] {
					looseVideoFiles = append(looseVideoFiles, looseEntry{entry: entry, rootPath: root})
				}
				continue
			}

			dirName := entry.Name()
			// [xiaoya 适配] 跳过特殊目录（ISO/json/画质演示/extras 等）
			if isXiaoyaSkipDir(dirName) || extrasExcludeDirs[strings.ToLower(dirName)] {
				s.logger.Debugf("[xiaoya] 混合库扫描跳过特殊目录: %s", dirName)
				continue
			}

			folderPath := vfsJoin(root, dirName)

			// 智能判断：该目录是电视剧还是电影
			// 归类优先级：明确剧集证据（季号/Season子目录/剧集命名）> 同目录多视频归组 > 日期命名（个人视频）> 电影。
			// 同一目录下的多个视频视为一部剧集的分集，即使文件名没有剧集命名特征；
			// 人名目录下仅有一个日期命名的视频也应归组为合集（个人视频场景）。
			// 多视频组包装层（多个子目录各自含多视频）不整体归组，放行下钻逐个归类。
			if s.isTVShowFolder(folderPath) || s.isSameDirOrDatedGroup(folderPath) {
				normalizedName := s.normalizeSeriesName(dirName)
				if normalizedName == "" {
					// 防御：纯季号目录名（如 "Season 01"）出现在分类目录下属于异常结构，
					// 用其父目录（mediaRoot）的 basename 作 fallback，避免空 key 合并
					rootBase := filepath.Base(root)
					normalizedName = s.normalizeSeriesName(rootBase)
					if normalizedName == "" {
						normalizedName = "__series_" + rootBase + "_" + dirName
					}
					s.logger.Warnf("[mixed] 子目录 %s 是纯季号目录，使用父目录名作系列名 fallback: %s", dirName, normalizedName)
				}
				seasonNum := s.extractSeasonFromDirName(dirName)
				seriesDirGroups[normalizedName] = append(seriesDirGroups[normalizedName], seriesFolder{
					path:      folderPath,
					dirName:   dirName,
					seasonNum: seasonNum,
				})
			} else {
				// 电影目录
				movieDirs = append(movieDirs, movieDirEntry{entry: entry, rootPath: root})
			}
		}
	}

	// === 阶段二：处理电视剧目录（复用 scanTVShowLibrary 的分组逻辑） ===
	for normalizedName, folders := range seriesDirGroups {
		if len(folders) == 1 && folders[0].seasonNum == 0 {
			// 单个目录且未识别到季号 → 独立处理
			f := folders[0]
			seriesTitle := s.extractSeriesTitle(f.dirName)
			newCount, err := s.scanSeriesFolder(library, f.path, seriesTitle)
			if err != nil {
				s.logger.Warnf("混合库-扫描剧集文件夹失败: %s, 错误: %v", f.path, err)
				continue
			}
			totalCount += newCount
		} else {
			// 多季合并
			newCount, err := s.scanMultiSeasonSeries(library, normalizedName, folders)
			if err != nil {
				s.logger.Warnf("混合库-扫描多季合集失败: %s, 错误: %v", normalizedName, err)
				continue
			}
			totalCount += newCount
		}
	}

	// === 阶段三：处理电影目录（扫描目录内的视频文件作为电影） ===
	for _, entry := range movieDirs {
		folderPath := vfsJoin(entry.rootPath, entry.entry.Name())
		err := s.walkLibraryPath(folderPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if !supportedExts[ext] {
				return nil
			}
			if s.skipUndersized(library, path, info.Size()) {
				return nil
			}
			if existing, err := s.mediaRepo.FindByFilePath(path); err == nil {
				s.deleteSourceDuplicateIfOrganized(library, existing, path, info)
				return nil // 已存在
			}
			if _, represented := s.findOrganizedHardlinkRecord(library, path, info); represented {
				return nil
			}
			title := s.extractTitle(filepath.Base(path))
			media := &model.Media{
				LibraryID: library.ID,
				Title:     title,
				FilePath:  path,
				FileSize:  info.Size(),
				MediaType: "movie",
			}
			s.probeMediaInfo(media)
			s.scanExternalSubtitles(media)

			// 识别本地 NFO 元数据与海报封面图片（与电影库逻辑一致，
			// 本地海报未命中时自动提取视频第一帧兜底）
			if nfoPath := s.nfoService.FindNFOForMedia(path); nfoPath != "" {
				if err := s.nfoService.ParseMovieNFO(nfoPath, media); err != nil {
					s.logger.Debugf("解析NFO失败: %s, 错误: %v", nfoPath, err)
				}
			}
			if poster, backdrop := s.nfoService.FindLocalImagesForMedia(path); poster != "" || backdrop != "" {
				if poster != "" {
					media.PosterPath = poster
				}
				if backdrop != "" {
					media.BackdropPath = backdrop
				}
			}

			if err := s.mediaRepo.Create(media); err != nil {
				s.logger.Warnf("保存媒体失败: %s, 错误: %v", path, err)
				return nil
			}
			totalCount++
			s.logger.Debugf("发现电影(混合库): %s [%s | %s | %s]", title, media.Resolution, media.VideoCodec, media.AudioCodec)
			s.broadcastScanEvent(EventScanProgress, &ScanProgressData{
				LibraryID:   library.ID,
				LibraryName: library.Name,
				Phase:       "scanning",
				NewFound:    totalCount,
				Message:     fmt.Sprintf("发现电影: %s [%s]", title, media.Resolution),
			})
			return nil
		})
		if err != nil {
			s.logger.Warnf("混合库-扫描电影目录失败: %s, 错误: %v", folderPath, err)
		}
	}

	// === 阶段四：处理根目录散落的视频文件（作为电影） ===
	for _, entry := range looseVideoFiles {
		filePath := filepath.Join(entry.rootPath, entry.entry.Name())
		info, err := entry.entry.Info()
		if err != nil {
			continue
		}
		if s.skipUndersized(library, filePath, info.Size()) {
			continue
		}
		if existing, err := s.mediaRepo.FindByFilePath(filePath); err == nil {
			s.deleteSourceDuplicateIfOrganized(library, existing, filePath, info)
			continue // 已存在
		}
		if _, represented := s.findOrganizedHardlinkRecord(library, filePath, info); represented {
			continue
		}
		title := s.extractTitle(entry.entry.Name())
		media := &model.Media{
			LibraryID: library.ID,
			Title:     title,
			FilePath:  filePath,
			FileSize:  info.Size(),
			MediaType: "movie",
		}
		s.probeMediaInfo(media)
		s.scanExternalSubtitles(media)

		// 识别本地 NFO 元数据与海报封面图片（与电影库逻辑一致，
		// 本地海报未命中时自动提取视频第一帧兜底）
		if nfoPath := s.nfoService.FindNFOForMedia(filePath); nfoPath != "" {
			if err := s.nfoService.ParseMovieNFO(nfoPath, media); err != nil {
				s.logger.Debugf("解析NFO失败: %s, 错误: %v", nfoPath, err)
			}
		}
		if poster, backdrop := s.nfoService.FindLocalImagesForMedia(filePath); poster != "" || backdrop != "" {
			if poster != "" {
				media.PosterPath = poster
			}
			if backdrop != "" {
				media.BackdropPath = backdrop
			}
		}

		if err := s.mediaRepo.Create(media); err != nil {
			s.logger.Warnf("保存媒体失败: %s, 错误: %v", filePath, err)
			continue
		}
		totalCount++
		s.logger.Debugf("发现电影(散落): %s [%s]", title, media.Resolution)
		s.broadcastScanEvent(EventScanProgress, &ScanProgressData{
			LibraryID:   library.ID,
			LibraryName: library.Name,
			Phase:       "scanning",
			NewFound:    totalCount,
			Message:     fmt.Sprintf("发现电影: %s [%s]", title, media.Resolution),
		})
	}

	// 收尾清理：遍历该库 DB 中所有文件路径，用 os.Stat 检查磁盘是否真的存在，
	// 不存在的直接删除。这样无论文件原来属于电影目录还是剧集目录，都能正确清理。
	// 之前的"限定范围"策略会遗漏被判为剧集目录的电影文件夹中的失效记录。
	dbPathSet, cleanupErr := s.mediaRepo.GetAllFilePathsByLibrary(library.ID)
	if cleanupErr == nil && dbPathSet != nil {
		staleRemoved := 0
		for dbPath := range dbPathSet {
			if _, statErr := s.statLibraryPath(dbPath); os.IsNotExist(statErr) {
				if m, findErr := s.mediaRepo.FindByFilePath(dbPath); findErr == nil && m != nil {
					if delErr := PurgeMediaCompletely(s.mediaRepo, s.cfg.Cache.CacheDir, s.logger, m, "扫描清理"); delErr != nil {
						continue
					}
					staleRemoved++
					s.logger.Infof("清理失效媒体记录及其关联数据（磁盘已不存在）: %s", dbPath)
				}
			}
		}
		if staleRemoved > 0 {
			s.logger.Infof("混合库 %s 清理失效媒体记录: %d 条", library.Name, staleRemoved)
			PurgeEmptySeriesInLibrary(s.seriesRepo, s.mediaRepo, s.cfg.Cache.CacheDir, s.logger, library.ID)
		}
	}

	s.logger.Infof("混合媒体库扫描完成: %s, 新增 %d 个媒体", library.Name, totalCount)
	return totalCount, nil
}

// isTVShowFolder 智能判断一个目录是否为电视剧文件夹
// 判断依据（满足任一即认定为电视剧）：
// 1. 目录名包含季号标识（如 S1、Season 1、第一季）
// 2. 目录内包含 Season 子目录
// 3. 目录内有多个视频文件且文件名匹配剧集命名模式（S01E01、EP01、第N集等）
//
// 注意：本函数用于媒体根目录的严格判定（根下散落的大量电影不能因数量多被
// 归组为剧集）。内容子目录级别的「同目录视频归组为剧集」由 isSameDirVideoGroup
// 负责，两者配合使用。
func (s *ScannerService) isTVShowFolder(folderPath string) bool {
	dirName := filepath.Base(folderPath)

	// 规则1: 目录名包含季号标识
	if s.extractSeasonFromDirName(dirName) > 0 {
		return true
	}

	videoFiles, hasSeasonSubdir := s.collectSeriesEvidence(folderPath)

	// 规则2: 包含 Season 子目录
	if hasSeasonSubdir {
		return true
	}

	// 只有0或1个视频文件 → 大概率是电影
	if len(videoFiles) <= 1 {
		return false
	}

	// 规则3: 多个视频文件中有匹配剧集命名模式的
	episodeMatchCount := 0
	for _, vf := range videoFiles {
		ep := s.parseEpisodeInfo(vf)
		if ep.EpisodeNum > 0 {
			episodeMatchCount++
		}
	}

	// 如果超过一半的视频文件匹配剧集模式，认定为电视剧
	if episodeMatchCount > 0 && episodeMatchCount >= len(videoFiles)/2 {
		return true
	}

	return false
}

// isSameDirVideoGroup 判断目录下的多个视频是否应归组为一部剧集。
//
// 归类规则（产品决策）：同一个目录下的多个视频文件视为同一部剧集的分集，
// 即使文件名没有任何剧集命名特征（如纯数字名、下载站原始名）。
// 剧集标题取目录名，集号由 collectEpisodes 自动解析/编号。
// 特典目录（extras/trailers 等）与样片文件不参与计数，避免
// 「单电影 + 花絮」被误判为剧集。
func (s *ScannerService) isSameDirVideoGroup(folderPath string) bool {
	videoFiles, _ := s.collectSeriesEvidence(folderPath)
	return len(videoFiles) >= 2
}

// hasMultiVideoBearingSubdirs 判断目录是否为「多视频组的包装层」：
// 存在 ≥2 个含视频的内容子目录，且其中至少一个含 ≥2 个视频。
// 这种结构（如 演员/作品A/a1.mp4+a2.mp4 与 演员/作品B/b1.mp4 并列）
// 说明当前目录只是分类/包装层——若整体归组会把多个独立作品折叠成
// 一个以包装层命名的错误大剧集。应返回 false 放行下钻，
// 让各子目录按各自身份独立归组。
//
// 对比：单包装链（人名/合集/多个视频）与嵌套单视频集合
// （Nina/Nina-01/x.mp4 + Nina-02/y.mp4，各子目录恰好 1 个视频）
// 不命中本判定，维持原有「收编为一部剧集」的行为。
func (s *ScannerService) hasMultiVideoBearingSubdirs(folderPath string) bool {
	entries, err := s.readDirLibraryPath(folderPath)
	if err != nil {
		return false
	}
	bearingSubdirs := 0
	hasMulti := false
	for _, e := range entries {
		if !e.IsDir() || isXiaoyaSkipDir(e.Name()) || extrasExcludeDirs[strings.ToLower(e.Name())] {
			continue
		}
		subVideos, _ := s.collectSeriesEvidence(vfsJoin(folderPath, e.Name()))
		switch {
		case len(subVideos) >= 2:
			bearingSubdirs++
			hasMulti = true
		case len(subVideos) == 1:
			bearingSubdirs++
		}
		if bearingSubdirs >= 2 && hasMulti {
			return true
		}
	}
	return false
}

// isSameDirOrDatedGroup 同目录归组的统一入口：多个无命名视频、或日期命名的
// 个人视频，在【非】多视频组包装层的前提下归组为一部剧集。
// 根级归组与逐目录归类共用本判定，保证两层行为一致。
//
// 产品决策：目录下只有一个视频文件时不归组为剧集（即使文件名形如日期，
// 也应作为独立的单视频处理），只有至少 2 个视频才同目录归组。
func (s *ScannerService) isSameDirOrDatedGroup(folderPath string) bool {
	if s.hasMultiVideoBearingSubdirs(folderPath) {
		return false
	}
	// 同目录至少要有 2 个可归组视频才归组为剧集；仅有 1 个视频
	// （包括日期命名的单视频）作为独立视频处理，不进剧集。
	if s.grouppableVideoCount(folderPath) < 2 {
		return false
	}
	return s.isSameDirVideoGroup(folderPath) || s.hasDatedVideo(folderPath)
}

// grouppableVideoCount 统计目录内可归组的视频数量（直属文件 + 一层非特典子目录），
// 与 collectSeriesEvidence 口径一致，用于判断同目录是否达到「多个视频」的下限。
func (s *ScannerService) grouppableVideoCount(folderPath string) int {
	videoFiles, _ := s.collectSeriesEvidence(folderPath)
	return len(videoFiles)
}

// isNestedSingleVideoCollection 判断目录的各内容子目录是否各自只含一个视频。
// 这种「包装层」结构（如 Nina/Nina-01/Nina-01.mp4）不应被当作分类层继续下钻。
// 返回 true 表示：至少有 2 个子目录各含恰好 1 个视频，且不存在含多个视频的子目录
// （存在多视频子目录说明那是分类层，应继续下钻让各子目录独立归组）。
func (s *ScannerService) isNestedSingleVideoCollection(path string, subDirs []os.DirEntry) bool {
	singleCount := 0
	for _, sd := range subDirs {
		if isXiaoyaSkipDir(sd.Name()) || extrasExcludeDirs[strings.ToLower(sd.Name())] {
			continue
		}
		entries, err := s.readDirLibraryPath(vfsJoin(path, sd.Name()))
		if err != nil {
			return false
		}
		videoCount := 0
		for _, e := range entries {
			if e.IsDir() {
				// 含更深层级：不是简单包装层，保守放行下钻
				return false
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if !supportedExts[ext] || isExtrasFile(e.Name()) {
				continue
			}
			baseName := strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
			if junkVideoNames[baseName] {
				continue
			}
			videoCount++
			if videoCount > 1 {
				return false
			}
		}
		if videoCount == 1 {
			singleCount++
		}
	}
	return singleCount >= 2
}

// hasDatedVideo 判断目录内（含一层非特典子目录）是否存在日期命名的视频。
// [个人视频场景] 人名目录下即使只有一个日期命名的视频（如 小红/VID_20240501_xxx.mp4），
// 也应归组为该人的剧集合集，而不是散落成独立电影。
func (s *ScannerService) hasDatedVideo(folderPath string) bool {
	entries, err := s.readDirLibraryPath(folderPath)
	if err != nil {
		return false
	}
	check := func(name string) bool {
		ext := strings.ToLower(filepath.Ext(name))
		if !supportedExts[ext] || isExtrasFile(name) {
			return false
		}
		baseName := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		if junkVideoNames[baseName] {
			return false
		}
		_, _, _, _, ok := matchDateEpisode(name)
		return ok
	}
	for _, e := range entries {
		if e.IsDir() {
			if extrasExcludeDirs[strings.ToLower(e.Name())] {
				continue
			}
			subEntries, err := s.readDirLibraryPath(vfsJoin(folderPath, e.Name()))
			if err == nil {
				for _, subEntry := range subEntries {
					if !subEntry.IsDir() && check(subEntry.Name()) {
						return true
					}
				}
			}
		} else if check(e.Name()) {
			return true
		}
	}
	return false
}

// junkVideoNames 目录内常见的垃圾/样片视频文件名（去扩展名后小写比对）
var junkVideoNames = map[string]bool{
	"sample": true, "trailer": true, "promo": true, "menu": true,
}

// collectSeriesEvidence 收集目录内的候选剧集视频证据。
// 返回值：
//   - videoFiles：正片候选视频文件名（直接位于目录内，或位于一层非特典子目录中；
//     已排除特典文件后缀、特典目录与样片命名）
//   - hasSeasonSubdir：是否存在 Season/Sxx/第N季/Specials 等标准季目录
func (s *ScannerService) collectSeriesEvidence(folderPath string) (videoFiles []string, hasSeasonSubdir bool) {
	entries, err := s.readDirLibraryPath(folderPath)
	if err != nil {
		return nil, false
	}

	collectVideo := func(name string) {
		ext := strings.ToLower(filepath.Ext(name))
		if !supportedExts[ext] || isExtrasFile(name) {
			return
		}
		baseName := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		if junkVideoNames[baseName] {
			return
		}
		videoFiles = append(videoFiles, name)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			for _, pattern := range seasonDirPatterns {
				if pattern.MatchString(entry.Name()) {
					hasSeasonSubdir = true
					break
				}
			}
			// 特典/花絮目录中的视频不算正片证据，且无需深入
			if extrasExcludeDirs[strings.ToLower(entry.Name())] {
				continue
			}
			// 递归检查子目录中的视频文件（只深入一层）
			subEntries, err := s.readDirLibraryPath(vfsJoin(folderPath, entry.Name()))
			if err == nil {
				for _, subEntry := range subEntries {
					if !subEntry.IsDir() {
						collectVideo(subEntry.Name())
					}
				}
			}
		} else {
			collectVideo(entry.Name())
		}
	}

	return videoFiles, hasSeasonSubdir
}

// ==================== 剧集扫描逻辑 ====================

// 常见分辨率数字，用于排除误匹配
var resolutionNums = map[int]bool{
	240: true, 360: true, 480: true, 540: true,
	720: true, 1080: true, 1440: true, 2160: true, 4320: true,
}

// isResolutionContext 检查匹配位置前后是否有分辨率标志（如 p, P, i, I）
func isResolutionContext(filename string, matchEnd int) bool {
	if matchEnd < len(filename) {
		nextChar := filename[matchEnd]
		if nextChar == 'p' || nextChar == 'P' || nextChar == 'i' || nextChar == 'I' {
			return true
		}
	}
	return false
}

// 剧集命名模式正则
var episodePatterns = []*regexp.Regexp{
	// 模式0: S01E01 / S1E1 / s01e01
	regexp.MustCompile(`(?i)S(\d{1,2})\s*E(\d{1,4})`),
	// 模式1: S01.E01
	regexp.MustCompile(`(?i)S(\d{1,2})\.E(\d{1,4})`),
	// 模式2: 1x01 / 01x01
	regexp.MustCompile(`(?i)(\d{1,2})x(\d{1,4})`),
	// 模式3: 第01集 / 第1集
	regexp.MustCompile(`第\s*(\d{1,4})\s*集`),
	// 模式4: EP01 / EP.01 / Episode 01
	regexp.MustCompile(`(?i)(?:EP|Episode)\s*\.?\s*(\d{1,4})`),
	// 模式5: OVA01 / OVA 01 / SP01 / SP 01（特殊剧集类型+数字）
	regexp.MustCompile(`(?i)(?:OVA|OAD|SP|SPECIAL|NCOP|NCED)\s*(\d{1,4})`),
	// 模式6: E01（单独的E+数字）
	regexp.MustCompile(`(?i)\bE(\d{1,4})\b`),
	// 模式7: [01] / [001] / [12END] / [24END] — 方括号内的数字（可能带END/FINAL/完等后缀）
	regexp.MustCompile(`(?i)\[(\d{2,4})(?:END|FINAL|完)?\]`),
	// 模式8: - 01 - / .01. / 空格01空格
	regexp.MustCompile(`[\-\.\s](\d{2,4})[\]\-\.\s]`),
}

// trailingUnderscoreEpPattern 下划线尾号：Saved_003、NANA_001（个人收藏常见命名）。
// 作为模式 10 在其他模式之后尝试，仅匹配文件名末尾的下划线编号。
var trailingUnderscoreEpPattern = regexp.MustCompile(`_(\d{1,4})$`)

// multiEpPatterns 多集连播文件正则（优先于单集模式匹配）
var multiEpPatterns = []*regexp.Regexp{
	// S01E02-E03 / S01E02-E05 / S01E02-e03
	regexp.MustCompile(`(?i)S(\d{1,2})E(\d{1,4})\s*[-–~]\s*E(\d{1,4})`),
	// S01E02-03 (无前缀 E 的范围)
	regexp.MustCompile(`(?i)S(\d{1,2})E(\d{1,4})\s*[-–~]\s*(\d{1,4})`),
}

// dateEpisodePattern 日期格式集号正则（用于脱口秀/日播剧等）
// 匹配: 2024.01.15 / 2024-01-15 / 2024_01_15
var (
	dateEpisodePattern = regexp.MustCompile(`((?:19|20)\d{2})[\.\-_](\d{2})[\.\-_](\d{2})`)
	// 紧凑格式：20240115（手机导出视频常见，如 VID_20240115_223045.mp4）
	dateEpisodeCompactPattern = regexp.MustCompile(`(?:^|[^\d])((?:19|20)\d{2})(\d{2})(\d{2})(?:[^\d]|$)`)
	// 中文格式：2024年1月15日
	dateEpisodeCJKPattern = regexp.MustCompile(`((?:19|20)\d{2})年(\d{1,2})月(\d{1,2})日`)
	// 美式紧凑日期：011921（月日年 6 位纯数字，个人收藏常见命名）
	dateEpisodeMMDDYYPattern = regexp.MustCompile(`^(\d{2})(\d{2})(\d{2})$`)
)

// trailingParenEpPattern 尾部括号编号：(01)、(3) 等（个人收藏常见命名）
var trailingParenEpPattern = regexp.MustCompile(`\(\s*(\d{1,4})\s*\)\s*$`)

// matchDateEpisode 从文件名中识别日期型集号。
// 返回年/月/日与命中的原始文本；由调用方负责数值合理性校验。
func matchDateEpisode(filename string) (year, month, day int, matched string, ok bool) {
	if m := dateEpisodePattern.FindStringSubmatch(filename); len(m) >= 4 {
		year, _ = strconv.Atoi(m[1])
		month, _ = strconv.Atoi(m[2])
		day, _ = strconv.Atoi(m[3])
		return year, month, day, m[0], true
	}
	if m := dateEpisodeCJKPattern.FindStringSubmatch(filename); len(m) >= 4 {
		year, _ = strconv.Atoi(m[1])
		month, _ = strconv.Atoi(m[2])
		day, _ = strconv.Atoi(m[3])
		return year, month, day, m[0], true
	}
	if m := dateEpisodeCompactPattern.FindStringSubmatch(filename); len(m) >= 4 {
		year, _ = strconv.Atoi(m[1])
		month, _ = strconv.Atoi(m[2])
		day, _ = strconv.Atoi(m[3])
		return year, month, day, m[1], true
	}
	// [个人视频] 纯 6 位数字文件名按美式 月日年(MMDDYY) 解析，如 011921 → 2021-01-19。
	// 仅当去掉扩展名后整体恰为 6 位数字时生效，避免误伤其他编号。
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	if m := dateEpisodeMMDDYYPattern.FindStringSubmatch(base); len(m) >= 4 {
		month, _ := strconv.Atoi(m[1])
		day, _ := strconv.Atoi(m[2])
		yy, _ := strconv.Atoi(m[3])
		if month >= 1 && month <= 12 && day >= 1 && day <= 31 && yy <= 38 {
			return 2000 + yy, month, day, base, true
		}
	}
	return 0, 0, 0, "", false
}

// 独立季号正则：从文件名中提取 S2、Season 2 等季号（不依赖集号）
var seasonInFilenamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bS(\d{1,2})\b`),
	regexp.MustCompile(`(?i)\bSeason\s*(\d{1,2})\b`),
	regexp.MustCompile(`第\s*(\d{1,2})\s*季`),
}

// Season目录模式
var seasonDirPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^Season\s*(\d{1,2})$`),
	regexp.MustCompile(`(?i)^S(\d{1,2})$`),
	regexp.MustCompile(`^第\s*(\d{1,2})\s*季$`),
	regexp.MustCompile(`(?i)^Specials?$`),   // 特别篇
	regexp.MustCompile(`(?i)^Season\s*0+$`), // Season 0 / Season 00（Emby 特别篇格式）
}

// seriesFolder 多季合并时使用的目录信息
