package handler

import (
	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/repository"
	"github.com/fan-video/fan-video/internal/service"
	"go.uber.org/zap"
)

// NewLiteHandlers wires only handlers exposed by the NAS-oriented lite server.
// Optional full-profile handlers remain nil and therefore cannot accidentally
// leak routes from the full server into the lightweight runtime.
func NewLiteHandlers(services *service.Services, repos *repository.Repositories, cfg *config.Config, logger *zap.SugaredLogger) *Handlers {
	return &Handlers{
		Auth:    &AuthHandler{authService: services.Auth, serverName: cfg.App.ServerName, logger: logger},
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
			backupService:     service.NewSystemBackupService(repos.DB(), cfg, logger),
		},
		Subtitle:    &SubtitleHandler{scanner: services.Scanner, streamService: services.Stream, logger: logger},
		Metadata:    &MetadataHandler{metadataService: services.Metadata, logger: logger},
		Playlist:    &PlaylistHandler{playlistService: services.Playlist, logger: logger},
		Recommend:   &RecommendHandler{recommendService: services.Recommend, logger: logger},
		WS:          &WSHandler{hub: services.WSHub, logger: logger},
		Bookmark:    &BookmarkHandler{bookmarkService: services.Bookmark, logger: logger},
		Comment:     &CommentHandler{commentService: service.NewCommentService(repos.Comment, repos.Media, logger), logger: logger},
		Stats:       &StatsHandler{statsService: services.Stats, logger: logger},
		FileManager: &FileManagerHandler{fileService: services.FileManager, logger: logger},
		SubtitleSearch: &SubtitleSearchHandler{
			subtitleSearch: services.SubtitleSearch,
			streamService:  services.Stream,
			logger:         logger,
		},
		BatchMetadata: &BatchMetadataHandler{batchService: services.BatchMetadata, importExportSvc: services.ImportExport, logger: logger},
		Storage:       NewStorageHandler(services.WebDAV, services.RemoteStorage, cfg, logger),
		Collection: &CollectionHandler{
			collectionService: services.Collection,
			streamService:     services.Stream,
			logger:            logger,
		},
		// 首页手动精选轮播
		HomeFeatured: NewHomeFeaturedHandler(repos.HomeFeatured, repos.Media, repos.Series, repos.Library, logger),
	}
}
