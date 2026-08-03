package service

import (
	"fmt"
	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
)

type CIInstanceSvc struct {
	repo     *repository.CIInstanceRepo
	typeRepo *repository.CITypeRepo
	snapRepo *repository.ConfigSnapshotRepo
}

func NewCIInstanceSvc(repo *repository.CIInstanceRepo, typeRepo *repository.CITypeRepo, snapRepo *repository.ConfigSnapshotRepo) *CIInstanceSvc {
	return &CIInstanceSvc{repo: repo, typeRepo: typeRepo, snapRepo: snapRepo}
}

func (s *CIInstanceSvc) List(filter repository.CIInstanceFilter) ([]model.CIInstance, int64, error) {
	return s.repo.List(filter)
}

func (s *CIInstanceSvc) Get(id uint64) (*model.CIInstance, error) { return s.repo.GetByID(id) }

func (s *CIInstanceSvc) Create(ci *model.CIInstance) error {
	ciType, err := s.typeRepo.GetByID(ci.CITypeID)
	if err != nil { return fmt.Errorf("ci type not found: %w", err) }
	if ciType.IsAbstract { return fmt.Errorf("cannot create instance of abstract type '%s'", ciType.Name) }
	if ci.Attributes == nil { ci.Attributes = model.JSONMap{} }
	ci.SyncRedundantFields()
	code, err := s.repo.GenerateCICode(ciType.Name)
	if err != nil { return err }
	ci.CICode = code
	ci.Source = "manual"
	return s.repo.Create(ci)
}

func (s *CIInstanceSvc) Update(ci *model.CIInstance) error {
	existing, err := s.repo.GetByID(ci.ID)
	if err != nil { return err }
	ci.SyncRedundantFields()
	if err := s.repo.Update(ci); err != nil { return err }
	// write snapshot asynchronously
	go func() {
		snap := &model.ConfigSnapshot{
			CIID: ci.ID, SnapshotData: existing.Attributes, ChangeType: "update", Source: strPtr("manual"),
		}
		_ = s.snapRepo.Create(snap)
	}()
	return nil
}

func (s *CIInstanceSvc) Delete(id uint64) error { return s.repo.Delete(id) }

func (s *CIInstanceSvc) GetDistributionByType() ([]repository.TypeDistribution, error) {
	return s.repo.DistributionByType()
}
func (s *CIInstanceSvc) GetDistributionByStatus() (map[string]int64, error) {
	return s.repo.DistributionByStatus()
}
func (s *CIInstanceSvc) TotalCount() (int64, error) { return s.repo.TotalCount() }
func (s *CIInstanceSvc) TrendByMonth(months int) ([]map[string]interface{}, error) {
	return s.repo.TrendByMonth(months)
}

func strPtr(s string) *string { return &s }