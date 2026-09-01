package model

import "gorm.io/gorm"

// AutoMigrateLite migrates only tables used by the NAS-oriented lite profile.
// Optional modules must opt in at startup before their persistence tables are
// created. Existing full-profile tables are left untouched, so deployments can
// switch between Lite and Full without destructive migrations.
func AutoMigrateLite(db *gorm.DB) error {
	// 先清理历史重复行，保证后续给 media 建的 (library_id, file_path) 唯一索引成功
	if err := DedupeMediaByPath(db); err != nil {
		return err
	}

	models := []any{
		&User{},
		&LoginLog{},
		&AuditLog{},
		&InviteCode{},
		&Library{},
		&SystemSetting{},
		&Series{},
		&Media{},
		&Person{},
		&MediaPerson{},
		&WatchHistory{},
		&Favorite{},
		&WatchLater{},
		&Playlist{},
		&PlaylistItem{},
		&Bookmark{},
		&Comment{},
		&ContentRating{},
		&UserPermission{},
		&PlaybackStats{},
		&RecommendCache{},
		&MovieCollection{},
		&SystemLog{},
		&FileOperationLog{},

		// Local Media Analysis 是 Lite 核心能力（本地 ffmpeg 计算，无网络依赖）。
		&VideoChapter{},
		&VideoHighlight{},
		&AIAnalysisTask{},
		&CoverCandidate{},

		// 首页手动精选轮播
		&HomeFeatured{},
	}

	if err := db.AutoMigrate(models...); err != nil {
		return err
	}

	// The safety net only adds columns to core tables that already exist. It
	// never creates tables belonging to disabled optional modules.
	ensureSQLiteColumns(db)
	return nil
}
