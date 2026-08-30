package handler

// Handlers 聚合所有 HTTP 处理器。
//
// 注意：仅保留 Lite/NAS 发行版实际构造并挂路由的处理器。全功能轨
//（Music/Photo/Federation/Plugin/Cast/Danmaku/Notification/
// UserProfile/OfflineDownload/ABR/Preprocess/SubtitlePreprocess/SmartRename/
// LazyIngest/ScanPostProcess 等）在 Lite 二进制的路由中从未被构造，属于不可达
// 死代码，已连同其 handler 源码一并移除。
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
	WS             *WSHandler
	Bookmark       *BookmarkHandler
	Comment        *CommentHandler
	Stats          *StatsHandler
	FileManager    *FileManagerHandler
	SubtitleSearch *SubtitleSearchHandler
	BatchMetadata  *BatchMetadataHandler

	// 电影系列合集
	Collection *CollectionHandler
	// V2.1: WebDAV 存储管理
	Storage *StorageHandler
	// 首页手动精选轮播
	HomeFeatured *HomeFeaturedHandler
	// 海报缩略图管理
	Thumbnail *ThumbnailHandler
}