package repository

import (
	"github.com/fan-video/fan-video/internal/model"
	"gorm.io/gorm"
)

// HomeFeaturedRepo 首页手动精选轮播配置仓储。
type HomeFeaturedRepo struct {
	db *gorm.DB
}

func NewHomeFeaturedRepo(db *gorm.DB) *HomeFeaturedRepo {
	return &HomeFeaturedRepo{db: db}
}

// List 按添加时间升序返回全部精选条目。
func (r *HomeFeaturedRepo) List() ([]model.HomeFeatured, error) {
	var items []model.HomeFeatured
	if err := r.db.Order("created_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// FindByID 按主键查找单条配置。
func (r *HomeFeaturedRepo) FindByID(id string) (*model.HomeFeatured, error) {
	var item model.HomeFeatured
	if err := r.db.First(&item, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// ExistsByItem 判断某媒体/剧集是否已被收录（(item_type,item_id) 唯一）。
func (r *HomeFeaturedRepo) ExistsByItem(itemType, itemID string) (bool, error) {
	var count int64
	if err := r.db.Model(&model.HomeFeatured{}).
		Where("item_type = ? AND item_id = ?", itemType, itemID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *HomeFeaturedRepo) Create(item *model.HomeFeatured) error {
	return r.db.Create(item).Error
}

// Delete 按主键删除，返回是否确实删除了记录。
func (r *HomeFeaturedRepo) Delete(id string) (bool, error) {
	result := r.db.Delete(&model.HomeFeatured{}, "id = ?", id)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
