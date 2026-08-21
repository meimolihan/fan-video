package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// PhotoService 图片库管理服务
// 新增照片浏览、相册管理、智能分类等图像处理能力
type PhotoService struct {
	db       *gorm.DB
	logger   *zap.SugaredLogger
	mu       sync.RWMutex
	thumbDir string
}

// Photo 照片
type Photo struct {
	ID        string `json:"id" gorm:"primaryKey;type:text"`
	LibraryID string `json:"library_id" gorm:"index;type:text;not null"`
	AlbumID   string `json:"album_id" gorm:"index;type:text"`
	FileName  string `json:"file_name" gorm:"type:text"`
	FilePath  string `json:"file_path" gorm:"type:text;uniqueIndex"`
	FileSize  int64  `json:"file_size"`
	Format    string `json:"format" gorm:"type:text"` // jpg / png / webp / heic / raw
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	ThumbPath string `json:"thumb_path" gorm:"type:text"` // 缩略图路径
	// EXIF 元数据
	CameraMake   string     `json:"camera_make" gorm:"type:text"`
	CameraModel  string     `json:"camera_model" gorm:"type:text"`
	LensModel    string     `json:"lens_model" gorm:"type:text"`
	FocalLength  string     `json:"focal_length" gorm:"type:text"`
	Aperture     string     `json:"aperture" gorm:"type:text"`
	ShutterSpeed string     `json:"shutter_speed" gorm:"type:text"`
	ISO          int        `json:"iso"`
	TakenAt      *time.Time `json:"taken_at"`  // 拍摄时间
	Latitude     float64    `json:"latitude"`  // GPS 纬度
	Longitude    float64    `json:"longitude"` // GPS 经度
	// 智能分类
	Tags       string    `json:"tags" gorm:"type:text"`       // 自动标签（逗号分隔）
	FaceIDs    string    `json:"face_ids" gorm:"type:text"`   // 识别到的人脸ID
	SceneType  string    `json:"scene_type" gorm:"type:text"` // 场景类型（风景/人像/美食等）
	ColorTone  string    `json:"color_tone" gorm:"type:text"` // 色调（暖色/冷色/中性）
	IsFavorite bool      `json:"is_favorite" gorm:"default:false"`
	IsHidden   bool      `json:"is_hidden" gorm:"default:false"`
	Rating     int       `json:"rating" gorm:"default:0"` // 1-5 星评分
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// PhotoAlbum 相册
type PhotoAlbum struct {
	ID           string    `json:"id" gorm:"primaryKey;type:text"`
	UserID       string    `json:"user_id" gorm:"index;type:text"`
	Name         string    `json:"name" gorm:"type:text;not null"`
	Description  string    `json:"description" gorm:"type:text"`
	CoverPhotoID string    `json:"cover_photo_id" gorm:"type:text"`
	Type         string    `json:"type" gorm:"type:text;default:manual"` // manual / auto / smart / face
	SmartRule    string    `json:"smart_rule" gorm:"type:text"`          // 智能相册规则（JSON）
	PhotoCount   int       `json:"photo_count" gorm:"default:0"`
	IsPublic     bool      `json:"is_public" gorm:"default:false"`
	SortOrder    int       `json:"sort_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Photos       []Photo   `json:"photos,omitempty" gorm:"foreignKey:AlbumID"`
}

// FaceCluster 人脸聚类
type FaceCluster struct {
	ID         string    `json:"id" gorm:"primaryKey;type:text"`
	Name       string    `json:"name" gorm:"type:text"`        // 人物名称（用户标注）
	SamplePath string    `json:"sample_path" gorm:"type:text"` // 代表性人脸图片
	PhotoCount int       `json:"photo_count" gorm:"default:0"`
	CreatedAt  time.Time `json:"created_at"`
}

// 支持的图片格式
var supportedImageFormats = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	".heic": true, ".heif": true, ".gif": true, ".bmp": true,
	".tiff": true, ".tif": true, ".raw": true, ".cr2": true,
	".nef": true, ".arw": true, ".dng": true, ".svg": true,
}

func NewPhotoService(db *gorm.DB, thumbDir string, logger *zap.SugaredLogger) *PhotoService {
	db.AutoMigrate(&Photo{}, &PhotoAlbum{}, &FaceCluster{})

	if thumbDir == "" {
		thumbDir = "./data/thumbnails/photos"
	}
	os.MkdirAll(thumbDir, 0755)

	return &PhotoService{db: db, logger: logger, thumbDir: thumbDir}
}

// ScanPhotoLibrary 扫描图片库目录
func (s *PhotoService) ScanPhotoLibrary(libraryID, dirPath string) (int, error) {
	var count int
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !supportedImageFormats[ext] {
			return nil
		}

		// 检查是否已存在
		var existing int64
		s.db.Model(&Photo{}).Where("file_path = ?", path).Count(&existing)
		if existing > 0 {
			return nil
		}

		photo := &Photo{
			ID:        fmt.Sprintf("ph_%d", time.Now().UnixNano()),
			LibraryID: libraryID,
			FileName:  info.Name(),
			FilePath:  path,
			FileSize:  info.Size(),
			Format:    strings.TrimPrefix(ext, "."),
		}

		// 尝试从目录名推断相册
		dirName := filepath.Base(filepath.Dir(path))
		if dirName != "" && dirName != "." && dirName != filepath.Base(dirPath) {
			// 查找或创建相册
			var album PhotoAlbum
			result := s.db.Where("name = ? AND user_id = ''", dirName).First(&album)
			if result.Error != nil {
				album = PhotoAlbum{
					ID:   fmt.Sprintf("pa_%d", time.Now().UnixNano()),
					Name: dirName,
					Type: "auto",
				}
				s.db.Create(&album)
			}
			photo.AlbumID = album.ID
		}

		if err := s.db.Create(photo).Error; err != nil {
			s.logger.Warnf("添加照片失败: %s -> %v", path, err)
			return nil
		}
		count++
		return nil
	})

	// 更新相册照片计数
	s.updateAlbumCounts()

	s.logger.Infof("图片库扫描完成: 发现 %d 张新照片", count)
	return count, err
}

// ListPhotos 获取照片列表
func (s *PhotoService) ListPhotos(libraryID string, page, size int, sort string, filters map[string]string) ([]Photo, int64, error) {
	var photos []Photo
	var total int64

	query := s.db.Model(&Photo{}).Where("is_hidden = ?", false)
	if libraryID != "" {
		query = query.Where("library_id = ?", libraryID)
	}

	// 应用过滤器
	if albumID, ok := filters["album_id"]; ok && albumID != "" {
		query = query.Where("album_id = ?", albumID)
	}
	if tag, ok := filters["tag"]; ok && tag != "" {
		query = query.Where("tags LIKE ?", "%"+tag+"%")
	}
	if scene, ok := filters["scene"]; ok && scene != "" {
		query = query.Where("scene_type = ?", scene)
	}
	if fav, ok := filters["favorite"]; ok && fav == "true" {
		query = query.Where("is_favorite = ?", true)
	}

	query.Count(&total)

	switch sort {
	case "date_asc":
		query = query.Order("COALESCE(taken_at, created_at) ASC")
	case "name":
		query = query.Order("file_name ASC")
	case "size":
		query = query.Order("file_size DESC")
	case "rating":
		query = query.Order("rating DESC")
	default:
		query = query.Order("COALESCE(taken_at, created_at) DESC")
	}

	err := query.Offset((page - 1) * size).Limit(size).Find(&photos).Error
	return photos, total, err
}

// GetPhoto 获取照片详情
func (s *PhotoService) GetPhoto(photoID string) (*Photo, error) {
	var photo Photo
	err := s.db.First(&photo, "id = ?", photoID).Error
	return &photo, err
}

// ListAlbums 获取相册列表
func (s *PhotoService) ListAlbums(userID string) ([]PhotoAlbum, error) {
	var albums []PhotoAlbum
	err := s.db.Where("user_id = ? OR user_id = '' OR is_public = ?", userID, true).
		Order("sort_order ASC, created_at DESC").Find(&albums).Error
	return albums, err
}

// CreateAlbum 创建相册
func (s *PhotoService) CreateAlbum(userID, name, description string) (*PhotoAlbum, error) {
	album := &PhotoAlbum{
		ID:          fmt.Sprintf("pa_%d", time.Now().UnixNano()),
		UserID:      userID,
		Name:        name,
		Description: description,
		Type:        "manual",
	}
	return album, s.db.Create(album).Error
}

// AddPhotosToAlbum 添加照片到相册
func (s *PhotoService) AddPhotosToAlbum(albumID string, photoIDs []string) error {
	result := s.db.Model(&Photo{}).Where("id IN ?", photoIDs).Update("album_id", albumID)
	if result.Error != nil {
		return result.Error
	}
	s.updateAlbumCounts()
	return nil
}

// RemovePhotosFromAlbum 从相册移除照片
func (s *PhotoService) RemovePhotosFromAlbum(albumID string, photoIDs []string) error {
	result := s.db.Model(&Photo{}).Where("id IN ? AND album_id = ?", photoIDs, albumID).
		Update("album_id", "")
	if result.Error != nil {
		return result.Error
	}
	s.updateAlbumCounts()
	return nil
}

// ToggleFavorite 切换收藏状态
func (s *PhotoService) ToggleFavorite(photoID string) (bool, error) {
	var photo Photo
	if err := s.db.First(&photo, "id = ?", photoID).Error; err != nil {
		return false, err
	}
	photo.IsFavorite = !photo.IsFavorite
	s.db.Save(&photo)
	return photo.IsFavorite, nil
}

// SetRating 设置评分
func (s *PhotoService) SetRating(photoID string, rating int) error {
	if rating < 0 || rating > 5 {
		return fmt.Errorf("评分范围为 0-5")
	}
	return s.db.Model(&Photo{}).Where("id = ?", photoID).Update("rating", rating).Error
}

// UpdateTags 更新照片标签
func (s *PhotoService) UpdateTags(photoID string, tags []string) error {
	return s.db.Model(&Photo{}).Where("id = ?", photoID).
		Update("tags", strings.Join(tags, ",")).Error
}

// SearchPhotos 搜索照片
func (s *PhotoService) SearchPhotos(query string, limit int) ([]Photo, error) {
	var photos []Photo
	searchQuery := "%" + query + "%"
	err := s.db.Where("file_name LIKE ? OR tags LIKE ? OR scene_type LIKE ?",
		searchQuery, searchQuery, searchQuery).
		Limit(limit).Find(&photos).Error
	return photos, err
}

// GetPhotoStats 获取图片库统计
func (s *PhotoService) GetPhotoStats(libraryID string) map[string]interface{} {
	var totalPhotos int64
	var totalSize int64
	var albumCount int64
	var favoriteCount int64

	query := s.db.Model(&Photo{})
	if libraryID != "" {
		query = query.Where("library_id = ?", libraryID)
	}

	query.Count(&totalPhotos)
	query.Select("COALESCE(SUM(file_size), 0)").Scan(&totalSize)
	s.db.Model(&PhotoAlbum{}).Count(&albumCount)
	query.Where("is_favorite = ?", true).Count(&favoriteCount)

	// 按格式统计
	type formatStat struct {
		Format string
		Count  int64
	}
	var formatStats []formatStat
	s.db.Model(&Photo{}).Select("format, COUNT(*) as count").Group("format").Scan(&formatStats)

	formats := make(map[string]int64)
	for _, fs := range formatStats {
		formats[fs.Format] = fs.Count
	}

	return map[string]interface{}{
		"total_photos":   totalPhotos,
		"total_size":     totalSize,
		"album_count":    albumCount,
		"favorite_count": favoriteCount,
		"formats":        formats,
	}
}

// ListFaceClusters 获取人脸聚类列表
func (s *PhotoService) ListFaceClusters() ([]FaceCluster, error) {
	var clusters []FaceCluster
	err := s.db.Order("photo_count DESC").Find(&clusters).Error
	return clusters, err
}

// NameFaceCluster 为人脸聚类命名
func (s *PhotoService) NameFaceCluster(clusterID, name string) error {
	return s.db.Model(&FaceCluster{}).Where("id = ?", clusterID).Update("name", name).Error
}

// DeleteAlbum 删除相册
func (s *PhotoService) DeleteAlbum(albumID, userID string) error {
	// 将相册中的照片解除关联
	s.db.Model(&Photo{}).Where("album_id = ?", albumID).Update("album_id", "")
	return s.db.Where("id = ? AND user_id = ?", albumID, userID).Delete(&PhotoAlbum{}).Error
}

// ==================== 内部方法 ====================

func (s *PhotoService) updateAlbumCounts() {
	s.db.Exec(`
		UPDATE photo_albums SET photo_count = (
			SELECT COUNT(*) FROM photos WHERE photos.album_id = photo_albums.id
		)
	`)
}
