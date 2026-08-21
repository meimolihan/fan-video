package service

// Services 聚合所有服务（fan-video 精简版）
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
	Cast                *CastService
	Bookmark            *BookmarkService
	Comment             *CommentService
	Danmaku             *DanmakuService
	Permission          *PermissionService
	FileWatcher         *FileWatcherService
	NFO                 *NFOService
	Stats               *StatsService
	Webhook             *WebhookService
	VFS                 *VFSManager
	WebDAV              *WebDAVService
	RemoteStorage       *RemoteStorageService // V2.3: Alist / S3 统一管理
	WSHub               *WSHub
	FileManager         *FileManagerService
	Notification        *NotificationService
	SubtitleSearch      *SubtitleSearchService
	BatchMetadata       *BatchMetadataService
	ImportExport        *MediaImportExportService
	EmbyCompat          *EmbyCompatService
	// V2: 中期发展规划服务
	UserProfile     *UserProfileService
	OfflineDownload *OfflineDownloadService
	ABR             *ABRService
	Plugin          *PluginService
	Music           *MusicService
	Photo           *PhotoService
	Federation      *FederationService
	// V6: P1~P3 新增功能
	Preprocess         *PreprocessService
	SubtitlePreprocess *SubtitlePreprocessService
	GPUMonitor         *GPUMonitor
	// 电影系列合集
	Collection *CollectionService
	// 智能扫描重命名（独立子系统，不复用 FileManager）
	SmartRename *SmartRenameService
	// 扫描后处理：虚拟归类与命名映射（仅 DB 层；不动磁盘）
	ScanPostProcess *ScanPostProcessService
	// 懒人入库：源目录 → 自动分类/命名 → 建库 → 扫描
	LazyIngest *LazyIngestService
}
