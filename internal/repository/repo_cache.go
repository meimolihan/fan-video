package repository

import (
	"time"

	"github.com/fan-video/fan-video/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ==================== AICacheRepo ====================

// AICacheRepo AI 缓存持久化仓储
type AICacheRepo struct {
	db *gorm.DB
}

// Get 获取缓存条目（自动过滤过期）
func (r *AICacheRepo) Get(key string) (string, bool) {
	var entry model.AICacheEntry
	err := r.db.Where("cache_key = ? AND expires_at > ?", key, time.Now()).First(&entry).Error
	if err != nil {
		return "", false
	}
	return entry.Value, true
}

// Set 写入缓存条目（upsert）
func (r *AICacheRepo) Set(key, value string, ttlHours int) error {
	entry := model.AICacheEntry{
		CacheKey:  key,
		Value:     value,
		ExpiresAt: time.Now().Add(time.Duration(ttlHours) * time.Hour),
		CreatedAt: time.Now(),
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "cache_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "expires_at"}),
	}).Create(&entry).Error
}

// Delete 删除缓存条目
func (r *AICacheRepo) Delete(key string) error {
	return r.db.Where("cache_key = ?", key).Delete(&model.AICacheEntry{}).Error
}

// CleanExpired 清理过期缓存
func (r *AICacheRepo) CleanExpired() (int64, error) {
	result := r.db.Where("expires_at < ?", time.Now()).Delete(&model.AICacheEntry{})
	return result.RowsAffected, result.Error
}

// Count 获取缓存条目总数
func (r *AICacheRepo) Count() (int64, error) {
	var count int64
	err := r.db.Model(&model.AICacheEntry{}).Count(&count).Error
	return count, err
}

// CountActive 获取有效缓存条目数
func (r *AICacheRepo) CountActive() (int64, error) {
	var count int64
	err := r.db.Model(&model.AICacheEntry{}).Where("expires_at > ?", time.Now()).Count(&count).Error
	return count, err
}

// ClearAll 清空所有缓存
func (r *AICacheRepo) ClearAll() (int64, error) {
	result := r.db.Where("1 = 1").Delete(&model.AICacheEntry{})
	return result.RowsAffected, result.Error
}

// ==================== RecommendCacheRepo ====================

// RecommendCacheRepo 推荐结果缓存仓储
type RecommendCacheRepo struct {
	db *gorm.DB
}

// Get 获取用户的推荐缓存
func (r *RecommendCacheRepo) Get(userID string) (string, bool) {
	var cache model.RecommendCache
	err := r.db.Where("user_id = ? AND expires_at > ?", userID, time.Now()).First(&cache).Error
	if err != nil {
		return "", false
	}
	return cache.Results, true
}

// Set 写入推荐缓存
func (r *RecommendCacheRepo) Set(userID, results string, ttlMinutes int) error {
	cache := model.RecommendCache{
		UserID:    userID,
		Results:   results,
		ExpiresAt: time.Now().Add(time.Duration(ttlMinutes) * time.Minute),
		UpdatedAt: time.Now(),
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"results", "expires_at", "updated_at"}),
	}).Create(&cache).Error
}

// Invalidate 使用户的推荐缓存失效
func (r *RecommendCacheRepo) Invalidate(userID string) error {
	return r.db.Where("user_id = ?", userID).Delete(&model.RecommendCache{}).Error
}

// InvalidateAll 使所有用户的推荐缓存失效。
// 媒体库增删会改变全局候选集，因此不能只清理发起操作的用户。
func (r *RecommendCacheRepo) InvalidateAll() (int64, error) {
	result := r.db.Where("1 = 1").Delete(&model.RecommendCache{})
	return result.RowsAffected, result.Error
}

// CleanExpired 清理过期缓存
func (r *RecommendCacheRepo) CleanExpired() (int64, error) {
	result := r.db.Where("expires_at < ?", time.Now()).Delete(&model.RecommendCache{})
	return result.RowsAffected, result.Error
}
