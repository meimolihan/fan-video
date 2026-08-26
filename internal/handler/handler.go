package handler

import (
	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/repository"
	"github.com/fan-video/fan-video/internal/service"
	"go.uber.org/zap"
)

// Handlers 聚合所有HTTP处理器
type Handlers struct {
	Auth           *AuthHandler
	Library        *LibraryHandler
	Media          *MediaHandler
	Series         *SeriesHandler
	Stream         *ArtifactStreamHandler
	User           *UserHandler
	Admin          *AdminHandler
	Subtitle       *SubtitleHandler
	Metadata       *MetadataHandler
	Playlist       *PlaylistHandler
	Recommend      *RecommendHandler
	Cast           *CastHandler
	WS             *WSHandler
	Bookmark       *BookmarkHandler
	Comment        *CommentHandler
	Danmaku        *DanmakuHandler
	Stats          *StatsHandler
	FileManager    *FileManagerHandler
	Notification   *NotificationHandler
	SubtitleSearch *SubtitleSearchHandler
	BatchMetadata  *BatchMetadataHandler
	EmbyCompat     *EmbyCompatHandler
	// V2: 中期发展规划处理器
	UserProfile     *UserProfileHandler
	OfflineDownload *OfflineDownloadHandler
	ABR             *ABRHandler
	Plugin          *PluginHandler
	Music           *MusicHandler
	Photo           *PhotoHandler
	Federation      *FederationHandler
	// V6: P1~P3 新增处理器
	// 视频预处理
	Preprocess *PreprocessHandler
	// 字幕预处理
	SubtitlePreprocess *SubtitlePreprocessHandler
	// 电影系列合集
	Collection *CollectionHandler
	// V2.1: WebDAV 存储管理
	Storage *StorageHandler
	// 智能扫描重命名
	SmartRename *SmartRenameHandler
	// 扫描后处理：虚拟归类与命名映射（仅 DB 层）
	ScanPostProcess *ScanPostProcessHandler
	// 懒人入库（一键入库）
	LazyIngest *LazyIngestHandler
	// 首页手动精选轮播
	HomeFeatured *HomeFeaturedHandler
	// 海报缩略图管理
	Thumbnail *ThumbnailHandler
}

func NewHandlers(services *service.Services, repos *repository.Repositories, cfg *config.Config, logger *zap.SugaredLogger) *Handlers {
	h := &Handlers{
		Auth:    &AuthHandler{authService: services.Auth, serverName: cfg.Emby.ServerName, logger: logger},
		Library: &LibraryHandler{libService: services.Library, permSvc: services.Permission, logger: logger},
		Media:   &MediaHandler{mediaService: services.Media, personRepo: repos.Person, mediaPersonRepo: repos.MediaPerson, logger: logger},
		Series:  &SeriesHandler{seriesService: services.Series, mediaPersonRepo: repos.MediaPerson, logger: logger},
		Stream: NewArtifactStreamHandler(&StreamHandler{
			streamService: services.Stream,
			logger:        logger,
		}),
		User: &UserHandler{userService: services.User, authService: services.Auth, mediaService: services.Media, loginLogRepo: repos.LoginLog, logger: logger},
		Admin: &AdminHandler{
			userService:       services.User,
			authService:       services.Auth,
			mediaExecution:    services.MediaExecution,
			permissionService: services.Permission,
			libraryService:    services.Library,
			metadataService:   services.Metadata,
			seriesService:     services.Series,
			settingRepo:       repos.SystemSetting,
			libraryRepo:       repos.Library,
			loginLogRepo:      repos.LoginLog,
			auditLogRepo:      repos.AuditLog,
			inviteRepo:        repos.InviteCode,
			cfg:               cfg,
			logger:            logger,
			db:                repos.DB(),
		},
		Subtitle:  &SubtitleHandler{scanner: services.Scanner, streamService: services.Stream, logger: logger},
		Metadata:  &MetadataHandler{metadataService: services.Metadata, logger: logger},
		Playlist:  &PlaylistHandler{playlistService: services.Playlist, logger: logger},
		Recommend: &RecommendHandler{recommendService: services.Recommend, logger: logger},
		Cast:      &CastHandler{castService: services.Cast, logger: logger},
		WS:        &WSHandler{hub: services.WSHub, logger: logger},
		Bookmark:  &BookmarkHandler{bookmarkService: services.Bookmark, logger: logger},
		Comment:   &CommentHandler{commentService: services.Comment, logger: logger},
		Danmaku:   &DanmakuHandler{danmakuService: services.Danmaku, logger: logger},
		Stats:     &StatsHandler{statsService: services.Stats, logger: logger},
		FileManager:    &FileManagerHandler{fileService: services.FileManager, logger: logger},
		Notification:   &NotificationHandler{notifyService: services.Notification, logger: logger},
		SubtitleSearch: &SubtitleSearchHandler{subtitleSearch: services.SubtitleSearch, streamService: services.Stream, logger: logger},
		BatchMetadata:  &BatchMetadataHandler{batchService: services.BatchMetadata, importExportSvc: services.ImportExport, logger: logger},
		EmbyCompat:     &EmbyCompatHandler{embyService: services.EmbyCompat, logger: logger},
		// V2
		UserProfile:     &UserProfileHandler{profileService: services.UserProfile, logger: logger},
		OfflineDownload: &OfflineDownloadHandler{downloadService: services.OfflineDownload, logger: logger},
		ABR:             &ABRHandler{abrService: services.ABR, logger: logger},
		Plugin:          &PluginHandler{pluginService: services.Plugin, logger: logger},
		Music:           &MusicHandler{musicService: services.Music, logger: logger},
		Photo:           &PhotoHandler{photoService: services.Photo, logger: logger},
		Federation:      &FederationHandler{federationService: services.Federation, logger: logger},
		// V6: P1~P3 新增处理器
		// 视频预处理
		Preprocess: NewPreprocessHandler(services.Preprocess),
		// 字幕预处理
		SubtitlePreprocess: NewSubtitlePreprocessHandler(services.SubtitlePreprocess),
		// 电影系列合集
		Collection: &CollectionHandler{collectionService: services.Collection, streamService: services.Stream, logger: logger},
		// V2.1: WebDAV 存储管理
		Storage: NewStorageHandler(services.WebDAV, services.RemoteStorage, cfg, logger),
		// 智能扫描重命名
		SmartRename: NewSmartRenameHandler(services.SmartRename, logger),
		// 扫描后处理：虚拟归类与命名映射（仅 DB 层）
		ScanPostProcess: NewScanPostProcessHandler(services.ScanPostProcess, repos.ScanClassification, logger),
		// 懒人入库（一键入库）
		LazyIngest: NewLazyIngestHandler(services.LazyIngest, logger),
		// 首页手动精选轮播
		HomeFeatured: NewHomeFeaturedHandler(repos.HomeFeatured, repos.Media, repos.Series, logger),
		// 海报缩略图管理
		Thumbnail: NewThumbnailHandler(repos.DB(), logger),
	}

	return h
}
