package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/fan-video/fan-video/internal/model"
)

type seriesFolder struct {
	path      string // 完整路径
	dirName   string // 原始目录名
	seasonNum int    // 从目录名提取的季号（0表示未识别到季号）
}

// EpisodeInfo 解析出的剧集信息
type EpisodeInfo struct {
	SeasonNum     int
	EpisodeNum    int
	EpisodeNumEnd int // 多集连播结束集号（0=单集），如 S01E02-E05 → Start=2, End=5
	EpisodeTitle  string
	AirDate       string // 日期格式集号：2024-01-15（脱口秀/日播剧）
	FilePath      string
	FileInfo      os.FileInfo
	// 个人视频标记：collectEpisodes 在做「日期命名归一化」时置位，
	// 表示该片段属于个人视频（不对外生成 SxxExx 季集标签）。
	IsPersonal bool
}

// scanTVShowLibrary 扫描剧集库（基于文件夹的合集识别 + 根目录散落文件智能归类）
func (s *ScannerService) scanTVShowLibrary(library *model.Library) (int, error) {
	var totalNewEpisodes int

	allPaths := library.AllPaths()
	s.logger.Infof("剧集库扫描开始: %s, 路径数: %d, 路径列表: %v", library.Name, len(allPaths), allPaths)

	// [xiaoya 适配] 将所有媒体库根展开为多个"真实剧集根"
	// 普通用户的平铺目录会返回 [library.AllPaths()]（完全向后兼容）
	var mediaRoots []string
	for _, p := range allPaths {
		mediaRoots = append(mediaRoots, s.collectMediaRoots(p, "tvshow")...)
	}
	s.logger.Infof("[剧集扫描] 展开后的媒体根目录数: %d", len(mediaRoots))
	for i, mr := range mediaRoots {
		s.logger.Infof("[剧集扫描]   媒体根[%d]: %s", i, mr)
	}

	// 收集根目录下的散落视频文件，按系列名分组（跨所有 roots）
	type looseFile struct {
		entry    os.DirEntry
		info     os.FileInfo
		rootPath string
	}
	seriesGroups := make(map[string][]looseFile) // 系列名 -> 文件列表

	// 标准化系列名 -> 目录列表（跨所有 roots）
	seriesDirGroups := make(map[string][]seriesFolder)

	// === 阶段一：对每个媒体根分别收集剧集目录和散落视频 ===
	for _, root := range mediaRoots {
		entries, err := s.readDirLibraryPath(root)
		if err != nil {
			s.logger.Warnf("读取剧集根目录失败: %s, 错误: %v", root, err)
			continue
		}
		s.logger.Infof("剧集根 %s 包含 %d 个条目", root, len(entries))

		for _, entry := range entries {
			if !entry.IsDir() {
				// 根目录下的视频文件
				ext := strings.ToLower(filepath.Ext(entry.Name()))
				if supportedExts[ext] {
					filePath := vfsJoin(root, entry.Name())
					info, _ := entry.Info()
					if info == nil {
						continue
					}
					if existing, err := s.mediaRepo.FindByFilePath(filePath); err == nil {
						s.deleteSourceDuplicateIfOrganized(library, existing, filePath, info)
						// 已归属某剧集的文件不再重复处理；
						// 无归属的旧行（旧版本按散落记录导入）仍参与下方智能归类，补挂到虚拟合集
						if existing.SeriesID != "" {
							continue // 已存在且已归属
						}
					}
					if _, represented := s.findOrganizedHardlinkRecord(library, filePath, info); represented {
						continue
					}
					// 从文件名提取系列名称用于智能归类
					seriesName := s.extractSeriesNameFromFile(entry.Name())
					if seriesName == "" {
						seriesName = "__ungrouped__"
					}
					seriesGroups[seriesName] = append(seriesGroups[seriesName], looseFile{entry: entry, info: info, rootPath: root})
				}
				continue
			}

			dirName := entry.Name()
			// [xiaoya 适配] 跳过特殊目录（ISO/json/画质演示/extras 等）
			if isXiaoyaSkipDir(dirName) || extrasExcludeDirs[strings.ToLower(dirName)] {
				s.logger.Debugf("[xiaoya] 剧集扫描跳过特殊目录: %s", dirName)
				continue
			}

			folderPath := vfsJoin(root, dirName)

			// 从目录名提取标准化系列名（去掉季号标识）和季号
			normalizedName := s.normalizeSeriesName(dirName)
			seasonNum := s.extractSeasonFromDirName(dirName)

			// 防御：如果目录名本身是"纯季号目录"（如 Season 01、S01、第一季），
			// 说明当前 root 根本不是剧集名目录、而是某个剧集名目录本身。
			// 使用 root 的 basename 作为系列名，并从该目录名反推季号。
			if normalizedName == "" || isSeasonOnlyDirName(dirName) {
				rootBase := filepath.Base(root)
				fallback := s.normalizeSeriesName(rootBase)
				if fallback == "" {
					fallback = s.extractSeriesTitle(rootBase)
				}
				if fallback == "" {
					fallback = rootBase
				}
				s.logger.Warnf("[剧集扫描] 检测到纯季号子目录: %s（位于 %s），使用父目录名作为系列名: %s", dirName, root, fallback)
				normalizedName = fallback
				if seasonNum == 0 {
					seasonNum = s.extractSeasonFromDirName(dirName)
				}
			}

			seriesDirGroups[normalizedName] = append(seriesDirGroups[normalizedName], seriesFolder{
				path:      folderPath,
				dirName:   dirName,
				seasonNum: seasonNum,
			})
		}
	}

	// [剧集扫描] 诊断日志：打印 seriesDirGroups 分组结果
	s.logger.Infof("[剧集扫描] 系列目录分组完成，共 %d 个分组", len(seriesDirGroups))
	for name, folders := range seriesDirGroups {
		dirNames := make([]string, 0, len(folders))
		for _, f := range folders {
			dirNames = append(dirNames, f.dirName)
		}
		s.logger.Infof("[剧集扫描]   系列 \"%s\" -> %d 个目录: %v", name, len(folders), dirNames)
	}

	// === 阶段二：处理分组后的目录 ===
	for normalizedName, folders := range seriesDirGroups {
		if len(folders) == 1 && folders[0].seasonNum == 0 {
			// 单个目录且未识别到季号 → 按原有逻辑独立处理
			f := folders[0]
			seriesTitle := s.extractSeriesTitle(f.dirName)
			newCount, err := s.scanSeriesFolder(library, f.path, seriesTitle)
			if err != nil {
				s.logger.Warnf("扫描剧集文件夹失败: %s, 错误: %v", f.path, err)
				continue
			}
			totalNewEpisodes += newCount
		} else {
			// 多个目录属于同一系列（如"一拳超人 S1"和"一拳超人 S2"）
			// 或单个目录但明确包含季号标识 → 合并到同一个 Series
			newCount, err := s.scanMultiSeasonSeries(library, normalizedName, folders)
			if err != nil {
				s.logger.Warnf("扫描多季合集失败: %s, 错误: %v", normalizedName, err)
				continue
			}
			totalNewEpisodes += newCount
		}
	}

	// [C 方案] 对 __ungrouped__ 做二次归类：用 ParseEpisodeFilename 再抢救一次，
	// 能识别出系列名的文件从 __ungrouped__ 迁移到对应系列分组。
	if stuck, ok := seriesGroups["__ungrouped__"]; ok && len(stuck) > 0 {
		var residual []looseFile
		for _, f := range stuck {
			parsed := ParseEpisodeFilename(f.entry.Name())
			if parsed.SeriesTitle != "" && len([]rune(parsed.SeriesTitle)) >= 2 {
				seriesGroups[parsed.SeriesTitle] = append(seriesGroups[parsed.SeriesTitle], f)
				continue
			}
			residual = append(residual, f)
		}
		if len(residual) == 0 {
			delete(seriesGroups, "__ungrouped__")
		} else {
			seriesGroups["__ungrouped__"] = residual
		}
	}

	// 处理根目录散落文件的智能归类
	for seriesName, files := range seriesGroups {
		if seriesName == "__ungrouped__" {
			// 彻底无法识别系列名的文件，独立入库（保底）
			for _, f := range files {
				filePath := vfsJoin(f.rootPath, f.entry.Name())
				// 已在库中的记录（含无归属旧行）保持原状，避免重复插入
				if _, err := s.mediaRepo.FindByFilePath(filePath); err == nil {
					continue
				}
				if s.skipUndersized(library, filePath, f.info.Size()) {
					continue
				}
				title := s.extractTitle(f.entry.Name())
				media := &model.Media{
					LibraryID: library.ID,
					Title:     title,
					FilePath:  filePath,
					FileSize:  f.info.Size(),
					MediaType: "episode",
				}
				s.probeMediaInfo(media)
				s.scanExternalSubtitles(media)
				ep := s.parseEpisodeInfo(f.entry.Name())
				media.SeasonNum = ep.SeasonNum
				media.EpisodeNum = ep.EpisodeNum
				media.EpisodeTitle = ep.EpisodeTitle
				if err := s.mediaRepo.Create(media); err != nil {
					s.logger.Warnf("保存媒体失败: %s, 错误: %v", filePath, err)
				}
				totalNewEpisodes++
			}
			continue
		}

		// 有多个同名系列的文件或者能识别系列名的文件，自动创建合集
		actualSeriesName := seriesName

		// 为同系列的散落文件创建虚拟合集
		// 使用"__loose__:系列名"作为虚拟文件夹路径来区分
		virtualFolderPath := filepath.Join(library.Path, "__loose__:"+actualSeriesName)

		series, err := s.seriesRepo.FindByFolderPath(virtualFolderPath)
		if err != nil {
			series = &model.Series{
				LibraryID:  library.ID,
				Title:      actualSeriesName,
				FolderPath: virtualFolderPath,
			}
			if err := s.seriesRepo.Create(series); err != nil {
				s.logger.Warnf("创建散落剧集合集失败: %s, 错误: %v", actualSeriesName, err)
				continue
			}
			s.logger.Infof("创建散落剧集合集: %s (ID=%s)", actualSeriesName, series.ID)
		}

		seasonSet := make(map[int]bool)
		var newCount int

		for _, f := range files {
			filePath := vfsJoin(f.rootPath, f.entry.Name())
			ep := s.parseEpisodeInfo(f.entry.Name())
			if ep.SeasonNum == 0 {
				ep.SeasonNum = 1
			}

			// 历史无归属记录：补挂到当前虚拟合集，而不是重复入库
			if existing, err := s.mediaRepo.FindByFilePath(filePath); err == nil {
				seasonSet[ep.SeasonNum] = true
				if existing.SeriesID != series.ID {
					existing.SeriesID = series.ID
					existing.MediaType = "episode"
					existing.Title = actualSeriesName
					existing.SeasonNum = ep.SeasonNum
					existing.EpisodeNum = ep.EpisodeNum
					s.mediaRepo.Update(existing)
					s.logger.Infof("历史散落媒体补挂到合集: %s -> %s", f.entry.Name(), actualSeriesName)
				}
				continue
			}
			if s.skipUndersized(library, filePath, f.info.Size()) {
				continue
			}

			media := &model.Media{
				LibraryID:    library.ID,
				SeriesID:     series.ID,
				Title:        actualSeriesName,
				FilePath:     filePath,
				FileSize:     f.info.Size(),
				MediaType:    "episode",
				SeasonNum:    ep.SeasonNum,
				EpisodeNum:   ep.EpisodeNum,
				EpisodeTitle: ep.EpisodeTitle,
			}
			s.probeMediaInfo(media)
			s.scanExternalSubtitles(media)

			if err := s.mediaRepo.Create(media); err != nil {
				s.logger.Warnf("保存剧集失败: %s, 错误: %v", filePath, err)
				continue
			}

			seasonSet[ep.SeasonNum] = true
			newCount++

			s.logger.Debugf("发现散落剧集: %s [%s]", filepath.Base(ep.FilePath), media.Resolution)
			s.broadcastScanEvent(EventScanProgress, &ScanProgressData{
				LibraryID:   library.ID,
				LibraryName: library.Name,
				Phase:       "scanning",
				NewFound:    newCount,
				Message:     fmt.Sprintf("发现: %s", filepath.Base(ep.FilePath)),
			})
		}

		// 更新合集统计
		allEpisodes, _ := s.mediaRepo.ListBySeriesID(series.ID)
		series.EpisodeCount = len(allEpisodes)
		series.SeasonCount = len(seasonSet)
		s.seriesRepo.Update(series)

		s.logger.Infof("散落剧集归类完成: %s, 新增 %d 集, 共 %d 季 %d 集",
			actualSeriesName, newCount, series.SeasonCount, series.EpisodeCount)

		totalNewEpisodes += newCount
	}

	// 收尾清理：检查该库 DB 中所有文件路径，磁盘上不存在的视为失效记录删除
	dbPathSet, ppErr := s.mediaRepo.GetAllFilePathsByLibrary(library.ID)
	if ppErr == nil && dbPathSet != nil {
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
			s.logger.Infof("剧集库 %s 清理失效媒体记录: %d 条", library.Name, staleRemoved)
			PurgeEmptySeriesInLibrary(s.seriesRepo, s.mediaRepo, s.cfg.Cache.CacheDir, s.logger, library.ID)
		}
	}

	return totalNewEpisodes, nil
}

// normalizeSeriesName 标准化系列名：从目录名中去掉季号标识、idtag、年份后缀，返回纯系列名
// 例如:
//
//	"一拳超人 S1"                              → "一拳超人"
//	"Breaking Bad Season 2"                    → "Breaking Bad"
//	"一拳超人 第二季"                          → "一拳超人"
//	"一拳超人 第二季 (2018) [tmdbid-74956]"    → "一拳超人"
func (s *ScannerService) normalizeSeriesName(dirName string) string {
	// 防御：如果输入本身就是"纯季号目录名"（Season 01 / S01 / 第X季），
	// 它绝不可能是剧集标题，直接返回空字符串，让调用方走特殊处理逻辑，
	// 避免把不同剧集的 Season XX 错误归并到同一个系列。
	if isSeasonOnlyDirName(dirName) {
		return ""
	}

	title := s.extractSeriesTitle(dirName) // 先清理年份、编码等标记

	// 移除 idtag 标记（[tmdbid-xxx] / [imdbid-xxx]），它们可能出现在标题尾部
	idtagPattern := regexp.MustCompile(`(?i)\s*\[(tmdbid|imdbid|tvdbid)-[^\]]+\]\s*`)
	title = idtagPattern.ReplaceAllString(title, " ")

	// 移除年份 (1900) - (2099)（即使是中间出现）
	yearMidPattern := regexp.MustCompile(`\s*[\(\[]\s*(19|20)\d{2}\s*[\)\]]\s*`)
	title = yearMidPattern.ReplaceAllString(title, " ")

	// 移除季号标识
	seasonPatterns := []string{
		`(?i)\s*S\d{1,2}\s*$`,            // 末尾 S1, S02
		`(?i)\s*Season\s*\d{1,2}\s*$`,    // 末尾 Season 1
		`\s*第\s*[一二三四五六七八九十\d]+\s*季\s*$`, // 末尾 第一季, 第2季
		`\s*第\s*[一二三四五六七八九十\d]+\s*部\s*$`, // 末尾 第一部, 第2部
	}
	for _, p := range seasonPatterns {
		re := regexp.MustCompile(p)
		title = re.ReplaceAllString(title, "")
	}

	// 收敛多余空格
	title = regexp.MustCompile(`\s+`).ReplaceAllString(title, " ")
	title = strings.TrimSpace(title)
	if title == "" {
		// 如果标准化后为空（极端情况），回退使用原始清理标题
		return s.extractSeriesTitle(dirName)
	}
	return title
}

// extractSeasonFromDirName 从目录名中提取季号
// 例如: "一拳超人 S2" → 2, "Breaking Bad Season 1" → 1, "一拳超人 第二季" → 2
func (s *ScannerService) extractSeasonFromDirName(dirName string) int {
	// 支持 S1, S02 格式
	if m := regexp.MustCompile(`(?i)\bS(\d{1,2})\b`).FindStringSubmatch(dirName); len(m) >= 2 {
		num, _ := strconv.Atoi(m[1])
		if num > 0 && num <= 30 {
			return num
		}
	}
	// 支持 Season 1, Season 02 格式
	if m := regexp.MustCompile(`(?i)\bSeason\s*(\d{1,2})\b`).FindStringSubmatch(dirName); len(m) >= 2 {
		num, _ := strconv.Atoi(m[1])
		if num > 0 && num <= 30 {
			return num
		}
	}
	// 支持中文 "第1季", "第二季"
	if m := regexp.MustCompile(`第\s*(\d{1,2})\s*季`).FindStringSubmatch(dirName); len(m) >= 2 {
		num, _ := strconv.Atoi(m[1])
		if num > 0 && num <= 30 {
			return num
		}
	}
	// 支持中文数字 "第一季" ~ "第十季"
	cnNumMap := map[string]int{
		"一": 1, "二": 2, "三": 3, "四": 4, "五": 5,
		"六": 6, "七": 7, "八": 8, "九": 9, "十": 10,
	}
	if m := regexp.MustCompile(`第\s*([一二三四五六七八九十]+)\s*季`).FindStringSubmatch(dirName); len(m) >= 2 {
		if num, ok := cnNumMap[m[1]]; ok {
			return num
		}
	}
	return 0
}

// scanMultiSeasonSeries 扫描属于同一系列的多季目录，将其合并到一个 Series 中
// folders 中的 seriesFolder 包含各个季目录的路径、目录名和从目录名提取的季号
func (s *ScannerService) scanMultiSeasonSeries(library *model.Library, seriesTitle string, folders []seriesFolder) (int, error) {
	s.logger.Infof("扫描多季合集: %s (%d 个目录)", seriesTitle, len(folders))

	// 查找或创建统一的 Series 合集
	// 优先按第一个目录的 FolderPath 查找（兼容旧数据），
	// 然后按标题+媒体库查找，最后创建新的
	var series *model.Series

	// 1. 尝试按任意一个目录的 FolderPath 查找已有 Series
	for _, f := range folders {
		if existing, err := s.seriesRepo.FindByFolderPath(f.path); err == nil {
			series = existing
			break
		}
	}

	// 2. 按标题+媒体库查找（可能之前已经合并过）
	if series == nil {
		if existing, err := s.seriesRepo.FindByTitleAndLibrary(seriesTitle, library.ID); err == nil {
			series = existing
		}
	}

	// 3. 创建新合集，FolderPath 使用第一个目录（或虚拟路径）
	if series == nil {
		// 使用"__multi__:系列名"作为虚拟路径，标识这是一个多季合并的合集
		virtualPath := filepath.Join(library.Path, "__multi__:"+seriesTitle)
		series = &model.Series{
			LibraryID:  library.ID,
			Title:      seriesTitle,
			FolderPath: virtualPath,
		}
		if err := s.seriesRepo.Create(series); err != nil {
			return 0, fmt.Errorf("创建多季合集失败: %w", err)
		}
		s.logger.Infof("创建多季合集: %s (ID=%s, %d 个季目录)", seriesTitle, series.ID, len(folders))
	}

	// 识别本地 NFO 信息文件（从各季目录中查找）
	for _, f := range folders {
		if nfoPath := s.nfoService.FindNFOFile(f.path); nfoPath != "" {
			if err := s.nfoService.ParseTVShowNFO(nfoPath, series); err != nil {
				s.logger.Debugf("解析多季合集NFO失败: %s, 错误: %v", nfoPath, err)
			} else {
				s.logger.Debugf("从NFO读取多季合集元数据: %s -> %s", nfoPath, series.Title)
			}
			break // 只用第一个找到的NFO
		}
	}

	// 识别本地海报封面图片（从各季目录中查找；含封面子目录）
	for _, f := range folders {
		if poster, backdrop := s.nfoService.FindLocalImagesDeep(f.path); poster != "" || backdrop != "" {
			if poster != "" && series.PosterPath == "" {
				series.PosterPath = poster
				s.logger.Debugf("发现多季合集本地海报: %s", poster)
			}
			if backdrop != "" && series.BackdropPath == "" {
				series.BackdropPath = backdrop
				s.logger.Debugf("发现多季合集本地背景图: %s", backdrop)
			}
			if series.PosterPath != "" && series.BackdropPath != "" {
				break
			}
		}
	}

	// 保存NFO和图片更新
	s.seriesRepo.Update(series)

	var totalNewCount int
	seasonSet := make(map[int]bool)
	// [个人影视库] 多季目录中任一带日期归一化的个人视频，合集整体打标
	isPersonalSeries := false

	// 扫描每个季目录
	for _, f := range folders {
		episodes := s.collectEpisodes(f.path)
		if len(episodes) == 0 {
			s.logger.Debugf("多季合集目录无视频文件: %s", f.path)
			continue
		}
		if episodes[0].IsPersonal {
			isPersonalSeries = true
		}

		// 如果目录名带有明确的季号，且剧集文件未识别出季号，则使用目录季号
		dirSeasonNum := f.seasonNum
		if dirSeasonNum == 0 {
			// 尝试用 parseSeasonFromDir 再识别一次
			dirSeasonNum = s.parseSeasonFromDir(f.dirName)
		}

		// === 集号重编逻辑 ===
		// 当检测到同一季目录下的集号是全局连续编号（延续上一季），而非从1开始时，
		// 自动重新编为季内相对编号。
		// 例如：第二季目录下文件名编号 [13][14]...[24]，应重编为 1,2,...,12
		if dirSeasonNum > 1 && len(episodes) > 0 {
			// 收集本目录下属于相同季号的"普通"剧集（排除OVA/SP等特殊类型的集号）
			var normalEpNums []int
			for _, ep := range episodes {
				// 判断是否为特殊剧集类型（OVA/SP等），它们的集号不参与重编判断
				isSpecial := false
				if m := episodePatterns[5].FindStringSubmatch(filepath.Base(ep.FilePath)); len(m) >= 2 {
					isSpecial = true
				}
				if !isSpecial && ep.EpisodeNum > 0 {
					normalEpNums = append(normalEpNums, ep.EpisodeNum)
				}
			}

			// 如果普通集号的最小值大于1，且集号是连续的，说明是全局编号需要重编
			if len(normalEpNums) > 0 {
				sort.Ints(normalEpNums)
				minEp := normalEpNums[0]

				if minEp > 1 {
					// 检查集号是否大致连续（允许少量缺失）
					isSequential := true
					for i := 1; i < len(normalEpNums); i++ {
						gap := normalEpNums[i] - normalEpNums[i-1]
						if gap > 2 { // 允许最多跳1集
							isSequential = false
							break
						}
					}

					if isSequential {
						// 计算偏移量，将集号重编为从1开始
						offset := minEp - 1
						s.logger.Infof("多季合集集号重编: %s 第%d季, 集号偏移 -%d (原始范围: %d~%d → 重编为 1~%d)",
							seriesTitle, dirSeasonNum, offset, minEp, normalEpNums[len(normalEpNums)-1], len(normalEpNums))

						for i := range episodes {
							// 只重编普通剧集，不重编OVA/SP等
							isSpecial := false
							if m := episodePatterns[5].FindStringSubmatch(filepath.Base(episodes[i].FilePath)); len(m) >= 2 {
								isSpecial = true
							}
							if !isSpecial && episodes[i].EpisodeNum > offset {
								episodes[i].EpisodeNum -= offset
							}
						}
					}
				}
			}
		}

		for _, ep := range episodes {
			// 季号分配：
			// 当目录名有明确季号时，优先使用目录季号（除非文件名中有不同的、合理的季号如S2标识的OVA）
			seasonNum := ep.SeasonNum
			if dirSeasonNum > 0 {
				// 如果文件名中的季号与目录季号不同且>1，说明文件自带了明确季号（如OVA标S2），保留它
				// 否则一律使用目录季号
				if seasonNum <= 1 || seasonNum == dirSeasonNum {
					seasonNum = dirSeasonNum
				}
			}
			if seasonNum == 0 {
				seasonNum = 1
			}

			// 检查是否已存在，如果存在则修正可能的脏数据（如 episode_title、season_num、episode_num）
			if existing, err := s.mediaRepo.FindByFilePath(ep.FilePath); err == nil {
				if s.deleteSourceDuplicateIfOrganized(library, existing, ep.FilePath, ep.FileInfo) {
					seasonSet[seasonNum] = true
					continue
				}
				seasonSet[seasonNum] = true
				needUpdate := false
				// [历史数据修复] 同 scanSeriesFolder：无剧集归属的旧行补挂到本合集，
				// 避免重扫后合集集数为 0、前端剧集列表为空。
				if existing.SeriesID == "" {
					existing.SeriesID = series.ID
					existing.MediaType = "episode"
					needUpdate = true
					s.logger.Infof("历史媒体记录补挂到多季合集: %s -> %s", filepath.Base(ep.FilePath), seriesTitle)
				}
				// [个人影视库] 分集名称与标题使用真实文件名（去扩展名）
				if displayTitle := strings.TrimSuffix(filepath.Base(ep.FilePath), filepath.Ext(ep.FilePath)); existing.Title != displayTitle {
					existing.Title = displayTitle
					needUpdate = true
				}
				if displayTitle := strings.TrimSuffix(filepath.Base(ep.FilePath), filepath.Ext(ep.FilePath)); existing.EpisodeTitle != displayTitle {
					existing.EpisodeTitle = displayTitle
					needUpdate = true
				}
				// [个人影视库] 旧数据重扫时补打个人视频标记，保证 SxxExx 展示抑制生效
				if ep.IsPersonal && !existing.IsPersonal {
					existing.IsPersonal = true
					needUpdate = true
				}
				if existing.SeasonNum != seasonNum {
					existing.SeasonNum = seasonNum
					needUpdate = true
				}
				if existing.EpisodeNum != ep.EpisodeNum {
					existing.EpisodeNum = ep.EpisodeNum
					needUpdate = true
				}
				// [海报回填] 同 scanSeriesFolder：旧数据缺本地封面时重扫补齐；
				// 旧版共享的通用命名封面也一并重算，保证每个视频独立海报。
				healPoster := existing.PosterPath != "" && s.nfoService.IsLegacySharedCover(existing.PosterPath)
				if existing.PosterPath == "" || healPoster || existing.BackdropPath == "" {
					poster, backdrop := s.nfoService.FindLocalImagesForMedia(ep.FilePath)
					if poster != "" && (existing.PosterPath == "" || healPoster) {
						existing.PosterPath = poster
						needUpdate = true
					}
					if backdrop != "" && existing.BackdropPath == "" {
						existing.BackdropPath = backdrop
						needUpdate = true
					}
				}
				if needUpdate {
					s.mediaRepo.Update(existing)
				}
				continue
			}
			if _, represented := s.findOrganizedHardlinkRecord(library, ep.FilePath, ep.FileInfo); represented {
				seasonSet[seasonNum] = true
				continue
			}
			if s.skipUndersized(library, ep.FilePath, ep.FileInfo.Size()) {
				continue
			}

			media := &model.Media{
				LibraryID:    library.ID,
				SeriesID:     series.ID,
				Title:        strings.TrimSuffix(filepath.Base(ep.FilePath), filepath.Ext(ep.FilePath)),
				FilePath:     ep.FilePath,
				FileSize:     ep.FileInfo.Size(),
				MediaType:    "episode",
				SeasonNum:    seasonNum,
				EpisodeNum:   ep.EpisodeNum,
				EpisodeTitle: strings.TrimSuffix(filepath.Base(ep.FilePath), filepath.Ext(ep.FilePath)),
				IsPersonal:   ep.IsPersonal,
			}

			s.probeMediaInfo(media)
			s.attachLocalEpisodeArt(media)
			s.scanExternalSubtitles(media)

			if err := s.mediaRepo.Create(media); err != nil {
				s.logger.Warnf("保存剧集失败: %s, 错误: %v", ep.FilePath, err)
				continue
			}

			seasonSet[seasonNum] = true
			totalNewCount++

			s.logger.Debugf("发现剧集(多季): %s [%s | %s]",
				filepath.Base(ep.FilePath), media.Resolution, media.VideoCodec)
			s.broadcastScanEvent(EventScanProgress, &ScanProgressData{
				LibraryID:   library.ID,
				LibraryName: library.Name,
				Phase:       "scanning",
				NewFound:    totalNewCount,
				Message:     fmt.Sprintf("发现: %s", filepath.Base(ep.FilePath)),
			})
		}
	}

	// 更新合集统计信息
	allEpisodes, _ := s.mediaRepo.ListBySeriesID(series.ID)
	series.EpisodeCount = len(allEpisodes)
	series.SeasonCount = len(seasonSet)
	if isPersonalSeries {
		series.IsPersonal = true
	}
	s.seriesRepo.Update(series)

	if totalNewCount > 0 {
		s.logger.Infof("多季合集扫描完成: %s, 新增 %d 集, 共 %d 季 %d 集",
			seriesTitle, totalNewCount, series.SeasonCount, series.EpisodeCount)
	}

	return totalNewCount, nil
}

// attachLocalEpisodeArt 为分集挂载本地图片：与视频同名的图片、
// 封面子目录中的同名图（如 011921.mp4 + M痴女_封面/011921.jpg），
// 以及单视频目录下的通用封面。与电影库导入逻辑保持一致。
func (s *ScannerService) attachLocalEpisodeArt(media *model.Media) {
	poster, backdrop := s.nfoService.FindLocalImagesForMedia(media.FilePath)
	if poster != "" && media.PosterPath == "" {
		media.PosterPath = poster
	}
	if backdrop != "" && media.BackdropPath == "" {
		media.BackdropPath = backdrop
	}
}

// scanSeriesFolder 扫描单个剧集文件夹
func (s *ScannerService) scanSeriesFolder(library *model.Library, folderPath, seriesTitle string) (int, error) {
	s.logger.Infof("扫描剧集: %s (%s)", seriesTitle, folderPath)

	// 查找或创建剧集合集条目
	series, err := s.seriesRepo.FindByFolderPath(folderPath)
	if err != nil {
		// 新剧集，创建合集条目
		series = &model.Series{
			LibraryID:  library.ID,
			Title:      seriesTitle,
			FolderPath: folderPath,
		}
		if err := s.seriesRepo.Create(series); err != nil {
			return 0, fmt.Errorf("创建剧集合集失败: %w", err)
		}
		s.logger.Infof("创建剧集合集: %s (ID=%s)", seriesTitle, series.ID)
	}

	// 识别本地 NFO 信息文件并解析剧集元数据
	if nfoPath := s.nfoService.FindNFOFile(folderPath); nfoPath != "" {
		if err := s.nfoService.ParseTVShowNFO(nfoPath, series); err != nil {
			s.logger.Debugf("解析剧集NFO失败: %s, 错误: %v", nfoPath, err)
		} else {
			s.logger.Debugf("从NFO读取剧集元数据: %s -> %s", nfoPath, series.Title)
			// 如果NFO中有标题，更新seriesTitle用于后续剧集
			if series.Title != "" {
				seriesTitle = series.Title
			}
		}
	}

	// 识别本地海报封面图片（含封面子目录，如 剧名/xxx_封面/01.jpg）
	if poster, backdrop := s.nfoService.FindLocalImagesDeep(folderPath); poster != "" || backdrop != "" {
		if poster != "" && series.PosterPath == "" {
			series.PosterPath = poster
			s.logger.Debugf("发现剧集本地海报: %s", poster)
		}
		if backdrop != "" && series.BackdropPath == "" {
			series.BackdropPath = backdrop
			s.logger.Debugf("发现剧集本地背景图: %s", backdrop)
		}
	}

	// 保存NFO和图片更新
	s.seriesRepo.Update(series)

	// 收集所有剧集文件
	episodes := s.collectEpisodes(folderPath)

	if len(episodes) == 0 {
		s.logger.Debugf("剧集文件夹无视频文件: %s", folderPath)
		// 如果该合集下已经没有任何剧集，清理这个空合集
		existingEpisodes, _ := s.mediaRepo.ListBySeriesID(series.ID)
		if len(existingEpisodes) == 0 {
			s.seriesRepo.Delete(series.ID)
			s.logger.Infof("清理空合集: %s (ID=%s)", seriesTitle, series.ID)
		}
		return 0, nil
	}

	// 导入剧集
	var newCount int
	seasonSet := make(map[int]bool)

	for _, ep := range episodes {
		// 检查是否已存在，如果存在则修正可能的脏数据
		if existing, err := s.mediaRepo.FindByFilePath(ep.FilePath); err == nil {
			if s.deleteSourceDuplicateIfOrganized(library, existing, ep.FilePath, ep.FileInfo) {
				seasonSet[ep.SeasonNum] = true
				continue
			}
			seasonSet[ep.SeasonNum] = true
			needUpdate := false
			// [历史数据修复] 该文件此前可能被当作独立电影/未归类分集入库（无剧集归属）。
			// 现在目录结构明确其属于本剧，补挂 SeriesID 并修正类型；否则剧集会一直
			// 因 episode_count=0 被列表过滤，表现为「剧集页没有内容」。
			if existing.SeriesID == "" {
				existing.SeriesID = series.ID
				existing.MediaType = "episode"
				needUpdate = true
				s.logger.Infof("历史媒体记录补挂到剧集: %s -> %s", filepath.Base(ep.FilePath), seriesTitle)
			}
			// [个人影视库] 分集标题与名称一律使用真实文件名（去扩展名），
			// 不展示「第N集」这类通用命名；重扫时同步修正旧数据。
			if displayTitle := strings.TrimSuffix(filepath.Base(ep.FilePath), filepath.Ext(ep.FilePath)); existing.Title != displayTitle {
				existing.Title = displayTitle
				needUpdate = true
			}
			// [个人影视库] 分集标题一律使用真实文件名（去扩展名），
			// 不展示「第N集」或日期等派生命名；重扫时同步修正旧数据。
			if displayTitle := strings.TrimSuffix(filepath.Base(ep.FilePath), filepath.Ext(ep.FilePath)); existing.EpisodeTitle != displayTitle {
				existing.EpisodeTitle = displayTitle
				needUpdate = true
			}
			// [个人影视库] 旧数据重扫时补打个人视频标记，保证 SxxExx 展示抑制生效
			if ep.IsPersonal && !existing.IsPersonal {
				existing.IsPersonal = true
				needUpdate = true
			}
			if existing.SeasonNum != ep.SeasonNum {
				existing.SeasonNum = ep.SeasonNum
				needUpdate = true
			}
			if existing.EpisodeNum != ep.EpisodeNum {
				existing.EpisodeNum = ep.EpisodeNum
				needUpdate = true
			}
			// [海报回填] 旧数据可能没有挂本地封面图，重扫时补齐，
			// 避免用户必须清库重扫才能看到海报。
			// 旧版共享的通用命名封面也一并重算，保证每个视频独立海报。
			healPoster := existing.PosterPath != "" && s.nfoService.IsLegacySharedCover(existing.PosterPath)
			if existing.PosterPath == "" || healPoster || existing.BackdropPath == "" {
				poster, backdrop := s.nfoService.FindLocalImagesForMedia(ep.FilePath)
				if poster != "" && (existing.PosterPath == "" || healPoster) {
					existing.PosterPath = poster
					needUpdate = true
				}
				if backdrop != "" && existing.BackdropPath == "" {
					existing.BackdropPath = backdrop
					needUpdate = true
				}
			}
			if needUpdate {
				s.mediaRepo.Update(existing)
			}
			continue
		}
		if _, represented := s.findOrganizedHardlinkRecord(library, ep.FilePath, ep.FileInfo); represented {
			seasonSet[ep.SeasonNum] = true
			continue
		}
		if s.skipUndersized(library, ep.FilePath, ep.FileInfo.Size()) {
			continue
		}

		// [个人影视库] 分集名称与标题使用真实文件名（去扩展名）
		displayTitle := strings.TrimSuffix(filepath.Base(ep.FilePath), filepath.Ext(ep.FilePath))

		media := &model.Media{
			LibraryID:    library.ID,
			SeriesID:     series.ID,
			Title:        displayTitle,
			FilePath:     ep.FilePath,
			FileSize:     ep.FileInfo.Size(),
			MediaType:    "episode",
			SeasonNum:    ep.SeasonNum,
			EpisodeNum:   ep.EpisodeNum,
			EpisodeTitle: displayTitle,
			IsPersonal:   ep.IsPersonal,
		}

		s.probeMediaInfo(media)
		s.attachLocalEpisodeArt(media)
		s.scanExternalSubtitles(media)

		if err := s.mediaRepo.Create(media); err != nil {
			s.logger.Warnf("保存剧集失败: %s, 错误: %v", ep.FilePath, err)
			continue
		}

		seasonSet[ep.SeasonNum] = true
		newCount++

		s.logger.Debugf("发现剧集: %s [%s | %s]", filepath.Base(ep.FilePath), media.Resolution, media.VideoCodec)
		s.broadcastScanEvent(EventScanProgress, &ScanProgressData{
			LibraryID:   library.ID,
			LibraryName: library.Name,
			Phase:       "scanning",
			NewFound:    newCount,
			Message:     fmt.Sprintf("发现: %s", displayTitle),
		})
	}

	// 更新合集统计信息
	allEpisodes, _ := s.mediaRepo.ListBySeriesID(series.ID)
	series.EpisodeCount = len(allEpisodes)
	series.SeasonCount = len(seasonSet)
	// [个人影视库] 个人视频合集：同目录任一文件被归一化为个人视频，则整个合集同步标记
	if len(episodes) > 0 && episodes[0].IsPersonal {
		series.IsPersonal = true
	}
	s.seriesRepo.Update(series)

	s.logger.Infof("剧集扫描完成: %s, 新增 %d 集, 共 %d 季 %d 集",
		seriesTitle, newCount, series.SeasonCount, series.EpisodeCount)

	return newCount, nil
}

// collectEpisodes 递归收集剧集文件夹下的所有视频文件
func (s *ScannerService) collectEpisodes(folderPath string) []EpisodeInfo {
	var episodes []EpisodeInfo

	s.walkLibraryPath(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !supportedExts[ext] {
			return nil
		}

		fileName := filepath.Base(path)
		ep := s.parseEpisodeInfo(fileName)

		// 尝试从Season目录名获取季号（如果文件名中没有季号）
		if ep.SeasonNum == 0 {
			parentDir := filepath.Base(filepath.Dir(path))
			if seasonNum := s.parseSeasonFromDir(parentDir); seasonNum > 0 {
				ep.SeasonNum = seasonNum
			}
		}

		// 默认季号为1
		if ep.SeasonNum == 0 {
			ep.SeasonNum = 1
		}

		ep.FilePath = path
		ep.FileInfo = info

		episodes = append(episodes, ep)
		return nil
	})

	// 按季号+集号排序
	sort.Slice(episodes, func(i, j int) bool {
		if episodes[i].SeasonNum != episodes[j].SeasonNum {
			return episodes[i].SeasonNum < episodes[j].SeasonNum
		}
		return episodes[i].EpisodeNum < episodes[j].EpisodeNum
	})

	// [个人视频场景] 目录内文件以日期命名为主（如人名目录下的 2024-01-15.mp4、
	// VID_20240115_223045.mp4）时，改为时间顺序编号：
	// 全部归入第 1 季、顺序集号、日期写入分集标题，
	// 避免「第 24 季 第 115 集」这类对个人视频无意义的年份/月日编码。
	// 允许少量非日期杂项文件（如 000000-000.mp4 占位、封面误入等）：
	// 当日期文件占比过半且未出现显式季集编号（S01E02 / 第3集）时，仍按时间顺序归一化，
	// 未标注日期的文件按名称排在末尾。
	datedCount := 0
	hasExplicitNumbering := false
	for _, ep := range episodes {
		if ep.AirDate != "" {
			datedCount++
		} else if episodePatterns[0].MatchString(filepath.Base(ep.FilePath)) ||
			episodePatterns[3].MatchString(filepath.Base(ep.FilePath)) {
			// 未标日期但带显式季集编号（S01E02 / 第3集）：这是真正的剧集结构，不做时间归一化
			hasExplicitNumbering = true
		}
	}
	majorityDated := len(episodes) > 0 && datedCount*2 >= len(episodes) && !hasExplicitNumbering
	// [个人影视库] 目录本身被判为「日期命名的个人视频」归组（如人名目录下出现日期视频）
	// 时，即使日期文件不是绝对多数，也按个人视频处理：时间顺序编号、不打 SxxExx。
	personalFolder := len(episodes) > 0 && !hasExplicitNumbering && s.hasDatedVideo(folderPath)
	if majorityDated || personalFolder {
		sort.SliceStable(episodes, func(i, j int) bool {
			di, dj := episodes[i].AirDate != "", episodes[j].AirDate != ""
			if di != dj {
				return di // 有日期的在前，无日期的排末尾
			}
			if episodes[i].AirDate != episodes[j].AirDate {
				return episodes[i].AirDate < episodes[j].AirDate
			}
			return episodes[i].FilePath < episodes[j].FilePath
		})
		for i := range episodes {
			episodes[i].SeasonNum = 1
			episodes[i].EpisodeNum = i + 1
			episodes[i].IsPersonal = true
			if episodes[i].AirDate != "" {
				episodes[i].EpisodeTitle = episodes[i].AirDate
			}
		}
		return episodes
	}

	// 如果所有集号都是0，按文件名排序后自动编号
	allZero := true
	for _, ep := range episodes {
		if ep.EpisodeNum > 0 {
			allZero = false
			break
		}
	}
	if allZero {
		sort.Slice(episodes, func(i, j int) bool {
			return episodes[i].FilePath < episodes[j].FilePath
		})
		for i := range episodes {
			episodes[i].EpisodeNum = i + 1
		}
		return episodes
	}

	// 部分文件没有解析到集号（如「花絮」「未命名」混在正常命名的分集中）：
	// 按文件路径顺序排在已解析的最大集号之后继续编号，避免出现「第0集」
	hasUnnumbered := false
	for i := range episodes {
		if episodes[i].EpisodeNum <= 0 {
			hasUnnumbered = true
			break
		}
	}
	if hasUnnumbered {
		maxNum := 0
		for i := range episodes {
			if episodes[i].EpisodeNum > maxNum {
				maxNum = episodes[i].EpisodeNum
			}
			if episodes[i].EpisodeNumEnd > maxNum {
				maxNum = episodes[i].EpisodeNumEnd
			}
		}
		sort.Slice(episodes, func(i, j int) bool {
			return episodes[i].FilePath < episodes[j].FilePath
		})
		next := maxNum
		for i := range episodes {
			if episodes[i].EpisodeNum <= 0 {
				next++
				episodes[i].EpisodeNum = next
			}
		}
	}

	return episodes
}

// parseEpisodeInfo 从文件名解析剧集信息
// 支持的命名格式：
//
//	标准格式: [字幕组][剧名][One-Punch Man][01][1280x720][简体]
//	季集格式: [HYSUB][ONE PUNCH MAN S2][OVA01][GB_MP4][1280X720].mp4
//	通用格式: S01E01, 1x01, 第1集, EP01, OVA01 等
func (s *ScannerService) parseEpisodeInfo(filename string) EpisodeInfo {
	var ep EpisodeInfo

	// 预处理：移除文件扩展名，方便后续解析
	nameWithoutExt := strings.TrimSuffix(filename, filepath.Ext(filename))

	// === 阶段零：多集连播检测（优先于单集匹配） ===

	// 多集模式0: S01E02-E03 / S01E02-E05
	if m := multiEpPatterns[0].FindStringSubmatch(filename); len(m) >= 4 {
		sNum, _ := strconv.Atoi(m[1])
		eStart, _ := strconv.Atoi(m[2])
		eEnd, _ := strconv.Atoi(m[3])
		if eEnd > eStart && sNum <= 30 {
			ep.SeasonNum = sNum
			ep.EpisodeNum = eStart
			ep.EpisodeNumEnd = eEnd
			ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, m[0])
			return ep
		}
	}

	// 多集模式1: S01E02-03 (无前缀E的范围)
	if m := multiEpPatterns[1].FindStringSubmatch(filename); len(m) >= 4 {
		sNum, _ := strconv.Atoi(m[1])
		eStart, _ := strconv.Atoi(m[2])
		eEnd, _ := strconv.Atoi(m[3])
		if eEnd > eStart && sNum <= 30 && !resolutionNums[eEnd] {
			ep.SeasonNum = sNum
			ep.EpisodeNum = eStart
			ep.EpisodeNumEnd = eEnd
			ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, m[0])
			return ep
		}
	}

	// === 阶段零-B：日期格式集号检测（日播剧/脱口秀/个人视频） ===
	if year, month, day, matched, ok := matchDateEpisode(filename); ok {
		// 验证日期合理性
		if year >= 1990 && year <= 2099 && month >= 1 && month <= 12 && day >= 1 && day <= 31 {
			// 不与 SxxExx 冲突：如果同时有 S01E01 格式，优先使用 SxxExx
			if !episodePatterns[0].MatchString(filename) && !episodePatterns[1].MatchString(filename) {
				ep.AirDate = fmt.Sprintf("%04d-%02d-%02d", year, month, day)
				// 将日期编码为集号: MMDD (方便排序)
				ep.EpisodeNum = month*100 + day
				ep.SeasonNum = year - 2000 // 年份作为季号标识（如 2024 → 24）
				ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, matched)
				return ep
			}
		}
	}

	// === 阶段一：提取集号（原有逻辑） ===

	// 模式 0: S01E01 — 最精确的格式，同时包含季号和集号
	if m := episodePatterns[0].FindStringSubmatch(filename); len(m) >= 3 {
		sNum, _ := strconv.Atoi(m[1])
		eNum, _ := strconv.Atoi(m[2])
		// 排除明显不合理的值：集号恰好是分辨率
		if !resolutionNums[eNum] || sNum <= 30 {
			ep.SeasonNum = sNum
			ep.EpisodeNum = eNum
			ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, m[0])
			return ep
		}
	}

	// 模式 1: S01.E01
	if m := episodePatterns[1].FindStringSubmatch(filename); len(m) >= 3 {
		sNum, _ := strconv.Atoi(m[1])
		eNum, _ := strconv.Atoi(m[2])
		if !resolutionNums[eNum] || sNum <= 30 {
			ep.SeasonNum = sNum
			ep.EpisodeNum = eNum
			ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, m[0])
			return ep
		}
	}

	// 模式 2: 1x01 — 排除分辨率如 "1920x1080" "1280x720"
	if m := episodePatterns[2].FindStringSubmatch(filename); len(m) >= 3 {
		sNum, _ := strconv.Atoi(m[1])
		eNum, _ := strconv.Atoi(m[2])
		if !resolutionNums[eNum] && !resolutionNums[sNum] && sNum < 100 {
			ep.SeasonNum = sNum
			ep.EpisodeNum = eNum
			ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, m[0])
			return ep
		}
	}

	// 模式 3: 第01集
	if m := episodePatterns[3].FindStringSubmatch(filename); len(m) >= 2 {
		ep.EpisodeNum, _ = strconv.Atoi(m[1])
		ep.SeasonNum = s.extractSeasonFromFilename(filename)
		ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, m[0])
		return ep
	}

	// 模式 4: EP01 / Episode 01
	if m := episodePatterns[4].FindStringSubmatch(filename); len(m) >= 2 {
		ep.EpisodeNum, _ = strconv.Atoi(m[1])
		ep.SeasonNum = s.extractSeasonFromFilename(filename)
		ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, m[0])
		return ep
	}

	// 模式 5: OVA01 / SP01 / SPECIAL01 等特殊剧集类型
	if m := episodePatterns[5].FindStringSubmatch(filename); len(m) >= 2 {
		ep.EpisodeNum, _ = strconv.Atoi(m[1])
		ep.SeasonNum = s.extractSeasonFromFilename(filename)
		ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, m[0])
		return ep
	}

	// 模式 6: E01（单独的E+数字）— 需排除分辨率上下文
	if m := episodePatterns[6].FindStringSubmatchIndex(filename); m != nil {
		full := filename[m[0]:m[1]]
		sub := filename[m[2]:m[3]]
		eNum, _ := strconv.Atoi(sub)
		if !resolutionNums[eNum] && !isResolutionContext(filename, m[1]) {
			ep.EpisodeNum = eNum
			ep.SeasonNum = s.extractSeasonFromFilename(filename)
			ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, full)
			return ep
		}
	}

	// 模式 7: [01] / [001] — 方括号内的纯数字（字幕组常用格式）
	if m := episodePatterns[7].FindStringSubmatch(filename); len(m) >= 2 {
		num, _ := strconv.Atoi(m[1])
		// 排除年份和分辨率数字
		if num > 0 && num < 1900 && !resolutionNums[num] {
			ep.EpisodeNum = num
			ep.SeasonNum = s.extractSeasonFromFilename(filename)
			ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, m[0])
			return ep
		}
	}

	// 模式 8: - 01 - / .01. — 最宽松的匹配，需要严格过滤
	if m := episodePatterns[8].FindStringSubmatchIndex(filename); m != nil {
		sub := filename[m[2]:m[3]]
		num, _ := strconv.Atoi(sub)
		if num > 0 && num < 1900 && !resolutionNums[num] && !isResolutionContext(filename, m[1]) {
			ep.EpisodeNum = num
			ep.SeasonNum = s.extractSeasonFromFilename(filename)
			ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, filename[m[0]:m[1]])
			return ep
		}
	}

	// 模式 9: 尾部括号编号 Short (01) / ゆうき美羽 (3) / 松本メイ(1)（个人收藏常见命名）
	if m := trailingParenEpPattern.FindStringSubmatch(nameWithoutExt); len(m) >= 2 {
		num, _ := strconv.Atoi(m[1])
		if num > 0 && num < 1900 && !resolutionNums[num] {
			ep.EpisodeNum = num
			ep.SeasonNum = s.extractSeasonFromFilename(filename)
			ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, m[0])
			return ep
		}
	}

	// 模式 10: 下划线尾号 Saved_003 / NANA_001（个人收藏常见命名）
	if m := trailingUnderscoreEpPattern.FindStringSubmatch(nameWithoutExt); len(m) >= 2 {
		num, _ := strconv.Atoi(m[1])
		if num > 0 && num < 1900 && !resolutionNums[num] {
			ep.EpisodeNum = num
			ep.SeasonNum = s.extractSeasonFromFilename(filename)
			ep.EpisodeTitle = s.extractEpisodeTitle(nameWithoutExt, m[0])
			return ep
		}
	}

	// === 阶段二（C 方案）：统一电视剧解析器兜底 ===
	// 处理 03A / 02B / 特别篇1 / SP01 / OVA02 / 单纯数字结尾等 parseEpisodeInfo 无法识别的脏命名
	if parsed := ParseEpisodeFilename(filename); parsed.EpisodeNum > 0 {
		ep.EpisodeNum = parsed.EpisodeNum
		ep.EpisodeNumEnd = parsed.EpisodeNumEnd
		ep.SeasonNum = parsed.SeasonNum
		if parsed.IsSpecial {
			ep.SeasonNum = 0 // 特别篇归 Season 0
		}
		ep.EpisodeTitle = parsed.VersionTag // 把 "A" "B" 等版本号塞到 EpisodeTitle 作提示
		return ep
	}

	return ep
}

// extractSeasonFromFilename 从文件名中独立提取季号
// 处理文件名中包含 S2、Season 2、第2季 等情况（不依赖集号格式）
func (s *ScannerService) extractSeasonFromFilename(filename string) int {
	for _, pattern := range seasonInFilenamePatterns {
		if m := pattern.FindStringSubmatch(filename); len(m) >= 2 {
			num, _ := strconv.Atoi(m[1])
			if num > 0 && num <= 30 {
				return num
			}
		}
	}
	return 0
}

// extractEpisodeTitle 从文件名中提取集标题（集号模式之后的部分）
func (s *ScannerService) extractEpisodeTitle(nameWithoutExt string, matchedPattern string) string {
	idx := strings.Index(nameWithoutExt, matchedPattern)
	if idx < 0 {
		return ""
	}
	after := nameWithoutExt[idx+len(matchedPattern):]
	// 清理开头的分隔符和空格
	after = strings.TrimLeft(after, " .-_")
	if after == "" {
		return ""
	}
	// 去除尾部常见的元信息标记（分辨率/编码/组名等括号内容）
	// 例如 "[1080p]" "(BDRip)" "[FLAC]" 等
	metaPattern := regexp.MustCompile(`[\[\(].*[\]\)]`)
	after = metaPattern.ReplaceAllString(after, "")
	after = strings.TrimRight(after, " .-_")
	// 如果剩余内容太短或全是数字，则不作为标题
	if len(after) <= 1 {
		return ""
	}
	// 排除纯数字（可能是分辨率等残留）
	if _, err := strconv.Atoi(after); err == nil {
		return ""
	}
	// 排除分辨率字符串（如 720p、1080p、4K 等）
	resPattern := regexp.MustCompile(`(?i)^\d{3,4}[pi]$|^[248]K$`)
	if resPattern.MatchString(after) {
		return ""
	}
	// 排除纯技术性标记（编码/混流/来源等），这些不是有意义的剧集标题
	// 例如：remux, remux nvl, x264, HEVC, BDRip, WEB-DL 等
	techPattern := regexp.MustCompile(`(?i)^[\s\-\.]*(?:remux|re-?mux|nvl|x26[45]|h\.?26[45]|hevc|avc|aac|flac|dts|bdri?p|dvdri?p|web-?dl|web-?rip|blu-?ray|hdr|10bit|ma[25]\.?[01]|truehd|atmos|opus)(?:[\s\-\.]+(?:remux|nvl|x26[45]|h\.?26[45]|hevc|avc|aac|flac|dts|bdri?p|dvdri?p|web-?dl|web-?rip|blu-?ray|hdr|10bit|ma[25]\.?[01]|truehd|atmos|opus))*[\s\-\.]*$`)
	if techPattern.MatchString(after) {
		return ""
	}
	return after
}

// parseSeasonFromDir 从Season目录名解析季号
func (s *ScannerService) parseSeasonFromDir(dirName string) int {
	for _, pattern := range seasonDirPatterns {
		if m := pattern.FindStringSubmatch(dirName); len(m) >= 2 {
			num, _ := strconv.Atoi(m[1])
			return num
		}
		// Specials特别篇 -> 季号 0
		if pattern.MatchString(dirName) && strings.Contains(strings.ToLower(dirName), "special") {
			return 0
		}
	}
	return 0
}

// extractSeriesNameFromFile 从视频文件名中提取系列名称
// 适用于根目录下散落的剧集文件，如 [HYSUB][ONE PUNCH MAN][01].mkv -> ONE PUNCH MAN
func (s *ScannerService) extractSeriesNameFromFile(filename string) string {
	// 去掉扩展名
	name := strings.TrimSuffix(filename, filepath.Ext(filename))

	// 模式1: [字幕组][系列名][集号] 格式
	// 匹配方括号中的内容，提取第二个方括号的内容作为系列名
	bracketPattern := regexp.MustCompile(`\[([^\[\]]+)\]`)
	matches := bracketPattern.FindAllStringSubmatch(name, -1)
	if len(matches) >= 2 {
		// 遍历方括号内容，找到最可能是系列名的部分
		// 跳过: 纯数字（集号）、分辨率（720P/1080P）、编码格式等
		skipPatterns := []*regexp.Regexp{
			regexp.MustCompile(`(?i)^\d+$`),                                                          // 纯数字
			regexp.MustCompile(`(?i)^\d{3,4}[PpKk]$`),                                                // 分辨率如720P
			regexp.MustCompile(`(?i)^\d+[Xx]\d+$`),                                                   // 分辨率如1280X720
			regexp.MustCompile(`(?i)^(BIG5|GB|UTF-?8|MP4|MKV|AVI|HEVC|H\.?26[45]|AAC|FLAC|x26[45])`), // 编码/格式
			regexp.MustCompile(`(?i)^(BIG5_MP4|GB_MP4|CHS|CHT|JPN|ENG)`),                             // 字幕/编码组合
			regexp.MustCompile(`(?i)^S\d+E\d+$`),                                                     // 剧集号 S01E01
			regexp.MustCompile(`(?i)^EP?\s*\d+$`),                                                    // EP01
			regexp.MustCompile(`(?i)^V\d+$`),                                                         // 版本号 V2
			regexp.MustCompile(`(?i)^(WebRip|BDRip|DVDRip|WEB-DL|BluRay|HDTV)$`),                     // 来源
		}

		// 通常第一个方括号是字幕组，第二个是系列名
		// 但也可能系列名在其他位置，需要智能判断
		candidates := []string{}
		for _, m := range matches {
			content := strings.TrimSpace(m[1])
			if content == "" {
				continue
			}
			skip := false
			for _, sp := range skipPatterns {
				if sp.MatchString(content) {
					skip = true
					break
				}
			}
			if !skip {
				candidates = append(candidates, content)
			}
		}

		// 如果有多个候选项，选择第二个（通常第一个是字幕组名）
		if len(candidates) >= 2 {
			return candidates[1]
		}
		if len(candidates) == 1 {
			return candidates[0]
		}
	}

	// 模式2: 尝试从文件名中移除集号信息后得到系列名
	// 先去掉所有方括号内容和常见标记
	cleanName := name
	cleanName = bracketPattern.ReplaceAllString(cleanName, " ")

	// 移除集号模式 S01E01, EP01, E01, 第N集
	epPatterns := []string{
		`(?i)S\d{1,2}\s*E\d{1,4}`,
		`(?i)S\d{1,2}\.\s*E\d{1,4}`,
		`(?i)\d{1,2}x\d{1,4}`,
		`第\s*\d{1,4}\s*集`,
		// 中文常见的期/话/回计数（综艺、脱口秀、动漫）
		`第\s*\d{1,4}\s*期`,
		`第\s*\d{1,4}\s*[話话]`,
		`第\s*\d{1,4}\s*回`,
		// 日播/日期型集号：20240115、2024.01.15、2024-01-15（要求完整 8 位日期，避免误伤年份）
		`\b(19|20)\d{2}\s*[.\-_]?\s*\d{2}\s*[.\-_]?\s*\d{2}\b`,
		`(?i)(?:EP|Episode)\s*\.?\s*\d{1,4}`,
		`(?i)\bE\d{1,4}\b`,
	}
	for _, p := range epPatterns {
		re := regexp.MustCompile(p)
		cleanName = re.ReplaceAllString(cleanName, " ")
	}

	// 移除分辨率、编码等常见标记
	cleanPatterns := []string{
		`(?i)\b(BluRay|BDRip|HDRip|WEB-?DL|WEBRip|HDTV|COMPLETE)\b`,
		`(?i)\b(1080p|720p|2160p|4K)\b`,
		`(?i)\b(x264|x265|HEVC|AAC|FLAC)\b`,
	}
	for _, p := range cleanPatterns {
		re := regexp.MustCompile(p)
		cleanName = re.ReplaceAllString(cleanName, " ")
	}

	// 清理分隔符和多余空格
	cleanName = strings.ReplaceAll(cleanName, ".", " ")
	cleanName = strings.ReplaceAll(cleanName, "_", " ")
	cleanName = strings.ReplaceAll(cleanName, "-", " ")
	cleanName = regexp.MustCompile(`\s+`).ReplaceAllString(cleanName, " ")
	cleanName = strings.TrimSpace(cleanName)

	// 移除末尾的纯数字（可能是集号）
	cleanName = regexp.MustCompile(`\s+\d{1,4}\s*$`).ReplaceAllString(cleanName, "")
	cleanName = strings.TrimSpace(cleanName)

	// === C 方案：统一清洗（去广告、去站点标签、去编码噪声、剥离季号） ===
	cleanName = NormalizeSeriesTitle(cleanName)

	if len(cleanName) > 0 {
		return cleanName
	}

	// === C 方案兜底：原有逻辑都失败 → 调用新的电视剧解析器 ===
	if parsed := ParseEpisodeFilename(filename); parsed.SeriesTitle != "" {
		return parsed.SeriesTitle
	}

	return ""
}

// extractSeriesTitle 从文件夹名提取剧集标题
func (s *ScannerService) extractSeriesTitle(folderName string) string {
	title := folderName

	// 移除年份信息，如 "Breaking Bad (2008)"
	yearRegex := regexp.MustCompile(`\s*[\(\[]\.?(\d{4})[\)\]]\.?\s*$`)
	title = yearRegex.ReplaceAllString(title, "")

	// 清理常见标记
	cleanPatterns := []string{
		`(?i)\b(BluRay|BDRip|HDRip|WEB-?DL|WEBRip|HDTV|COMPLETE)\b`,
		`(?i)\b(1080p|720p|2160p|4K)\b`,
		`(?i)\b(x264|x265|HEVC)\b`,
	}
	for _, p := range cleanPatterns {
		re := regexp.MustCompile(p)
		title = re.ReplaceAllString(title, "")
	}

	// 替换常见分隔符
	title = strings.ReplaceAll(title, ".", " ")
	title = strings.ReplaceAll(title, "_", " ")

	// 清理多余空格
	title = regexp.MustCompile(`\s+`).ReplaceAllString(title, " ")
	title = strings.TrimSpace(title)

	// === C 方案：套上统一清洗（去【xxx压制】、【Q群】、[站点]、季号尾缀等） ===
	if normalized := NormalizeSeriesTitle(title); normalized != "" {
		return normalized
	}
	return title
}

// broadcastScanEvent 广播扫描事件
