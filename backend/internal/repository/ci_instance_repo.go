package repository

import (
	"fmt"
	"time"

	"github-cmdb/internal/model"
	"gorm.io/gorm"
)

type CIInstanceRepo struct {
	db *gorm.DB
}

func NewCIInstanceRepo(db *gorm.DB) *CIInstanceRepo {
	return &CIInstanceRepo{db: db}
}

type CIInstanceFilter struct {
	CITypeID *uint64 `form:"ci_type_id"`
	Status   string  `form:"status"`
	Keyword  string  `form:"q"`
	IP       string  `form:"ip"`
	SN       string  `form:"sn"`
	AssetTag string  `form:"asset_tag"`
	DeptID   *uint64 `form:"department_id"`
	Page     int     `form:"page"`
	PageSize int     `form:"page_size"`
}

func (r *CIInstanceRepo) List(filter CIInstanceFilter) ([]model.CIInstance, int64, error) {
	var instances []model.CIInstance
	var total int64
	query := r.db.Model(&model.CIInstance{}).Preload("CIType")

	if filter.CITypeID != nil { query = query.Where("ci_type_id = ?", *filter.CITypeID) }
	if filter.Status != ""   { query = query.Where("status = ?", filter.Status) }
	if filter.IP != ""       { query = query.Where("ip_address = ?", filter.IP) }
	if filter.SN != ""       { query = query.Where("sn = ?", filter.SN) }
	if filter.AssetTag != "" { query = query.Where("asset_tag = ?", filter.AssetTag) }
	if filter.DeptID != nil  { query = query.Where("department_id = ?", *filter.DeptID) }
	if filter.Keyword != "" {
		kw := "%" + filter.Keyword + "%"
		query = query.Where("name LIKE ? OR ci_code LIKE ? OR ip_address LIKE ? OR sn LIKE ? OR asset_tag LIKE ?", kw, kw, kw, kw, kw)
	}

	if err := query.Count(&total).Error; err != nil { return nil, 0, err }

	page := filter.Page
	if page < 1 { page = 1 }
	size := filter.PageSize
	if size < 1 || size > 100 { size = 20 }

	err := query.Order("updated_at DESC").Offset((page - 1) * size).Limit(size).Find(&instances).Error
	return instances, total, err
}

func (r *CIInstanceRepo) GetByID(id uint64) (*model.CIInstance, error) {
	var ci model.CIInstance
	err := r.db.Preload("CIType").First(&ci, id).Error
	if err != nil { return nil, err }
	return &ci, nil
}

func (r *CIInstanceRepo) GetByCICode(code string) (*model.CIInstance, error) {
	var ci model.CIInstance
	err := r.db.Where("ci_code = ?", code).First(&ci).Error
	if err != nil { return nil, err }
	return &ci, nil
}

func (r *CIInstanceRepo) GetByExternalID(externalID string) (*model.CIInstance, error) {
	var ci model.CIInstance
	err := r.db.Where("source_detail = ? AND source = ?", externalID, "auto_discovery").First(&ci).Error
	if err != nil { return nil, err }
	return &ci, nil
}

func (r *CIInstanceRepo) GetByTypeAndIP(ciTypeID uint64, ip string) (*model.CIInstance, error) {
	var ci model.CIInstance
	err := r.db.Where("ci_type_id = ? AND ip_address = ?", ciTypeID, ip).First(&ci).Error
	if err != nil { return nil, err }
	return &ci, nil
}

func (r *CIInstanceRepo) Create(ci *model.CIInstance) error { return r.db.Create(ci).Error }
func (r *CIInstanceRepo) Update(ci *model.CIInstance) error { return r.db.Save(ci).Error }
func (r *CIInstanceRepo) Delete(id uint64) error            { return r.db.Delete(&model.CIInstance{}, id).Error }

func (r *CIInstanceRepo) UpdateLastSeen(id uint64) error {
	now := time.Now()
	return r.db.Model(&model.CIInstance{}).Where("id = ?", id).Update("last_seen_at", now).Error
}

func (r *CIInstanceRepo) GenerateCICode(typeName string) (string, error) {
	var maxID uint64
	r.db.Model(&model.CIInstance{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID)
	return fmt.Sprintf("%s-%06d", typeName, maxID+1), nil
}

type TypeDistribution struct {
	CITypeID uint64 `json:"ci_type_id"`
	TypeName string `json:"type_name"`
	Count    int64  `json:"count"`
}

func (r *CIInstanceRepo) DistributionByType() ([]TypeDistribution, error) {
	var results []TypeDistribution
	err := r.db.Model(&model.CIInstance{}).
		Select("ci_instance.ci_type_id, ci_type.display_name as type_name, COUNT(*) as count").
		Joins("JOIN ci_type ON ci_type.id = ci_instance.ci_type_id").
		Group("ci_instance.ci_type_id, ci_type.display_name").
		Order("count DESC").Scan(&results).Error
	return results, err
}

func (r *CIInstanceRepo) DistributionByStatus() (map[string]int64, error) {
	type row struct{ Status string; Count int64 }
	var rows []row
	err := r.db.Model(&model.CIInstance{}).Select("status, COUNT(*) as count").Group("status").Scan(&rows).Error
	if err != nil { return nil, err }
	result := make(map[string]int64)
	for _, row := range rows { result[row.Status] = row.Count }
	return result, nil
}

func (r *CIInstanceRepo) TotalCount() (int64, error) {
	var count int64
	err := r.db.Model(&model.CIInstance{}).Count(&count).Error
	return count, err
}

func (r *CIInstanceRepo) TrendByMonth(months int) ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	err := r.db.Model(&model.CIInstance{}).
		Select("DATE_FORMAT(created_at, '%Y-%m') as month, COUNT(*) as count").
		Where("created_at >= DATE_SUB(NOW(), INTERVAL ? MONTH)", months).
		Group("month").Order("month ASC").Scan(&results).Error
	return results, err
}