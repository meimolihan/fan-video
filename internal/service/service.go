package service

// Services 聚合所有服务（fan-video 精简版）
//
// 注意：仅保留 Lite/NAS 发行版实际构造的服务字段。全功能轨
//（Cast/Danmaku/Notification/Webhook/EmbyCompat/UserProfile/OfflineDownload/
// ABR/Plugin/Music/Photo/Federation/Preprocess/SubtitlePreprocess/GPUMonitor/
// SmartRename/ScanPostProcess/LazyIngest）在 Lite 路径中从不赋值、也从不被读取，
// 属于不可达死代码，已连同其 service 源码一并移除。
type Services struct {
	User                *UserService
	Auth                *AuthService
	Library             *LibraryService
	Media               *MediaService
	Series              *SeriesService
	Stream              *StreamService
	MediaExecution      *MediaExecutionService
	ArtifactMaintenance *ArtifactMaintenanceService
	Metadata            *MetadataService
	Scanner             *ScannerService
	Playlist            *PlaylistService
	Recommend           *RecommendService
	Bookmark            *BookmarkService
	Comment             *CommentService
	Permission          *PermissionService
	FileWatcher         *FileWatcherService
	NFO                 *NFOService
	Stats               *StatsService
	VFS                 *VFSManager
	WebDAV              *WebDAVService
	RemoteStorage       *RemoteStorageService // V2.3: Alist / S3 统一管理
	WSHub               *WSHub
	FileManager         *FileManagerService
	SubtitleSearch      *SubtitleSearchService
	BatchMetadata       *BatchMetadataService
	ImportExport        *MediaImportExportService

	// 电影系列合集
	Collection *CollectionService
}