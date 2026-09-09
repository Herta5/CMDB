// 本文件封装资源核心的 GORM 数据访问，平台模块不得直接使用数据库。
package resource

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// CreateSource 保存已加密的接入源，仓储永远不接收明文凭证。
func (r *Repository) CreateSource(ctx context.Context, source *Source) error {
	return r.db.WithContext(ctx).Create(source).Error
}

// UpdateSource 保存经过服务层校验的非敏感配置和可选新密文。
func (r *Repository) UpdateSource(ctx context.Context, source *Source, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(source).Updates(updates).Error
}

// DeleteSource 删除接入源及数据库外键约束下的资源和同步任务。
func (r *Repository) DeleteSource(ctx context.Context, source *Source) error {
	return r.db.WithContext(ctx).Delete(source).Error
}

// ListJobs 在项目边界内按时间倒序分页返回同步历史。
func (r *Repository) ListJobs(ctx context.Context, projectID, sourceID uint64, provider string, offset, limit int) ([]SyncJob, int64, error) {
	query := r.db.WithContext(ctx).Model(&SyncJob{}).Where("project_id = ?", projectID)
	if sourceID > 0 {
		query = query.Where("source_id = ?", sourceID)
	}
	if provider != "" {
		query = query.Where("source_id IN (?)", r.db.Model(&Source{}).Select("id").Where("project_id = ? AND provider = ?", projectID, provider))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var jobs []SyncJob
	if err := query.Order("started_at DESC, id DESC").Offset(offset).Limit(limit).Find(&jobs).Error; err != nil {
		return nil, 0, err
	}
	return jobs, total, nil
}

// ListDueSources 返回当前应调度的已启用接入源。
func (r *Repository) ListDueSources(ctx context.Context, now time.Time) ([]Source, error) {
	var sources []Source
	err := r.db.WithContext(ctx).Where("enabled = ? AND (next_sync_at IS NULL OR next_sync_at <= ?)", true, now).Find(&sources).Error
	return sources, err
}

// CreateAudit 写入已经过调用方白名单化的审计内容。
func (r *Repository) CreateAudit(ctx context.Context, projectID uint64, action, resourceType string, resourceID uint64, detail map[string]any) error {
	encoded, _ := json.Marshal(detail)
	return r.db.WithContext(ctx).Create(&AuditLog{ProjectID: &projectID, Action: action, ResourceType: resourceType, ResourceID: strconv.FormatUint(resourceID, 10), Detail: encoded}).Error
}

// ListSources 按项目和可选平台过滤接入源。
func (r *Repository) ListSources(ctx context.Context, projectID uint64, provider string) ([]Source, error) {
	query := r.db.WithContext(ctx).Where("project_id = ?", projectID)
	if provider != "" {
		query = query.Where("provider = ?", provider)
	}
	var values []Source
	if err := query.Order("id ASC").Find(&values).Error; err != nil {
		return nil, err
	}
	return values, nil
}

// ListResources 按项目及可选条件分页查询资源，并加载访问端点。
func (r *Repository) ListResources(ctx context.Context, projectID uint64, provider, resourceType, lifecycle string, offset, limit int) ([]Resource, int64, error) {
	query := r.db.WithContext(ctx).Model(&Resource{}).Where("project_id = ?", projectID)
	if provider != "" {
		query = query.Where("provider = ?", provider)
	}
	if resourceType != "" {
		query = query.Where("resource_type = ?", resourceType)
	}
	if lifecycle != "" {
		query = query.Where("lifecycle_status = ?", lifecycle)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var values []Resource
	if err := query.Preload("Endpoints").Order("id DESC").Offset(offset).Limit(limit).Find(&values).Error; err != nil {
		return nil, 0, err
	}
	return values, total, nil
}

// Repository 是统一资源核心的持久化实现。
type Repository struct{ db *gorm.DB }

// NewRepository 创建由平台数据库连接管理的资源仓储。
func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

// Transaction 保证单类快照的更新与失联判断原子提交。
func (r *Repository) Transaction(ctx context.Context, operation func(*gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(operation)
}

// FindSource 读取接入源，密文只交给同步服务解密。
func (r *Repository) FindSource(ctx context.Context, id uint64) (*Source, error) {
	var source Source
	if err := r.db.WithContext(ctx).First(&source, id).Error; err != nil {
		return nil, err
	}
	return &source, nil
}

// CreateJob 记录同步开始状态。
func (r *Repository) CreateJob(ctx context.Context, job *SyncJob) error {
	return r.db.WithContext(ctx).Create(job).Error
}

// SaveJob 保存任务的最终脱敏状态与统计。
func (r *Repository) SaveJob(ctx context.Context, job *SyncJob) error {
	return r.db.WithContext(ctx).Save(job).Error
}

// UpdateSourceSchedule 记录最近同步与下一次调度时间。
func (r *Repository) UpdateSourceSchedule(ctx context.Context, source *Source) error {
	return r.db.WithContext(ctx).Model(&Source{}).Where("id = ?", source.ID).Updates(map[string]any{"last_sync_at": source.LastSyncAt, "next_sync_at": source.NextSyncAt}).Error
}

// PurgeLostBefore 删除截止时间前持续失联的资源，端点由外键级联删除。
func (r *Repository) PurgeLostBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	var deleted int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var resources []Resource
		if err := tx.Where("lifecycle_status = ? AND missing_since <= ?", LifecycleLost, cutoff).Find(&resources).Error; err != nil {
			return err
		}
		for _, value := range resources {
			projectID := value.ProjectID
			detail, _ := json.Marshal(map[string]any{"source_id": value.SourceID, "provider": value.Provider, "resource_type": value.ResourceType})
			if err := tx.Create(&AuditLog{ProjectID: &projectID, Action: "resource.deleted", ResourceType: value.ResourceType, ResourceID: value.ExternalID, Detail: detail}).Error; err != nil {
				return err
			}
		}
		ids := make([]uint64, 0, len(resources))
		for _, value := range resources {
			ids = append(ids, value.ID)
		}
		result := tx.Where("id IN ?", ids).Delete(&Resource{})
		deleted = result.RowsAffected
		return result.Error
	})
	return deleted, err
}
