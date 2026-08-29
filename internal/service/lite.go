package service

import (
	"fmt"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

// NewLiteServices creates the NAS-oriented server profile.
//
// The lite profile keeps the complete movie/series experience while avoiding
// startup of unrelated subsystems such as music, photos, federation, plugins,
// preprocessing workers and Emby compatibility. Metadata artwork is resolved
// locally only (NFO + same-name images); no network scraping is performed.
func NewLiteServices(repos *repository.Repositories, cfg *config.Config, logger *zap.SugaredLogger) *Services {
	mediaExecution, err := NewMediaExecutionService(repos.DB(), cfg, logger)
	if err != nil {
		panic(fmt.Sprintf("initialize media execution service: %v", err))
	}
	artifactMaintenance := NewArtifactMaintenanceService(repos.Transcode, cfg, logger)
	scanner := NewScannerService(repos.Media, repos.Series, cfg, logger)
	metadata := NewMetadataService(repos.Media, repos.Series, cfg, logger)

	wsHub := NewWSHub(logger)
	go wsHub.Run()
	scanner.SetWSHub(wsHub)
	artifactMaintenance.SetWSHub(wsHub)
	metadata.SetWSHub(wsHub)

	nfoService := NewNFOService(logger, cfg)
	metadata.SetNFOService(nfoService)

	libraryService := NewLibraryService(
		repos.Library,
		repos.Media,
		repos.Series,
		repos.Favorite,
		repos.WatchHistory,
		repos.MediaPerson,
		nil, // 不迁移或访问 AI 扫描归类表
		repos.RecommendCache,
		repos.PlaybackStats,
		repos.MediaProbe,
		repos.MovieCollection,
		cfg,
		scanner,
		metadata,
		logger,
	)
	libraryService.SetWSHub(wsHub)

	fileWatcher := NewFileWatcherService(cfg, logger, repos.Library, repos.Media, repos.Series, scanner, metadata)
	fileWatcher.SetWSHub(wsHub)
	if err := fileWatcher.Start(); err != nil {
		logger.Errorf("文件监听服务启动失败: %v", err)
	}
	libraryService.SetFileWatcher(fileWatcher)

	vfsManager := NewVFSManager(logger)
	scanner.SetVFSManager(vfsManager)
	nfoService.SetVFSManager(vfsManager)

	// Remote storage remains available but is initialized only when explicitly
	// configured, so local-library users do not pay for remote connection probes.
	webDAVService := NewWebDAVService(cfg, logger, vfsManager)
	if cfg.Storage.WebDAV.Enabled {
		if err := webDAVService.Initialize(); err != nil {
			logger.Warnf("WebDAV 服务初始化失败: %v", err)
		}
	}
	remoteStorageService := NewRemoteStorageService(cfg, logger, vfsManager)
	if cfg.Storage.Alist.Enabled || cfg.Storage.S3.Enabled {
		if err := remoteStorageService.Initialize(); err != nil {
			logger.Warnf("远程存储服务初始化失败: %v", err)
		}
	}
	SetGlobalRemoteStorageService(remoteStorageService)

	recommendService := NewRecommendService(
		repos.Media,
		repos.Series,
		repos.WatchHistory,
		repos.Favorite,
		repos.RecommendCache,
		logger,
	)

	fileManager := NewFileManagerService(
		repos.Media,
		repos.Series,
		repos.FileOpLog,
		metadata,
		logger,
	)
	fileManager.SetWSHub(wsHub)

	subtitleSearchService := NewSubtitleSearchService("", cfg.Cache.CacheDir, logger)
	batchMetadataService := NewBatchMetadataService(repos.DB(), logger)
	importExportService := NewMediaImportExportService(repos.DB(), logger)
	statsService := NewStatsService(repos.PlaybackStats, repos.Media, logger)
	collectionService := NewCollectionService(repos.MovieCollection, repos.Media, logger)

	streamService := NewStreamService(repos.Media, repos.Series, mediaExecution, cfg, logger)
	streamService.SetSettingRepo(repos.SystemSetting)
	streamService.SetVFSManager(vfsManager)
	streamService.SetNFOService(nfoService)

	svcs := &Services{
		User:                NewUserService(repos.User, repos.AuditLog, cfg, logger),
		Auth:                NewAuthService(repos.User, repos.InviteCode, repos.LoginLog, repos.AuditLog, cfg, logger),
		Library:             libraryService,
		Media:               NewMediaService(repos.Media, repos.Series, repos.WatchHistory, repos.Favorite, repos.WatchLater, repos.Library, repos.PlaybackStats, cfg, logger),
		Series:              NewSeriesService(repos.Series, repos.Media, logger),
		Stream:              streamService,
		MediaExecution:      mediaExecution,
		ArtifactMaintenance: artifactMaintenance,
		Metadata:            metadata,
		Scanner:             scanner,
		Playlist:            NewPlaylistService(repos.Playlist, logger),
		Recommend:           recommendService,
		Bookmark:            NewBookmarkService(repos.Bookmark, repos.Media, logger),
		Permission:          NewPermissionService(repos.UserPermission, repos.ContentRating, repos.WatchHistory, logger),
		FileWatcher:         fileWatcher,
		NFO:                 nfoService,
		Stats:               statsService,
		VFS:                 vfsManager,
		WebDAV:              webDAVService,
		RemoteStorage:       remoteStorageService,
		WSHub:               wsHub,
		FileManager:         fileManager,
		SubtitleSearch:      subtitleSearchService,
		BatchMetadata:       batchMetadataService,
		ImportExport:        importExportService,
		Collection:          collectionService,
	}

	svcs.Series.SetMediaPersonRepo(repos.MediaPerson)
	svcs.Series.SetStreamService(streamService)
	svcs.Library.SetSeriesService(svcs.Series)
	svcs.Library.SetCollectionService(collectionService)

	// Scanning performs indexing and local artwork matching only.
	// Expensive video/subtitle preprocessing workers are not started automatically.
	scanner.SetOnScanComplete(func(libraryID string) {
		logger.Infof("媒体库扫描完成 library_id=%s（未启动预处理 worker）", libraryID)
	})

	// 启动后台清理一次历史残留数据（联动清理机制上线前删除的视频留下的
	// 观看历史/收藏/弹幕/空剧集壳/缓存刮削图等），完成后通知前端刷新
	go func() {
		if result := scanner.CleanupResidualData(); result != nil &&
			(result.RemovedMedia > 0 || result.RemovedRelatedRows > 0 ||
				result.RemovedSeries > 0 || result.RemovedCollections > 0 || result.RemovedCacheDirs > 0) {
			wsHub.BroadcastEvent(EventLibraryUpdated, &LibraryChangedData{
				Action: "media_removed",
				Message: fmt.Sprintf("已清理残留数据: 媒体 %d, 关联记录 %d, 空剧集 %d",
					result.RemovedMedia, result.RemovedRelatedRows, result.RemovedSeries),
			})
		}
	}()

	return svcs
}
