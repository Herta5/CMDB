// 本文件定义跨平台共享的接入源、资源、端点和同步任务模型。
package resource

import (
	"encoding/json"
	"time"
)

const (
	// ProviderAliyun 表示阿里云接入模块。
	ProviderAliyun = "aliyun"
	// ProviderAWS 表示 AWS 接入模块。
	ProviderAWS = "aws"
	// LifecycleActive 表示资源在最近一次成功采集中存在。
	LifecycleActive = "active"
	// LifecycleLost 表示资源在最近一次成功采集中缺失但仍处于保留期。
	LifecycleLost = "lost"
)

// Source 表示归属于唯一业务项目的平台接入配置，密文禁止序列化。
type Source struct {
	ID                  uint64          `gorm:"primaryKey" json:"id"`
	ProjectID           uint64          `gorm:"not null;index" json:"project_id"`
	Provider            string          `gorm:"size:32;not null" json:"provider"`
	Name                string          `gorm:"size:128;not null" json:"name"`
	Region              string          `gorm:"size:128;not null" json:"region"`
	EncryptedCredential string          `gorm:"type:text;not null" json:"-"`
	CredentialHint      string          `gorm:"size:128;not null" json:"credential_hint"`
	Config              json.RawMessage `gorm:"type:json" json:"config"`
	Enabled             bool            `gorm:"not null" json:"enabled"`
	SyncIntervalMinutes int             `gorm:"not null" json:"sync_interval_minutes"`
	LastSyncAt          *time.Time      `json:"last_sync_at"`
	NextSyncAt          *time.Time      `json:"next_sync_at"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

// TableName 将接入源映射到避免歧义的资源接入源表。
func (Source) TableName() string { return "resource_sources" }

// Resource 以接入源、类型和云端唯一标识保持跨同步幂等。
type Resource struct {
	ID              uint64          `gorm:"primaryKey" json:"id"`
	ProjectID       uint64          `gorm:"not null;index" json:"project_id"`
	SourceID        uint64          `gorm:"not null;uniqueIndex:uk_resource_identity" json:"source_id"`
	Provider        string          `gorm:"size:32;not null;index" json:"provider"`
	ResourceType    string          `gorm:"size:64;not null;uniqueIndex:uk_resource_identity" json:"resource_type"`
	ExternalID      string          `gorm:"size:255;not null;uniqueIndex:uk_resource_identity" json:"external_id"`
	Name            string          `gorm:"size:255;not null" json:"name"`
	Region          string          `gorm:"size:128;not null" json:"region"`
	Zone            string          `gorm:"size:128;not null" json:"zone"`
	CloudStatus     string          `gorm:"size:64;not null" json:"cloud_status"`
	LifecycleStatus string          `gorm:"size:32;not null;index" json:"lifecycle_status"`
	RawAttributes   json.RawMessage `gorm:"type:json" json:"raw_attributes"`
	FirstSeenAt     time.Time       `json:"first_seen_at"`
	LastSeenAt      time.Time       `json:"last_seen_at"`
	MissingSince    *time.Time      `json:"missing_since"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	Endpoints       []Endpoint      `gorm:"foreignKey:ResourceID" json:"endpoints"`
}

// TableName 将访问端点映射到统一资源端点表。
func (Endpoint) TableName() string { return "resource_endpoints" }

// Endpoint 保存资源原始访问地址及动态解析结果，不能把解析 IP 当作固定属性。
type Endpoint struct {
	ID          uint64          `gorm:"primaryKey" json:"id"`
	ResourceID  uint64          `gorm:"not null;index" json:"resource_id"`
	Kind        string          `gorm:"size:32;not null" json:"kind"`
	Address     string          `gorm:"size:512;not null" json:"address"`
	Port        int             `json:"port"`
	Protocol    string          `gorm:"size:32;not null" json:"protocol"`
	ResolvedIPs json.RawMessage `gorm:"type:json" json:"resolved_ips"`
	ResolvedAt  *time.Time      `json:"resolved_at"`
}

// SyncJob 记录一次同步的状态和脱敏统计，不保存凭证或完整请求响应。
type SyncJob struct {
	ID            uint64          `gorm:"primaryKey" json:"id"`
	ProjectID     uint64          `gorm:"not null;index" json:"project_id"`
	SourceID      uint64          `gorm:"not null;index" json:"source_id"`
	PreviousJobID *uint64         `gorm:"index" json:"previous_job_id"`
	Status        string          `gorm:"size:32;not null" json:"status"`
	Trigger       string          `gorm:"size:32;not null" json:"trigger"`
	Statistics    json.RawMessage `gorm:"type:json" json:"statistics"`
	ErrorSummary  string          `gorm:"size:500;not null" json:"error_summary"`
	StartedAt     time.Time       `json:"started_at"`
	FinishedAt    *time.Time      `json:"finished_at"`
}

// AuditLog 保存不含敏感信息的资源操作轨迹，资源物理删除后仍独立保留。
type AuditLog struct {
	ID           uint64          `gorm:"primaryKey" json:"id"`
	ActorID      *uint64         `json:"actor_id"`
	ProjectID    *uint64         `gorm:"index" json:"project_id"`
	Action       string          `gorm:"size:128;not null" json:"action"`
	ResourceType string          `gorm:"size:64;not null" json:"resource_type"`
	ResourceID   string          `gorm:"size:255" json:"resource_id"`
	Detail       json.RawMessage `gorm:"type:json" json:"detail"`
	RequestIP    string          `gorm:"size:45" json:"request_ip"`
	CreatedAt    time.Time       `json:"created_at"`
}

// TableName 复用平台基础迁移中的长期审计表。
func (AuditLog) TableName() string { return "audit_logs" }
