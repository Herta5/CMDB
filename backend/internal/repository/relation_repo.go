package repository

import (
	"fmt"
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


// --- Multi-Level Topology ---

type TopologyNode struct {
	CIID      uint64 `json:"ci_id"`
	CIName    string `json:"ci_name"`
	CITypeID  uint64 `json:"ci_type_id"`
	TypeName  string `json:"type_name"`
	Status    string `json:"status"`
	IPAddress string `json:"ip_address"`
	Depth     int    `json:"depth"`
}

type TopologyEdge struct {
	SourceCIID uint64 `json:"source_ci_id"`
	TargetCIID uint64 `json:"target_ci_id"`
	RuleName   string `json:"rule_name"`
	Label      string `json:"label"`
}

type TopologyGraph struct {
	Nodes []TopologyNode `json:"nodes"`
	Edges []TopologyEdge `json:"edges"`
}

func (r *RelationRepo) MultiLevelTopology(ciID uint64, maxDepth int) (*TopologyGraph, error) {
	if maxDepth <= 0 { maxDepth = 3 }

	type rawRow struct {
		CIID      uint64
		RelatedID uint64
		Depth     int
		RuleName  string
		Label     string
	}

	sql := `WITH RECURSIVE topo_chain AS (
		SELECT ri.source_ci_id AS ci_id, ri.target_ci_id AS related_id, 1 AS depth,
			ri.rule_id, rr.name AS rule_name, rr.display_name AS label
		FROM ci_relation_instance ri
		JOIN ci_relation_rule rr ON ri.rule_id = rr.id
		WHERE ri.source_ci_id = ? OR ri.target_ci_id = ?
		UNION ALL
		SELECT ri.source_ci_id, ri.target_ci_id, tc.depth + 1,
			ri.rule_id, rr.name, rr.display_name
		FROM ci_relation_instance ri
		JOIN ci_relation_rule rr ON ri.rule_id = rr.id
		JOIN topo_chain tc ON (ri.source_ci_id = tc.ci_id OR ri.target_ci_id = tc.ci_id
			OR ri.source_ci_id = tc.related_id OR ri.target_ci_id = tc.related_id)
		WHERE tc.depth < ?
	)
	SELECT DISTINCT t.ci_id, t.related_id, t.depth, t.rule_name, t.label
	FROM topo_chain t ORDER BY t.depth`

	var rows []rawRow
	err := r.db.Raw(sql, ciID, ciID, maxDepth).Scan(&rows).Error
	if err != nil { return nil, err }

	nodeSet := make(map[uint64]bool)
	nodeSet[ciID] = true
	edgeSet := make(map[string]bool)
	var edges []TopologyEdge

	for _, row := range rows {
		nodeSet[row.CIID] = true
		nodeSet[row.RelatedID] = true
		key := fmt.Sprintf("%d-%d-%s", row.CIID, row.RelatedID, row.RuleName)
		if !edgeSet[key] {
			edgeSet[key] = true
			edges = append(edges, TopologyEdge{
				SourceCIID: row.CIID, TargetCIID: row.RelatedID,
				RuleName: row.RuleName, Label: row.Label,
			})
		}
	}

	var ciIDs []uint64
	for id := range nodeSet { ciIDs = append(ciIDs, id) }

	var cis []struct {
		ID       uint64
		Name     string
		CITypeID uint64
		TypeName string
		Status   string
		IPAddr   *string
	}
	r.db.Table("ci_instance").
		Select("ci_instance.id, ci_instance.name, ci_instance.ci_type_id, ci_type.display_name AS type_name, ci_instance.status, ci_instance.ip_address AS ip_addr").
		Joins("JOIN ci_type ON ci_type.id = ci_instance.ci_type_id").
		Where("ci_instance.id IN ?", ciIDs).Scan(&cis)

	ciMap := make(map[uint64]struct{ Name, TypeName, Status string; CITypeID uint64; IPAddr *string })
	for _, c := range cis {
		ciMap[c.ID] = struct{ Name, TypeName, Status string; CITypeID uint64; IPAddr *string }{
			c.Name, c.TypeName, c.Status, c.CITypeID, c.IPAddr,
		}
	}

	depthMap := make(map[uint64]int)
	depthMap[ciID] = 0
	for _, row := range rows {
		if _, ok := depthMap[row.CIID]; !ok { depthMap[row.CIID] = row.Depth }
		if _, ok := depthMap[row.RelatedID]; !ok { depthMap[row.RelatedID] = row.Depth }
	}

	var nodes []TopologyNode
	for id := range nodeSet {
		info := ciMap[id]
		ip := ""
		if info.IPAddr != nil { ip = *info.IPAddr }
		nodes = append(nodes, TopologyNode{
			CIID: id, CIName: info.Name, CITypeID: info.CITypeID,
			TypeName: info.TypeName, Status: info.Status, IPAddress: ip,
			Depth: depthMap[id],
		})
	}

	return &TopologyGraph{Nodes: nodes, Edges: edges}, nil
}