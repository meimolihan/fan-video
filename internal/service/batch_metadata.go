package service

import (
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// BatchMetadataService 批量元数据编辑服务
type BatchMetadataService struct {
	db     *gorm.DB
	logger *zap.SugaredLogger
}

// BatchUpdateRequest 批量更新请求
type BatchUpdateRequest struct {
	MediaIDs []string          `json:"media_ids"`
	Updates  map[string]string `json:"updates"` // 字段名 -> 新值
}

// BatchUpdateResult 批量更新结果
type BatchUpdateResult struct {
	Total   int      `json:"total"`
	Success int      `json:"success"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors"`
}

func NewBatchMetadataService(db *gorm.DB, logger *zap.SugaredLogger) *BatchMetadataService {
	return &BatchMetadataService{
		db:     db,
		logger: logger,
	}
}

// BatchUpdateMedia 批量更新媒体元数据
func (s *BatchMetadataService) BatchUpdateMedia(req BatchUpdateRequest) (*BatchUpdateResult, error) {
	result := &BatchUpdateResult{
		Total:  len(req.MediaIDs),
		Errors: make([]string, 0),
	}

	if len(req.MediaIDs) == 0 {
		return result, fmt.Errorf("未选择任何媒体")
	}

	// 允许批量更新的字段白名单
	allowedFields := map[string]string{
		"genres":   "genres",
		"year":     "year",
		"rating":   "rating",
		"country":  "country",
		"language": "language",
		"studio":   "studio",
		"tagline":  "tagline",
	}

	// 构建更新映射
	updateMap := make(map[string]interface{})
	for field, value := range req.Updates {
		if dbField, ok := allowedFields[field]; ok {
			updateMap[dbField] = value
		}
	}

	if len(updateMap) == 0 {
		return result, fmt.Errorf("没有有效的更新字段")
	}

	updateMap["updated_at"] = time.Now()

	// 执行批量更新
	tx := s.db.Table("media").
		Where("id IN ?", req.MediaIDs).
		Updates(updateMap)

	if tx.Error != nil {
		return result, fmt.Errorf("批量更新失败: %w", tx.Error)
	}

	result.Success = int(tx.RowsAffected)
	result.Failed = result.Total - result.Success

	s.logger.Infof("批量元数据更新完成: 成功 %d, 失败 %d", result.Success, result.Failed)
	return result, nil
}

// BatchUpdateSeries 批量更新剧集合集元数据
func (s *BatchMetadataService) BatchUpdateSeries(seriesIDs []string, updates map[string]string) (*BatchUpdateResult, error) {
	result := &BatchUpdateResult{
		Total:  len(seriesIDs),
		Errors: make([]string, 0),
	}

	allowedFields := map[string]string{
		"genres":   "genres",
		"year":     "year",
		"rating":   "rating",
		"country":  "country",
		"language": "language",
		"studio":   "studio",
	}

	updateMap := make(map[string]interface{})
	for field, value := range updates {
		if dbField, ok := allowedFields[field]; ok {
			updateMap[dbField] = value
		}
	}

	if len(updateMap) == 0 {
		return result, fmt.Errorf("没有有效的更新字段")
	}

	updateMap["updated_at"] = time.Now()

	tx := s.db.Table("series").
		Where("id IN ?", seriesIDs).
		Updates(updateMap)

	if tx.Error != nil {
		return result, fmt.Errorf("批量更新失败: %w", tx.Error)
	}

	result.Success = int(tx.RowsAffected)
	result.Failed = result.Total - result.Success

	return result, nil
}

// ==================== 媒体库导入/导出服务 ====================

// MediaImportExportService 媒体库导入/导出服务
type MediaImportExportService struct {
	db     *gorm.DB
	logger *zap.SugaredLogger
}

// ImportResult 导入结果
type ImportResult struct {
	Total    int      `json:"total"`
	Imported int      `json:"imported"`
	Skipped  int      `json:"skipped"`
	Failed   int      `json:"failed"`
	Errors   []string `json:"errors"`
}

// ExportData 导出数据结构
type ExportData struct {
	Version   string                 `json:"version"`
	ExportAt  time.Time              `json:"export_at"`
	Source    string                 `json:"source"`
	Libraries []ExportLibrary        `json:"libraries"`
	Media     []ExportMedia          `json:"media"`
	Series    []ExportSeries         `json:"series"`
	Extra     map[string]interface{} `json:"extra,omitempty"`
}

// ExportLibrary 导出的媒体库
type ExportLibrary struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

// ExportMedia 导出的媒体
type ExportMedia struct {
	Title     string  `json:"title"`
	OrigTitle string  `json:"orig_title"`
	Year      int     `json:"year"`
	Overview  string  `json:"overview"`
	Rating    float64 `json:"rating"`
	Genres    string  `json:"genres"`
	FilePath  string  `json:"file_path"`
	MediaType string  `json:"media_type"`
	TMDbID    int     `json:"tmdb_id,omitempty"`
	Country   string  `json:"country,omitempty"`
	Language  string  `json:"language,omitempty"`
	Studio    string  `json:"studio,omitempty"`
}

// ExportSeries 导出的剧集合集
type ExportSeries struct {
	Title      string  `json:"title"`
	OrigTitle  string  `json:"orig_title"`
	Year       int     `json:"year"`
	Overview   string  `json:"overview"`
	Rating     float64 `json:"rating"`
	Genres     string  `json:"genres"`
	FolderPath string  `json:"folder_path"`
	TMDbID     int     `json:"tmdb_id,omitempty"`
}

func NewMediaImportExportService(db *gorm.DB, logger *zap.SugaredLogger) *MediaImportExportService {
	return &MediaImportExportService{
		db:     db,
		logger: logger,
	}
}

// ExportMediaLibrary 导出媒体库数据
func (s *MediaImportExportService) ExportMediaLibrary(libraryID string) (*ExportData, error) {
	export := &ExportData{
		Version:  "1.0",
		ExportAt: time.Now(),
		Source:   "fan-video",
	}

	// 导出媒体库信息
	var libraries []struct {
		Name string
		Path string
		Type string
	}
	query := s.db.Table("libraries").Select("name, path, type")
	if libraryID != "" {
		query = query.Where("id = ?", libraryID)
	}
	query.Find(&libraries)

	for _, lib := range libraries {
		export.Libraries = append(export.Libraries, ExportLibrary{
			Name: lib.Name,
			Path: lib.Path,
			Type: lib.Type,
		})
	}

	// 导出媒体
	var mediaList []struct {
		Title     string
		OrigTitle string `gorm:"column:orig_title"`
		Year      int
		Overview  string
		Rating    float64
		Genres    string
		FilePath  string `gorm:"column:file_path"`
		MediaType string `gorm:"column:media_type"`
		TMDbID    int    `gorm:"column:tmdb_id"`
		Country   string
		Language  string
		Studio    string
	}
	mediaQuery := s.db.Table("media").Select("title, orig_title, year, overview, rating, genres, file_path, media_type, tmdb_id, country, language, studio")
	if libraryID != "" {
		mediaQuery = mediaQuery.Where("library_id = ?", libraryID)
	}
	mediaQuery.Find(&mediaList)

	for _, m := range mediaList {
		export.Media = append(export.Media, ExportMedia{
			Title:     m.Title,
			OrigTitle: m.OrigTitle,
			Year:      m.Year,
			Overview:  m.Overview,
			Rating:    m.Rating,
			Genres:    m.Genres,
			FilePath:  m.FilePath,
			MediaType: m.MediaType,
			TMDbID:    m.TMDbID,
			Country:   m.Country,
			Language:  m.Language,
			Studio:    m.Studio,
		})
	}

	// 导出剧集合集
	var seriesList []struct {
		Title      string
		OrigTitle  string `gorm:"column:orig_title"`
		Year       int
		Overview   string
		Rating     float64
		Genres     string
		FolderPath string `gorm:"column:folder_path"`
		TMDbID     int    `gorm:"column:tmdb_id"`
	}
	seriesQuery := s.db.Table("series").Select("title, orig_title, year, overview, rating, genres, folder_path, tmdb_id")
	if libraryID != "" {
		seriesQuery = seriesQuery.Where("library_id = ?", libraryID)
	}
	seriesQuery.Find(&seriesList)

	for _, sr := range seriesList {
		export.Series = append(export.Series, ExportSeries{
			Title:      sr.Title,
			OrigTitle:  sr.OrigTitle,
			Year:       sr.Year,
			Overview:   sr.Overview,
			Rating:     sr.Rating,
			Genres:     sr.Genres,
			FolderPath: sr.FolderPath,
			TMDbID:     sr.TMDbID,
		})
	}

	return export, nil
}

// ImportFromExportData 从导出数据导入
func (s *MediaImportExportService) ImportFromExportData(data *ExportData, targetLibraryID string) (*ImportResult, error) {
	result := &ImportResult{
		Total:  len(data.Media) + len(data.Series),
		Errors: make([]string, 0),
	}

	for _, m := range data.Media {
		var count int64
		s.db.Table("media").Where("file_path = ?", m.FilePath).Count(&count)
		if count > 0 {
			result.Skipped++
			continue
		}

		media := map[string]interface{}{
			"library_id": targetLibraryID,
			"title":      m.Title,
			"orig_title": m.OrigTitle,
			"year":       m.Year,
			"overview":   m.Overview,
			"rating":     m.Rating,
			"genres":     m.Genres,
			"file_path":  m.FilePath,
			"media_type": m.MediaType,
			"tmdb_id":    m.TMDbID,
			"country":    m.Country,
			"language":   m.Language,
			"studio":     m.Studio,
			"created_at": time.Now(),
			"updated_at": time.Now(),
		}

		if err := s.db.Table("media").Create(media).Error; err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("导入 %s 失败: %v", m.Title, err))
			continue
		}
		result.Imported++
	}

	return result, nil
}
