package repository

import (
	"github-cmdb/internal/model"
	"gorm.io/gorm"
)

type AuditRepo struct {
	db *gorm.DB
}

func NewAuditRepo(db *gorm.DB) *AuditRepo {
	return &AuditRepo{db: db}
}

type AuditFilter struct {
	Username string `form:"username"`
	Method   string `form:"method"`
	Path     string `form:"path"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

func (r *AuditRepo) List(filter AuditFilter) ([]model.AuditLog, int64, error) {
	var logs []model.AuditLog
	var total int64
	query := r.db.Model(&model.AuditLog{})

	if filter.Username != "" { query = query.Where("username LIKE ?", "%"+filter.Username+"%") }
	if filter.Method != ""   { query = query.Where("method = ?", filter.Method) }
	if filter.Path != ""     { query = query.Where("path LIKE ?", "%"+filter.Path+"%") }

	if err := query.Count(&total).Error; err != nil { return nil, 0, err }

	page := filter.Page; if page < 1 { page = 1 }
	size := filter.PageSize; if size < 1 || size > 100 { size = 20 }

	err := query.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&logs).Error
	return logs, total, err
}

func (r *AuditRepo) Create(log *model.AuditLog) error {
	return r.db.Create(log).Error
}

// Webhook methods
func (r *AuditRepo) CreateWebhook(w *model.WebhookRecord) error {
	return r.db.Create(w).Error
}

func (r *AuditRepo) ListWebhooks(page, pageSize int) ([]model.WebhookRecord, int64, error) {
	var records []model.WebhookRecord
	var total int64
	query := r.db.Model(&model.WebhookRecord{})
	query.Count(&total)
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 100 { pageSize = 20 }
	err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&records).Error
	return records, total, err
}