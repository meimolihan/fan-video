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

// MusicService 音乐库集成服务
// 扩展为音视频一体化媒体中心，支持音乐文件管理、播放列表、歌词显示
type MusicService struct {
	db     *gorm.DB
	logger *zap.SugaredLogger
	mu     sync.RWMutex
}

// MusicTrack 音乐曲目
type MusicTrack struct {
	ID          string    `json:"id" gorm:"primaryKey;type:text"`
	LibraryID   string    `json:"library_id" gorm:"index;type:text;not null"`
	AlbumID     string    `json:"album_id" gorm:"index;type:text"`
	Title       string    `json:"title" gorm:"type:text;not null"`
	Artist      string    `json:"artist" gorm:"type:text"`
	AlbumArtist string    `json:"album_artist" gorm:"type:text"`
	Album       string    `json:"album" gorm:"type:text"`
	Genre       string    `json:"genre" gorm:"type:text"`
	Year        int       `json:"year"`
	TrackNum    int       `json:"track_num"`
	DiscNum     int       `json:"disc_num"`
	Duration    float64   `json:"duration"` // 时长（秒）
	FilePath    string    `json:"file_path" gorm:"type:text;uniqueIndex"`
	FileSize    int64     `json:"file_size"`
	Format      string    `json:"format" gorm:"type:text"`      // mp3 / flac / aac / wav / ogg
	Bitrate     int       `json:"bitrate"`                      // 比特率（kbps）
	SampleRate  int       `json:"sample_rate"`                  // 采样率（Hz）
	Channels    int       `json:"channels"`                     // 声道数
	CoverPath   string    `json:"cover_path" gorm:"type:text"`  // 封面图路径
	LyricsPath  string    `json:"lyrics_path" gorm:"type:text"` // 歌词文件路径
	LyricsText  string    `json:"lyrics_text" gorm:"type:text"` // 内嵌歌词
	PlayCount   int       `json:"play_count" gorm:"default:0"`
	Loved       bool      `json:"loved" gorm:"default:false"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MusicAlbum 音乐专辑
type MusicAlbum struct {
	ID            string       `json:"id" gorm:"primaryKey;type:text"`
	LibraryID     string       `json:"library_id" gorm:"index;type:text;not null"`
	Title         string       `json:"title" gorm:"type:text;not null"`
	Artist        string       `json:"artist" gorm:"type:text"`
	Year          int          `json:"year"`
	Genre         string       `json:"genre" gorm:"type:text"`
	CoverPath     string       `json:"cover_path" gorm:"type:text"`
	FolderPath    string       `json:"folder_path" gorm:"type:text"`
	TrackCount    int          `json:"track_count"`
	TotalDuration float64      `json:"total_duration"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
	Tracks        []MusicTrack `json:"tracks,omitempty" gorm:"foreignKey:AlbumID"`
}

// MusicPlaylist 音乐播放列表
type MusicPlaylist struct {
	ID        string              `json:"id" gorm:"primaryKey;type:text"`
	UserID    string              `json:"user_id" gorm:"index;type:text;not null"`
	Name      string              `json:"name" gorm:"type:text;not null"`
	CoverPath string              `json:"cover_path" gorm:"type:text"`
	IsPublic  bool                `json:"is_public" gorm:"default:false"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
	Items     []MusicPlaylistItem `json:"items,omitempty" gorm:"foreignKey:PlaylistID"`
}

// MusicPlaylistItem 播放列表项
type MusicPlaylistItem struct {
	ID         string      `json:"id" gorm:"primaryKey;type:text"`
	PlaylistID string      `json:"playlist_id" gorm:"index;type:text;not null"`
	TrackID    string      `json:"track_id" gorm:"type:text;not null"`
	SortOrder  int         `json:"sort_order"`
	Track      *MusicTrack `json:"track,omitempty" gorm:"foreignKey:TrackID"`
}

// 支持的音频格式
var supportedAudioFormats = map[string]bool{
	".mp3": true, ".flac": true, ".aac": true, ".m4a": true,
	".wav": true, ".ogg": true, ".wma": true, ".ape": true,
	".alac": true, ".opus": true, ".aiff": true,
}

func NewMusicService(db *gorm.DB, logger *zap.SugaredLogger) *MusicService {
	db.AutoMigrate(&MusicTrack{}, &MusicAlbum{}, &MusicPlaylist{}, &MusicPlaylistItem{})
	return &MusicService{db: db, logger: logger}
}

// ScanMusicLibrary 扫描音乐库目录
func (s *MusicService) ScanMusicLibrary(libraryID, dirPath string) (int, error) {
	var count int
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !supportedAudioFormats[ext] {
			return nil
		}

		// 检查是否已存在
		var existing int64
		s.db.Model(&MusicTrack{}).Where("file_path = ?", path).Count(&existing)
		if existing > 0 {
			return nil
		}

		// 创建基础记录（后续可通过元数据解析增强）
		track := &MusicTrack{
			ID:        fmt.Sprintf("mt_%d", time.Now().UnixNano()),
			LibraryID: libraryID,
			Title:     strings.TrimSuffix(info.Name(), ext),
			FilePath:  path,
			FileSize:  info.Size(),
			Format:    strings.TrimPrefix(ext, "."),
		}

		// 尝试从目录结构推断专辑和艺术家
		dir := filepath.Dir(path)
		albumName := filepath.Base(dir)
		artistName := filepath.Base(filepath.Dir(dir))
		if albumName != "" && albumName != "." {
			track.Album = albumName
		}
		if artistName != "" && artistName != "." && artistName != filepath.Base(dirPath) {
			track.Artist = artistName
		}

		// 查找同目录下的歌词文件
		lrcPath := strings.TrimSuffix(path, ext) + ".lrc"
		if _, err := os.Stat(lrcPath); err == nil {
			track.LyricsPath = lrcPath
		}

		// 查找封面图
		for _, coverName := range []string{"cover.jpg", "cover.png", "folder.jpg", "album.jpg"} {
			coverPath := filepath.Join(dir, coverName)
			if _, err := os.Stat(coverPath); err == nil {
				track.CoverPath = coverPath
				break
			}
		}

		if err := s.db.Create(track).Error; err != nil {
			s.logger.Warnf("添加音乐曲目失败: %s -> %v", path, err)
			return nil
		}
		count++
		return nil
	})

	// 自动创建专辑
	if count > 0 {
		s.autoCreateAlbums(libraryID)
	}

	s.logger.Infof("音乐库扫描完成: 发现 %d 首新曲目", count)
	return count, err
}

// autoCreateAlbums 自动根据曲目信息创建专辑
func (s *MusicService) autoCreateAlbums(libraryID string) {
	type albumGroup struct {
		Album  string
		Artist string
	}

	var groups []albumGroup
	s.db.Model(&MusicTrack{}).
		Where("library_id = ? AND album != '' AND album_id = ''", libraryID).
		Select("DISTINCT album, artist").
		Scan(&groups)

	for _, g := range groups {
		albumID := fmt.Sprintf("ma_%d", time.Now().UnixNano())
		album := &MusicAlbum{
			ID:        albumID,
			LibraryID: libraryID,
			Title:     g.Album,
			Artist:    g.Artist,
		}

		// 统计曲目数和总时长
		var stats struct {
			Count    int
			Duration float64
		}
		s.db.Model(&MusicTrack{}).
			Where("library_id = ? AND album = ? AND artist = ?", libraryID, g.Album, g.Artist).
			Select("COUNT(*) as count, COALESCE(SUM(duration), 0) as duration").
			Scan(&stats)

		album.TrackCount = stats.Count
		album.TotalDuration = stats.Duration

		// 使用第一首曲目的封面
		var firstTrack MusicTrack
		s.db.Where("library_id = ? AND album = ? AND artist = ?", libraryID, g.Album, g.Artist).
			Order("track_num ASC").First(&firstTrack)
		album.CoverPath = firstTrack.CoverPath
		album.FolderPath = filepath.Dir(firstTrack.FilePath)

		s.db.Create(album)

		// 更新曲目的 album_id
		s.db.Model(&MusicTrack{}).
			Where("library_id = ? AND album = ? AND artist = ?", libraryID, g.Album, g.Artist).
			Update("album_id", albumID)

		time.Sleep(time.Millisecond) // 避免ID冲突
	}
}

// ListTracks 获取曲目列表
func (s *MusicService) ListTracks(libraryID string, page, size int, sort string) ([]MusicTrack, int64, error) {
	var tracks []MusicTrack
	var total int64

	query := s.db.Model(&MusicTrack{})
	if libraryID != "" {
		query = query.Where("library_id = ?", libraryID)
	}

	query.Count(&total)

	switch sort {
	case "title":
		query = query.Order("title ASC")
	case "artist":
		query = query.Order("artist ASC, album ASC, track_num ASC")
	case "recent":
		query = query.Order("created_at DESC")
	case "popular":
		query = query.Order("play_count DESC")
	default:
		query = query.Order("artist ASC, album ASC, track_num ASC")
	}

	err := query.Offset((page - 1) * size).Limit(size).Find(&tracks).Error
	return tracks, total, err
}

// ListAlbums 获取专辑列表
func (s *MusicService) ListAlbums(libraryID string, page, size int) ([]MusicAlbum, int64, error) {
	var albums []MusicAlbum
	var total int64

	query := s.db.Model(&MusicAlbum{})
	if libraryID != "" {
		query = query.Where("library_id = ?", libraryID)
	}

	query.Count(&total)
	err := query.Order("artist ASC, year DESC").
		Offset((page - 1) * size).Limit(size).Find(&albums).Error
	return albums, total, err
}

// GetAlbumWithTracks 获取专辑详情（含曲目）
func (s *MusicService) GetAlbumWithTracks(albumID string) (*MusicAlbum, error) {
	var album MusicAlbum
	err := s.db.Preload("Tracks", func(db *gorm.DB) *gorm.DB {
		return db.Order("disc_num ASC, track_num ASC")
	}).First(&album, "id = ?", albumID).Error
	return &album, err
}

// SearchMusic 搜索音乐
func (s *MusicService) SearchMusic(query string, limit int) ([]MusicTrack, error) {
	var tracks []MusicTrack
	searchQuery := "%" + query + "%"
	err := s.db.Where("title LIKE ? OR artist LIKE ? OR album LIKE ?",
		searchQuery, searchQuery, searchQuery).
		Limit(limit).Find(&tracks).Error
	return tracks, err
}

// GetLyrics 获取歌词
func (s *MusicService) GetLyrics(trackID string) (string, error) {
	var track MusicTrack
	if err := s.db.First(&track, "id = ?", trackID).Error; err != nil {
		return "", err
	}

	// 优先返回内嵌歌词
	if track.LyricsText != "" {
		return track.LyricsText, nil
	}

	// 读取外部歌词文件
	if track.LyricsPath != "" {
		data, err := os.ReadFile(track.LyricsPath)
		if err == nil {
			return string(data), nil
		}
	}

	return "", fmt.Errorf("未找到歌词")
}

// IncrementPlayCount 增加播放次数
func (s *MusicService) IncrementPlayCount(trackID string) {
	s.db.Model(&MusicTrack{}).Where("id = ?", trackID).
		UpdateColumn("play_count", gorm.Expr("play_count + 1"))
}

// ToggleLove 切换喜爱状态
func (s *MusicService) ToggleLove(trackID string) (bool, error) {
	var track MusicTrack
	if err := s.db.First(&track, "id = ?", trackID).Error; err != nil {
		return false, err
	}
	track.Loved = !track.Loved
	s.db.Save(&track)
	return track.Loved, nil
}

// ==================== 播放列表管理 ====================

// CreatePlaylist 创建音乐播放列表
func (s *MusicService) CreatePlaylist(userID, name string) (*MusicPlaylist, error) {
	playlist := &MusicPlaylist{
		ID:     fmt.Sprintf("mpl_%d", time.Now().UnixNano()),
		UserID: userID,
		Name:   name,
	}
	return playlist, s.db.Create(playlist).Error
}

// ListPlaylists 获取用户的播放列表
func (s *MusicService) ListPlaylists(userID string) ([]MusicPlaylist, error) {
	var playlists []MusicPlaylist
	err := s.db.Where("user_id = ? OR is_public = ?", userID, true).
		Order("updated_at DESC").Find(&playlists).Error
	return playlists, err
}

// AddToPlaylist 添加曲目到播放列表
func (s *MusicService) AddToPlaylist(playlistID string, trackIDs []string) error {
	var maxOrder int
	s.db.Model(&MusicPlaylistItem{}).Where("playlist_id = ?", playlistID).
		Select("COALESCE(MAX(sort_order), 0)").Scan(&maxOrder)

	for i, trackID := range trackIDs {
		item := &MusicPlaylistItem{
			ID:         fmt.Sprintf("mpli_%d", time.Now().UnixNano()),
			PlaylistID: playlistID,
			TrackID:    trackID,
			SortOrder:  maxOrder + i + 1,
		}
		s.db.Create(item)
		time.Sleep(time.Millisecond)
	}

	s.db.Model(&MusicPlaylist{}).Where("id = ?", playlistID).Update("updated_at", time.Now())
	return nil
}

// RemoveFromPlaylist 从播放列表移除曲目
func (s *MusicService) RemoveFromPlaylist(playlistID, itemID string) error {
	return s.db.Where("id = ? AND playlist_id = ?", itemID, playlistID).
		Delete(&MusicPlaylistItem{}).Error
}

// GetPlaylistWithTracks 获取播放列表详情（含曲目）
func (s *MusicService) GetPlaylistWithTracks(playlistID string) (*MusicPlaylist, error) {
	var playlist MusicPlaylist
	err := s.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order ASC")
	}).Preload("Items.Track").First(&playlist, "id = ?", playlistID).Error
	return &playlist, err
}

// DeletePlaylist 删除播放列表
func (s *MusicService) DeletePlaylist(playlistID, userID string) error {
	s.db.Where("playlist_id = ?", playlistID).Delete(&MusicPlaylistItem{})
	return s.db.Where("id = ? AND user_id = ?", playlistID, userID).Delete(&MusicPlaylist{}).Error
}
