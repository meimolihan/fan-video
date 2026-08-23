package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"go.uber.org/zap"
)

// NFOService NFO 本地元数据解析服务
// 支持 Kodi / Emby / Jellyfin 风格的 NFO XML 文件
//
// V2.1: 支持 webdav:// 前缀路径，通过 VFSManager 读取远程 NFO 与图片
// V2.2: 支持视频第一帧提取作为海报兜底
type NFOService struct {
	logger *zap.SugaredLogger

	// V2.1: VFS 管理器（可选，nil 时纯本地模式）
	vfsMgr *VFSManager

	// V2.2: 配置（用于获取 ffmpeg 路径）
	cfg *config.Config
}

func NewNFOService(logger *zap.SugaredLogger, cfg *config.Config) *NFOService {
	return &NFOService{logger: logger, cfg: cfg}
}

// SetVFSManager 注入 VFS 管理器（V2.1，用于 webdav:// NFO 支持）
func (s *NFOService) SetVFSManager(vfsMgr *VFSManager) {
	s.vfsMgr = vfsMgr
}

// ==================== VFS 辅助方法 ====================

// readFile 读取文件（支持 webdav://）
func (s *NFOService) readFile(p string) ([]byte, error) {
	if s.vfsMgr != nil && IsWebDAVPath(p) {
		f, err := s.vfsMgr.Open(p)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(f)
	}
	return os.ReadFile(p)
}

// statPath 获取文件信息（支持 webdav://）
func (s *NFOService) statPath(p string) (os.FileInfo, error) {
	if s.vfsMgr != nil && IsWebDAVPath(p) {
		return s.vfsMgr.Stat(p)
	}
	return os.Stat(p)
}

// readDir 读取目录（支持 webdav://）
func (s *NFOService) readDir(p string) ([]os.DirEntry, error) {
	if s.vfsMgr != nil && IsWebDAVPath(p) {
		entries, err := s.vfsMgr.ReadDir(p)
		if err != nil {
			return nil, err
		}
		result := make([]os.DirEntry, len(entries))
		copy(result, entries)
		return result, nil
	}
	return os.ReadDir(p)
}

// joinPath 路径拼接（webdav:// 用正斜杠）
func (s *NFOService) joinPath(base, name string) string {
	if IsWebDAVPath(base) {
		base = strings.TrimRight(base, "/")
		return base + "/" + strings.TrimLeft(name, "/")
	}
	return filepath.Join(base, name)
}

// dirOf 提取目录（webdav:// 用正斜杠规则）
func (s *NFOService) dirOf(p string) string {
	if IsWebDAVPath(p) {
		i := strings.LastIndex(p, "/")
		if i <= len("webdav:/") {
			return p
		}
		return p[:i]
	}
	return filepath.Dir(p)
}

// ==================== NFO XML 结构体 ====================

// NFOMovie 电影 NFO XML 根元素
type NFOMovie struct {
	XMLName      xml.Name   `xml:"movie"`
	Title        string     `xml:"title"`
	OrigTitle    string     `xml:"originaltitle"`
	SortTitle    string     `xml:"sorttitle"`
	Year         int        `xml:"year"`
	Premiered    string     `xml:"premiered"`
	ReleaseDate  string     `xml:"releasedate"`
	Release      string     `xml:"release"`
	Plot         string     `xml:"plot"`
	Outline      string     `xml:"outline"`
	OriginalPlot string     `xml:"originalplot"`
	Tagline      string     `xml:"tagline"`
	Rating       float64    `xml:"rating"`
	Runtime      int        `xml:"runtime"`
	Num          string     `xml:"num"`
	MPAA         string     `xml:"mpaa"`
	CustomRating string     `xml:"customrating"`
	CountryCode  string     `xml:"countrycode"`
	Studio       string     `xml:"studio"`
	Maker        string     `xml:"maker"`
	Publisher    string     `xml:"publisher"`
	Label        string     `xml:"label"`
	Website      string     `xml:"website"`
	Country      string     `xml:"country"`
	TMDbID       int        `xml:"tmdbid"`
	DoubanID     string     `xml:"doubanid"`
	Genres       []string   `xml:"genre"`
	Tags         []string   `xml:"tag"`
	Directors    []string   `xml:"director"`
	Actors       []NFOActor `xml:"actor"`
}

// NFOTVShow 剧集 NFO XML 根元素
type NFOTVShow struct {
	XMLName   xml.Name   `xml:"tvshow"`
	Title     string     `xml:"title"`
	OrigTitle string     `xml:"originaltitle"`
	Year      int        `xml:"year"`
	Plot      string     `xml:"plot"`
	Rating    float64    `xml:"rating"`
	Studio    string     `xml:"studio"`
	Country   string     `xml:"country"`
	TMDbID    int        `xml:"tmdbid"`
	DoubanID  string     `xml:"doubanid"`
	Genres    []string   `xml:"genre"`
	Directors []string   `xml:"director"`
	Actors    []NFOActor `xml:"actor"`
}

// NFOActor NFO 演员信息
type NFOActor struct {
	Name      string `xml:"name"`
	Role      string `xml:"role"`
	Thumb     string `xml:"thumb"`
	SortOrder int    `xml:"sortorder"`
}

// ==================== 解析方法 ====================

// ParseMovieNFO 解析电影 NFO 文件并将数据应用到 Media 对象
func (s *NFOService) ParseMovieNFO(nfoPath string, media *model.Media) error {
	data, err := s.readFile(nfoPath)
	if err != nil {
		return fmt.Errorf("读取NFO文件失败: %w", err)
	}

	var nfo NFOMovie
	if err := xml.Unmarshal(data, &nfo); err != nil {
		// 尝试作为 tvshow 解析
		var tvNFO NFOTVShow
		if err2 := xml.Unmarshal(data, &tvNFO); err2 != nil {
			return fmt.Errorf("解析NFO XML失败: %w", err)
		}
		// 如果是 tvshow 格式，转换后应用
		s.applyTVShowNFOToMedia(media, &tvNFO)
		return nil
	}

	s.applyMovieNFOToMedia(media, &nfo)
	return nil
}

// ParseTVShowNFO 解析剧集 NFO 文件并将数据应用到 Series 对象
func (s *NFOService) ParseTVShowNFO(nfoPath string, series *model.Series) error {
	data, err := s.readFile(nfoPath)
	if err != nil {
		return fmt.Errorf("读取NFO文件失败: %w", err)
	}

	var nfo NFOTVShow
	if err := xml.Unmarshal(data, &nfo); err != nil {
		return fmt.Errorf("解析NFO XML失败: %w", err)
	}

	s.applyTVShowNFOToSeries(series, &nfo)
	return nil
}

// GetActorsFromNFO 从 NFO 文件中提取演员列表
func (s *NFOService) GetActorsFromNFO(nfoPath string) ([]NFOActor, []string, error) {
	data, err := s.readFile(nfoPath)
	if err != nil {
		return nil, nil, err
	}

	// 先尝试 movie
	var movie NFOMovie
	if err := xml.Unmarshal(data, &movie); err == nil && movie.Title != "" {
		return movie.Actors, movie.Directors, nil
	}

	// 再尝试 tvshow
	var tvshow NFOTVShow
	if err := xml.Unmarshal(data, &tvshow); err == nil && tvshow.Title != "" {
		return tvshow.Actors, tvshow.Directors, nil
	}

	return nil, nil, fmt.Errorf("无法解析NFO文件")
}

// ==================== 本地图片扫描 ====================

// 常见视频文件扩展名
var nfoVideoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".mov": true,
	".wmv": true, ".flv": true, ".webm": true, ".m4v": true, ".ts": true,
	".rmvb": true, ".rm": true, ".3gp": true, ".mpg": true, ".mpeg": true,
	".strm": true,
}

// 常见图片文件扩展名
var nfoImageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}

// 常见本地海报文件名（按优先级排序，支持 .jpg/.jpeg/.png/.webp）
var standardPosterNames = []string{
	"poster.jpg", "poster.jpeg", "poster.png", "poster.webp",
	"cover.jpg", "cover.jpeg", "cover.png", "cover.webp",
	"folder.jpg", "folder.jpeg", "folder.png", "folder.webp",
	"thumb.jpg", "thumb.jpeg", "thumb.png", "thumb.webp",
	"movie.jpg", "movie.jpeg", "movie.png",
	"show.jpg", "show.jpeg", "show.png",
}

// 常见本地背景图文件名
var standardBackdropNames = []string{
	"fanart.jpg", "fanart.jpeg", "fanart.png", "fanart.webp",
	"backdrop.jpg", "backdrop.jpeg", "backdrop.png", "backdrop.webp",
	"banner.jpg", "banner.jpeg", "banner.png", "banner.webp",
	"background.jpg", "background.jpeg", "background.png", "background.webp",
	"clearart.jpg", "clearart.jpeg", "clearart.png",
	"landscape.jpg", "landscape.jpeg", "landscape.png",
}

// FindLocalImages 在指定目录下查找本地图片（poster/fanart/banner 等）
// 适用于剧集/合集等场景，不区分具体视频文件
// 支持 jpg、png、webp 等常见图片格式
func (s *NFOService) FindLocalImages(dir string) (poster, backdrop string) {
	for _, name := range standardPosterNames {
		path := s.joinPath(dir, name)
		if _, err := s.statPath(path); err == nil {
			poster = path
			break
		}
	}

	for _, name := range standardBackdropNames {
		path := s.joinPath(dir, name)
		if _, err := s.statPath(path); err == nil {
			backdrop = path
			break
		}
	}

	// 如果没有找到标准命名的海报，尝试查找目录中的第一张图片作为海报
	if poster == "" {
		entries, err := s.readDir(dir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					ext := strings.ToLower(filepath.Ext(entry.Name()))
					if nfoImageExts[ext] {
						// 排除已识别为backdrop的文件
						candidate := s.joinPath(dir, entry.Name())
						if candidate != backdrop {
							poster = candidate
							break
						}
					}
				}
			}
		}
	}

	return poster, backdrop
}

// FindLocalImagesDeep 在目录及其一层子目录中查找海报/背景图。
// 相比 FindLocalImages 额外覆盖「封面位于子目录」的个人收藏常见结构
// （如 剧名/M痴女_封面/011921.jpg、図鉴_封面/メイリン.JPG）：
// 优先匹配封面命名子目录（封面图/封面/cover/poster/img 等，前缀或包含均可），
// 未命中再回退任意子目录中的第一张图片。
func (s *NFOService) FindLocalImagesDeep(dir string) (poster, backdrop string) {
	poster, backdrop = s.FindLocalImages(dir)
	if poster != "" && backdrop != "" {
		return poster, backdrop
	}

	entries, err := s.readDir(dir)
	if err != nil {
		return poster, backdrop
	}
	imageExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}
	firstImageIn := func(subDir string) string {
		subEntries, err := s.readDir(subDir)
		if err != nil {
			return ""
		}
		for _, entry := range subEntries {
			if entry.IsDir() {
				continue
			}
			if imageExts[strings.ToLower(filepath.Ext(entry.Name()))] {
				return s.joinPath(subDir, entry.Name())
			}
		}
		return ""
	}

	// 封面命名的子目录优先（前缀或包含关键词，如 M痴女_封面、cover_2）
	allSubs := subDirsFromEntries(entries)
	isCoverNamed := func(name string) bool {
		lower := strings.ToLower(name)
		for _, known := range localCoverSubDirs {
			if strings.HasPrefix(lower, known) || strings.Contains(lower, known) {
				return true
			}
		}
		return false
	}
	for _, sub := range allSubs {
		if !isCoverNamed(sub) {
			continue
		}
		if p := firstImageIn(s.joinPath(dir, sub)); p != "" {
			if poster == "" {
				poster = p
				if backdrop != "" {
					break
				}
			} else if backdrop == "" {
				backdrop = p
				break
			}
		}
	}
	// 任意子目录兜底
	for _, sub := range allSubs {
		if isCoverNamed(sub) {
			continue
		}
		if p := firstImageIn(s.joinPath(dir, sub)); p != "" {
			if poster == "" {
				poster = p
				if backdrop != "" {
					break
				}
			} else if backdrop == "" {
				backdrop = p
				break
			}
		}
	}
	return poster, backdrop
}

// FindLocalImagesForMedia 根据媒体文件路径查找对应的本地图片
// 方案 C：优先匹配与视频同名的图片，当目录下只有一个视频文件时才使用通用封面
// 解决多部影片在同一目录下共用同一张封面的问题
func (s *NFOService) FindLocalImagesForMedia(mediaFilePath string) (poster, backdrop string) {
	dir := s.dirOf(mediaFilePath)
	baseName := strings.TrimSuffix(filepath.Base(mediaFilePath), filepath.Ext(mediaFilePath))

	// === 阶段1：优先查找与视频文件同名的图片 ===
	posterSuffixes := []string{
		"-poster.jpg", "-poster.jpeg", "-poster.png", "-poster.webp",
		"-cover.jpg", "-cover.jpeg", "-cover.png", "-cover.webp",
		"-thumb.jpg", "-thumb.jpeg", "-thumb.png", "-thumb.webp",
		".jpg", ".jpeg", ".png", ".webp",
	}
	backdropSuffixes := []string{
		"-fanart.jpg", "-fanart.png", "-fanart.webp",
		"-backdrop.jpg", "-backdrop.png", "-backdrop.webp",
		"-banner.jpg", "-banner.png", "-banner.webp",
	}

	for _, suffix := range posterSuffixes {
		path := s.joinPath(dir, baseName+suffix)
		if _, err := s.statPath(path); err == nil {
			poster = path
			break
		}
	}

	for _, suffix := range backdropSuffixes {
		path := s.joinPath(dir, baseName+suffix)
		if _, err := s.statPath(path); err == nil {
			backdrop = path
			break
		}
	}

	// === 阶段1b：任意子目录中的同名图片 ===
	// 子目录名不限定（如「封面图_xxx」「图片」或任意命名），
	// 如 流浪地球.mp4 -> 封面图_xxx/流浪地球.jpg
	var coverSubs []string
	if entries, err := s.readDir(dir); err == nil {
		subDirs := subDirsFromEntries(entries)
		coverSubs = matchCoverSubDirsFromEntries(entries)
		if poster == "" {
			for _, sub := range subDirs {
				subDir := s.joinPath(dir, sub)
				for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
					path := s.joinPath(subDir, baseName+ext)
					if _, err := s.statPath(path); err == nil {
						poster = path
						break
					}
				}
				if poster != "" {
					break
				}
			}
		}
	}

	// === 阶段2：统计目录中的视频文件数量（用于阶段3判断） ===
	countEntries, err := s.readDir(dir)
	if err != nil {
		return poster, backdrop
	}
	videoCount := 0
	for _, entry := range countEntries {
		if !entry.IsDir() {
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if nfoVideoExts[ext] {
				videoCount++
				if videoCount > 1 {
					break
				}
			}
		}
	}

	// === 阶段3：根据视频文件数量决定是否使用通用封面 ===
	if videoCount <= 1 {
		for _, name := range standardPosterNames {
			path := s.joinPath(dir, name)
			if _, err := s.statPath(path); err == nil {
				poster = path
				break
			}
		}

		// 封面子目录中的通用命名（如 封面图/poster.jpg）
		if poster == "" {
			for _, sub := range coverSubs {
				subDir := s.joinPath(dir, sub)
				for _, name := range standardPosterNames {
					path := s.joinPath(subDir, name)
					if _, err := s.statPath(path); err == nil {
						poster = path
						break
					}
				}
				if poster != "" {
					break
				}
			}
		}

		for _, name := range standardBackdropNames {
			path := s.joinPath(dir, name)
			if _, err := s.statPath(path); err == nil {
				backdrop = path
				break
			}
		}

		// 兜底：目录中第一张图片
		if poster == "" {
			for _, entry := range countEntries {
				if !entry.IsDir() {
					ext := strings.ToLower(filepath.Ext(entry.Name()))
					if nfoImageExts[ext] {
						candidate := s.joinPath(dir, entry.Name())
						if candidate != backdrop {
							poster = candidate
							break
						}
					}
				}
			}
		}
	} else {
		s.logger.Debugf("目录 %s 下有 %d 个视频文件，跳过通用封面分配", dir, videoCount)
	}

	// === 阶段4：如果仍未找到海报，提取视频第一帧作为兜底 ===
	// 仅对本地文件生效（不支持 webdav:// 等远程路径）
	// 注意：此阶段在阶段3之后执行，确保优先使用通用封面
	if poster == "" && !IsWebDAVPath(mediaFilePath) {
		if firstFrame, err := s.EnsureFirstFramePoster(mediaFilePath); err == nil && firstFrame != "" {
			poster = firstFrame
			s.logger.Debugf("本地海报未找到，提取视频第一帧: %s -> %s", mediaFilePath, poster)
		}
	}

	return poster, backdrop
}

// FindNFOFile 在指定目录下查找 NFO 文件
func (s *NFOService) FindNFOFile(dir string) string {
	entries, err := s.readDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".nfo") {
			return s.joinPath(dir, entry.Name())
		}
	}
	return ""
}

// FindNFOForMedia 根据媒体文件路径查找关联的 NFO 文件
func (s *NFOService) FindNFOForMedia(mediaFilePath string) string {
	// 策略1: 同名 .nfo 文件
	ext := filepath.Ext(mediaFilePath)
	nfoPath := strings.TrimSuffix(mediaFilePath, ext) + ".nfo"
	if _, err := s.statPath(nfoPath); err == nil {
		return nfoPath
	}

	// 策略2: 目录下任意 .nfo 文件
	dir := s.dirOf(mediaFilePath)
	return s.FindNFOFile(dir)
}

// ==================== 视频第一帧提取 ====================

// firstFrameMu 串行化首帧提取，避免并发请求对同一视频重复 spawn ffmpeg
var firstFrameMu sync.Mutex

// EnsureFirstFramePoster 提取视频第一帧作为封面海报（带持久化缓存）。
//
// 缓存键 = 文件绝对路径 + 大小 + 修改时间：
//   - 不同目录下的同名视频互不影响（修复旧实现仅按文件名缓存导致的串图问题）；
//   - 视频文件被替换后自动重新提取（旧实现永远返回过期缓存）；
//
// 缓存持久化在 cfg.Cache.CacheDir/covers/frames 下（旧实现放在易失的系统临时目录，
// 重启后全部丢失且数据库仍指向失效路径）。提取失败时自动从「第1秒处」回退到
// 「真正的第一帧」，兼容时长不足 1 秒的视频。
func (s *NFOService) EnsureFirstFramePoster(videoPath string) (string, error) {
	// 跳过 webdav 等远程路径与 strm 文本文件
	if IsWebDAVPath(videoPath) {
		return "", fmt.Errorf("不支持远程路径")
	}
	if strings.EqualFold(filepath.Ext(videoPath), ".strm") {
		return "", fmt.Errorf("strm 文件不支持提取首帧")
	}

	info, err := os.Stat(videoPath)
	if err != nil {
		return "", fmt.Errorf("视频文件不存在: %w", err)
	}

	cacheDir := s.firstFrameCacheDir()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("创建缓存目录失败: %w", err)
	}

	key := firstFrameCacheKey(videoPath, info)
	outputPath := filepath.Join(cacheDir, key+".jpg")

	// 命中有效缓存直接返回
	if st, statErr := os.Stat(outputPath); statErr == nil && st.Size() > 0 {
		return outputPath, nil
	}

	firstFrameMu.Lock()
	defer firstFrameMu.Unlock()

	// 双重检查：并发场景下可能已被其他请求生成
	if st, statErr := os.Stat(outputPath); statErr == nil && st.Size() > 0 {
		return outputPath, nil
	}

	tmpPath := filepath.Join(cacheDir, "."+key+".tmp.jpg")
	defer os.Remove(tmpPath)

	// FFmpeg 提取单帧：-ss 放在 -i 前 = input seeking（快）；
	// 先试第1秒处（避开片头黑帧），输出无效再回退到真正的第一帧
	for _, seekPos := range []string{"00:00:01", "0"} {
		args := []string{
			"-y",
			"-ss", seekPos,
			"-i", videoPath,
			"-frames:v", "1",
			"-q:v", "2",
			tmpPath,
		}
		cmd := exec.Command(s.ffmpegPath(), args...)
		output, cmdErr := cmd.CombinedOutput()

		if st, statErr := os.Stat(tmpPath); statErr == nil && st.Size() > 0 {
			if renameErr := os.Rename(tmpPath, outputPath); renameErr != nil {
				return "", fmt.Errorf("保存封面失败: %w", renameErr)
			}
			s.logger.Debugf("提取视频首帧成功(seek=%s): %s -> %s", seekPos, videoPath, outputPath)
			return outputPath, nil
		}
		s.logger.Debugf("提取视频首帧失败(seek=%s): %s - %v\n%s", seekPos, videoPath, cmdErr, string(output))
	}

	return "", fmt.Errorf("无法从视频提取首帧: %s", videoPath)
}

// firstFrameCacheDir 首帧封面缓存目录：优先使用应用缓存目录（随容器卷持久化）
func (s *NFOService) firstFrameCacheDir() string {
	if s.cfg != nil && strings.TrimSpace(s.cfg.Cache.CacheDir) != "" {
		return filepath.Join(s.cfg.Cache.CacheDir, "covers", "frames")
	}
	return filepath.Join(os.TempDir(), "fan-video", "frames")
}

// IsFirstFrameCachePath 判断给定路径是否位于首帧封面缓存目录内。
// 用于识别"剧集海报"是真实 artwork（本地图片/刮削图）还是从某个分集首帧借用的封面：
// 借用的封面不应被分集的海报回退链再次继承，否则同一剧集的所有分集都会显示同一张图。
func (s *NFOService) IsFirstFrameCachePath(path string) bool {
	if s == nil || strings.TrimSpace(path) == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	cacheRoot := s.firstFrameCacheDir()
	if absRoot, err := filepath.Abs(cacheRoot); err == nil {
		cacheRoot = absRoot
	}
	return strings.HasPrefix(abs, cacheRoot+string(filepath.Separator))
}

// legacyGenericCoverBases 通用命名封面的文件名主干（去扩展名后小写比对）。
// 这类命名指向「目录级」封面，在多视频目录中被旧版逻辑共享错配。
var legacyGenericCoverBases = map[string]bool{
	"poster": true, "cover": true, "folder": true, "thumb": true,
	"movie": true, "show": true, "fanart": true, "banner": true,
}

// IsLegacySharedCover 判断已入库的海报路径是否为通用命名封面。
// 多视频目录中，这类封面通常是旧版匹配逻辑共享给同目录所有视频的，
// 重扫时应重算（同名图片 > 子目录同名图 > 首帧），保证每个视频独立海报。
// 单视频目录不受影响：通用封面在该场景下唯一对应唯一视频，无需重算。
func (s *NFOService) IsLegacySharedCover(posterPath string) bool {
	if posterPath == "" || s.IsFirstFrameCachePath(posterPath) {
		return false
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(posterPath), filepath.Ext(posterPath)))
	return legacyGenericCoverBases[base]
}

// firstFrameCacheKey 生成防碰撞、随文件内容变化自动失效的缓存键
func firstFrameCacheKey(videoPath string, info os.FileInfo) string {
	absPath := videoPath
	if abs, err := filepath.Abs(videoPath); err == nil {
		absPath = abs
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d",
		absPath, info.Size(), info.ModTime().UnixNano())))
	return hex.EncodeToString(h[:])[:20]
}

// ffmpegPath 解析可用的 ffmpeg 可执行文件路径
func (s *NFOService) ffmpegPath() string {
	if s.cfg != nil && s.cfg.App.FFmpegPath != "" {
		return s.cfg.App.FFmpegPath
	}
	return "ffmpeg"
}

// ==================== 应用 NFO 数据 ====================

func (s *NFOService) applyMovieNFOToMedia(media *model.Media, nfo *NFOMovie) {
	if nfo.Title != "" {
		media.Title = nfo.Title
	}
	if nfo.OrigTitle != "" {
		media.OrigTitle = nfo.OrigTitle
	}
	if nfo.SortTitle != "" {
		media.SortTitle = nfo.SortTitle
	}
	if nfo.Year > 0 {
		media.Year = nfo.Year
	}
	if nfo.Premiered != "" {
		media.Premiered = nfo.Premiered
	}
	// 发行日期优先级：releasedate > release > premiered
	if nfo.ReleaseDate != "" {
		media.ReleaseDate = nfo.ReleaseDate
	} else if nfo.Release != "" {
		media.ReleaseDate = nfo.Release
	} else if nfo.Premiered != "" {
		media.ReleaseDate = nfo.Premiered
	}
	if nfo.Plot != "" {
		media.Overview = nfo.Plot
	}
	if nfo.Outline != "" {
		media.Outline = nfo.Outline
	}
	if nfo.OriginalPlot != "" {
		media.OriginalPlot = nfo.OriginalPlot
	}
	if nfo.Rating > 0 {
		media.Rating = nfo.Rating
	}
	if nfo.Runtime > 0 {
		media.Runtime = nfo.Runtime
	}
	if len(nfo.Genres) > 0 {
		media.Genres = strings.Join(nfo.Genres, ",")
	}
	if len(nfo.Tags) > 0 {
		media.Tags = strings.Join(nfo.Tags, ",")
	}
	if nfo.Tagline != "" {
		media.Tagline = nfo.Tagline
	}
	if nfo.Studio != "" {
		media.Studio = nfo.Studio
	}
	if nfo.Maker != "" {
		media.Maker = nfo.Maker
	}
	if nfo.Publisher != "" {
		media.Publisher = nfo.Publisher
	}
	if nfo.Label != "" {
		media.Label = nfo.Label
	}
	if nfo.Num != "" {
		media.Num = nfo.Num
	}
	// MPAA 优先级：mpaa > customrating
	if nfo.MPAA != "" {
		media.MPAA = nfo.MPAA
	} else if nfo.CustomRating != "" {
		media.MPAA = nfo.CustomRating
	}
	if nfo.CountryCode != "" {
		media.CountryCode = nfo.CountryCode
	}
	if nfo.Website != "" {
		media.Website = nfo.Website
	}
	if nfo.Country != "" {
		media.Country = nfo.Country
	}
	if nfo.TMDbID > 0 {
		media.TMDbID = nfo.TMDbID
	}
	if nfo.DoubanID != "" {
		media.DoubanID = nfo.DoubanID
	}
}

func (s *NFOService) applyTVShowNFOToMedia(media *model.Media, nfo *NFOTVShow) {
	if nfo.Title != "" {
		media.Title = nfo.Title
	}
	if nfo.OrigTitle != "" {
		media.OrigTitle = nfo.OrigTitle
	}
	if nfo.Year > 0 {
		media.Year = nfo.Year
	}
	if nfo.Plot != "" {
		media.Overview = nfo.Plot
	}
	if nfo.Rating > 0 {
		media.Rating = nfo.Rating
	}
	if len(nfo.Genres) > 0 {
		media.Genres = strings.Join(nfo.Genres, ",")
	}
	if nfo.Country != "" {
		media.Country = nfo.Country
	}
}

func (s *NFOService) applyTVShowNFOToSeries(series *model.Series, nfo *NFOTVShow) {
	if nfo.Title != "" {
		series.Title = nfo.Title
	}
	if nfo.OrigTitle != "" {
		series.OrigTitle = nfo.OrigTitle
	}
	if nfo.Year > 0 {
		series.Year = nfo.Year
	}
	if nfo.Plot != "" {
		series.Overview = nfo.Plot
	}
	if nfo.Rating > 0 {
		series.Rating = nfo.Rating
	}
	if len(nfo.Genres) > 0 {
		series.Genres = strings.Join(nfo.Genres, ",")
	}
	if nfo.Studio != "" {
		series.Studio = nfo.Studio
	}
	if nfo.Country != "" {
		series.Country = nfo.Country
	}
	if nfo.TMDbID > 0 {
		series.TMDbID = nfo.TMDbID
	}
	if nfo.DoubanID != "" {
		series.DoubanID = nfo.DoubanID
	}
}

// localCoverSubDirs 常见封面子目录名前缀
var localCoverSubDirs = []string{"封面图", "封面", "cover", "covers", "poster", "posters", "img", "images", "图片"}

// subDirsFromEntries 返回目录列表中的全部子目录名（保持遍历顺序）
func subDirsFromEntries(entries []os.DirEntry) []string {
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	return dirs
}

// matchCoverSubDirsFromEntries 基于已有目录列表匹配封面子目录（供 webdav 等复用）
func matchCoverSubDirsFromEntries(entries []os.DirEntry) []string {
	var matched []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		for _, known := range localCoverSubDirs {
			if strings.HasPrefix(name, known) {
				matched = append(matched, entry.Name())
				break
			}
		}
	}
	return matched
}

// sameArtworkPath 判断两个海报/剧照路径是否指向同一资源
func sameArtworkPath(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if strings.Contains(a, "://") || strings.Contains(b, "://") {
		return strings.EqualFold(a, b)
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// isSeriesCacheArtworkPath 判断路径是否为剧集缓存图片目录内的资源
func isSeriesCacheArtworkPath(p, seriesID string) bool {
	p = strings.TrimSpace(p)
	seriesID = strings.TrimSpace(seriesID)
	if p == "" || seriesID == "" {
		return false
	}
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(p)))
	needle := "/images/series/" + strings.ToLower(seriesID) + "/"
	return strings.Contains(normalized, needle)
}
