package service

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fan-video/fan-video/internal/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// UserProfileService 多用户配置文件系统
// 实现精细化的用户权限管理，包括儿童专属模式和家长控制功能
type UserProfileService struct {
	db     *gorm.DB
	logger *zap.SugaredLogger
	mu     sync.RWMutex
}

// UserProfile 用户配置文件
type UserProfile struct {
	ID        string    `json:"id" gorm:"primaryKey;type:text"`
	UserID    string    `json:"user_id" gorm:"index;type:text;not null"`
	Name      string    `json:"name" gorm:"type:text;not null"`  // 配置文件名称（如"爸爸"、"小明"）
	AvatarURL string    `json:"avatar_url" gorm:"type:text"`     // 头像
	IsKids    bool      `json:"is_kids" gorm:"default:false"`    // 是否为儿童模式
	PIN       string    `json:"-" gorm:"type:text"`              // 切换配置文件的PIN码
	IsDefault bool      `json:"is_default" gorm:"default:false"` // 是否为默认配置
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// 儿童模式设置
	KidsSettings *KidsProfileSettings `json:"kids_settings,omitempty" gorm:"serializer:json;type:text"`
	// 家长控制设置
	ParentalControl *ParentalControlSettings `json:"parental_control,omitempty" gorm:"serializer:json;type:text"`
}

// KidsProfileSettings 儿童模式设置
type KidsProfileSettings struct {
	MaxRating         string   `json:"max_rating"`           // 最高允许分级（G / PG）
	AllowedGenres     []string `json:"allowed_genres"`       // 允许的类型（动画、教育等）
	BlockedGenres     []string `json:"blocked_genres"`       // 屏蔽的类型（恐怖、暴力等）
	AllowedLibraries  []string `json:"allowed_libraries"`    // 允许访问的媒体库
	DailyTimeLimit    int      `json:"daily_time_limit"`     // 每日观看时长限制（分钟），0=不限
	BedtimeStart      string   `json:"bedtime_start"`        // 就寝时间开始（如 "21:00"）
	BedtimeEnd        string   `json:"bedtime_end"`          // 就寝时间结束（如 "07:00"）
	AutoPlayNext      bool     `json:"auto_play_next"`       // 是否自动播放下一集
	MaxEpisodesPerDay int      `json:"max_episodes_per_day"` // 每日最大集数，0=不限
}

// ParentalControlSettings 家长控制设置
type ParentalControlSettings struct {
	Enabled            bool   `json:"enabled"`               // 是否启用家长控制
	MasterPIN          string `json:"master_pin"`            // 主控PIN码（用于解锁设置）
	RequirePINForAdult bool   `json:"require_pin_for_adult"` // 观看成人内容需要PIN
	MonitorHistory     bool   `json:"monitor_history"`       // 监控观看历史
	AllowProfileSwitch bool   `json:"allow_profile_switch"`  // 允许自由切换配置文件
	NotifyOnViolation  bool   `json:"notify_on_violation"`   // 违规时通知家长
}

// ProfileWatchLog 配置文件观看日志（用于家长监控）
type ProfileWatchLog struct {
	ID        string    `json:"id" gorm:"primaryKey;type:text"`
	ProfileID string    `json:"profile_id" gorm:"index;type:text;not null"`
	MediaID   string    `json:"media_id" gorm:"index;type:text;not null"`
	Title     string    `json:"title" gorm:"type:text"`
	Duration  int       `json:"duration"`                    // 观看时长（秒）
	Rating    string    `json:"rating" gorm:"type:text"`     // 内容分级
	Blocked   bool      `json:"blocked"`                     // 是否被拦截
	Reason    string    `json:"reason" gorm:"type:text"`     // 拦截原因
	Date      string    `json:"date" gorm:"index;type:text"` // YYYY-MM-DD
	CreatedAt time.Time `json:"created_at"`
}

// ProfileDailyUsage 配置文件每日使用统计
type ProfileDailyUsage struct {
	ID           string `json:"id" gorm:"primaryKey;type:text"`
	ProfileID    string `json:"profile_id" gorm:"index;type:text;not null"`
	Date         string `json:"date" gorm:"index;type:text;not null"`
	WatchMinutes int    `json:"watch_minutes"`
	EpisodeCount int    `json:"episode_count"`
	MovieCount   int    `json:"movie_count"`
}

func NewUserProfileService(db *gorm.DB, logger *zap.SugaredLogger) *UserProfileService {
	// 自动迁移配置文件相关表
	db.AutoMigrate(&UserProfile{}, &ProfileWatchLog{}, &ProfileDailyUsage{})
	return &UserProfileService{db: db, logger: logger}
}

// CreateProfile 创建用户配置文件
func (s *UserProfileService) CreateProfile(userID string, profile *UserProfile) error {
	profile.UserID = userID
	if profile.ID == "" {
		profile.ID = fmt.Sprintf("prof_%d", time.Now().UnixNano())
	}

	// 如果是第一个配置文件，设为默认
	var count int64
	s.db.Model(&UserProfile{}).Where("user_id = ?", userID).Count(&count)
	if count == 0 {
		profile.IsDefault = true
	}

	// 儿童模式默认设置
	if profile.IsKids && profile.KidsSettings == nil {
		profile.KidsSettings = &KidsProfileSettings{
			MaxRating:         "PG",
			AllowedGenres:     []string{"动画", "家庭", "教育", "冒险", "喜剧"},
			BlockedGenres:     []string{"恐怖", "惊悚", "暴力", "犯罪"},
			DailyTimeLimit:    120, // 默认2小时
			BedtimeStart:      "21:00",
			BedtimeEnd:        "07:00",
			AutoPlayNext:      false,
			MaxEpisodesPerDay: 5,
		}
	}

	return s.db.Create(profile).Error
}

// ListProfiles 获取用户的所有配置文件
func (s *UserProfileService) ListProfiles(userID string) ([]UserProfile, error) {
	var profiles []UserProfile
	err := s.db.Where("user_id = ?", userID).Order("is_default DESC, created_at ASC").Find(&profiles).Error
	return profiles, err
}

// GetProfile 获取配置文件详情
func (s *UserProfileService) GetProfile(profileID string) (*UserProfile, error) {
	var profile UserProfile
	err := s.db.First(&profile, "id = ?", profileID).Error
	return &profile, err
}

// UpdateProfile 更新配置文件
func (s *UserProfileService) UpdateProfile(profileID string, updates map[string]interface{}) error {
	return s.db.Model(&UserProfile{}).Where("id = ?", profileID).Updates(updates).Error
}

// DeleteProfile 删除配置文件
func (s *UserProfileService) DeleteProfile(profileID string) error {
	var profile UserProfile
	if err := s.db.First(&profile, "id = ?", profileID).Error; err != nil {
		return err
	}
	if profile.IsDefault {
		return fmt.Errorf("不能删除默认配置文件")
	}
	// 同时删除关联的观看日志
	s.db.Where("profile_id = ?", profileID).Delete(&ProfileWatchLog{})
	s.db.Where("profile_id = ?", profileID).Delete(&ProfileDailyUsage{})
	return s.db.Delete(&profile).Error
}

// SwitchProfile 切换配置文件（验证PIN）
func (s *UserProfileService) SwitchProfile(profileID, pin string) (*UserProfile, error) {
	var profile UserProfile
	if err := s.db.First(&profile, "id = ?", profileID).Error; err != nil {
		return nil, fmt.Errorf("配置文件不存在")
	}

	// 如果配置文件设置了PIN，需要验证
	if profile.PIN != "" && profile.PIN != pin {
		return nil, fmt.Errorf("PIN码错误")
	}

	return &profile, nil
}

// CheckContentAccess 检查配置文件是否可以访问某内容
func (s *UserProfileService) CheckContentAccess(profileID string, media *model.Media) (bool, string) {
	var profile UserProfile
	if err := s.db.First(&profile, "id = ?", profileID).Error; err != nil {
		return true, "" // 配置文件不存在，默认允许
	}

	if !profile.IsKids || profile.KidsSettings == nil {
		return true, "" // 非儿童模式，允许
	}

	ks := profile.KidsSettings

	// 检查媒体库访问权限
	if len(ks.AllowedLibraries) > 0 {
		allowed := false
		for _, libID := range ks.AllowedLibraries {
			if libID == media.LibraryID {
				allowed = true
				break
			}
		}
		if !allowed {
			return false, "该媒体库不在允许列表中"
		}
	}

	// 检查类型过滤
	mediaGenres := strings.Split(media.Genres, ",")
	for _, blocked := range ks.BlockedGenres {
		for _, genre := range mediaGenres {
			if strings.TrimSpace(genre) == blocked {
				return false, fmt.Sprintf("内容类型 \"%s\" 已被屏蔽", blocked)
			}
		}
	}

	// 检查就寝时间
	if ks.BedtimeStart != "" && ks.BedtimeEnd != "" {
		now := time.Now()
		nowStr := now.Format("15:04")
		if isInBedtime(nowStr, ks.BedtimeStart, ks.BedtimeEnd) {
			return false, fmt.Sprintf("当前为就寝时间（%s - %s）", ks.BedtimeStart, ks.BedtimeEnd)
		}
	}

	// 检查每日时长限制
	if ks.DailyTimeLimit > 0 {
		todayMinutes := s.getTodayWatchMinutes(profileID)
		if todayMinutes >= ks.DailyTimeLimit {
			return false, fmt.Sprintf("今日观看时长已达上限（%d分钟）", ks.DailyTimeLimit)
		}
	}

	// 检查每日集数限制
	if ks.MaxEpisodesPerDay > 0 && media.MediaType == "episode" {
		todayEpisodes := s.getTodayEpisodeCount(profileID)
		if todayEpisodes >= ks.MaxEpisodesPerDay {
			return false, fmt.Sprintf("今日观看集数已达上限（%d集）", ks.MaxEpisodesPerDay)
		}
	}

	return true, ""
}

// RecordWatch 记录观看日志
func (s *UserProfileService) RecordWatch(profileID, mediaID, title string, durationSec int, rating string, blocked bool, reason string) {
	today := time.Now().Format("2006-01-02")

	log := &ProfileWatchLog{
		ID:        fmt.Sprintf("pwl_%d", time.Now().UnixNano()),
		ProfileID: profileID,
		MediaID:   mediaID,
		Title:     title,
		Duration:  durationSec,
		Rating:    rating,
		Blocked:   blocked,
		Reason:    reason,
		Date:      today,
	}
	s.db.Create(log)

	// 更新每日使用统计
	if !blocked {
		var usage ProfileDailyUsage
		result := s.db.Where("profile_id = ? AND date = ?", profileID, today).First(&usage)
		if result.Error != nil {
			usage = ProfileDailyUsage{
				ID:        fmt.Sprintf("pdu_%d", time.Now().UnixNano()),
				ProfileID: profileID,
				Date:      today,
			}
		}
		usage.WatchMinutes += durationSec / 60
		if durationSec > 0 {
			usage.EpisodeCount++
		}
		s.db.Save(&usage)
	}
}

// GetWatchLogs 获取观看日志（家长监控）
func (s *UserProfileService) GetWatchLogs(profileID string, days int) ([]ProfileWatchLog, error) {
	var logs []ProfileWatchLog
	since := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	err := s.db.Where("profile_id = ? AND date >= ?", profileID, since).
		Order("created_at DESC").Find(&logs).Error
	return logs, err
}

// GetDailyUsage 获取每日使用统计
func (s *UserProfileService) GetDailyUsage(profileID string, days int) ([]ProfileDailyUsage, error) {
	var usage []ProfileDailyUsage
	since := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	err := s.db.Where("profile_id = ? AND date >= ?", profileID, since).
		Order("date DESC").Find(&usage).Error
	return usage, err
}

// GetProfileStats 获取配置文件统计概览
func (s *UserProfileService) GetProfileStats(profileID string) map[string]interface{} {
	today := time.Now().Format("2006-01-02")

	var todayUsage ProfileDailyUsage
	s.db.Where("profile_id = ? AND date = ?", profileID, today).First(&todayUsage)

	var totalLogs int64
	s.db.Model(&ProfileWatchLog{}).Where("profile_id = ?", profileID).Count(&totalLogs)

	var blockedCount int64
	s.db.Model(&ProfileWatchLog{}).Where("profile_id = ? AND blocked = ?", profileID, true).Count(&blockedCount)

	var weekMinutes int64
	weekAgo := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	s.db.Model(&ProfileDailyUsage{}).
		Where("profile_id = ? AND date >= ?", profileID, weekAgo).
		Select("COALESCE(SUM(watch_minutes), 0)").Scan(&weekMinutes)

	return map[string]interface{}{
		"today_minutes":  todayUsage.WatchMinutes,
		"today_episodes": todayUsage.EpisodeCount,
		"week_minutes":   weekMinutes,
		"total_logs":     totalLogs,
		"blocked_count":  blockedCount,
	}
}

// ==================== 内部辅助方法 ====================

func (s *UserProfileService) getTodayWatchMinutes(profileID string) int {
	today := time.Now().Format("2006-01-02")
	var usage ProfileDailyUsage
	s.db.Where("profile_id = ? AND date = ?", profileID, today).First(&usage)
	return usage.WatchMinutes
}

func (s *UserProfileService) getTodayEpisodeCount(profileID string) int {
	today := time.Now().Format("2006-01-02")
	var usage ProfileDailyUsage
	s.db.Where("profile_id = ? AND date = ?", profileID, today).First(&usage)
	return usage.EpisodeCount
}

// isInBedtime 检查当前时间是否在就寝时间范围内
func isInBedtime(now, start, end string) bool {
	// 处理跨午夜的情况（如 21:00 - 07:00）
	if start > end {
		return now >= start || now < end
	}
	return now >= start && now < end
}
