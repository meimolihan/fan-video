package service

import (
	"fmt"
	"time"

	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"go.uber.org/zap"
)

// StatsService 播放统计和观影报告服务
type StatsService struct {
	statsRepo *repository.PlaybackStatsRepo
	mediaRepo *repository.MediaRepo
	logger    *zap.SugaredLogger
}

func NewStatsService(statsRepo *repository.PlaybackStatsRepo, mediaRepo *repository.MediaRepo, logger *zap.SugaredLogger) *StatsService {
	return &StatsService{
		statsRepo: statsRepo,
		mediaRepo: mediaRepo,
		logger:    logger,
	}
}

// RecordPlayback 记录一次播放统计
func (s *StatsService) RecordPlayback(userID, mediaID string, watchMinutes float64) error {
	stat := &model.PlaybackStats{
		UserID:       userID,
		MediaID:      mediaID,
		WatchMinutes: watchMinutes,
		Date:         time.Now().Format("2006-01-02"),
	}
	return s.statsRepo.Record(stat)
}

// UserStatsOverview 用户统计概览
type UserStatsOverview struct {
	TotalMinutes float64                  `json:"total_minutes"`
	TotalHours   float64                  `json:"total_hours"`
	DailyStats   []map[string]interface{} `json:"daily_stats"`
	TopGenres    []map[string]interface{} `json:"top_genres"`
	MostWatched  []map[string]interface{} `json:"most_watched"`
}

// GetUserOverview 获取用户统计概览（最近30天）
func (s *StatsService) GetUserOverview(userID string) (*UserStatsOverview, error) {
	endDate := time.Now().Format("2006-01-02")
	startDate := time.Now().AddDate(0, 0, -30).Format("2006-01-02")

	totalMinutes, err := s.statsRepo.GetUserTotalMinutes(userID)
	if err != nil {
		return nil, fmt.Errorf("获取总时长失败: %w", err)
	}

	dailyStats, _ := s.statsRepo.GetUserDailyStats(userID, startDate, endDate)
	topGenres, _ := s.statsRepo.GetUserTopGenres(userID, 5)
	mostWatched, _ := s.statsRepo.GetMostWatchedMedia(userID, 10)

	return &UserStatsOverview{
		TotalMinutes: totalMinutes,
		TotalHours:   totalMinutes / 60,
		DailyStats:   dailyStats,
		TopGenres:    topGenres,
		MostWatched:  mostWatched,
	}, nil
}
