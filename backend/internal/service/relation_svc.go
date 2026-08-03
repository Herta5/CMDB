package service

import (
	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
)

type RelationSvc struct {
	repo *repository.RelationRepo
}

func NewRelationSvc(repo *repository.RelationRepo) *RelationSvc {
	return &RelationSvc{repo: repo}
}

func (s *RelationSvc) ListRules() ([]model.CIRelationRule, error)           { return s.repo.ListRules() }
func (s *RelationSvc) CreateRule(rule *model.CIRelationRule) error           { return s.repo.CreateRule(rule) }
func (s *RelationSvc) UpdateRule(rule *model.CIRelationRule) error           { return s.repo.UpdateRule(rule) }
func (s *RelationSvc) DeleteRule(id uint64) error                             { return s.repo.DeleteRule(id) }

type RelationInstanceFilter struct {
	SourceCIID uint64 `form:"source_ci_id"`
	TargetCIID uint64 `form:"target_ci_id"`
	RuleID     uint64 `form:"rule_id"`
}

func (s *RelationSvc) ListInstances(filter RelationInstanceFilter) ([]model.CIRelationInstance, error) {
	return s.repo.ListInstances(filter.SourceCIID, filter.TargetCIID, filter.RuleID)
}

func (s *RelationSvc) ListInstancesByCI(ciID uint64) ([]model.CIRelationInstance, error) {
	return s.repo.GetInstancesByCI(ciID)
}
func (s *RelationSvc) CreateInstance(inst *model.CIRelationInstance) error { return s.repo.CreateInstance(inst) }
func (s *RelationSvc) DeleteInstance(id uint64) error                       { return s.repo.DeleteInstance(id) }

func (s *RelationSvc) Topology(ciID uint64, depth int) ([]model.CIRelationInstance, error) {
	if depth <= 0 { depth = 3 }
	return s.repo.Topology(ciID, depth)
}

func (s *RelationSvc) ImpactAnalysis(ciID uint64, depth int) ([]repository.ImpactNode, error) {
	if depth <= 0 { depth = 5 }
	return s.repo.ImpactAnalysis(ciID, depth)
}