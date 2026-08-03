package repository

import (
	"fmt"
	"time"

	"github-cmdb/internal/model"
	"gorm.io/gorm"
)

type ChangeRepo struct {
	db *gorm.DB
}

func NewChangeRepo(db *gorm.DB) *ChangeRepo {
	return &ChangeRepo{db: db}
}

type ChangeFilter struct {
	Status   string `form:"status"`
	Priority string `form:"priority"`
	Keyword  string `form:"q"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

func (r *ChangeRepo) List(filter ChangeFilter) ([]model.ChangeTicket, int64, error) {
	var tickets []model.ChangeTicket
	var total int64
	query := r.db.Model(&model.ChangeTicket{}).Preload("CITarget.CIType")

	if filter.Status != ""   { query = query.Where("status = ?", filter.Status) }
	if filter.Priority != "" { query = query.Where("priority = ?", filter.Priority) }
	if filter.Keyword != "" {
		kw := "%" + filter.Keyword + "%"
		query = query.Where("title LIKE ? OR ticket_no LIKE ?", kw, kw)
	}

	if err := query.Count(&total).Error; err != nil { return nil, 0, err }

	page := filter.Page; if page < 1 { page = 1 }
	size := filter.PageSize; if size < 1 || size > 100 { size = 20 }

	err := query.Order("updated_at DESC").Offset((page - 1) * size).Limit(size).Find(&tickets).Error
	return tickets, total, err
}

func (r *ChangeRepo) GetByID(id uint64) (*model.ChangeTicket, error) {
	var ticket model.ChangeTicket
	err := r.db.Preload("CITarget.CIType").First(&ticket, id).Error
	if err != nil { return nil, err }
	return &ticket, nil
}

func (r *ChangeRepo) Create(ticket *model.ChangeTicket) error {
	var maxID uint64
	r.db.Model(&model.ChangeTicket{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID)
	ticket.TicketNo = fmt.Sprintf("CHG-%s-%04d", time.Now().Format("20060102"), maxID+1)
	return r.db.Create(ticket).Error
}

func (r *ChangeRepo) Update(ticket *model.ChangeTicket) error {
	return r.db.Save(ticket).Error
}

func (r *ChangeRepo) Delete(id uint64) error {
	return r.db.Delete(&model.ChangeTicket{}, id).Error
}

func (r *ChangeRepo) CountByStatus() (map[string]int64, error) {
	type row struct {
		Status string
		Count  int64
	}
	var rows []row
	err := r.db.Model(&model.ChangeTicket{}).Select("status, COUNT(*) as count").Group("status").Scan(&rows).Error
	if err != nil { return nil, err }
	result := make(map[string]int64)
	for _, row := range rows { result[row.Status] = row.Count }
	return result, nil
}