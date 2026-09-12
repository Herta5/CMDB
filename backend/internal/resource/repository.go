// 本文件封装资源核心的 GORM 数据访问，平台模块不得直接使用数据库。
package resource

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"cmdb/internal/audit"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// CreateSource 保存已加密的接入源，仓储永远不接收明文凭证。
func (r *Repository) CreateSource(ctx context.Context, source *Source) error {
	// 即便上层启用 SQL 日志，凭证专用写入也不能把绑定的完整密文带入日志。
	return sourceIdentityWriteError(r.db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).WithContext(ctx).Create(source).Error)
}

// UpdateSource 保存经过服务层校验的非敏感配置和可选新密文。
func (r *Repository) UpdateSource(ctx context.Context, source *Source, updates map[string]any) error {
	return sourceIdentityWriteError(r.db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).WithContext(ctx).Model(source).Updates(updates).Error)
}

// sourceIdentityWriteError 以数据库唯一约束作为并发归属的最终裁决，禁止将账号值或底层 SQL 返回给调用方。
func sourceIdentityWriteError(err error) error {
	// 生产统一开启 TranslateError；来源更新不修改主键，唯一业务索引只有云账号归属。
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrCloudAccountConflict
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" && pgError.ConstraintName == "uk_resource_sources_provider_account_verified" {
		return ErrCloudAccountConflict
	}
	// SQLite 用于离线业务测试，错误文本只参与内部分类，绝不作为业务响应。
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: resource_sources.provider, resource_sources.cloud_account_id") {
		return ErrCloudAccountConflict
	}
	return err
}

// ProjectIsEnabled 只供新增身份验证动作检查项目状态；事务内共享锁防止检查后项目被并发停用。
func (r *Repository) ProjectIsEnabled(ctx context.Context, projectID uint64) (bool, error) {
	var project struct{ Status string }
	if err := r.db.WithContext(ctx).Table("projects").Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ?", projectID).Take(&project).Error; err != nil {
		return false, err
	}
	return project.Status == "enabled", nil
}

// lockSourceForProject 在身份提交事务内重读并锁定来源，避免并发验证覆盖其他事务已经确认的账号。
func (r *Repository) lockSourceForProject(ctx context.Context, projectID, sourceID uint64) (*Source, error) {
	var source Source
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND project_id = ?", sourceID, projectID).Take(&source).Error; err != nil {
		return nil, err
	}
	return &source, nil
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
	err := r.db.WithContext(ctx).Where("enabled = ? AND identity_status = ? AND cloud_account_id <> '' AND (next_sync_at IS NULL OR next_sync_at <= ?)", true, IdentityStatusVerified, now).Find(&sources).Error
	return sources, err
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

// ListResources 跨三张资产表合并查询，并在项目边界内统一分页。
func (r *Repository) ListResources(ctx context.Context, projectID uint64, provider, resourceType, lifecycle string, offset, limit int) ([]Resource, int64, error) {
	tables := []string{"resources_servers", "resources_databases", "resources_load_balancers"}
	if resourceType != "" {
		table, err := assetTableForType(resourceType)
		if err != nil {
			return nil, 0, err
		}
		tables = []string{table}
	}
	values := make([]Resource, 0)
	for _, table := range tables {
		query := r.db.WithContext(ctx).Table(table).Where("project_id = ?", projectID)
		if provider != "" {
			query = query.Where("provider = ?", provider)
		}
		if resourceType != "" {
			query = query.Where("resource_type = ?", resourceType)
		}
		if lifecycle != "" {
			query = query.Where("asset_status = ?", lifecycle)
		}
		var rows []assetRow
		if err := query.Find(&rows).Error; err != nil {
			return nil, 0, err
		}
		for _, row := range rows {
			values = append(values, resourceFromRow(row, table))
		}
	}
	sort.SliceStable(values, func(i, j int) bool { return values[i].UpdatedAt.After(values[j].UpdatedAt) })
	total := int64(len(values))
	if offset >= len(values) {
		return []Resource{}, total, nil
	}
	end := offset + limit
	if end > len(values) {
		end = len(values)
	}
	return values[offset:end], total, nil
}

// Repository 是统一资源核心的持久化实现。
type Repository struct{ db *gorm.DB }

// NewRepository 创建由平台数据库连接管理的资源仓储。
func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

// Transaction 保证一次同步任务内的资源变化、生命周期处理、统计和审计原子提交。
func (r *Repository) Transaction(ctx context.Context, operation func(*gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(operation)
}

// WithAuditTransaction 将人工接入源变更和对应审计绑定到同一数据库事务。
func (r *Repository) WithAuditTransaction(ctx context.Context, operation func(*Repository, audit.Recorder) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return operation(&Repository{db: tx}, audit.NewRepository(tx))
	})
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

// FindJob 读取单个同步任务，服务层继续校验项目归属和可重试状态。
func (r *Repository) FindJob(ctx context.Context, id uint64) (*SyncJob, error) {
	var job SyncJob
	if err := r.db.WithContext(ctx).First(&job, id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

// RecoverableJobs 返回重启前尚未执行的排队任务。
func (r *Repository) RecoverableJobs(ctx context.Context) ([]SyncJob, error) {
	var jobs []SyncJob
	err := r.db.WithContext(ctx).Where("status = ?", "queued").Order("id ASC").Find(&jobs).Error
	return jobs, err
}

// PendingSourceJobs 返回尚未终结的历史待验证来源任务，交给服务层沿用失败审计事务。
func (r *Repository) PendingSourceJobs(ctx context.Context) ([]SyncJob, error) {
	var jobs []SyncJob
	err := r.db.WithContext(ctx).Where("status IN ? AND source_id IN (?)", []string{"queued", "running"}, r.db.Model(&Source{}).Select("id").Where("identity_status <> ? OR cloud_account_id IS NULL OR cloud_account_id = ''", IdentityStatusVerified)).Find(&jobs).Error
	return jobs, err
}

// FailInterruptedJobs 将进程中断时遗留的运行任务结束为脱敏失败状态。
func (r *Repository) FailInterruptedJobs(ctx context.Context, finished time.Time) error {
	return r.db.WithContext(ctx).Model(&SyncJob{}).Where("status = ?", "running").Updates(map[string]any{"status": "failed", "error_summary": "服务重启导致任务中断，可重新执行", "finished_at": finished}).Error
}
