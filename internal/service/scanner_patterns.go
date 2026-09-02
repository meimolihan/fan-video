package service

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// 支持的视频文件扩展名
var supportedExts = map[string]bool{
	".mkv":  true,
	".mp4":  true,
	".avi":  true,
	".mov":  true,
	".wmv":  true,
	".flv":  true,
	".webm": true,
	".m4v":  true,
	".ts":   true,
	".strm": true, // STRM 远程流文件
}

// extrasExcludeDirs Emby/Kodi 标准的非正片内容目录名（小写）
var extrasExcludeDirs = map[string]bool{
	"extras":            true,
	"extra":             true,
	"featurettes":       true,
	"behind the scenes": true,
	"deleted scenes":    true,
	"interviews":        true,
	"trailers":          true,
	"trailer":           true,
	"samples":           true,
	"sample":            true,
	"shorts":            true,
	"scenes":            true,
	"bonus":             true,
	"bonus features":    true,
}

// extrasSuffixes Emby 标准的特典文件命名后缀（小写）
var extrasSuffixes = []string{
	"-behindthescenes", "-deleted", "-featurette",
	"-interview", "-scene", "-short", "-trailer", "-sample",
}

// ==================== xiaoya / 小雅多级分类目录适配 ====================
//
// 适配以下典型目录结构（媒体库根直接选到 /media/xiaoya 即可，无需用户配置）：
//   xiaoya/
//     ├── 115/
//     │   ├── 电视剧/【我推的孩子】(2024)/Season 1/*.mkv
//     │   ├── 电影/...
//     │   └── 动漫/...
//     ├── 电视剧/...
//     ├── 电影/...
//     └── ISO/                 ← 直接跳过
//
// 策略：
//   1. 扫描入口会先调用 expandCategoryRoots 把"分类目录"穿透展开成真实媒体根列表；
//   2. extrasExcludeDirs + xiaoyaSkipDirs 在 Walk 过程中直接 SkipDir；
//   3. 标题提取同时兼容中文/全角括号与【】《》等装饰符号。

// xiaoyaCategoryDirs 已知的"分类目录"名（需要穿透递归，向下一层找真正的剧集/电影目录）
// key 使用原样（含中文）的目录名；比较时会忽略大小写
var xiaoyaCategoryDirs = map[string]bool{
	// 中文分类
	"电视剧": true, "电影": true, "动漫": true, "短剧": true,
	"纪录片": true, "纪录片（已刮削）": true, "纪录片(已刮削)": true,
	"综艺": true, "演唱会": true, "音乐": true, "每日更新": true,
	// xiaoya 常见的"来源分组"目录
	"115": true, "115盘": true, "阿里云盘": true, "夸克": true, "夸克网盘": true,
	"每日更新夸克": true, "xiaoya": true, "小雅": true,
	// Jellyfin / Emby / Plex 标准英文分类目录（必须支持，否则会把整库误判为单部剧集）
	"movies": true, "movie": true, "films": true, "film": true,
	"tv": true, "tv shows": true, "tvshows": true, "shows": true, "tv-shows": true, "tv_shows": true,
	"series": true, "tvseries": true, "tv series": true,
	"anime": true, "animation": true, "cartoons": true,
	"documentaries": true, "documentary": true, "docs": true,
	"music videos": true, "musicvideos": true, "concerts": true,
	"kids": true, "children": true, "family": true,
	// 常见整理后的暂存目录（不应被识别为剧集名）
	"_unsorted": true, "unsorted": true, "untagged": true, "incoming": true, "inbox": true,
	"_organized": true, "organized": true,
}

// xiaoyaNonTVCategoryDirs 在"电视剧扫描"场景下需要整体忽略的分类目录
// 这些目录下的视频通常是 MV/演唱会/音乐/综艺/每日更新的散落短视频，
// 直接参与剧集聚合会产生大量噪声"伪剧集"。
// 比较时会忽略大小写与首尾空白
var xiaoyaNonTVCategoryDirs = map[string]bool{
	"综艺":   true,
	"演唱会":  true,
	"音乐":   true,
	"mv":   true,
	"每日更新": true,
}

// isNonTVCategoryDirName 判断目录名是否为"电视剧扫描"应忽略的非剧集分类
func isNonTVCategoryDirName(name string) bool {
	n := strings.TrimSpace(name)
	return xiaoyaNonTVCategoryDirs[strings.ToLower(n)] || xiaoyaNonTVCategoryDirs[n]
}

// xiaoyaSkipDirs 完全跳过（不扫描内部）的特殊目录
// 比较时会忽略大小写
var xiaoyaSkipDirs = map[string]bool{
	"iso":    true,
	"json":   true,
	"画质演示":   true,
	"画质演示测试": true,
	"画质演示测试（4k，8k，hdr，dolby）": true,
	"bdmv":        true, // 完整蓝光结构，当前不支持解析
	"certificate": true,
	"backup":      true,
}

// isCategoryDirName 按名字判断是否为已知的"分类目录"
func isCategoryDirName(name string) bool {
	return xiaoyaCategoryDirs[strings.ToLower(strings.TrimSpace(name))] ||
		xiaoyaCategoryDirs[strings.TrimSpace(name)]
}

// isXiaoyaSkipDir 按名字判断是否为需要完全跳过的特殊目录
func isXiaoyaSkipDir(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if xiaoyaSkipDirs[lower] {
		return true
	}
	// 简单兜底：目录名以 "画质演示" 开头的一律跳过
	if strings.HasPrefix(strings.TrimSpace(name), "画质演示") {
		return true
	}
	return false
}

// seasonOnlyDirRe 用来识别纯季号目录名（这种目录名不能作为系列标题）
//
//	匹配示例："Season 01", "Season1", "S01", "S1", "第一季", "第02季", "第 2 季", "第二部"
var seasonOnlyDirRe = regexp.MustCompile(`(?i)^\s*(?:season\s*\d{1,2}|s\d{1,2}|第\s*[一二三四五六七八九十\d]+\s*[季部])\s*$`)

// isSeasonOnlyDirName 判断目录名是否是"纯季号"目录（不是真正的剧集名称）
// 这种目录通常作为剧集名目录的子目录存在，例如 一拳超人/Season 01/xxx.mp4
func isSeasonOnlyDirName(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return true
	}
	return seasonOnlyDirRe.MatchString(n)
}

// looksLikeSeriesFolder 判断给定目录看起来像一个"标准剧集合集"目录
//  1. 直接含视频文件；或
//  2. 含至少一个"季号"子目录（Season 01 / S01 / 第X季）；或
//  3. 含 tvshow.nfo
//
// looksLikeSeriesFolder 判断指定目录是否"看起来像一个剧集文件夹"
// 严格条件：必须出现以下任一明确特征：
//  1. 目录内含 tvshow.nfo
//  2. 目录内含明确的 Season XX 子目录（即标准剧集合集结构）
//  3. 目录内含视频文件，且这些视频从命名上看是剧集（含 SxxExx / 第x集 等明确剧集关键字）
//
// 旧实现"任何子目录里有视频文件就返回 true"会被混合库误命中（如 _unsorted 目录有零散视频
// 时把整个 _organized 库根误判为剧集合集，导致不下钻、Movies/TV Shows 被合并）。
func (s *ScannerService) looksLikeSeriesFolder(path string) bool {
	entries, err := s.readDirLibraryPath(path)
	if err != nil {
		return false
	}
	var hasEpisodicVideo bool
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() {
			lower := strings.ToLower(name)
			if lower == "tvshow.nfo" {
				return true
			}
			ext := strings.ToLower(filepath.Ext(name))
			if supportedExts[ext] {
				// 仅当文件名包含明确的剧集编号特征时才算
				if hasEpisodicNamePattern(name) {
					hasEpisodicVideo = true
				}
			}
			continue
		}
		if isSeasonOnlyDirName(name) {
			return true
		}
	}
	return hasEpisodicVideo
}

// hasEpisodicNamePattern 文件名是否包含明确的"剧集编号"特征
// 例如：S01E02 / s1e2 / 第03话 / 第3集 / EP05 / E12
func hasEpisodicNamePattern(name string) bool {
	lower := strings.ToLower(name)
	// SxxExx / sxe
	if matched, _ := regexp.MatchString(`(?i)\bs\d{1,2}[\s_.-]*e\d{1,3}\b`, lower); matched {
		return true
	}
	// 中文剧集："第N集" / "第N话" / "第N回"
	if matched, _ := regexp.MatchString(`第\s*\d{1,4}\s*[集话話回]`, name); matched {
		return true
	}
	// "EP12" / "EP-12"
	if matched, _ := regexp.MatchString(`(?i)\bep[\s_.-]*\d{1,3}\b`, lower); matched {
		return true
	}
	return false
}

// yearInNameAnyBracketPattern 兼容中文/全角括号的年份正则
//
//	支持: (2024) [2024] （2024） 【2024】
var yearInNameAnyBracketPattern = regexp.MustCompile(`[\(\[（【]\s*((?:19|20)\d{2})\s*[\)\]）】]`)

// normalizeXiaoyaTitle 清洗 xiaoya/小雅风格的标题，返回去除装饰符号后的干净标题
// 例如：
//
//	"【我推的孩子】"     → "我推的孩子"
//	"#居酒屋新干线"     → "居酒屋新干线"
//	"《三体》"           → "三体"
//	"3 Body Problem"  → "3 Body Problem"（保持不变）
func normalizeXiaoyaTitle(raw string) string {
	if raw == "" {
		return raw
	}
	s := raw

	// 1. 移除首尾常见装饰前缀/后缀字符（# ＃ ★ ☆ ♥ ♡ 以及未配对的半边括号）
	s = regexp.MustCompile(`^[#＃★☆♥♡・·\s]+`).ReplaceAllString(s, "")

	// 2. 成对装饰括号内容保留：【xxx】→ xxx，《xxx》→ xxx，「xxx」→ xxx，『xxx』→ xxx
	pairPatterns := []*regexp.Regexp{
		regexp.MustCompile(`[【](.*?)[】]`),
		regexp.MustCompile(`[《](.*?)[》]`),
		regexp.MustCompile(`[「](.*?)[」]`),
		regexp.MustCompile(`[『](.*?)[』]`),
		regexp.MustCompile(`[〈](.*?)[〉]`),
	}
	for _, p := range pairPatterns {
		// 反复替换直到稳定（处理嵌套情况）
		for {
			next := p.ReplaceAllString(s, "$1")
			if next == s {
				break
			}
			s = next
		}
	}

	// 3. 全角空格 → 半角空格，全角破折号 → 半角
	s = strings.ReplaceAll(s, "　", " ")

	// 4. 清理多余空格
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// isExtrasPath 判断文件路径是否在非正片目录下
func isExtrasPath(filePath string) bool {
	parts := strings.Split(filepath.ToSlash(filePath), "/")
	for _, part := range parts {
		if extrasExcludeDirs[strings.ToLower(part)] {
			return true
		}
	}
	return false
}

// isExtrasFile 判断文件名是否含有非正片后缀
func isExtrasFile(filename string) bool {
	lower := strings.ToLower(strings.TrimSuffix(filename, filepath.Ext(filename)))
	for _, suffix := range extrasSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// idTagPatterns 从文件名/文件夹名中提取元数据 ID 的正则
var idTagPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)[\[\{](tmdbid|tmdb)[=\-](\d+)[\]\}]`),
	regexp.MustCompile(`(?i)[\[\{](imdbid|imdb)[=\-](tt\d+)[\]\}]`),
	regexp.MustCompile(`(?i)[\[\{](tvdbid|tvdb)[=\-](\d+)[\]\}]`),
}

// yearInNamePattern 从文件名/文件夹名中提取年份 (2009) 或 [2009]
var yearInNamePattern = regexp.MustCompile(`[\(\[]((?:19|20)\d{2})[\)\]]`)

// parseIDFromName 从文件名/文件夹名中提取元数据 ID
func parseIDFromName(name string) (idType string, idValue string) {
	for _, pattern := range idTagPatterns {
		if m := pattern.FindStringSubmatch(name); len(m) >= 3 {
			return strings.ToLower(m[1]), m[2]
		}
	}
	return "", ""
}

// stackingPatterns 多 CD/多版本堆叠检测正则（P2）
var stackingPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)[_\-\.\s](cd|disc|disk|part|pt|dvd)\s*(\d+)`),
	regexp.MustCompile(`(?i)[_\-\.\s](cd|disc|disk|part|pt|dvd)\s*([a-d])`),
}

// versionPatterns 多版本检测正则（P2: Director's Cut, Extended, Remastered 等）
var versionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(director'?s?\s*cut|extended|unrated|remastered|theatrical|imax|criterion|special\s*edition)`),
	regexp.MustCompile(`(?i)\b(remux|2160p|1080p|720p|4k|uhd|hdr|sdr|3d)\b`),
}

// extractYearFromName 从文件名/文件夹名中提取年份
// 优先匹配标准 ASCII 括号 (2024)/[2024]；失败时再尝试中文/全角括号 （2024）/【2024】（xiaoya 常见）
func extractYearFromName(name string) int {
	if m := yearInNamePattern.FindStringSubmatch(name); len(m) >= 2 {
		year, _ := strconv.Atoi(m[1])
		if year >= 1900 && year <= 2099 {
			return year
		}
	}
	if m := yearInNameAnyBracketPattern.FindStringSubmatch(name); len(m) >= 2 {
		year, _ := strconv.Atoi(m[1])
		if year >= 1900 && year <= 2099 {
			return year
		}
	}
	return 0
}

// FFprobeResult FFprobe输出结构
