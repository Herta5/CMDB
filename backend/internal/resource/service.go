// 本文件集中实现跨平台同步、恢复、失联和三天清理规则。
package resource

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Service 协调采集快照与统一资源仓储。
type Service struct {
	repository  *Repository
	cipher      *CredentialCipher
	now         func() time.Time
	sourceLocks sync.Map
}

// ErrSyncAlreadyRunning 表示同一接入源已有同步任务正在执行。
var ErrSyncAlreadyRunning = errors.New("接入源同步任务正在执行")

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

// NewService 创建资源服务，调用方必须提供部署密钥派生的凭证加密器。
func NewService(repository *Repository, cipher *CredentialCipher) *Service {
	return &Service{repository: repository, cipher: cipher, now: time.Now}
}

// CreateSource 校验平台配置并在进入仓储前加密凭证。
func (s *Service) CreateSource(ctx context.Context, input CreateSourceInput) (*Source, error) {
	if s.repository == nil || s.cipher == nil {
		return nil, errors.New("资源服务不可用")
	}
	if input.ProjectID == 0 || input.Name == "" || !validProvider(input.Provider) || len(input.Credential) == 0 || !json.Valid(input.Credential) {
		return nil, errors.New("接入源参数无效")
	}
	interval := input.SyncIntervalMinutes
	if interval == 0 {
		interval = 60
	}
	if interval < 5 || interval > 10080 {
		return nil, errors.New("同步周期无效")
	}
	encrypted, err := s.cipher.Encrypt(input.Credential)
	if err != nil {
		return nil, err
	}
	config := input.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	if !json.Valid(config) {
		return nil, errors.New("接入源配置无效")
	}
	next := s.now().Add(time.Duration(interval) * time.Minute)
	source := &Source{ProjectID: input.ProjectID, Provider: input.Provider, Name: input.Name, Region: input.Region, EncryptedCredential: encrypted, CredentialHint: "已安全配置", Config: config, Enabled: true, SyncIntervalMinutes: interval, NextSyncAt: &next}
	if err := s.repository.CreateSource(ctx, source); err != nil {
		return nil, err
	}
	_ = s.repository.CreateAudit(ctx, source.ProjectID, "source.created", "resource_source", source.ID, map[string]any{"provider": source.Provider, "name": source.Name})
	return source, nil
}

// UpdateSource 更新项目内接入源；只有显式提供凭证时才替换已有密文。
func (s *Service) UpdateSource(ctx context.Context, projectID, sourceID uint64, input UpdateSourceInput) (*Source, error) {
	source, err := s.FindSourceForProject(ctx, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	if input.Name == "" || input.SyncIntervalMinutes < 5 || input.SyncIntervalMinutes > 10080 {
		return nil, errors.New("接入源参数无效")
	}
	config := input.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	if !json.Valid(config) || (len(input.Credential) > 0 && !json.Valid(input.Credential)) {
		return nil, errors.New("接入源参数无效")
	}
	updates := map[string]any{"name": input.Name, "region": input.Region, "config": config, "enabled": input.Enabled, "sync_interval_minutes": input.SyncIntervalMinutes}
	if len(input.Credential) > 0 {
		encrypted, encryptErr := s.cipher.Encrypt(input.Credential)
		if encryptErr != nil {
			return nil, encryptErr
		}
		updates["encrypted_credential"] = encrypted
		updates["credential_hint"] = "已安全配置"
	}
	if err := s.repository.UpdateSource(ctx, source, updates); err != nil {
		return nil, err
	}
	_ = s.repository.CreateAudit(ctx, projectID, "source.updated", "resource_source", sourceID, map[string]any{"provider": source.Provider, "name": input.Name, "credential_replaced": len(input.Credential) > 0})
	return s.repository.FindSource(ctx, sourceID)
}

// DeleteSource 删除项目内接入源，但审计日志不通过外键级联删除。
func (s *Service) DeleteSource(ctx context.Context, projectID, sourceID uint64) error {
	source, err := s.FindSourceForProject(ctx, projectID, sourceID)
	if err != nil {
		return err
	}
	if err := s.repository.CreateAudit(ctx, projectID, "source.deleted", "resource_source", sourceID, map[string]any{"provider": source.Provider, "name": source.Name}); err != nil {
		return err
	}
	return s.repository.DeleteSource(ctx, source)
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
	if err != nil || source.ProjectID != projectID {
		return nil, gorm.ErrRecordNotFound
	}
	return source, nil
}

// ListResources 提供受项目边界限制的分页资源查询。
func (s *Service) ListResources(ctx context.Context, projectID uint64, provider, resourceType, lifecycle string, page, pageSize int) ([]Resource, int64, error) {
	if projectID == 0 {
		return nil, 0, errors.New("项目参数无效")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	return s.repository.ListResources(ctx, projectID, provider, resourceType, lifecycle, (page-1)*pageSize, pageSize)
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
	return provider == ProviderAliyun || provider == ProviderAWS || provider == ProviderKubernetes
}

// Sync 执行一次接入源同步，单类失败不会影响其他成功类型。
func (s *Service) Sync(ctx context.Context, sourceID uint64, trigger string, collector Collector) (*SyncJob, error) {
	// TryLock 只拒绝同一接入源的重入，不阻塞其他接入源并行采集。
	lockValue, _ := s.sourceLocks.LoadOrStore(sourceID, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	if !lock.TryLock() {
		return nil, ErrSyncAlreadyRunning
	}
	defer lock.Unlock()

	source, err := s.repository.FindSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	job := &SyncJob{ProjectID: source.ProjectID, SourceID: source.ID, Status: "running", Trigger: trigger, StartedAt: now, ErrorSummary: ""}
	if err := s.repository.CreateJob(ctx, job); err != nil {
		return nil, err
	}
	credential, err := s.cipher.Decrypt(source.EncryptedCredential)
	if err != nil {
		return s.finishFailed(ctx, job, "凭证解密失败", err)
	}
	results, collectErr := collector.Collect(ctx, *source, credential)
	for index := range credential {
		credential[index] = 0
	}
	if collectErr != nil {
		return s.finishFailed(ctx, job, "接入源认证或采集失败", collectErr)
	}
	statistics := map[string]map[string]int{}
	failed := 0
	for _, result := range results {
		if result.Err != nil {
			failed++
			statistics[result.ResourceType] = map[string]int{"added": 0, "updated": 0, "restored": 0, "lost": 0, "deleted": 0, "failed": 1}
			continue
		}
		counts, err := s.applyType(ctx, *source, result.ResourceType, result.Snapshots, now)
		if err != nil {
			return s.finishFailed(ctx, job, "资源写入失败", err)
		}
		statistics[result.ResourceType] = counts
	}
	job.Status = "success"
	if failed > 0 {
		job.Status = "partial_success"
	}
	job.Statistics, _ = json.Marshal(statistics)
	finished := s.now()
	job.FinishedAt = &finished
	source.LastSyncAt = &finished
	next := finished.Add(time.Duration(source.SyncIntervalMinutes) * time.Minute)
	source.NextSyncAt = &next
	if err := s.repository.SaveJob(ctx, job); err != nil {
		return nil, err
	}
	if err := s.repository.UpdateSourceSchedule(ctx, source); err != nil {
		return nil, err
	}
	_ = s.repository.CreateAudit(ctx, source.ProjectID, "source.synced", "resource_source", source.ID, map[string]any{"trigger": trigger, "status": job.Status, "statistics": statistics})
	return job, nil
}

// finishFailed 只记录脱敏摘要，不把底层错误或凭证内容写入任务。
func (s *Service) finishFailed(ctx context.Context, job *SyncJob, summary string, cause error) (*SyncJob, error) {
	job.Status = "failed"
	job.ErrorSummary = summary
	finished := s.now()
	job.FinishedAt = &finished
	if err := s.repository.SaveJob(ctx, job); err != nil {
		return nil, err
	}
	if errors.Is(cause, ErrAuthenticationFailed) {
		return job, nil
	}
	return job, cause
}

// applyType 原子写入一个成功资源类型，并只对该类型执行缺失判定。
func (s *Service) applyType(ctx context.Context, source Source, resourceType string, snapshots []Snapshot, now time.Time) (map[string]int, error) {
	counts := map[string]int{"added": 0, "updated": 0, "restored": 0, "lost": 0, "deleted": 0, "failed": 0}
	seen := make([]string, 0, len(snapshots))
	err := s.repository.Transaction(ctx, func(tx *gorm.DB) error {
		for _, snapshot := range snapshots {
			seen = append(seen, snapshot.ExternalID)
			var existing Resource
			lookupErr := tx.Where("source_id = ? AND resource_type = ? AND external_id = ?", source.ID, resourceType, snapshot.ExternalID).First(&existing).Error
			action := "resource.updated"
			if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
				counts["added"]++
				action = "resource.created"
			} else if lookupErr != nil {
				return lookupErr
			} else if existing.LifecycleStatus == LifecycleLost {
				counts["restored"]++
				action = "resource.restored"
			} else {
				counts["updated"]++
			}
			value := Resource{ProjectID: source.ProjectID, SourceID: source.ID, Provider: source.Provider, ResourceType: resourceType, ExternalID: snapshot.ExternalID, Name: snapshot.Name, Region: snapshot.Region, Zone: snapshot.Zone, CloudStatus: snapshot.CloudStatus, LifecycleStatus: LifecycleActive, RawAttributes: snapshot.RawAttributes, FirstSeenAt: now, LastSeenAt: now}
			updates := map[string]any{"name": value.Name, "region": value.Region, "zone": value.Zone, "cloud_status": value.CloudStatus, "lifecycle_status": LifecycleActive, "raw_attributes": value.RawAttributes, "last_seen_at": now, "missing_since": nil}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_id"}, {Name: "resource_type"}, {Name: "external_id"}}, DoUpdates: clause.Assignments(updates)}).Create(&value).Error; err != nil {
				return err
			}
			var persisted Resource
			if err := tx.Where("source_id = ? AND resource_type = ? AND external_id = ?", source.ID, resourceType, snapshot.ExternalID).First(&persisted).Error; err != nil {
				return err
			}
			projectID := source.ProjectID
			detail, _ := json.Marshal(map[string]any{"source_id": source.ID, "provider": source.Provider, "resource_type": resourceType, "external_id": snapshot.ExternalID})
			if err := tx.Create(&AuditLog{ProjectID: &projectID, Action: action, ResourceType: resourceType, ResourceID: snapshot.ExternalID, Detail: detail}).Error; err != nil {
				return err
			}
			if err := tx.Where("resource_id = ?", persisted.ID).Delete(&Endpoint{}).Error; err != nil {
				return err
			}
			for _, endpoint := range snapshot.Endpoints {
				ips, _ := json.Marshal(endpoint.ResolvedIPs)
				if err := tx.Create(&Endpoint{ResourceID: persisted.ID, Kind: endpoint.Kind, Address: endpoint.Address, Port: endpoint.Port, Protocol: endpoint.Protocol, ResolvedIPs: ips}).Error; err != nil {
					return err
				}
			}
		}
		query := tx.Model(&Resource{}).Where("resources.source_id = ? AND resources.resource_type = ? AND resources.lifecycle_status = ?", source.ID, resourceType, LifecycleActive)
		if len(seen) > 0 {
			query = query.Where("resources.external_id NOT IN ?", seen)
		}
		var missing []Resource
		if err := query.Find(&missing).Error; err != nil {
			return err
		}
		ids := make([]uint64, 0, len(missing))
		for _, value := range missing {
			ids = append(ids, value.ID)
		}
		result := tx.Model(&Resource{}).Where("id IN ?", ids).Updates(map[string]any{"lifecycle_status": LifecycleLost, "missing_since": now})
		counts["lost"] = int(result.RowsAffected)
		if result.Error != nil {
			return result.Error
		}
		for _, value := range missing {
			projectID := source.ProjectID
			detail, _ := json.Marshal(map[string]any{"source_id": source.ID, "provider": source.Provider, "resource_type": resourceType})
			if err := tx.Create(&AuditLog{ProjectID: &projectID, Action: "resource.lost", ResourceType: resourceType, ResourceID: value.ExternalID, Detail: detail}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return counts, err
}

// PurgeLostResources 物理删除连续失联满 72 小时的资源。
func (s *Service) PurgeLostResources(ctx context.Context) (int64, error) {
	return s.repository.PurgeLostBefore(ctx, s.now().Add(-72*time.Hour))
}

// SyncDueSources 并行执行所有已到期接入源；同源互斥仍由 Sync 统一保证。
func (s *Service) SyncDueSources(ctx context.Context, collectors map[string]Collector) {
	sources, err := s.repository.ListDueSources(ctx, s.now())
	if err != nil {
		return
	}
	var group sync.WaitGroup
	for index := range sources {
		source := sources[index]
		collector := collectors[source.Provider]
		if collector == nil {
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			_, _ = s.Sync(ctx, source.ID, "scheduled", collector)
		}()
	}
	group.Wait()
}

// StartScheduler 启动资源后台调度，并在服务上下文结束时自动退出。
func (s *Service) StartScheduler(ctx context.Context, collectors map[string]Collector) {
	go func() {
		// 服务启动后立即补跑已到期任务，再按分钟检查并清理过期失联资源。
		s.SyncDueSources(ctx, collectors)
		_, _ = s.PurgeLostResources(ctx)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.SyncDueSources(ctx, collectors)
				_, _ = s.PurgeLostResources(ctx)
			}
		}
	}()
}
