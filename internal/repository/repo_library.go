package repository

import (
	"github.com/fan-video/fan-video/internal/model"
	"gorm.io/gorm"
)

// ==================== LibraryRepo ====================

type LibraryRepo struct {
	db *gorm.DB
}

func (r *LibraryRepo) Create(lib *model.Library) error {
	return r.db.Create(lib).Error
}

func (r *LibraryRepo) FindByID(id string) (*model.Library, error) {
	var lib model.Library
	err := r.db.First(&lib, "id = ?", id).Error
	return &lib, err
}

func (r *LibraryRepo) List() ([]model.Library, error) {
	var libs []model.Library
	err := r.db.Find(&libs).Error
	return libs, err
}

func (r *LibraryRepo) Update(lib *model.Library) error {
	return r.db.Save(lib).Error
}

func (r *LibraryRepo) Delete(id string) error {
	return r.db.Delete(&model.Library{}, "id = ?", id).Error
}
