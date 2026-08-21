package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/fan-video/fan-video/internal/service"
	"go.uber.org/zap"
)

// ==================== 多用户配置文件 Handler ====================

type UserProfileHandler struct {
	profileService *service.UserProfileService
	logger         *zap.SugaredLogger
}

func (h *UserProfileHandler) ListProfiles(c *gin.Context) {
	userID := c.GetString("user_id")
	profiles, err := h.profileService.ListProfiles(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": profiles})
}

func (h *UserProfileHandler) CreateProfile(c *gin.Context) {
	userID := c.GetString("user_id")
	var profile service.UserProfile
	if err := c.ShouldBindJSON(&profile); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}
	if err := h.profileService.CreateProfile(userID, &profile); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "配置文件已创建", "data": profile})
}

func (h *UserProfileHandler) GetProfile(c *gin.Context) {
	profileID := c.Param("id")
	profile, err := h.profileService.GetProfile(profileID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置文件不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": profile})
}

func (h *UserProfileHandler) UpdateProfile(c *gin.Context) {
	profileID := c.Param("id")
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}
	if err := h.profileService.UpdateProfile(profileID, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "配置文件已更新"})
}

func (h *UserProfileHandler) DeleteProfile(c *gin.Context) {
	profileID := c.Param("id")
	if err := h.profileService.DeleteProfile(profileID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "配置文件已删除"})
}

func (h *UserProfileHandler) SwitchProfile(c *gin.Context) {
	profileID := c.Param("id")
	var req struct {
		PIN string `json:"pin"`
	}
	c.ShouldBindJSON(&req)
	profile, err := h.profileService.SwitchProfile(profileID, req.PIN)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": profile})
}

func (h *UserProfileHandler) GetWatchLogs(c *gin.Context) {
	profileID := c.Param("id")
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	logs, err := h.profileService.GetWatchLogs(profileID, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": logs})
}

func (h *UserProfileHandler) GetDailyUsage(c *gin.Context) {
	profileID := c.Param("id")
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	usage, err := h.profileService.GetDailyUsage(profileID, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": usage})
}

func (h *UserProfileHandler) GetProfileStats(c *gin.Context) {
	profileID := c.Param("id")
	stats := h.profileService.GetProfileStats(profileID)
	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// ==================== 离线下载 Handler ====================

type OfflineDownloadHandler struct {
	downloadService *service.OfflineDownloadService
	logger          *zap.SugaredLogger
}

func (h *OfflineDownloadHandler) CreateDownload(c *gin.Context) {
	userID := c.GetString("user_id")
	var req struct {
		MediaID  string `json:"media_id"`
		Title    string `json:"title"`
		FileSize int64  `json:"file_size"`
		FilePath string `json:"file_path"`
		Quality  string `json:"quality"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}
	task, err := h.downloadService.CreateDownload(userID, req.MediaID, req.Title, req.FileSize, req.FilePath, req.Quality)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "下载任务已创建", "data": task})
}

func (h *OfflineDownloadHandler) BatchDownload(c *gin.Context) {
	userID := c.GetString("user_id")
	var req service.BatchDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}
	tasks, errors := h.downloadService.BatchCreateDownloads(userID, req)
	c.JSON(http.StatusOK, gin.H{"data": tasks, "errors": errors})
}

func (h *OfflineDownloadHandler) ListDownloads(c *gin.Context) {
	userID := c.GetString("user_id")
	status := c.Query("status")
	tasks, err := h.downloadService.GetUserDownloads(userID, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tasks})
}

func (h *OfflineDownloadHandler) GetQueueInfo(c *gin.Context) {
	userID := c.GetString("user_id")
	info, err := h.downloadService.GetQueueInfo(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": info})
}

func (h *OfflineDownloadHandler) CancelDownload(c *gin.Context) {
	userID := c.GetString("user_id")
	taskID := c.Param("id")
	if err := h.downloadService.CancelDownload(taskID, userID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "下载已取消"})
}

func (h *OfflineDownloadHandler) PauseDownload(c *gin.Context) {
	userID := c.GetString("user_id")
	taskID := c.Param("id")
	if err := h.downloadService.PauseDownload(taskID, userID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "下载已暂停"})
}

func (h *OfflineDownloadHandler) ResumeDownload(c *gin.Context) {
	userID := c.GetString("user_id")
	taskID := c.Param("id")
	if err := h.downloadService.ResumeDownload(taskID, userID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "下载已恢复"})
}

func (h *OfflineDownloadHandler) DeleteDownload(c *gin.Context) {
	userID := c.GetString("user_id")
	taskID := c.Param("id")
	if err := h.downloadService.DeleteDownload(taskID, userID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "下载已删除"})
}

// ==================== 插件系统 Handler ====================

type PluginHandler struct {
	pluginService *service.PluginService
	logger        *zap.SugaredLogger
}

func (h *PluginHandler) ListPlugins(c *gin.Context) {
	plugins, err := h.pluginService.ListPlugins()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": plugins})
}

func (h *PluginHandler) GetPlugin(c *gin.Context) {
	pluginID := c.Param("id")
	info, manifest, err := h.pluginService.GetPlugin(pluginID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "插件不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": info, "manifest": manifest})
}

func (h *PluginHandler) EnablePlugin(c *gin.Context) {
	pluginID := c.Param("id")
	if err := h.pluginService.EnablePlugin(pluginID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "插件已启用"})
}

func (h *PluginHandler) DisablePlugin(c *gin.Context) {
	pluginID := c.Param("id")
	if err := h.pluginService.DisablePlugin(pluginID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "插件已禁用"})
}

func (h *PluginHandler) UninstallPlugin(c *gin.Context) {
	pluginID := c.Param("id")
	if err := h.pluginService.UninstallPlugin(pluginID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "插件已卸载"})
}

func (h *PluginHandler) UpdatePluginConfig(c *gin.Context) {
	pluginID := c.Param("id")
	var config map[string]interface{}
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的配置"})
		return
	}
	if err := h.pluginService.UpdatePluginConfig(pluginID, config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "插件配置已更新"})
}

func (h *PluginHandler) ScanPlugins(c *gin.Context) {
	discovered, err := h.pluginService.ScanPluginDir()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": discovered})
}

// ==================== 音乐库 Handler ====================

type MusicHandler struct {
	musicService *service.MusicService
	logger       *zap.SugaredLogger
}

func (h *MusicHandler) ListTracks(c *gin.Context) {
	libraryID := c.Query("library_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "50"))
	sort := c.DefaultQuery("sort", "artist")

	tracks, total, err := h.musicService.ListTracks(libraryID, page, size, sort)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tracks, "total": total, "page": page, "size": size})
}

func (h *MusicHandler) ListAlbums(c *gin.Context) {
	libraryID := c.Query("library_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "30"))

	albums, total, err := h.musicService.ListAlbums(libraryID, page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": albums, "total": total, "page": page, "size": size})
}

func (h *MusicHandler) GetAlbum(c *gin.Context) {
	albumID := c.Param("id")
	album, err := h.musicService.GetAlbumWithTracks(albumID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "专辑不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": album})
}

func (h *MusicHandler) SearchMusic(c *gin.Context) {
	query := c.Query("q")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	tracks, err := h.musicService.SearchMusic(query, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tracks})
}

func (h *MusicHandler) GetLyrics(c *gin.Context) {
	trackID := c.Param("id")
	lyrics, err := h.musicService.GetLyrics(trackID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": lyrics})
}

func (h *MusicHandler) ToggleLove(c *gin.Context) {
	trackID := c.Param("id")
	loved, err := h.musicService.ToggleLove(trackID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"loved": loved})
}

func (h *MusicHandler) ScanLibrary(c *gin.Context) {
	var req struct {
		LibraryID string `json:"library_id"`
		Path      string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}
	count, err := h.musicService.ScanMusicLibrary(req.LibraryID, req.Path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "扫描完成", "count": count})
}

func (h *MusicHandler) ListPlaylists(c *gin.Context) {
	userID := c.GetString("user_id")
	playlists, err := h.musicService.ListPlaylists(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": playlists})
}

func (h *MusicHandler) CreatePlaylist(c *gin.Context) {
	userID := c.GetString("user_id")
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}
	playlist, err := h.musicService.CreatePlaylist(userID, req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": playlist})
}

func (h *MusicHandler) GetPlaylist(c *gin.Context) {
	playlistID := c.Param("id")
	playlist, err := h.musicService.GetPlaylistWithTracks(playlistID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "播放列表不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": playlist})
}

func (h *MusicHandler) AddToPlaylist(c *gin.Context) {
	playlistID := c.Param("id")
	var req struct {
		TrackIDs []string `json:"track_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}
	if err := h.musicService.AddToPlaylist(playlistID, req.TrackIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已添加到播放列表"})
}

// ==================== 图片库 Handler ====================

type PhotoHandler struct {
	photoService *service.PhotoService
	logger       *zap.SugaredLogger
}

func (h *PhotoHandler) ListPhotos(c *gin.Context) {
	libraryID := c.Query("library_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "50"))
	sort := c.DefaultQuery("sort", "date_desc")

	filters := map[string]string{
		"album_id": c.Query("album_id"),
		"tag":      c.Query("tag"),
		"scene":    c.Query("scene"),
		"favorite": c.Query("favorite"),
	}

	photos, total, err := h.photoService.ListPhotos(libraryID, page, size, sort, filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": photos, "total": total, "page": page, "size": size})
}

func (h *PhotoHandler) GetPhoto(c *gin.Context) {
	photoID := c.Param("id")
	photo, err := h.photoService.GetPhoto(photoID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "照片不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": photo})
}

func (h *PhotoHandler) ListAlbums(c *gin.Context) {
	userID := c.GetString("user_id")
	albums, err := h.photoService.ListAlbums(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": albums})
}

func (h *PhotoHandler) CreateAlbum(c *gin.Context) {
	userID := c.GetString("user_id")
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}
	album, err := h.photoService.CreateAlbum(userID, req.Name, req.Description)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": album})
}

func (h *PhotoHandler) AddPhotosToAlbum(c *gin.Context) {
	albumID := c.Param("id")
	var req struct {
		PhotoIDs []string `json:"photo_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}
	if err := h.photoService.AddPhotosToAlbum(albumID, req.PhotoIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "照片已添加到相册"})
}

func (h *PhotoHandler) ToggleFavorite(c *gin.Context) {
	photoID := c.Param("id")
	fav, err := h.photoService.ToggleFavorite(photoID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"is_favorite": fav})
}

func (h *PhotoHandler) SetRating(c *gin.Context) {
	photoID := c.Param("id")
	var req struct {
		Rating int `json:"rating"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}
	if err := h.photoService.SetRating(photoID, req.Rating); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "评分已更新"})
}

func (h *PhotoHandler) SearchPhotos(c *gin.Context) {
	query := c.Query("q")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	photos, err := h.photoService.SearchPhotos(query, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": photos})
}

func (h *PhotoHandler) GetStats(c *gin.Context) {
	libraryID := c.Query("library_id")
	stats := h.photoService.GetPhotoStats(libraryID)
	c.JSON(http.StatusOK, gin.H{"data": stats})
}

func (h *PhotoHandler) ScanLibrary(c *gin.Context) {
	var req struct {
		LibraryID string `json:"library_id"`
		Path      string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}
	count, err := h.photoService.ScanPhotoLibrary(req.LibraryID, req.Path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "扫描完成", "count": count})
}

// ==================== 联邦架构 Handler ====================

type FederationHandler struct {
	federationService *service.FederationService
	logger            *zap.SugaredLogger
}

func (h *FederationHandler) ListNodes(c *gin.Context) {
	nodes, err := h.federationService.ListNodes()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": nodes})
}

func (h *FederationHandler) RegisterNode(c *gin.Context) {
	var req struct {
		Name   string `json:"name"`
		URL    string `json:"url"`
		APIKey string `json:"api_key"`
		Role   string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}
	node, err := h.federationService.RegisterNode(req.Name, req.URL, req.APIKey, req.Role)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "节点已注册", "data": node})
}

func (h *FederationHandler) RemoveNode(c *gin.Context) {
	nodeID := c.Param("id")
	if err := h.federationService.RemoveNode(nodeID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "节点已移除"})
}

func (h *FederationHandler) SyncNode(c *gin.Context) {
	nodeID := c.Param("id")
	syncType := c.DefaultQuery("type", "full")
	task, err := h.federationService.SyncFromNode(nodeID, syncType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "同步已开始", "data": task})
}

func (h *FederationHandler) SearchSharedMedia(c *gin.Context) {
	query := c.Query("q")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	media, total, err := h.federationService.SearchSharedMedia(query, page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": media, "total": total})
}

func (h *FederationHandler) GetSharedMediaStream(c *gin.Context) {
	mediaID := c.Param("id")
	streamURL, err := h.federationService.GetSharedMediaStream(mediaID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"stream_url": streamURL})
}

func (h *FederationHandler) GetStats(c *gin.Context) {
	stats, err := h.federationService.GetStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": stats})
}

func (h *FederationHandler) GetSyncTasks(c *gin.Context) {
	nodeID := c.Query("node_id")
	tasks, err := h.federationService.GetSyncTasks(nodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tasks})
}

// 联邦 API 端点（供其他节点调用）
func (h *FederationHandler) Health(c *gin.Context) {
	health := h.federationService.GetLocalHealth()
	c.JSON(http.StatusOK, health)
}

func (h *FederationHandler) MediaList(c *gin.Context) {
	media, err := h.federationService.GetLocalMediaList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": media})
}

// ==================== ABR Handler ====================

type ABRHandler struct {
	abrService *service.ABRService
	logger     *zap.SugaredLogger
}

func (h *ABRHandler) GetStatus(c *gin.Context) {
	status := h.abrService.GetABRStatus()
	c.JSON(http.StatusOK, gin.H{"data": status})
}

func (h *ABRHandler) GetGPUInfo(c *gin.Context) {
	info := h.abrService.GetGPUInfo()
	c.JSON(http.StatusOK, gin.H{"data": info})
}

func (h *ABRHandler) CleanCache(c *gin.Context) {
	mediaID := c.Query("media_id")
	if mediaID != "" {
		h.abrService.CleanABRCache(mediaID)
		c.JSON(http.StatusOK, gin.H{"message": "ABR 缓存已清理"})
	} else {
		size, err := h.abrService.CleanAllABRCache()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "所有 ABR 缓存已清理", "freed_bytes": size})
	}
}
