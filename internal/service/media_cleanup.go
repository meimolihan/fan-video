package service

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ResidualCleanupResult 残留数据清理结果统计
type ResidualCleanupResult struct {
	RemovedMedia       int   `json:"removed_media"`        // 清理的失效媒体记录数（磁盘文件已不存在）
	RemovedRelatedRows int64 `json:"removed_related_rows"` // 清理的孤儿关联数据行数（引用已删除媒体的观看历史/收藏/弹幕等）
	RemovedSeries      int   `json:"removed_series"`       // 清理的空剧集合集数
	RemovedCollections int64 `json:"removed_collections"`  // 清理的空电影合集数
	RemovedCacheDirs   int   `json:"removed_cache_dirs"`   // 清理的孤儿缓存目录数（刮削图/预处理产物）

	HasError bool   `json:"-"`
	ErrorMsg string `json:"-"`
}

// ==================== 媒体删除联动清理 ====================
//
// 视频文件被删除后（扫描清理 / 手动删除 / 文件监听即时删除），仅删除 medias 表记录
// 会留下大量"残留信息"：观看历史、收藏、弹幕、评论、播放统计、章节、精彩片段、
// 封面候选、演职人员关联，以及缓存目录中的刮削图片与预处理产物。
// 这里提供统一的彻底清理入口，供所有删除路径复用。

// purgeMediaCacheArtifacts 删除媒体在缓存目录中的衍生文件：
//   - cache/images/<mediaID>/      刮削海报/背景图（保存到媒体同目录失败时的回退位置）
//   - cache/preprocess/<mediaID>/  预处理产物（雪碧图/HLS 变体/缩略图等）
func purgeMediaCacheArtifacts(cacheDir string, mediaID string, logger *zap.SugaredLogger) {
	if cacheDir == "" || mediaID == "" {
		return
	}
	for _, sub := range []string{
		filepath.Join("images", mediaID),
		filepath.Join("preprocess", mediaID),
	} {
		dir := filepath.Join(cacheDir, sub)
		if err := os.RemoveAll(dir); err != nil {
			logger.Warnf("清理媒体缓存产物失败: %s, 错误: %v", dir, err)
		}
	}
}

// PurgeMediaCompletely 彻底删除一个媒体：关联数据 → 缓存产物 → 数据库记录。
// 即使中间步骤失败也会继续尝试删除主记录，返回首个错误供调用方记录日志。
func PurgeMediaCompletely(
	mediaRepo *repository.MediaRepo,
	cacheDir string,
	logger *zap.SugaredLogger,
	media *model.Media,
	reason string,
) error {
	var firstErr error

	if n, err := mediaRepo.DeleteRelatedDataByMediaIDs([]string{media.ID}); err != nil {
		logger.Warnf("清理媒体关联数据失败: %s (%s), 错误: %v", media.FilePath, reason, err)
		if firstErr == nil {
			firstErr = err
		}
	} else if n > 0 {
		logger.Infof("已清理媒体关联数据 %d 条: %s (%s)", n, media.FilePath, reason)
	}

	purgeMediaCacheArtifacts(cacheDir, media.ID, logger)

	if err := mediaRepo.DeleteByID(media.ID); err != nil {
		logger.Warnf("删除媒体记录失败: %s (%s), 错误: %v", media.FilePath, reason, err)
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// PurgeSeriesCompletely 彻底删除一个剧集合集：演职人员关联 → 缓存刮削图 → 合集记录。
func PurgeSeriesCompletely(
	seriesRepo *repository.SeriesRepo,
	cacheDir string,
	logger *zap.SugaredLogger,
	series *model.Series,
) error {
	db := seriesRepo.DB()
	if db.Migrator().HasTable(&model.MediaPerson{}) {
		if err := db.Unscoped().Where("series_id = ?", series.ID).
			Delete(&model.MediaPerson{}).Error; err != nil {
			logger.Warnf("清理剧集演职人员关联失败: %s, 错误: %v", series.Title, err)
		}
	}
	if cacheDir != "" && series.ID != "" {
		if err := os.RemoveAll(filepath.Join(cacheDir, "images", "series", series.ID)); err != nil {
			logger.Warnf("清理剧集缓存图片失败: %s, 错误: %v", series.Title, err)
		}
	}
	return seriesRepo.Delete(series.ID)
}

// PurgeEmptySeriesInLibrary 清理指定媒体库下已经没有任何剧集文件的空合集，
// 返回清理数量。扫描删除失效媒体后调用，避免"视频全删了但合集壳还在"。
func PurgeEmptySeriesInLibrary(
	seriesRepo *repository.SeriesRepo,
	mediaRepo *repository.MediaRepo,
	cacheDir string,
	logger *zap.SugaredLogger,
	libraryID string,
) int {
	seriesList, err := seriesRepo.ListByLibraryID(libraryID)
	if err != nil || len(seriesList) == 0 {
		return 0
	}

	ids := make([]string, 0, len(seriesList))
	for i := range seriesList {
		ids = append(ids, seriesList[i].ID)
	}
	var withEpisodes []string
	if err := mediaRepo.DB().Model(&model.Media{}).
		Where("series_id IN ?", ids).
		Distinct().
		Pluck("series_id", &withEpisodes).Error; err != nil {
		logger.Warnf("查询剧集占用失败: 库 %s, 错误: %v", libraryID, err)
		return 0
	}
	hasEpisodes := make(map[string]bool, len(withEpisodes))
	for _, id := range withEpisodes {
		hasEpisodes[id] = true
	}

	purged := 0
	for i := range seriesList {
		if hasEpisodes[seriesList[i].ID] {
			continue
		}
		if err := PurgeSeriesCompletely(seriesRepo, cacheDir, logger, &seriesList[i]); err != nil {
			logger.Warnf("清理空合集失败: %s, 错误: %v", seriesList[i].Title, err)
			continue
		}
		purged++
		logger.Infof("清理空合集（剧集已全部移除）: %s (ID=%s)", seriesList[i].Title, seriesList[i].ID)
	}
	return purged
}

// ==================== 残留数据深度清理 ====================
//
// 处理"历史遗留"的残留：在联动清理机制上线之前删除的视频，其关联数据、
// 空剧集壳、空合集壳与缓存产物会永久留在数据库/磁盘上，重新扫描无法触达
// （媒体记录早已不在）。此入口不依赖扫描，直接按引用完整性做全量清理：
//  1. 逐条 stat 校验媒体文件是否仍存在（STRM 远程流跳过），失效则彻底清除
//  2. 删除所有引用不存在媒体的孤儿行（观看历史/收藏/弹幕/评论/统计等）
//  3. 清理没有任何剧集的空合集（含其缓存刮削图）
//  4. 清理没有任何电影的空合集壳
//  5. 清理缓存目录中没有对应媒体/剧集的孤儿目录

const residualChunkSize = 400 // SQLite IN 参数上限安全值

func chunkStrings(ids []string) [][]string {
	var chunks [][]string
	for i := 0; i < len(ids); i += residualChunkSize {
		end := i + residualChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[i:end])
	}
	return chunks
}

// deleteOrphanRowsByMediaID 删除表中 media_id 不在 validMediaIDs 内的所有行
func deleteOrphanRowsByMediaID(db *gorm.DB, table any, validMediaIDs map[string]bool) int64 {
	var referenced []string
	if err := db.Model(table).Distinct().Pluck("media_id", &referenced).Error; err != nil {
		return 0
	}
	var orphans []string
	for _, id := range referenced {
		if id != "" && !validMediaIDs[id] {
			orphans = append(orphans, id)
		}
	}
	if len(orphans) == 0 {
		return 0
	}
	var total int64
	for _, chunk := range chunkStrings(orphans) {
		result := db.Unscoped().Where("media_id IN ?", chunk).Delete(table)
		if result.Error == nil {
			total += result.RowsAffected
		}
	}
	return total
}

// looksLikeUUID 判断目录名是否形如 UUID（36 字符、4 个连字符），
// 防止误删缓存目录中非媒体 ID 命名的其他资产
func looksLikeUUID(name string) bool {
	if len(name) != 36 || strings.Count(name, "-") != 4 {
		return false
	}
	for _, r := range name {
		if r != '-' && !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') && !(r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// CleanupResidualData 全量清理残留数据。幂等，可重复调用；
// 在服务启动时后台执行一次，也可通过管理接口手动触发。
func (s *ScannerService) CleanupResidualData() *ResidualCleanupResult {
	result := &ResidualCleanupResult{}
	db := s.mediaRepo.DB()

	// ========== 1. 校验所有媒体文件的磁盘存在性 ==========
	type mediaRow struct {
		ID        string
		FilePath  string
		StreamURL string
	}
	var rows []mediaRow
	if err := db.Model(&model.Media{}).
		Select("id", "file_path", "stream_url").
		Find(&rows).Error; err != nil {
		result.HasError = true
		result.ErrorMsg = "查询媒体列表失败: " + err.Error()
		s.logger.Warnf("残留清理中止: %s", result.ErrorMsg)
		return result
	}

	validMedia := make(map[string]bool, len(rows))
	var stale []*model.Media
	for i := range rows {
		row := &rows[i]
		// STRM 远程流没有本地文件可校验；AI 整理虚拟路径由扫描流程处理
		if row.StreamURL != "" || row.FilePath == "" {
			validMedia[row.ID] = true
			continue
		}
		if _, statErr := s.statLibraryPath(row.FilePath); os.IsNotExist(statErr) {
			m := &model.Media{ID: row.ID, FilePath: row.FilePath}
			stale = append(stale, m)
		} else {
			validMedia[row.ID] = true
		}
	}
	for _, m := range stale {
		if err := PurgeMediaCompletely(s.mediaRepo, s.cfg.Cache.CacheDir, s.logger, m, "残留清理"); err != nil {
			result.HasError = true
			result.ErrorMsg = err.Error()
			// 主记录清理失败时视为仍存在，避免本轮误清其关联数据
			validMedia[m.ID] = true
			continue
		}
		result.RemovedMedia++
	}
	if result.RemovedMedia > 0 {
		s.logger.Infof("残留清理: 移除失效媒体 %d 条", result.RemovedMedia)
	}

	// ========== 2. 孤儿关联数据（引用已删除媒体） ==========
	coreTables := []any{
		&model.WatchHistory{},
		&model.Favorite{},
		&model.WatchLater{},
		&model.PlaylistItem{},
		&model.Bookmark{},
		&model.Comment{},
		&model.ContentRating{},
		&model.PlaybackStats{},
		&model.VideoChapter{},
		&model.VideoHighlight{},
		&model.AIAnalysisTask{},
		&model.CoverCandidate{},
	}
	optionalTables := []any{
		&model.DanmakuComment{},
		&model.TranscodeTask{},
		&model.PreprocessTask{},
		&model.SubtitlePreprocessTask{},
	}
	allTables := append(coreTables, optionalTables...)
	for _, table := range allTables {
		if !db.Migrator().HasTable(table) {
			continue
		}
		result.RemovedRelatedRows += deleteOrphanRowsByMediaID(db, table, validMedia)
	}
	// 演职人员关联同时支持 media_id 与 series_id 维度
	if db.Migrator().HasTable(&model.MediaPerson{}) {
		result.RemovedRelatedRows += deleteOrphanRowsByMediaID(db, &model.MediaPerson{}, validMedia)
	}
	if result.RemovedRelatedRows > 0 {
		s.logger.Infof("残留清理: 移除孤儿关联数据 %d 条", result.RemovedRelatedRows)
	}

	// ========== 3. 空剧集合集 ==========
	var seriesList []model.Series
	if err := db.Model(&model.Series{}).Find(&seriesList).Error; err == nil && len(seriesList) > 0 {
		allSeriesIDs := make([]string, 0, len(seriesList))
		for i := range seriesList {
			allSeriesIDs = append(allSeriesIDs, seriesList[i].ID)
		}
		hasEpisodes := make(map[string]bool)
		for _, chunk := range chunkStrings(allSeriesIDs) {
			var withEpisodes []string
			if err := db.Model(&model.Media{}).Where("series_id IN ?", chunk).
				Distinct().Pluck("series_id", &withEpisodes).Error; err == nil {
				for _, id := range withEpisodes {
					hasEpisodes[id] = true
				}
			}
		}
		purgedSeries := make(map[string]bool)
		for i := range seriesList {
			if hasEpisodes[seriesList[i].ID] {
				continue
			}
			if err := PurgeSeriesCompletely(s.seriesRepo, s.cfg.Cache.CacheDir, s.logger, &seriesList[i]); err == nil {
				result.RemovedSeries++
				purgedSeries[seriesList[i].ID] = true
			}
		}
		// 孤儿演职人员（series_id 已无对应合集）
		if db.Migrator().HasTable(&model.MediaPerson{}) {
			var referencedSeries []string
			if err := db.Model(&model.MediaPerson{}).Where("series_id != ''").
				Distinct().Pluck("series_id", &referencedSeries).Error; err == nil {
				validSeries := make(map[string]bool, len(seriesList))
				for i := range seriesList {
					validSeries[seriesList[i].ID] = true
				}
				for id := range purgedSeries {
					delete(validSeries, id)
				}
				var orphanPersons []string
				for _, sid := range referencedSeries {
					if sid != "" && !validSeries[sid] {
						orphanPersons = append(orphanPersons, sid)
					}
				}
				for _, chunk := range chunkStrings(orphanPersons) {
					if r := db.Unscoped().Where("series_id IN ?", chunk).Delete(&model.MediaPerson{}); r.Error == nil {
						result.RemovedRelatedRows += r.RowsAffected
					}
				}
			}
		}
	}
	if result.RemovedSeries > 0 {
		s.logger.Infof("残留清理: 移除空剧集合集 %d 个", result.RemovedSeries)
	}

	// ========== 4. 空电影合集 ==========
	var collectionIDs []string
	if err := db.Model(&model.MovieCollection{}).
		Pluck("id", &collectionIDs).Error; err == nil && len(collectionIDs) > 0 {
		// 分块查询仍被引用的合集，避免 IN 参数超限
		usedSet := make(map[string]bool)
		for _, chunk := range chunkStrings(collectionIDs) {
			var used []string
			if err := db.Model(&model.Media{}).Where("collection_id IN ?", chunk).
				Distinct().Pluck("collection_id", &used).Error; err == nil {
				for _, id := range used {
					usedSet[id] = true
				}
			}
		}
		var emptyCollections []string
		for _, id := range collectionIDs {
			if !usedSet[id] {
				emptyCollections = append(emptyCollections, id)
			}
		}
		for _, chunk := range chunkStrings(emptyCollections) {
			if r := db.Unscoped().Where("id IN ?", chunk).Delete(&model.MovieCollection{}); r.Error == nil {
				result.RemovedCollections += r.RowsAffected
			}
		}
	}
	if result.RemovedCollections > 0 {
		s.logger.Infof("残留清理: 移除空电影合集 %d 个", result.RemovedCollections)
	}

	// ========== 5. 孤儿缓存目录 ==========
	cacheDir := s.cfg.Cache.CacheDir
	if cacheDir != "" {
		// 行清理后重建有效集合，保证被清理的媒体/剧集目录一并移除
		freshMedia := make(map[string]bool)
		var mediaIDs []string
		if err := db.Model(&model.Media{}).Pluck("id", &mediaIDs).Error; err == nil {
			for _, id := range mediaIDs {
				freshMedia[id] = true
			}
		}
		freshSeries := make(map[string]bool)
		var seriesIDs []string
		if err := db.Model(&model.Series{}).Pluck("id", &seriesIDs).Error; err == nil {
			for _, id := range seriesIDs {
				freshSeries[id] = true
			}
		}

		removeIfOrphan := func(dir string, valid map[string]bool) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				return
			}
			for _, entry := range entries {
				name := entry.Name()
				if !entry.IsDir() || !looksLikeUUID(name) || valid[name] {
					continue
				}
				if err := os.RemoveAll(filepath.Join(dir, name)); err == nil {
					result.RemovedCacheDirs++
				} else {
					s.logger.Warnf("残留清理: 删除缓存目录失败: %s, 错误: %v", name, err)
				}
			}
		}
		removeIfOrphan(filepath.Join(cacheDir, "images"), freshMedia)
		removeIfOrphan(filepath.Join(cacheDir, "images", "series"), freshSeries)
		removeIfOrphan(filepath.Join(cacheDir, "preprocess"), freshMedia)
	}
	if result.RemovedCacheDirs > 0 {
		s.logger.Infof("残留清理: 移除孤儿缓存目录 %d 个", result.RemovedCacheDirs)
	}

	total := result.RemovedMedia + int(result.RemovedRelatedRows) + result.RemovedSeries +
		int(result.RemovedCollections) + result.RemovedCacheDirs
	if total > 0 {
		s.logger.Infof("残留数据清理完成: 媒体 %d, 关联数据 %d, 空剧集 %d, 空合集 %d, 缓存目录 %d",
			result.RemovedMedia, result.RemovedRelatedRows, result.RemovedSeries,
			result.RemovedCollections, result.RemovedCacheDirs)
	} else if !result.HasError {
		s.logger.Debugf("残留数据清理完成: 无残留")
	}
	return result
}
