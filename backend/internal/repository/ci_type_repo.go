package repository

import (
	"github-cmdb/internal/model"
	"gorm.io/gorm"
)

type CITypeRepo struct {
	db *gorm.DB
}

func NewCITypeRepo(db *gorm.DB) *CITypeRepo {
	return &CITypeRepo{db: db}
}

func (r *CITypeRepo) List() ([]model.CIType, error) {
	var types []model.CIType
	err := r.db.Order("sort_order ASC, id ASC").Find(&types).Error
	return types, err
}

func (r *CITypeRepo) GetByID(id uint64) (*model.CIType, error) {
	var t model.CIType
	err := r.db.First(&t, id).Error
	if err != nil { return nil, err }
	return &t, nil
}

// GetByName finds a CI type by its unique name.
func (r *CITypeRepo) GetByName(name string) (*model.CIType, error) {
	var t model.CIType
	err := r.db.Where("name = ?", name).First(&t).Error
	if err != nil { return nil, err }
	return &t, nil
}

func (r *CITypeRepo) GetByIDWithAttributes(id uint64) (*model.CIType, error) {
	var t model.CIType
	err := r.db.Preload("Attributes", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order ASC, id ASC")
	}).First(&t, id).Error
	if err != nil { return nil, err }
	return &t, nil
}

func (r *CITypeRepo) Create(t *model.CIType) error { return r.db.Create(t).Error }
func (r *CITypeRepo) Update(t *model.CIType) error { return r.db.Save(t).Error }
func (r *CITypeRepo) Delete(id uint64) error       { return r.db.Delete(&model.CIType{}, id).Error }

func (r *CITypeRepo) HasInstances(id uint64) (bool, error) {
	var count int64
	err := r.db.Model(&model.CIInstance{}).Where("ci_type_id = ?", id).Count(&count).Error
	return count > 0, err
}

func (r *CITypeRepo) ListAttributes(typeID uint64) ([]model.CIAttribute, error) {
	var attrs []model.CIAttribute
	err := r.db.Where("ci_type_id = ?", typeID).Order("sort_order ASC, id ASC").Find(&attrs).Error
	return attrs, err
}

func (r *CITypeRepo) CreateAttribute(attr *model.CIAttribute) error { return r.db.Create(attr).Error }
func (r *CITypeRepo) UpdateAttribute(attr *model.CIAttribute) error { return r.db.Save(attr).Error }
func (r *CITypeRepo) DeleteAttribute(id uint64) error               { return r.db.Delete(&model.CIAttribute{}, id).Error }