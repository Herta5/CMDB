package repository

import (
	"time"

	"github-cmdb/internal/model"
	"gorm.io/gorm"
)

// DiscoveryRepo handles discovery_strategy and discovery_history tables.
type DiscoveryRepo struct {
	db *gorm.DB
}

func NewDiscoveryRepo(db *gorm.DB) *DiscoveryRepo {
	return &DiscoveryRepo{db: db}
}

// --- Strategy CRUD ---

func (r *DiscoveryRepo) ListStrategies(page, size int) ([]model.DiscoveryStrategy, int64, error) {
	var list []model.DiscoveryStrategy
	var total int64
	query := r.db.Model(&model.DiscoveryStrategy{})
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	err := query.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
}

func (r *DiscoveryRepo) ListEnabled() ([]model.DiscoveryStrategy, error) {
	var list []model.DiscoveryStrategy
	err := r.db.Model(&model.DiscoveryStrategy{}).Where("enabled = ?", true).Find(&list).Error
	return list, err
}

func (r *DiscoveryRepo) GetStrategy(id uint64) (*model.DiscoveryStrategy, error) {
	var s model.DiscoveryStrategy
	err := r.db.First(&s, id).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *DiscoveryRepo) CreateStrategy(s *model.DiscoveryStrategy) error {
	return r.db.Create(s).Error
}

func (r *DiscoveryRepo) UpdateStrategy(s *model.DiscoveryStrategy) error {
	return r.db.Save(s).Error
}

func (r *DiscoveryRepo) DeleteStrategy(id uint64) error {
	return r.db.Delete(&model.DiscoveryStrategy{}, id).Error
}

func (r *DiscoveryRepo) UpdateRunStatus(s *model.DiscoveryStrategy) {
	now := time.Now()
	r.db.Model(s).Updates(map[string]interface{}{
		"last_run_at":     &now,
		"last_run_status": s.LastRunStatus,
	})
}

// --- History ---

func (r *DiscoveryRepo) ListHistory(strategyID uint64, page, size int) ([]model.DiscoveryHistory, int64, error) {
	var list []model.DiscoveryHistory
	var total int64
	query := r.db.Model(&model.DiscoveryHistory{}).Where("strategy_id = ?", strategyID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	err := query.Order("started_at DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
}

func (r *DiscoveryRepo) CreateHistory(h *model.DiscoveryHistory) error {
	return r.db.Create(h).Error
}