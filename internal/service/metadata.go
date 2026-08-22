package service

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

// MetadataService 本地元数据服务。
//
// 精简后的职责（无任何网络刮削）：
//  1. 本地海报匹配：在视频文件所在目录的任意子目录中查找与视频同名的图片
//     （如 流浪地球.mp4 ↔ 封面图_xxx/流浪地球.jpg），支持 .jpg .jpeg .png .webp；
//  2. 手动图片管理：上传本地图片 / 从 URL 设置图片；
//  3. 刮削状态回写与 WebSocket 进度广播。
type MetadataService struct {
	mediaRepo  *repository.MediaRepo
	seriesRepo *repository.SeriesRepo
	cfg        *config.Config
	logger     *zap.SugaredLogger
	client     *http.Client // 仅用于“从 URL 设置图片”的手动功能
	wsHub      *WSHub
	nfoService *NFOService // 本地 NFO 解析 + 本地图片匹配
}

func NewMetadataService(mediaRepo *repository.MediaRepo, seriesRepo *repository.SeriesRepo, cfg *config.Config, logger *zap.SugaredLogger) *MetadataService {
	return &MetadataService{
		mediaRepo:  mediaRepo,
		seriesRepo: seriesRepo,
		cfg:        cfg,
		logger:     logger,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

// SetWSHub 注入 WebSocket 广播器
func (s *MetadataService) SetWSHub(hub *WSHub) {
	s.wsHub = hub
}

// SetNFOService 注入 NFO 服务（本地图片匹配的核心实现）
func (s *MetadataService) SetNFOService(nfo *NFOService) {
	s.nfoService = nfo
}

// broadcastScrapeEvent 广播刮削事件
func (s *MetadataService) broadcastScrapeEvent(eventType string, data *ScrapeProgressData) {
	if s.wsHub != nil {
		s.wsHub.BroadcastEvent(eventType, data)
	}
}

// RefreshLocalArtworkForMedia 为单个媒体执行本地海报匹配。
// 匹配规则：视频同名图片优先（含任意名称子目录中的同名图片），
// 其次目录级通用命名（仅当目录下只有一个视频时）。
func (s *MetadataService) RefreshLocalArtworkForMedia(media *model.Media) error {
	if media == nil || media.FilePath == "" {
		return fmt.Errorf("媒体文件路径为空")
	}

	// 已手动锁定的记录不做覆盖
	if media.ScrapeStatus == "manual" {
		return nil
	}

	poster, backdrop := s.findLocalArtwork(media.FilePath)

	fields := map[string]any{
		"last_scrape_at": time.Now(),
	}
	if poster != "" && poster != media.PosterPath {
		fields["poster_path"] = poster
		media.PosterPath = poster
	}
	if backdrop != "" && backdrop != media.BackdropPath {
		fields["backdrop_path"] = backdrop
		media.BackdropPath = backdrop
	}

	if poster != "" {
		// 本地海报已就位，视为完成
		if media.ScrapeStatus != "scraped" {
			fields["scrape_status"] = "scraped"
			media.ScrapeStatus = "scraped"
		}
	} else if media.ScrapeStatus == "" || media.ScrapeStatus == "pending" {
		// 未找到也不标 failed：保持 pending，等待用户补充图片后重新扫描
		fields["scrape_status"] = "pending"
	}

	return s.mediaRepo.UpdateFields(media.ID, fields)
}

// findLocalArtwork 查找媒体文件对应的本地海报/背景图
func (s *MetadataService) findLocalArtwork(filePath string) (poster, backdrop string) {
	if s.nfoService != nil {
		return s.nfoService.FindLocalImagesForMedia(filePath)
	}
	nfo := NewNFOService(s.logger, s.cfg)
	return nfo.FindLocalImagesForMedia(filePath)
}

// ScrapeMedia 对单个媒体执行本地海报匹配（保留原接口语义，供手动触发）
func (s *MetadataService) ScrapeMedia(mediaID string) error {
	media, err := s.mediaRepo.FindByID(mediaID)
	if err != nil {
		return ErrMediaNotFound
	}

	s.broadcastScrapeEvent(EventScrapeStarted, &ScrapeProgressData{
		Total:      1,
		MediaTitle: media.Title,
		Message:    fmt.Sprintf("开始本地海报匹配: %s", media.Title),
	})

	err = s.RefreshLocalArtworkForMedia(media)

	s.broadcastScrapeEvent(EventScrapeCompleted, &ScrapeProgressData{
		Current:    1,
		Total:      1,
		Success:    boolToInt(err == nil),
		MediaTitle: media.Title,
		Message:    fmt.Sprintf("本地海报匹配完成: %s", media.Title),
	})

	return err
}

// ScrapeLibrary 对媒体库中的电影批量执行本地海报匹配
func (s *MetadataService) ScrapeLibrary(libraryID string, mediaList []model.Media) (int, int) {
	var needScrape []model.Media
	for _, media := range mediaList {
		// 跳过已完成的记录
		if media.ScrapeStatus == "scraped" && media.PosterPath != "" {
			continue
		}
		// 跳过手动标记的记录
		if media.ScrapeStatus == "manual" {
			continue
		}
		needScrape = append(needScrape, media)
	}

	total := len(needScrape)
	if total == 0 {
		return 0, 0
	}

	var success, failed int32

	s.broadcastScrapeEvent(EventScrapeStarted, &ScrapeProgressData{
		LibraryID: libraryID,
		Total:     total,
		Message:   fmt.Sprintf("开始本地海报匹配，共 %d 个媒体待处理", total),
	})

	for i := range needScrape {
		media := needScrape[i]
		if err := s.RefreshLocalArtworkForMedia(&media); err != nil {
			s.logger.Debugf("本地海报匹配失败: %s - %v", media.Title, err)
			atomic.AddInt32(&failed, 1)
		} else {
			atomic.AddInt32(&success, 1)
		}

		s.broadcastScrapeEvent(EventScrapeProgress, &ScrapeProgressData{
			LibraryID:  libraryID,
			Current:    i + 1,
			Total:      total,
			Success:    int(atomic.LoadInt32(&success)),
			Failed:     int(atomic.LoadInt32(&failed)),
			MediaTitle: media.Title,
			Message:    fmt.Sprintf("匹配进度: [%d/%d] %s", i+1, total, media.Title),
		})
	}

	finalSuccess := int(atomic.LoadInt32(&success))
	finalFailed := int(atomic.LoadInt32(&failed))

	s.broadcastScrapeEvent(EventScrapeCompleted, &ScrapeProgressData{
		LibraryID: libraryID,
		Current:   total,
		Total:     total,
		Success:   finalSuccess,
		Failed:    finalFailed,
		Message:   fmt.Sprintf("本地海报匹配完成: 成功 %d, 失败 %d", finalSuccess, finalFailed),
	})

	s.logger.Infof("本地海报匹配完成: 成功 %d, 失败 %d", finalSuccess, finalFailed)
	return finalSuccess, finalFailed
}

// ScrapeSeries 为剧集合集刷新本地海报（从剧集根目录匹配）
func (s *MetadataService) ScrapeSeries(seriesID string) error {
	if s.seriesRepo == nil {
		return fmt.Errorf("剧集仓储未初始化")
	}

	series, err := s.seriesRepo.FindByID(seriesID)
	if err != nil {
		return fmt.Errorf("剧集合集不存在: %s", seriesID)
	}

	if series.ScrapeStatus == "manual" {
		return nil
	}

	poster, backdrop := "", ""
	if s.nfoService != nil {
		// Deep 版本额外覆盖封面子目录（如 剧名/xxx_封面/01.jpg）
		poster, backdrop = s.nfoService.FindLocalImagesDeep(series.FolderPath)
	}

	now := time.Now()
	fields := map[string]any{
		"last_scrape_at": now,
	}
	if poster != "" {
		fields["poster_path"] = poster
		series.PosterPath = poster
	}
	if backdrop != "" {
		fields["backdrop_path"] = backdrop
		series.BackdropPath = backdrop
	}
	if poster != "" {
		fields["scrape_status"] = "scraped"
		series.ScrapeStatus = "scraped"
	}

	if err := s.seriesRepo.UpdateFields(series.ID, fields); err != nil {
		return fmt.Errorf("更新剧集合集失败: %w", err)
	}

	s.logger.Infof("剧集合集本地海报匹配完成: %s", series.Title)
	return nil
}

// ScrapeAllSeries 为媒体库下所有剧集合集刷新本地海报
func (s *MetadataService) ScrapeAllSeries(libraryID string) (int, int) {
	if s.seriesRepo == nil {
		return 0, 0
	}

	seriesList, err := s.seriesRepo.ListByLibraryID(libraryID)
	if err != nil {
		s.logger.Warnf("获取剧集合集列表失败: %v", err)
		return 0, 0
	}

	total := len(seriesList)
	if total == 0 {
		return 0, 0
	}

	success, failed := 0, 0
	for i := range seriesList {
		if err := s.ScrapeSeries(seriesList[i].ID); err != nil {
			failed++
		} else {
			success++
		}
	}

	s.logger.Infof("剧集合集本地海报匹配完成: 成功 %d, 失败 %d", success, failed)
	return success, failed
}

// SaveUploadedImageForMedia 保存上传的图片文件到本地，更新 Media 的图片路径
func (s *MetadataService) SaveUploadedImageForMedia(mediaID string, imageData []byte, ext string, imageType string) (string, error) {
	media, err := s.mediaRepo.FindByID(mediaID)
	if err != nil {
		return "", ErrMediaNotFound
	}

	// 先尝试保存到媒体文件同目录
	mediaDir := filepath.Dir(media.FilePath)
	baseName := strings.TrimSuffix(filepath.Base(media.FilePath), filepath.Ext(media.FilePath))
	localPath := filepath.Join(mediaDir, fmt.Sprintf("%s-%s%s", baseName, imageType, ext))

	if err := os.WriteFile(localPath, imageData, 0644); err != nil {
		// 回退到缓存目录
		cacheDir := filepath.Join(s.cfg.Cache.CacheDir, "images", media.ID)
		os.MkdirAll(cacheDir, 0755)
		localPath = filepath.Join(cacheDir, imageType+ext)
		if err := os.WriteFile(localPath, imageData, 0644); err != nil {
			return "", fmt.Errorf("保存图片文件失败: %w", err)
		}
	}

	if imageType == "poster" {
		media.PosterPath = localPath
	} else {
		media.BackdropPath = localPath
	}

	if err := s.mediaRepo.Update(media); err != nil {
		return "", fmt.Errorf("更新媒体数据失败: %w", err)
	}

	return localPath, nil
}

// SaveUploadedImageForSeries 保存上传的图片文件到本地，更新 Series 的图片路径
func (s *MetadataService) SaveUploadedImageForSeries(seriesID string, imageData []byte, ext string, imageType string) (string, error) {
	series, err := s.seriesRepo.FindByID(seriesID)
	if err != nil {
		return "", fmt.Errorf("剧集合集不存在")
	}

	cacheDir := filepath.Join(s.cfg.Cache.CacheDir, "images", "series", series.ID)
	os.MkdirAll(cacheDir, 0755)
	localPath := filepath.Join(cacheDir, imageType+ext)

	if err := os.WriteFile(localPath, imageData, 0644); err != nil {
		return "", fmt.Errorf("保存图片文件失败: %w", err)
	}

	if imageType == "poster" {
		series.PosterPath = localPath
	} else {
		series.BackdropPath = localPath
	}

	if err := s.seriesRepo.Update(series); err != nil {
		return "", fmt.Errorf("更新剧集数据失败: %w", err)
	}

	return localPath, nil
}

// DownloadURLImageForMedia 从 URL 下载图片并保存到本地，更新 Media 的图片路径
func (s *MetadataService) DownloadURLImageForMedia(mediaID string, imageURL string, imageType string) (string, error) {
	// 先验证媒体是否存在
	if _, err := s.mediaRepo.FindByID(mediaID); err != nil {
		return "", ErrMediaNotFound
	}

	imageData, ext, err := downloadImageFromURL(s.client, imageURL)
	if err != nil {
		return "", err
	}

	return s.SaveUploadedImageForMedia(mediaID, imageData, ext, imageType)
}

// DownloadURLImageForSeries 从 URL 下载图片并保存到本地，更新 Series 的图片路径
func (s *MetadataService) DownloadURLImageForSeries(seriesID string, imageURL string, imageType string) (string, error) {
	imageData, ext, err := downloadImageFromURL(s.client, imageURL)
	if err != nil {
		return "", err
	}

	return s.SaveUploadedImageForSeries(seriesID, imageData, ext, imageType)
}

// downloadImageFromURL 从 URL 下载图片数据（限制 10MB）
func downloadImageFromURL(client *http.Client, imageURL string) ([]byte, string, error) {
	resp, err := client.Get(imageURL)
	if err != nil {
		return nil, "", fmt.Errorf("下载图片失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("下载图片失败，HTTP状态码: %d", resp.StatusCode)
	}

	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("读取图片数据失败: %w", err)
	}

	if len(imageData) > 10*1024*1024 {
		return nil, "", fmt.Errorf("图片文件过大，最大支持10MB")
	}

	ext := ".jpg"
	ct := resp.Header.Get("Content-Type")
	switch {
	case strings.Contains(ct, "png"):
		ext = ".png"
	case strings.Contains(ct, "webp"):
		ext = ".webp"
	case strings.Contains(ct, "gif"):
		ext = ".gif"
	}

	return imageData, ext, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
