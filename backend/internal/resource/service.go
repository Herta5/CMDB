// 本文件集中实现跨平台同步、恢复、失联和 24 小时清理规则。
package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"cmdb/internal/audit"
	"gorm.io/gorm"
)

// Service 协调采集快照与统一资源仓储。
type Service struct {
	repository    *Repository
	cipher        *CredentialCipher
	adapters      map[string]ProviderAdapter
	now           func() time.Time
	sourceLocks   sync.Map
	auditRecorder audit.Recorder
}

// ErrSyncAlreadyRunning 表示同一接入源已有同步任务正在执行。
var ErrSyncAlreadyRunning = errors.New("接入源同步任务正在执行")

// ErrInvalidResourceQuery 表示资源筛选或排序参数不在公开白名单中。
var ErrInvalidResourceQuery = errors.New("资源查询参数无效")

var (
	// ErrInvalidSourceInput 只标识调用方可纠正的来源基础字段和周期错误，与内部失败分离。
	ErrInvalidSourceInput = errors.New("接入源参数无效")
	// ErrCloudAccountConflict 表示同平台云账号已由一个接入源占用，不披露其归属。
	ErrCloudAccountConflict = errors.New("该云账号已接入 CMDB")
	// ErrSourceIdentityMismatch 防止通过替换凭证将接入源指向另一个云账号。
	ErrSourceIdentityMismatch = errors.New("新凭证所属云账号与原接入源不一致")
	// ErrSchedulerRecoveryFailed 阻止恢复未完成的进程开放服务，不携带底层数据库或审计错误。
	ErrSchedulerRecoveryFailed = errors.New("恢复同步任务失败，服务未启动")
)

// CreateSourceInput 是创建接入源允许写入的项目边界和平台配置。
type CreateSourceInput struct {
	ProjectID           uint64
	Provider            string
	Name                string
	Region              string
	Credential          json.RawMessage
	Config              json.RawMessage
	SyncIntervalMinutes int
}

// UpdateSourceInput 是接入源可维护字段；Credential 为空表示保留原凭证。
type UpdateSourceInput struct {
	Name                string
	Region              string
	Credential          json.RawMessage
	Config              json.RawMessage
	Enabled             bool
	SyncIntervalMinutes int
}

// ConnectionTestResult 只报告各资源类型是否可访问，不携带云端原始响应。
type ConnectionTestResult struct {
	ReachableTypes []string `json:"reachable_types"`
	FailedTypes    []string `json:"failed_types"`
}

// NewService 创建资源服务，调用方必须提供部署密钥派生的凭证加密器。
func NewService(repository *Repository, cipher *CredentialCipher, adapters map[string]ProviderAdapter, recorders ...audit.Recorder) *Service {
	service := &Service{repository: repository, cipher: cipher, adapters: adapters, now: time.Now}
	if len(recorders) > 0 {
		service.auditRecorder = recorders[0]
	}
	return service
}

// CreateSource 校验平台配置并在进入仓储前加密凭证。
func (s *Service) CreateSource(ctx context.Context, input CreateSourceInput) (*Source, error) {
	if s.repository == nil || s.cipher == nil {
		return nil, errors.New("资源服务不可用")
	}
	if input.ProjectID == 0 || input.Name == "" || !validProvider(input.Provider) || len(input.Credential) == 0 || !json.Valid(input.Credential) {
		return nil, ErrInvalidSourceInput
	}
	interval := input.SyncIntervalMinutes
	if interval == 0 {
		interval = 60
	}
	if interval < 5 || interval > 10080 {
		return nil, ErrInvalidSourceInput
	}
	config := input.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	source := &Source{ProjectID: input.ProjectID, Provider: input.Provider, Name: input.Name, Region: input.Region, CredentialHint: "已安全配置", Config: config, Enabled: true, SyncIntervalMinutes: interval}
	accountID, err := s.resolveSourceIdentity(ctx, source, input.Credential)
	if err != nil {
		return nil, err
	}
	encrypted, err := s.cipher.Encrypt(input.Credential)
	if err != nil {
		return nil, err
	}
	verifiedAt := s.now()
	next := verifiedAt.Add(time.Duration(interval) * time.Minute)
	source.EncryptedCredential, source.CloudAccountID = encrypted, accountID
	source.IdentityVerifiedAt, source.NextSyncAt = &verifiedAt, &next
	projectID := source.ProjectID
	if err := s.withAuditTransaction(ctx, func(repository *Repository, recorder audit.Recorder) error {
		if err := repository.CreateSource(ctx, source); err != nil {
			return err
		}
		return recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectID, Action: audit.ActionSourceCreated, ResourceType: "resource_source", ResourceID: strconv.FormatUint(source.ID, 10), Detail: map[string]any{"provider": source.Provider, "source_name": source.Name}})
	}); err != nil {
		return nil, err
	}
	return source, nil
}

// UpdateSource 更新项目内接入源；只有显式提供凭证时才替换已有密文。
func (s *Service) UpdateSource(ctx context.Context, projectID, sourceID uint64, input UpdateSourceInput) (*Source, error) {
	source, err := s.FindSourceForProject(ctx, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	if input.Name == "" || input.SyncIntervalMinutes < 5 || input.SyncIntervalMinutes > 10080 {
		return nil, ErrInvalidSourceInput
	}
	config := input.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	adapter := s.adapters[source.Provider]
	if adapter == nil {
		return nil, errors.New("平台适配器不可用")
	}
	if err := adapter.ValidateConfig(config); err != nil {
		return nil, ErrInvalidProviderConfig
	}
	updates := map[string]any{"name": input.Name, "region": input.Region, "config": config, "enabled": input.Enabled, "sync_interval_minutes": input.SyncIntervalMinutes}
	credentialReplaced := hasCredential(input.Credential)
	if credentialReplaced {
		candidate := *source
		candidate.Region, candidate.Config = input.Region, config
		accountID, identityErr := s.resolveSourceIdentity(ctx, &candidate, input.Credential)
		if identityErr != nil {
			return nil, identityErr
		}
		if accountID != source.CloudAccountID {
			return nil, ErrSourceIdentityMismatch
		}
		encrypted, encryptErr := s.cipher.Encrypt(input.Credential)
		if encryptErr != nil {
			return nil, encryptErr
		}
		updates["encrypted_credential"] = encrypted
		updates["credential_hint"] = "已安全配置"
		updates["identity_verified_at"] = s.now()
	}
	projectIDCopy := projectID
	if err := s.withAuditTransaction(ctx, func(repository *Repository, recorder audit.Recorder) error {
		current, err := repository.lockSourceForProject(ctx, projectID, sourceID)
		if err != nil {
			return err
		}
		if current.CloudAccountID != source.CloudAccountID {
			return ErrSourceIdentityMismatch
		}
		if err := repository.UpdateSource(ctx, current, updates); err != nil {
			return err
		}
		return recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectIDCopy, Action: audit.ActionSourceUpdated, ResourceType: "resource_source", ResourceID: strconv.FormatUint(sourceID, 10), Detail: map[string]any{"provider": source.Provider, "source_name": input.Name, "credential_replaced": credentialReplaced}})
	}); err != nil {
		return nil, err
	}
	return s.repository.FindSource(ctx, sourceID)
}

// hasCredential 将缺失、null 和空字符串视为未提交；空对象仍须通过平台完整凭证校验。
func hasCredential(raw json.RawMessage) bool {
	value := bytes.TrimSpace(raw)
	return len(value) > 0 && !bytes.Equal(value, []byte("null")) && !bytes.Equal(value, []byte(`""`))
}

// resolveSourceIdentity 先严格检查平台输入再联网识别，所有上游错误必须收敛为有限领域错误。
func (s *Service) resolveSourceIdentity(ctx context.Context, source *Source, credential json.RawMessage) (string, error) {
	adapter := s.adapters[source.Provider]
	if adapter == nil {
		return "", errors.New("平台适配器不可用")
	}
	if err := adapter.ValidateCredential(credential); err != nil {
		return "", ErrInvalidProviderCredential
	}
	if err := adapter.ValidateConfig(source.Config); err != nil {
		return "", ErrInvalidProviderConfig
	}
	// 云调用使用独立的最短期缓冲，防止平台实现持有调用方原始输入。
	plain := append([]byte(nil), credential...)
	defer func() {
		for i := range plain {
			plain[i] = 0
		}
	}()
	accountID, err := adapter.ResolveCloudAccountID(ctx, *source, plain)
	if err != nil {
		for _, safe := range []error{ErrInvalidProviderCredential, ErrInvalidProviderConfig, ErrCloudAuthentication, ErrCloudPermission, ErrCloudNetwork} {
			if errors.Is(err, safe) {
				return "", safe
			}
		}
		return "", ErrCloudNetwork
	}
	if accountID == "" {
		return "", ErrCloudAuthentication
	}
	return accountID, nil
}

// DeleteSource 删除项目内接入源，但审计日志不通过外键级联删除。
func (s *Service) DeleteSource(ctx context.Context, projectID, sourceID uint64) error {
	source, err := s.FindSourceForProject(ctx, projectID, sourceID)
	if err != nil {
		return err
	}
	projectIDCopy := projectID
	err = s.withAuditTransaction(ctx, func(repository *Repository, recorder audit.Recorder) error {
		guard := NewDeletionGuard(repository.db)
		current, err := guard.LockSourceForOperation(ctx, projectID, sourceID)
		if err != nil {
			return err
		}
		if err := guard.CheckSourceDependencies(ctx, sourceID); err != nil {
			return err
		}
		source = current
		// 同一事务内确认无依赖后才写成功删除审计；外键拒绝时也必须一起回滚。
		if err := recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectIDCopy, Action: audit.ActionSourceDeleted, ResourceType: "resource_source", ResourceID: strconv.FormatUint(sourceID, 10), Detail: map[string]any{"provider": source.Provider, "source_name": source.Name}}); err != nil {
			return err
		}
		return repository.DeleteSource(ctx, source)
	})
	return NormalizeDeleteError(err)
}

// ListSources 仅返回指定项目和平台的接入源，密文字段受 JSON 标签保护。
func (s *Service) ListSources(ctx context.Context, projectID uint64, provider string) ([]Source, error) {
	if projectID == 0 || (provider != "" && !validProvider(provider)) {
		return nil, errors.New("接入源查询参数无效")
	}
	return s.repository.ListSources(ctx, projectID, provider)
}

// FindSourceForProject 验证接入源确实属于当前项目。
func (s *Service) FindSourceForProject(ctx context.Context, projectID, sourceID uint64) (*Source, error) {
	source, err := s.repository.FindSource(ctx, sourceID)
	if err != nil {
		// 数据库故障不是对象不存在，保留错误类别供 HTTP 边界返回安全内部故障。
		return nil, err
	}
	if source.ProjectID != projectID {
		return nil, gorm.ErrRecordNotFound
	}
	return source, nil
}

// ResourceListQuery 是资源列表公开查询契约，所有字段都必须经服务层白名单校验。
type ResourceListQuery struct {
	Providers     []string
	ResourceTypes []string
	SourceID      uint64
	Keyword       string
	Region        string
	CloudStatus   string
	AssetStatus   string
	SortBy        string
	SortOrder     string
	Page          int
	PageSize      int
}

// ListResources 提供受项目边界限制的分页资源查询。
func (s *Service) ListResources(ctx context.Context, projectID uint64, query ResourceListQuery) ([]Resource, int64, error) {
	if projectID == 0 {
		return nil, 0, ErrInvalidResourceQuery
	}
	query, err := normalizedResourceListQuery(query)
	if err != nil {
		return nil, 0, err
	}
	return s.repository.ListResources(ctx, projectID, query)
}

// ListAllResources 为系统管理员提供真正的跨项目统一分页；调用权限由 HTTP 装配层强制校验。
func (s *Service) ListAllResources(ctx context.Context, query ResourceListQuery) ([]Resource, int64, error) {
	query, err := normalizedResourceListQuery(query)
	if err != nil {
		return nil, 0, err
	}
	return s.repository.ListResources(ctx, 0, query)
}

func normalizedResourceListQuery(query ResourceListQuery) (ResourceListQuery, error) {
	for _, provider := range query.Providers {
		if !validProvider(provider) {
			return query, ErrInvalidResourceQuery
		}
	}
	for _, resourceType := range query.ResourceTypes {
		if _, err := assetTableForType(resourceType); err != nil {
			return query, ErrInvalidResourceQuery
		}
	}
	if query.AssetStatus != "" && query.AssetStatus != AssetStatusActive && query.AssetStatus != AssetStatusLost {
		return query, ErrInvalidResourceQuery
	}
	allowedSorts := map[string]bool{"name": true, "project": true, "source": true, "resource_type": true, "instance_type": true, "vcpu": true, "memory": true, "disk_size": true, "region": true, "cloud_status": true, "asset_status": true, "last_seen_at": true}
	if query.SortBy == "" {
		query.SortBy = "name"
	}
	if !allowedSorts[query.SortBy] {
		return query, ErrInvalidResourceQuery
	}
	if query.SortOrder == "" {
		query.SortOrder = "asc"
	}
	if query.SortOrder != "asc" && query.SortOrder != "desc" {
		return query, ErrInvalidResourceQuery
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 || query.PageSize > 200 {
		query.PageSize = 20
	}
	if query.Page > int(^uint(0)>>1)/query.PageSize {
		return query, ErrInvalidResourceQuery
	}
	return query, nil
}

// ListJobs 返回当前项目及可选接入源的同步历史。
func (s *Service) ListJobs(ctx context.Context, projectID, sourceID uint64, provider string, page, pageSize int) ([]SyncJob, int64, error) {
	if projectID == 0 || (provider != "" && !validProvider(provider)) {
		return nil, 0, errors.New("项目参数无效")
	}
	if sourceID > 0 {
		if _, err := s.FindSourceForProject(ctx, projectID, sourceID); err != nil {
			return nil, 0, err
		}
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	return s.repository.ListJobs(ctx, projectID, sourceID, provider, (page-1)*pageSize, pageSize)
}

// validProvider 限制接入源只能属于首期三个独立平台模块。
func validProvider(provider string) bool {
	// CMDB 当前仅开放两个公有云平台，旧客户端提交的已下线平台必须被拒绝。
	return provider == ProviderAliyun || provider == ProviderAWS
}

// Sync 执行一次接入源同步，单类失败不会影响其他成功类型。
func (s *Service) Sync(ctx context.Context, sourceID uint64, trigger string, collector Collector) (*SyncJob, error) {
	lock, ok := s.trySourceLock(sourceID)
	if !ok {
		return nil, ErrSyncAlreadyRunning
	}
	defer lock.Unlock()
	return s.executeSync(ctx, sourceID, trigger, collector, nil)
}

// EnqueueSync 创建持久化排队任务并在后台执行，请求返回不等待云平台采集完成。
func (s *Service) EnqueueSync(ctx context.Context, sourceID uint64, trigger string, collector Collector) (*SyncJob, error) {
	return s.enqueueSync(ctx, sourceID, trigger, collector, nil)
}

// enqueueSync 允许重试任务记录原任务标识，同时保持普通入队接口简洁。
func (s *Service) enqueueSync(ctx context.Context, sourceID uint64, trigger string, collector Collector, previousJobID *uint64) (*SyncJob, error) {
	lock, ok := s.trySourceLock(sourceID)
	if !ok {
		return nil, ErrSyncAlreadyRunning
	}
	source, err := s.repository.FindSource(ctx, sourceID)
	if err != nil {
		lock.Unlock()
		return nil, err
	}
	job := &SyncJob{ProjectID: source.ProjectID, SourceID: source.ID, PreviousJobID: previousJobID, Status: "queued", Trigger: trigger, StartedAt: s.now(), ErrorSummary: ""}
	if err := s.repository.CreateJob(ctx, job); err != nil {
		lock.Unlock()
		return nil, err
	}
	// 后台工作器使用独立快照，避免与 HTTP 正在序列化的 queued 返回值发生数据竞争。
	workerJob := *job
	workerContext := audit.DetachedContext(ctx)
	go func() {
		defer lock.Unlock()
		// HTTP 请求结束会取消原上下文，后台任务使用独立上下文完成状态落库。
		_, _ = s.executeSync(workerContext, sourceID, trigger, collector, &workerJob)
	}()
	return job, nil
}

// RetryJob 仅为当前项目的失败或部分成功任务创建新任务，历史记录保持不变。
func (s *Service) RetryJob(ctx context.Context, projectID, jobID uint64) (*SyncJob, error) {
	job, err := s.repository.FindJob(ctx, jobID)
	if err != nil || job.ProjectID != projectID {
		return nil, gorm.ErrRecordNotFound
	}
	source, err := s.FindSourceForProject(ctx, projectID, job.SourceID)
	if err != nil {
		return nil, err
	}
	if job.Status != "failed" && job.Status != "partial_success" {
		return nil, errors.New("同步任务当前不可重试")
	}
	collector := s.adapters[source.Provider]
	if collector == nil {
		return nil, errors.New("平台采集器不可用")
	}
	return s.enqueueSync(ctx, job.SourceID, "manual", collector, &job.ID)
}

// TestConnection 解密凭证调用采集器，但不写资源、不判断失联也不保存云端原始响应。
func (s *Service) TestConnection(ctx context.Context, projectID, sourceID uint64, collector Collector) (*ConnectionTestResult, error) {
	source, err := s.FindSourceForProject(ctx, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	credential, err := s.cipher.Decrypt(source.EncryptedCredential)
	if err != nil {
		return nil, err
	}
	defer func() {
		for index := range credential {
			credential[index] = 0
		}
	}()
	results, err := collector.Probe(ctx, *source, credential)
	if err != nil {
		projectIDCopy := projectID
		if auditErr := s.recordAudit(ctx, audit.Entry{ProjectID: &projectIDCopy, Action: audit.ActionSourceConnectionTested, ResourceType: "resource_source", ResourceID: strconv.FormatUint(sourceID, 10), Detail: map[string]any{"source_name": source.Name, "status": "failed", "error_code": safeConnectionErrorCode(err)}}); auditErr != nil {
			return nil, auditErr
		}
		return nil, err
	}
	// 空集合也必须编码为 JSON 数组，避免前端把 null 当作数组读取时误报连接失败。
	value := &ConnectionTestResult{ReachableTypes: []string{}, FailedTypes: []string{}}
	for _, result := range results {
		if result.Err == nil {
			value.ReachableTypes = append(value.ReachableTypes, result.ResourceType)
		} else {
			value.FailedTypes = append(value.FailedTypes, result.ResourceType)
		}
	}
	projectIDCopy := projectID
	if err := s.recordAudit(ctx, audit.Entry{ProjectID: &projectIDCopy, Action: audit.ActionSourceConnectionTested, ResourceType: "resource_source", ResourceID: strconv.FormatUint(sourceID, 10), Detail: map[string]any{"source_name": source.Name, "reachable_types": value.ReachableTypes, "failed_types": value.FailedTypes}}); err != nil {
		return nil, err
	}
	return value, nil
}

// safeConnectionErrorCode 将云 SDK 错误收敛为有限分类，底层响应文本不得进入审计。
func safeConnectionErrorCode(err error) string {
	if errors.Is(err, ErrAuthenticationFailed) {
		return "authentication_failed"
	}
	if errors.Is(err, ErrPermissionDenied) {
		return "permission_denied"
	}
	return "connection_failed"
}

// trySourceLock 非阻塞占用单个接入源，不影响其他接入源并行。
func (s *Service) trySourceLock(sourceID uint64) (*sync.Mutex, bool) {
	lockValue, _ := s.sourceLocks.LoadOrStore(sourceID, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	return lock, lock.TryLock()
}

// executeSync 执行同步主体；existingJob 非空时承接已返回给调用方的排队任务。
func (s *Service) executeSync(ctx context.Context, sourceID uint64, trigger string, collector Collector, existingJob *SyncJob) (*SyncJob, error) {

	source, err := s.repository.FindSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	job := existingJob
	if job == nil {
		job = &SyncJob{ProjectID: source.ProjectID, SourceID: source.ID, Status: "running", Trigger: trigger, StartedAt: now, ErrorSummary: ""}
		if err := s.repository.CreateJob(ctx, job); err != nil {
			return nil, err
		}
	} else {
		job.Status = "running"
		job.StartedAt = now
		if err := s.repository.SaveJob(ctx, job); err != nil {
			return nil, err
		}
	}
	credential, err := s.cipher.Decrypt(source.EncryptedCredential)
	if err != nil {
		return s.finishFailed(ctx, job, "凭证解密失败", err)
	}
	results, collectErr := collector.Collect(ctx, *source, credential)
	for index := range credential {
		credential[index] = 0
	}
	var expectedTypes []string
	if adapter := s.adapters[source.Provider]; adapter != nil {
		expectedTypes = adapter.ResourceTypes()
	}
	decision := DecideSyncResult(expectedTypes, results, collectErr)
	if decision.Status == "failed" {
		var statistics json.RawMessage
		if len(decision.Statistics) > 0 {
			statistics, _ = json.Marshal(decision.Statistics)
		}
		if err := s.convergeFailedJob(ctx, job, source, decision.ErrorSummary, statistics); err != nil {
			return job, err
		}
		// 保留整体故障向内部调用方报告错误的语义，返回值也必须去掉云端原文。
		if collectErr != nil && !errors.Is(collectErr, ErrAuthenticationFailed) && !errors.Is(collectErr, ErrPermissionDenied) {
			return job, errors.New(decision.ErrorSummary)
		}
		return job, nil
	}
	statistics := decision.Statistics
	changes := newSyncAuditChanges()
	// 所有成功类型的资源变化、删除、任务统计和调度时间使用同一事务，后续类型失败时不会留下无统计归属的部分写入。
	applyErr := s.repository.Transaction(ctx, func(tx *gorm.DB) error {
		// 资产新增与父删除共用项目→来源锁顺序；既有任务不因父对象随后停用而取消。
		current, err := NewDeletionGuard(tx).LockSourceForOperation(ctx, source.ProjectID, source.ID)
		if err != nil {
			return err
		}
		for _, result := range decision.Successful {
			counts, typeErr := s.applyType(ctx, tx, *source, result.ResourceType, result.Snapshots, now, changes)
			if typeErr != nil {
				return typeErr
			}
			statistics[result.ResourceType] = counts
		}
		job.Status = decision.Status
		job.ErrorSummary = decision.ErrorSummary
		job.Statistics, _ = json.Marshal(statistics)
		finished := s.now()
		job.FinishedAt = &finished
		source.LastSyncAt = &finished
		next := finished.Add(time.Duration(current.SyncIntervalMinutes) * time.Minute)
		source.NextSyncAt = &next
		if err := tx.Save(job).Error; err != nil {
			return err
		}
		if err := tx.Model(&Source{}).Where("id = ?", source.ID).Updates(map[string]any{"last_sync_at": source.LastSyncAt, "next_sync_at": source.NextSyncAt}).Error; err != nil {
			return err
		}
		projectID := source.ProjectID
		return audit.RecordInTransaction(ctx, tx, audit.Entry{ProjectID: &projectID, Action: audit.ActionSourceSynced, ResourceType: "resource_source", ResourceID: strconv.FormatUint(source.ID, 10), Detail: map[string]any{"job_id": job.ID, "source_name": source.Name, "provider": source.Provider, "trigger": trigger, "status": job.Status, "statistics": statistics, "changes": changes.snapshot(), "error_summary": job.ErrorSummary}})
	})
	if applyErr != nil {
		// 外层事务已经回滚全部资源变化，失败任务不得保留尚未生效的统计。
		job.Statistics = nil
		return s.finishFailed(ctx, job, "资源写入失败", applyErr)
	}
	return job, nil
}

// finishFailed 只记录脱敏摘要，不把底层错误或凭证内容写入任务。
func (s *Service) finishFailed(ctx context.Context, job *SyncJob, summary string, cause error) (*SyncJob, error) {
	source, err := s.repository.FindSource(ctx, job.SourceID)
	if err != nil {
		return job, err
	}
	if err := s.convergeFailedJob(ctx, job, source, summary, nil); err != nil {
		return job, err
	}
	if cause == nil || errors.Is(cause, ErrAuthenticationFailed) || errors.Is(cause, ErrPermissionDenied) {
		return job, nil
	}
	return job, errors.New(summary)
}

// convergeFailedJob 将失败终态、允许保留的统计、自动计划和失败审计原子提交；失败不具备最近成功语义。
func (s *Service) convergeFailedJob(ctx context.Context, job *SyncJob, source *Source, summary string, statistics json.RawMessage) error {
	finished := s.now()
	failed := *job
	failed.Status, failed.ErrorSummary, failed.Statistics, failed.FinishedAt = "failed", summary, statistics, &finished
	err := s.withAuditTransaction(ctx, func(repository *Repository, recorder audit.Recorder) error {
		// 与资源提交及父删除保持项目→来源锁顺序，同时读取最新配置周期。
		current, err := NewDeletionGuard(repository.db).LockSourceForOperation(ctx, source.ProjectID, source.ID)
		if err != nil {
			return err
		}
		if err := repository.SaveJob(ctx, &failed); err != nil {
			return err
		}
		if failed.Trigger == "scheduled" && failed.PreviousJobID == nil {
			next := finished.Add(time.Duration(current.SyncIntervalMinutes) * time.Minute)
			if err := repository.UpdateNextSyncAt(ctx, current.ID, next); err != nil {
				return err
			}
		}
		return recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &failed.ProjectID, Action: audit.ActionSourceSynced, ResourceType: "resource_source", ResourceID: strconv.FormatUint(failed.SourceID, 10), Detail: map[string]any{"job_id": failed.ID, "source_name": source.Name, "provider": source.Provider, "trigger": failed.Trigger, "status": failed.Status, "statistics": statistics, "changes": newSyncAuditChanges().snapshot(), "error_summary": summary}})
	})
	if err == nil {
		*job = failed
	}
	return err
}

// applyType 在任务事务内写入一个成功资源类型，并只对该类型执行失联和删除判断。
func (s *Service) applyType(ctx context.Context, tx *gorm.DB, source Source, resourceType string, snapshots []Snapshot, now time.Time, changes syncAuditChanges) (map[string]int, error) {
	counts := map[string]int{"added": 0, "updated": 0, "restored": 0, "lost": 0, "deleted": 0, "failed": 0}
	table, err := assetTableForType(resourceType)
	if err != nil {
		return counts, err
	}
	seen := make([]string, 0, len(snapshots))
	unchangedIDs := make([]uint64, 0, len(snapshots))
	for _, snapshot := range snapshots {
		seen = append(seen, snapshot.ExternalID)
		var existing assetRow
		lookupErr := tx.Table(table).Where("source_id = ? AND resource_type = ? AND external_id = ?", source.ID, resourceType, snapshot.ExternalID).First(&existing).Error
		change := ""
		var businessChanges map[string]any
		if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			counts["added"]++
			change = syncChangeCreated
		} else if lookupErr != nil {
			return counts, lookupErr
		} else if existing.AssetStatus == AssetStatusLost {
			counts["restored"]++
			change = syncChangeRestored
		} else {
			businessChanges = changedBusinessColumns(existing, table, snapshot)
			if len(businessChanges) == 0 {
				if jsonValuesEqual(existing.RawAttributes, snapshot.RawAttributes) {
					unchangedIDs = append(unchangedIDs, existing.ID)
				} else if err := tx.Table(table).Where("id = ?", existing.ID).Updates(map[string]any{"raw_attributes": snapshot.RawAttributes, "last_seen_at": now}).Error; err != nil {
					return counts, err
				}
				continue
			}
			counts["updated"]++
			change = syncChangeUpdated
		}
		updates := snapshotBusinessColumns(table, snapshot)
		if change == syncChangeUpdated {
			updates = businessChanges
			// 其他业务字段触发更新时，也要带上仅易变键变化的最新原始快照。
			if !jsonValuesEqual(existing.RawAttributes, snapshot.RawAttributes) {
				updates["raw_attributes"] = snapshot.RawAttributes
			}
		}
		for key, value := range map[string]any{"asset_status": AssetStatusActive, "last_seen_at": now, "missing_since": nil, "updated_at": now} {
			updates[key] = value
		}
		if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			updates["project_id"] = source.ProjectID
			updates["source_id"] = source.ID
			updates["provider"] = source.Provider
			updates["resource_type"] = resourceType
			updates["external_id"] = snapshot.ExternalID
			updates["first_seen_at"] = now
			updates["created_at"] = now
			if err := tx.Table(table).Create(updates).Error; err != nil {
				return counts, err
			}
		} else if err := tx.Table(table).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
			return counts, err
		}
		changes.add(change, resourceType, snapshot.ExternalID)
	}
	// 未变化资源只批量刷新最近发现时间，不改变业务更新时间，也不制造配置变更审计。
	if len(unchangedIDs) > 0 {
		if err := tx.Table(table).Where("id IN ?", unchangedIDs).Update("last_seen_at", now).Error; err != nil {
			return counts, err
		}
	}
	query := tx.Table(table).Where("source_id = ? AND resource_type = ? AND asset_status = ?", source.ID, resourceType, AssetStatusActive)
	if len(seen) > 0 {
		query = query.Where("external_id NOT IN ?", seen)
	}
	var missing []assetRow
	if err := query.Find(&missing).Error; err != nil {
		return counts, err
	}
	ids := make([]uint64, 0, len(missing))
	for _, value := range missing {
		ids = append(ids, value.ID)
	}
	if len(ids) > 0 {
		result := tx.Table(table).Where("id IN ?", ids).Updates(map[string]any{"asset_status": AssetStatusLost, "missing_since": now, "updated_at": now})
		counts["lost"] = int(result.RowsAffected)
		if result.Error != nil {
			return counts, result.Error
		}
	}
	for _, value := range missing {
		changes.add(syncChangeLost, resourceType, value.ExternalID)
	}

	// 仅成功采集的当前类型允许清理；重新出现的资源已在上方恢复，不会被这里误删。
	var expired []assetRow
	if err := tx.Table(table).Where("source_id = ? AND resource_type = ? AND asset_status = ? AND missing_since <= ?", source.ID, resourceType, AssetStatusLost, now.Add(-24*time.Hour)).Find(&expired).Error; err != nil {
		return counts, err
	}
	expiredIDs := make([]uint64, 0, len(expired))
	for _, value := range expired {
		expiredIDs = append(expiredIDs, value.ID)
	}
	if len(expiredIDs) > 0 {
		result := tx.Table(table).Where("id IN ?", expiredIDs).Delete(&assetRow{})
		counts["deleted"] = int(result.RowsAffected)
		if result.Error != nil {
			return counts, result.Error
		}
		for _, value := range expired {
			changes.add(syncChangeDeleted, resourceType, value.ExternalID)
		}
	}
	return counts, nil
}

// withAuditTransaction 始终提供真实事务；启用审计时将业务变更和审计绑定提交。
func (s *Service) withAuditTransaction(ctx context.Context, operation func(*Repository, audit.Recorder) error) error {
	if s.auditRecorder == nil {
		return s.repository.Transaction(ctx, func(tx *gorm.DB) error { return operation(NewRepository(tx), nil) })
	}
	return s.repository.WithAuditTransaction(ctx, operation)
}

// recordAuditWith 将 nil 记录器视为轻量测试未启用审计，生产路径始终收到事务记录器。
func recordAuditWith(ctx context.Context, recorder audit.Recorder, entry audit.Entry) error {
	if recorder == nil {
		return nil
	}
	return recorder.Record(ctx, entry)
}

// recordAudit 将非业务变更事件交给统一记录器；资源生命周期事务仍直接使用统一审计模型。
func (s *Service) recordAudit(ctx context.Context, entry audit.Entry) error {
	if s.auditRecorder == nil {
		return nil
	}
	return s.auditRecorder.Record(ctx, entry)
}

// SyncDueSources 并行执行所有已到期接入源；同源互斥仍由 Sync 统一保证。
func (s *Service) SyncDueSources(ctx context.Context) {
	sources, err := s.repository.ListDueSources(ctx, s.now())
	if err != nil {
		return
	}
	var group sync.WaitGroup
	for index := range sources {
		source := sources[index]
		collector := s.adapters[source.Provider]
		if collector == nil {
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			_, _ = s.EnqueueSync(ctx, source.ID, "scheduled", collector)
		}()
	}
	group.Wait()
}

// StartScheduler 完成启动恢复后才启动后台工作器，恢复失败必须由调用方阻止 HTTP 开放。
func (s *Service) StartScheduler(ctx context.Context) error {
	// 启动恢复先结束异常中断任务，再继续执行已持久化但尚未开始的排队任务。
	// 此临时服务只执行同步恢复，复用原有失败事务；长期工作器始终由当前服务及其同源锁管理。
	recovery := NewService(s.repository.forStartupRecovery(), s.cipher, s.adapters, s.auditRecorder)
	recovery.now = s.now
	if err := recovery.repository.FailInterruptedJobs(ctx, s.now()); err != nil {
		return ErrSchedulerRecoveryFailed
	}
	queued, err := recovery.repository.RecoverableJobs(ctx)
	if err != nil {
		return ErrSchedulerRecoveryFailed
	}
	// 在启动任何工作器前完整读取待恢复来源，避免后续查询失败留下仅部分启动的后台执行。
	sources := make([]Source, len(queued))
	for index := range queued {
		source, sourceErr := recovery.repository.FindSource(ctx, queued[index].SourceID)
		if sourceErr != nil || s.adapters[source.Provider] == nil {
			return ErrSchedulerRecoveryFailed
		}
		sources[index] = *source
	}
	for index := range queued {
		job := &queued[index]
		lock, ok := s.trySourceLock(job.SourceID)
		if !ok {
			continue
		}
		go func(collector Collector) {
			defer lock.Unlock()
			_, _ = s.executeSync(context.Background(), job.SourceID, job.Trigger, collector, job)
		}(s.adapters[sources[index].Provider])
	}
	go func() {
		// 服务启动后立即补跑已到期任务，再按分钟检查；资源清理由成功类型同步负责并记录任务统计。
		s.SyncDueSources(ctx)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.SyncDueSources(ctx)
			}
		}
	}()
	return nil
}
