package repository

import (
	"github-cmdb/internal/model"
	"gorm.io/gorm"
)

type ConfigSnapshotRepo struct {
	db *gorm.DB
}

func NewConfigSnapshotRepo(db *gorm.DB) *ConfigSnapshotRepo {
	return &ConfigSnapshotRepo{db: db}
}

func (r *ConfigSnapshotRepo) List(ciID uint64, page, pageSize int) ([]model.ConfigSnapshot, int64, error) {
	var snapshots []model.ConfigSnapshot
	var total int64
	query := r.db.Model(&model.ConfigSnapshot{}).Where("ci_id = ?", ciID)
	if err := query.Count(&total).Error; err != nil { return nil, 0, err }
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 100 { pageSize = 20 }
	err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&snapshots).Error
	return snapshots, total, err
}

func (r *ConfigSnapshotRepo) GetByID(id uint64) (*model.ConfigSnapshot, error) {
	var snap model.ConfigSnapshot
	err := r.db.First(&snap, id).Error
	if err != nil { return nil, err }
	return &snap, nil
}

func (r *ConfigSnapshotRepo) Create(snap *model.ConfigSnapshot) error {
	return r.db.Create(snap).Error
}
func (r *ConfigSnapshotRepo) GetLatestByCIID(ciID uint64) (*model.ConfigSnapshot, error) {
	var snap model.ConfigSnapshot
	err := r.db.Where("ci_id = ?", ciID).Order("created_at DESC").First(&snap).Error
	if err != nil { return nil, err }
	return &snap, nil
}