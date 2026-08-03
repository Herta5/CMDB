package repository

import (
	"github-cmdb/internal/model"
	"gorm.io/gorm"
)

type RelationRepo struct {
	db *gorm.DB
}

func NewRelationRepo(db *gorm.DB) *RelationRepo {
	return &RelationRepo{db: db}
}

// --- Rules ---

func (r *RelationRepo) ListRules() ([]model.CIRelationRule, error) {
	var rules []model.CIRelationRule
	err := r.db.Preload("SourceType").Preload("TargetType").Find(&rules).Error
	return rules, err
}

func (r *RelationRepo) GetRuleByID(id uint64) (*model.CIRelationRule, error) {
	var rule model.CIRelationRule
	err := r.db.First(&rule, id).Error
	if err != nil { return nil, err }
	return &rule, nil
}

func (r *RelationRepo) CreateRule(rule *model.CIRelationRule) error { return r.db.Create(rule).Error }
func (r *RelationRepo) UpdateRule(rule *model.CIRelationRule) error { return r.db.Save(rule).Error }
func (r *RelationRepo) DeleteRule(id uint64) error                  { return r.db.Delete(&model.CIRelationRule{}, id).Error }

// --- Instances ---

func (r *RelationRepo) ListInstances(sourceCIID, targetCIID, ruleID uint64) ([]model.CIRelationInstance, error) {
	var instances []model.CIRelationInstance
	query := r.db.Preload("Rule").Preload("SourceCI").Preload("TargetCI")
	if sourceCIID > 0 { query = query.Where("source_ci_id = ?", sourceCIID) }
	if targetCIID > 0 { query = query.Where("target_ci_id = ?", targetCIID) }
	if ruleID > 0     { query = query.Where("rule_id = ?", ruleID) }
	err := query.Find(&instances).Error
	return instances, err
}

func (r *RelationRepo) GetInstancesByCI(ciID uint64) ([]model.CIRelationInstance, error) {
	var instances []model.CIRelationInstance
	err := r.db.Preload("Rule").Preload("SourceCI.CIType").Preload("TargetCI.CIType").
		Where("source_ci_id = ? OR target_ci_id = ?", ciID, ciID).Find(&instances).Error
	return instances, err
}

func (r *RelationRepo) CreateInstance(inst *model.CIRelationInstance) error { return r.db.Create(inst).Error }
func (r *RelationRepo) DeleteInstance(id uint64) error                       { return r.db.Delete(&model.CIRelationInstance{}, id).Error }

// --- Topology ---

type ImpactNode struct {
	CIID     uint64 `json:"ci_id"`
	CIName   string `json:"ci_name"`
	CITypeID uint64 `json:"ci_type_id"`
	TypeName string `json:"type_name"`
	Depth    int    `json:"depth"`
}

func (r *RelationRepo) ImpactAnalysis(ciID uint64, maxDepth int) ([]ImpactNode, error) {
	if maxDepth <= 0 { maxDepth = 5 }
	var nodes []ImpactNode
	sql := `WITH RECURSIVE impact_chain AS (
		SELECT ri.source_ci_id AS ci_id, 1 AS depth
		FROM ci_relation_instance ri
		JOIN ci_relation_rule rr ON ri.rule_id = rr.id
		WHERE ri.target_ci_id = ? AND rr.is_hard_dependency = 1
		UNION ALL
		SELECT ri.source_ci_id, ic.depth + 1
		FROM ci_relation_instance ri
		JOIN ci_relation_rule rr ON ri.rule_id = rr.id
		JOIN impact_chain ic ON ri.target_ci_id = ic.ci_id
		WHERE rr.is_hard_dependency = 1 AND ic.depth < ?
	)
	SELECT DISTINCT ci.id AS ci_id, ci.name AS ci_name, ci.ci_type_id,
		ct.display_name AS type_name, ic.depth
	FROM impact_chain ic
	JOIN ci_instance ci ON ci.id = ic.ci_id
	JOIN ci_type ct ON ct.id = ci.ci_type_id
	ORDER BY ic.depth, ci.name`
	err := r.db.Raw(sql, ciID, maxDepth).Scan(&nodes).Error
	return nodes, err
}

func (r *RelationRepo) Topology(ciID uint64, maxDepth int) ([]model.CIRelationInstance, error) {
	var instances []model.CIRelationInstance
	err := r.db.Preload("Rule").Preload("SourceCI.CIType").Preload("TargetCI.CIType").
		Where("source_ci_id = ? OR target_ci_id = ?", ciID, ciID).Find(&instances).Error
	_ = maxDepth
	return instances, err
}