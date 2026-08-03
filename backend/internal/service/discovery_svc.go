package service

import (
	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
)

// DiscoverySvc handles discovery strategy business logic.
type DiscoverySvc struct {
	repo *repository.DiscoveryRepo
}

func NewDiscoverySvc(repo *repository.DiscoveryRepo) *DiscoverySvc {
	return &DiscoverySvc{repo: repo}
}

func (s *DiscoverySvc) ListStrategies(page, size int) ([]model.DiscoveryStrategy, int64, error) {
	return s.repo.ListStrategies(page, size)
}

func (s *DiscoverySvc) GetStrategy(id uint64) (*model.DiscoveryStrategy, error) {
	return s.repo.GetStrategy(id)
}

func (s *DiscoverySvc) CreateStrategy(st *model.DiscoveryStrategy) error {
	return s.repo.CreateStrategy(st)
}

func (s *DiscoverySvc) UpdateStrategy(st *model.DiscoveryStrategy) error {
	return s.repo.UpdateStrategy(st)
}

func (s *DiscoverySvc) DeleteStrategy(id uint64) error {
	return s.repo.DeleteStrategy(id)
}

func (s *DiscoverySvc) ListHistory(strategyID uint64, page, size int) ([]model.DiscoveryHistory, int64, error) {
	return s.repo.ListHistory(strategyID, page, size)
}