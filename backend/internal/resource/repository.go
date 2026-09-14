// 本文件封装资源核心的 GORM 数据访问，平台模块不得直接使用数据库。
package resource

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"cmdb/internal/audit"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// CreateSource 保存已加密的接入源，仓储永远不接收明文凭证。
func (r *Repository) CreateSource(ctx context.Context, source *Source) error {
	// 即便上层启用 SQL 日志，凭证专用写入也不能把绑定的完整密文带入日志。
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := NewDeletionGuard(tx).LockProjectForOperation(ctx, source.ProjectID); err != nil {
			return err
		}
		return sourceIdentityWriteError(tx.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Create(source).Error)
	})
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
	if errors.As(err, &pgError) && pgError.Code == "23505" && pgError.ConstraintName == "uk_resource_sources_provider_account" {
		return ErrCloudAccountConflict
	}
	// SQLite 用于离线业务测试，错误文本只参与内部分类，绝不作为业务响应。
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: resource_sources.provider, resource_sources.cloud_account_id") {
		return ErrCloudAccountConflict
	}
	return err
}

// lockSourceForProject 在凭证替换事务内重读并锁定来源，避免并发修改覆盖稳定账号身份。
func (r *Repository) lockSourceForProject(ctx context.Context, projectID, sourceID uint64) (*Source, error) {
	return NewDeletionGuard(r.db).LockSourceForOperation(ctx, projectID, sourceID)
}

// DeleteSource 只在事务内通过依赖守卫后删除来源，三类资产外键继续提供 RESTRICT 兜底。
func (r *Repository) DeleteSource(ctx context.Context, source *Source) error {
	result := r.db.WithContext(ctx).Delete(source)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
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

// ListResources 跨三张资产表合并查询，并在项目边界内统一搜索、排序和分页。
func (r *Repository) ListResources(ctx context.Context, projectID uint64, filter ResourceListQuery) ([]Resource, int64, error) {
	tables := []string{"resources_servers", "resources_databases", "resources_load_balancers"}
	if len(filter.ResourceTypes) > 0 {
		tableSet := make(map[string]bool)
		tables = tables[:0]
		for _, resourceType := range filter.ResourceTypes {
			table, _ := assetTableForType(resourceType)
			if !tableSet[table] {
				tables = append(tables, table)
				tableSet[table] = true
			}
		}
	}
	// 服务器列表是当前高频分页入口，必须由数据库在完整范围筛选、排序后只返回当前页。
	if len(tables) == 1 && tables[0] == "resources_servers" {
		return r.listServerResources(ctx, projectID, filter)
	}
	sourceQuery := r.db.WithContext(ctx)
	if projectID > 0 {
		sourceQuery = sourceQuery.Where("project_id = ?", projectID)
	}
	var sources []Source
	if err := sourceQuery.Find(&sources).Error; err != nil {
		return nil, 0, err
	}
	sourceNames := make(map[uint64]string, len(sources))
	for _, source := range sources {
		sourceNames[source.ID] = source.Name
	}
	var projectRows []struct {
		ID   uint64
		Name string
	}
	projectQuery := r.db.WithContext(ctx).Table("projects").Select("id", "name")
	if projectID > 0 {
		projectQuery = projectQuery.Where("id = ?", projectID)
	}
	if err := projectQuery.Find(&projectRows).Error; err != nil {
		return nil, 0, err
	}
	projectNames := make(map[uint64]string, len(projectRows))
	for _, project := range projectRows {
		projectNames[project.ID] = project.Name
	}
	values := make([]Resource, 0)
	for _, table := range tables {
		query := r.db.WithContext(ctx).Table(table)
		if projectID > 0 {
			query = query.Where("project_id = ?", projectID)
		}
		if len(filter.Providers) > 0 {
			query = query.Where("provider IN ?", filter.Providers)
		}
		if len(filter.ResourceTypes) > 0 {
			query = query.Where("resource_type IN ?", filter.ResourceTypes)
		}
		if filter.SourceID > 0 {
			query = query.Where("source_id = ?", filter.SourceID)
		}
		if filter.Region != "" {
			query = query.Where("region = ?", filter.Region)
		}
		if filter.CloudStatus != "" {
			query = query.Where("LOWER(cloud_status) = ?", strings.ToLower(filter.CloudStatus))
		}
		if filter.AssetStatus != "" {
			query = query.Where("asset_status = ?", filter.AssetStatus)
		}
		var rows []assetRow
		if err := query.Find(&rows).Error; err != nil {
			return nil, 0, err
		}
		for _, row := range rows {
			value := resourceFromRow(row, table)
			value.SourceName = sourceNames[value.SourceID]
			value.ProjectName = projectNames[value.ProjectID]
			if resourceMatchesKeyword(value, filter.Keyword) {
				values = append(values, value)
			}
		}
	}
	sort.SliceStable(values, func(i, j int) bool {
		comparison := compareResources(values[i], values[j], filter.SortBy)
		if comparison == 0 {
			// 次级身份排序固定升序，不随主排序方向翻转，保证分页边界稳定。
			return strings.Compare(resourceIdentity(values[i]), resourceIdentity(values[j])) < 0
		}
		if filter.SortOrder == "desc" {
			return comparison > 0
		}
		return comparison < 0
	})
	total := int64(len(values))
	offset, limit := (filter.Page-1)*filter.PageSize, filter.PageSize
	if offset >= len(values) {
		return []Resource{}, total, nil
	}
	end := offset + limit
	if end > len(values) {
		end = len(values)
	}
	return values[offset:end], total, nil
}

// listServerResources 使用固定投影排除原始属性，并让数据库承担搜索、计数、排序和分页。
func (r *Repository) listServerResources(ctx context.Context, projectID uint64, filter ResourceListQuery) ([]Resource, int64, error) {
	query := r.db.WithContext(ctx).Table("resources_servers AS assets").
		Joins("JOIN resource_sources AS sources ON sources.id = assets.source_id").
		Joins("JOIN projects ON projects.id = assets.project_id")
	if projectID > 0 {
		query = query.Where("assets.project_id = ?", projectID)
	}
	if len(filter.Providers) > 0 {
		query = query.Where("assets.provider IN ?", filter.Providers)
	}
	if len(filter.ResourceTypes) > 0 {
		query = query.Where("assets.resource_type IN ?", filter.ResourceTypes)
	}
	if filter.SourceID > 0 {
		query = query.Where("assets.source_id = ?", filter.SourceID)
	}
	if filter.Region != "" {
		query = query.Where("assets.region = ?", filter.Region)
	}
	if filter.CloudStatus != "" {
		query = query.Where("LOWER(assets.cloud_status) = ?", strings.ToLower(filter.CloudStatus))
	}
	if filter.AssetStatus != "" {
		query = query.Where("assets.asset_status = ?", filter.AssetStatus)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		pattern := "%" + strings.ToLower(escapedLikeValue(keyword)) + "%"
		query = query.Where("(LOWER(assets.name) LIKE ? ESCAPE '\\' OR LOWER(assets.external_id) LIKE ? ESCAPE '\\' OR LOWER(CAST(assets.private_ips AS TEXT)) LIKE ? ESCAPE '\\' OR LOWER(CAST(assets.public_ips AS TEXT)) LIKE ? ESCAPE '\\')", pattern, pattern, pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	orderColumn := map[string]string{
		"name": "LOWER(assets.name)", "project": "LOWER(projects.name)", "source": "LOWER(sources.name)",
		"resource_type": "assets.resource_type", "instance_type": "LOWER(assets.instance_type)", "vcpu": "assets.vcpu", "memory": "assets.memory",
		"region": "LOWER(assets.region)", "cloud_status": "LOWER(assets.cloud_status)", "asset_status": "assets.asset_status", "last_seen_at": "assets.last_seen_at",
	}[filter.SortBy]
	if filter.SortBy == "disk_size" {
		if r.db.Dialector.Name() == "postgres" {
			orderColumn = "COALESCE((SELECT SUM(CAST(disk.value->>'size_gib' AS BIGINT)) FROM json_array_elements(assets.disks) AS disk(value)), 0)"
		} else {
			orderColumn = "COALESCE((SELECT SUM(CAST(json_extract(disk.value, '$.size_gib') AS INTEGER)) FROM json_each(assets.disks) AS disk), 0)"
		}
	}
	orderDirection := "ASC"
	if filter.SortOrder == "desc" {
		orderDirection = "DESC"
	}
	selectColumns := "assets.id, assets.project_id, assets.source_id, assets.provider, assets.resource_type, assets.external_id, assets.name, assets.region, assets.zone, assets.cloud_status, assets.asset_status, assets.first_seen_at, assets.last_seen_at, assets.missing_since, assets.created_at, assets.updated_at, assets.instance_type, assets.vcpu, assets.memory, assets.private_ips, assets.public_ips, assets.disks, sources.name AS source_name, projects.name AS project_name"
	var rows []assetRow
	if err := query.Select(selectColumns).
		Order(orderColumn + " " + orderDirection).
		Order("assets.source_id ASC, assets.resource_type ASC, assets.external_id ASC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	values := make([]Resource, 0, len(rows))
	for _, row := range rows {
		value := resourceFromRow(row, "resources_servers")
		value.SourceName, value.ProjectName = row.SourceName, row.ProjectName
		values = append(values, value)
	}
	return values, total, nil
}

func escapedLikeValue(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
}

// resourceMatchesKeyword 对列表公开字段做不区分大小写的模糊匹配，服务器 IP 直接使用规范化端点视图。
func resourceMatchesKeyword(value Resource, keyword string) bool {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return true
	}
	if strings.Contains(strings.ToLower(value.Name), keyword) || strings.Contains(strings.ToLower(value.ExternalID), keyword) {
		return true
	}
	for _, endpoint := range value.Endpoints {
		if strings.Contains(strings.ToLower(endpoint.Address), keyword) {
			return true
		}
	}
	return false
}

func resourceIdentity(value Resource) string {
	return strconv.FormatUint(value.SourceID, 10) + "\x00" + value.ResourceType + "\x00" + value.ExternalID
}

func resourceDiskSize(value Resource) int64 {
	var total int64
	for _, disk := range value.Disks {
		total += disk.SizeGiB
	}
	return total
}

// compareResources 只比较经过服务层白名单验证的展示字段。
func compareResources(left, right Resource, field string) int {
	stringValues := func(value Resource) string {
		switch field {
		case "project":
			return value.ProjectName
		case "source":
			return value.SourceName
		case "resource_type":
			return value.ResourceType
		case "instance_type":
			return value.InstanceType
		case "region":
			return value.Region
		case "cloud_status":
			return value.CloudStatus
		case "asset_status":
			return value.AssetStatus
		default:
			return value.Name
		}
	}
	switch field {
	case "vcpu":
		return left.VCPU - right.VCPU
	case "memory":
		if left.Memory < right.Memory {
			return -1
		}
		if left.Memory > right.Memory {
			return 1
		}
		return 0
	case "disk_size":
		leftSize, rightSize := resourceDiskSize(left), resourceDiskSize(right)
		if leftSize < rightSize {
			return -1
		}
		if leftSize > rightSize {
			return 1
		}
		return 0
	case "last_seen_at":
		if left.LastSeenAt.Before(right.LastSeenAt) {
			return -1
		}
		if left.LastSeenAt.After(right.LastSeenAt) {
			return 1
		}
		return 0
	default:
		return strings.Compare(strings.ToLower(stringValues(left)), strings.ToLower(stringValues(right)))
	}
}

// Repository 是统一资源核心的持久化实现。
type Repository struct{ db *gorm.DB }

// NewRepository 创建由平台数据库连接管理的资源仓储。
func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

// forStartupRecovery 为启动恢复复用同一连接池但关闭底层日志，所有错误统一由启动门禁返回安全摘要。
func (r *Repository) forStartupRecovery() *Repository {
	return NewRepository(r.db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}))
}

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

// CreateJob 在统一父锁下创建任务，防止父删除检查通过后出现会被级联删除的新活动任务。
func (r *Repository) CreateJob(ctx context.Context, job *SyncJob) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, err := NewDeletionGuard(tx).LockSourceForOperation(ctx, job.ProjectID, job.SourceID)
		if err != nil {
			return err
		}
		return tx.Create(job).Error
	})
}

// SaveJob 保存任务的最终脱敏状态与统计。
func (r *Repository) SaveJob(ctx context.Context, job *SyncJob) error {
	return r.db.WithContext(ctx).Save(job).Error
}

// UpdateNextSyncAt 只推进自动计划，失败收敛不得同时刷新最近成功同步时间。
func (r *Repository) UpdateNextSyncAt(ctx context.Context, sourceID uint64, next time.Time) error {
	return r.db.WithContext(ctx).Model(&Source{}).Where("id = ?", sourceID).Update("next_sync_at", next).Error
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

// FailInterruptedJobs 将进程中断时遗留的运行任务结束为脱敏失败状态。
func (r *Repository) FailInterruptedJobs(ctx context.Context, finished time.Time) error {
	return r.db.WithContext(ctx).Model(&SyncJob{}).Where("status = ?", "running").Updates(map[string]any{"status": "failed", "error_summary": "服务重启导致任务中断，可重新执行", "finished_at": finished}).Error
}
