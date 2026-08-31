package handler

import (
	"net/http"
	"time"

	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"github.com/fan-video/fan-video/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// homeFeaturedMinItems 手动精选生效的最小条目数：
// 少于该数量时不接管首页轮播，继续使用默认推荐/最近添加逻辑。
const homeFeaturedMinItems = 2

// HomeFeaturedHandler 首页手动精选轮播。
// 管理端维护精选条目（电影 / 单集视频 / 剧集），首页接口在条目数达标时
// 按添加顺序输出拼装结果，优先级高于推荐算法。单集视频以 movie
// 类型条目存储，播放时直接定位到该文件本身。
type HomeFeaturedHandler struct {
	featuredRepo *repository.HomeFeaturedRepo
	mediaRepo    *repository.MediaRepo
	seriesRepo   *repository.SeriesRepo
	libRepo      *repository.LibraryRepo
	logger       *zap.SugaredLogger
}

func NewHomeFeaturedHandler(
	featuredRepo *repository.HomeFeaturedRepo,
	mediaRepo *repository.MediaRepo,
	seriesRepo *repository.SeriesRepo,
	libRepo *repository.LibraryRepo,
	logger *zap.SugaredLogger,
) *HomeFeaturedHandler {
	return &HomeFeaturedHandler{
		featuredRepo: featuredRepo,
		mediaRepo:    mediaRepo,
		seriesRepo:   seriesRepo,
		libRepo:      libRepo,
		logger:       logger,
	}
}

// HomeFeaturedEntry 管理端列表条目（带标题/年份便于展示）。
type HomeFeaturedEntry struct {
	ID        string    `json:"id"`
	ItemType  string    `json:"item_type"`
	ItemID    string    `json:"item_id"`
	Kind      string    `json:"kind,omitempty"` // movie | episode | series，便于前端展示精确类型
	Title     string    `json:"title"`
	Year      int       `json:"year,omitempty"`
	Valid     bool      `json:"valid"` // 引用的媒体/剧集是否仍存在
	CreatedAt time.Time `json:"created_at"`
}

func (h *HomeFeaturedHandler) toEntry(row model.HomeFeatured) HomeFeaturedEntry {
	entry := HomeFeaturedEntry{
		ID:        row.ID,
		ItemType:  row.ItemType,
		ItemID:    row.ItemID,
		CreatedAt: row.CreatedAt,
	}
	switch row.ItemType {
	case "movie":
		if media, err := h.mediaRepo.FindByID(row.ItemID); err == nil && media != nil {
			entry.Title = media.Title
			entry.Year = media.Year
			entry.Kind = media.MediaType // "movie"（电影）或 "episode"（单集视频）
			entry.Valid = true
		}
	case "series":
		if series, err := h.seriesRepo.FindByIDOnly(row.ItemID); err == nil && series != nil {
			entry.Title = series.Title
			entry.Year = series.Year
			entry.Kind = "series"
			entry.Valid = true
		}
	}
	if entry.Title == "" {
		entry.Title = "（引用已失效）"
	}
	return entry
}

// AdminList 管理端：列出全部精选条目（无论是否达到生效阈值）。
func (h *HomeFeaturedHandler) AdminList(c *gin.Context) {
	rows, err := h.featuredRepo.List()
	if err != nil {
		h.logger.Errorf("读取首页精选轮播配置失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取精选轮播配置失败"})
		return
	}
	entries := make([]HomeFeaturedEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, h.toEntry(row))
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      entries,
		"min_items": homeFeaturedMinItems,
	})
}

// Add 管理端：新增一条精选（movie=电影或单集视频，series=剧集）。
func (h *HomeFeaturedHandler) Add(c *gin.Context) {
	var req struct {
		ItemType string `json:"item_type" binding:"required"`
		ItemID   string `json:"item_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不完整"})
		return
	}
	if req.ItemType != "movie" && req.ItemType != "series" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "item_type 仅支持 movie 或 series"})
		return
	}

	switch req.ItemType {
	case "movie":
		media, err := h.mediaRepo.FindByID(req.ItemID)
		if err != nil || media == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "视频不存在或已被删除"})
			return
		}
	case "series":
		series, err := h.seriesRepo.FindByIDOnly(req.ItemID)
		if err != nil || series == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "剧集不存在或已被删除"})
			return
		}
	}

	exists, err := h.featuredRepo.ExistsByItem(req.ItemType, req.ItemID)
	if err != nil {
		h.logger.Errorf("查询精选轮播重复项失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败"})
		return
	}
	if exists {
		c.JSON(http.StatusConflict, gin.H{"error": "该条目已在精选列表中"})
		return
	}

	row := &model.HomeFeatured{ItemType: req.ItemType, ItemID: req.ItemID}
	if err := h.featuredRepo.Create(row); err != nil {
		h.logger.Errorf("保存首页精选轮播配置失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败"})
		return
	}

	count, _ := h.countRows()
	c.JSON(http.StatusOK, gin.H{
		"data":      h.toEntry(*row),
		"min_items": homeFeaturedMinItems,
		"total":     count,
		"active":    count >= homeFeaturedMinItems,
	})
}

// Remove 管理端：删除一条精选。
func (h *HomeFeaturedHandler) Remove(c *gin.Context) {
	id := c.Param("id")
	removed, err := h.featuredRepo.Delete(id)
	if err != nil {
		h.logger.Errorf("删除首页精选轮播条目失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}
	if !removed {
		c.JSON(http.StatusNotFound, gin.H{"error": "条目不存在"})
		return
	}

	count, _ := h.countRows()
	c.JSON(http.StatusOK, gin.H{
		"data":      gin.H{"id": id},
		"min_items": homeFeaturedMinItems,
		"total":     count,
		"active":    count >= homeFeaturedMinItems,
	})
}

// hiddenLibraryIDSet 返回隐藏媒体库的 ID 集合（首页轮播排除用）。
func (h *HomeFeaturedHandler) hiddenLibraryIDSet() map[string]struct{} {
	if h.libRepo == nil {
		return nil
	}
	ids, err := h.libRepo.HiddenIDs()
	if err != nil || len(ids) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

// ListForHome 首页：条目数达标时返回拼装好的 MixedItem 列表，
// 否则返回空数组，前端据此回落默认推荐逻辑。失效引用自动跳过。
func (h *HomeFeaturedHandler) ListForHome(c *gin.Context) {
	items := []service.MixedItem{}

	rows, err := h.featuredRepo.List()
	if err != nil {
		h.logger.Errorf("读取首页精选轮播配置失败: %v", err)
		c.JSON(http.StatusOK, gin.H{"data": items})
		return
	}
	if len(rows) < homeFeaturedMinItems {
		c.JSON(http.StatusOK, gin.H{"data": items, "active": false, "min_items": homeFeaturedMinItems})
		return
	}

	hidden := h.hiddenLibraryIDSet()

	for _, row := range rows {
		switch row.ItemType {
		case "movie":
			media, err := h.mediaRepo.FindByID(row.ItemID)
			if err != nil || media == nil {
				continue
			}
			if _, skip := hidden[media.LibraryID]; skip {
				continue
			}
			items = append(items, service.MixedItem{Type: "movie", Media: media})
		case "series":
			series, err := h.seriesRepo.FindByIDOnly(row.ItemID)
			if err != nil || series == nil {
				continue
			}
			if _, skip := hidden[series.LibraryID]; skip {
				continue
			}
			items = append(items, service.MixedItem{Type: "series", Series: series})
		}
	}

	if len(items) < homeFeaturedMinItems {
		c.JSON(http.StatusOK, gin.H{"data": items, "active": false, "min_items": homeFeaturedMinItems})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": items, "active": true, "min_items": homeFeaturedMinItems})
}

func (h *HomeFeaturedHandler) countRows() (int64, error) {
	rows, err := h.featuredRepo.List()
	if err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}
