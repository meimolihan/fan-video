package service

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// DebugScanPlan 只读诊断：模拟扫描器对媒体库目录的识别决策，不写数据库。
// 返回逐行文本报告，用于排查「剧集中没有内容」类问题。
func (s *ScannerService) DebugScanPlan(libraryPath string, kind string) []string {
	var out []string
	add := func(format string, args ...interface{}) {
		out = append(out, fmt.Sprintf(format, args...))
	}

	add("=== 扫描决策诊断（只读，不修改数据库） ===")
	add("库路径: %s", libraryPath)
	add("库类型: %s", kind)
	add("")

	roots := s.collectMediaRootInfos(libraryPath, kind)
	add("── 媒体根展开（collectMediaRootInfos）: 共 %d 个", len(roots))
	for _, mr := range roots {
		add("  [depth=%d] %s", mr.Depth, mr.Path)
	}
	add("")

	// 汇总「将会成为剧集」的目录组（模拟 seriesDirGroups）
	type plan struct {
		name     string
		season   int
		path     string
		episodes []EpisodeInfo
		note     string
	}
	groups := map[string][]plan{}
	var order []string

	for _, mr := range roots {
		root := mr.Path
		add("──────────────────────────────")
		add("媒体根 [depth=%d] %s", mr.Depth, root)

		entries, err := s.readDirLibraryPath(root)
		if err != nil {
			add("  ✗ 目录无法读取: %v", err)
			continue
		}

		var directVideos, looseFiles []string
		unsupported := map[string]int{}
		var subDirNames []string
		for _, e := range entries {
			if e.IsDir() {
				subDirNames = append(subDirNames, e.Name())
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if supportedExts[ext] {
				directVideos = append(directVideos, e.Name())
				looseFiles = append(looseFiles, e.Name())
			} else if ext != "" && !strings.HasPrefix(e.Name(), ".") {
				unsupported[ext]++
			}
		}
		sort.Strings(subDirNames)
		add("  直属视频 %d 个, 子目录 %d 个", len(directVideos), len(subDirNames))
		if len(unsupported) > 0 {
			keys := make([]string, 0, len(unsupported))
			total := 0
			for k, n := range unsupported {
				keys = append(keys, k)
				total += n
			}
			sort.Strings(keys)
			add("  ⚠ 不支持/被忽略的文件扩展名: %v（共 %d 个文件不会入库）", keys, total)
		}

		// 与 scanMixedLibrary 一致的三项判定
		seasonChildCount, nonSeasonChildCount := 0, 0
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if isSeasonOnlyDirName(e.Name()) {
				seasonChildCount++
			} else if !isXiaoyaSkipDir(e.Name()) && !extrasExcludeDirs[strings.ToLower(e.Name())] {
				nonSeasonChildCount++
			}
		}
		rootIsSeriesFolder := seasonChildCount > 0 && nonSeasonChildCount == 0

		rootHasSeriesEvidence := false
		if mr.Depth > 0 {
			rootHasSeriesEvidence = s.isTVShowFolder(root)
		} else {
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
				for _, name := range directVideos {
					if s.parseEpisodeInfo(name).EpisodeNum > 0 {
						rootHasSeriesEvidence = true
						break
					}
				}
			}
		}
		sameDirGrouped := mr.Depth > 0 && !rootIsSeriesFolder && !rootHasSeriesEvidence && (s.isSameDirVideoGroup(root) || s.hasDatedVideo(root))

		record := func(path, dirName, note string) {
			base := filepath.Base(path)
			normalizedName := s.normalizeSeriesName(base)
			if normalizedName == "" {
				normalizedName = "__series_" + base
			}
			seasonNum := s.extractSeasonFromDirName(dirName)
			title := s.extractSeriesTitle(dirName)
			eps := s.collectEpisodes(path)
			p := plan{name: normalizedName, season: seasonNum, path: path, episodes: eps, note: note}
			key := normalizedName
			if _, exists := groups[key]; !exists {
				order = append(order, key)
			}
			groups[key] = append(groups[key], p)
			add("  → 判定为剧集目录 (%s)", note)
			add("     系列名=%q 季号=%d 标题=%q", normalizedName, seasonNum, title)
			add("     收集到 %d 个分集:", len(eps))
			for _, ep := range eps {
				rel, _ := filepath.Rel(path, ep.FilePath)
				add("       · %-40s S%02d E%02d", rel, ep.SeasonNum, ep.EpisodeNum)
			}
		}

		rootClassifiedAsSeries := false
		switch {
		case rootIsSeriesFolder:
			record(root, filepath.Base(root),
				fmt.Sprintf("子目录全为季目录(%d)", seasonChildCount))
			rootClassifiedAsSeries = true
		case rootHasSeriesEvidence:
			record(root, filepath.Base(root), "直属视频命中剧集命名/isTVShowFolder")
			rootClassifiedAsSeries = true
		case sameDirGrouped:
			record(root, filepath.Base(root), "穿透内容目录且含多个视频→同目录归组")
			rootClassifiedAsSeries = true
		default:
			add("  → 媒体根本身不是剧集，按子目录分类:")
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				dirName := e.Name()
				if isXiaoyaSkipDir(dirName) || extrasExcludeDirs[strings.ToLower(dirName)] {
					add("     · [%s] 跳过特殊目录", dirName)
					continue
				}
				folderPath := vfsJoin(root, dirName)
				switch {
				case s.isTVShowFolder(folderPath):
					record(folderPath, dirName, "isTVShowFolder: 季目录/剧集命名证据")
				case s.isSameDirVideoGroup(folderPath):
					record(folderPath, dirName, "同目录多视频归组(isSameDirVideoGroup)")
				case s.hasDatedVideo(folderPath):
					record(folderPath, dirName, "日期命名视频归组(hasDatedVideo, 个人视频场景)")
				default:
					videos, _ := s.collectSeriesEvidence(folderPath)
					add("     · [%s] 判定为电影目录（正片候选视频 %d 个）", dirName, len(videos))
				}
			}
		}

		if len(looseFiles) > 0 && !rootClassifiedAsSeries {
			add("  散落直属视频 %d 个 → 将按电影入库（库根散落视频不归组）", len(looseFiles))
		}
	}

	add("")
	add("════ 最终剧集分组预览 ════")
	if len(groups) == 0 {
		add("⚠ 没有任何目录会被识别为剧集！这就是「剧集中没有内容」的直接原因。")
		add("  请把本报告完整发给开发者。")
		return out
	}
	for _, key := range order {
		plans := groups[key]
		totalEps := 0
		for _, p := range plans {
			totalEps += len(p.episodes)
		}
		if len(plans) > 1 {
			add("▶ 系列 %q（%d 个目录合并为同一部剧）: 预计 %d 集", key, len(plans), totalEps)
		} else {
			add("▶ 系列 %q: 预计 %d 集", key, totalEps)
		}
		for _, p := range plans {
			add("    目录: %s", p.path)
		}
	}

	return out
}
