// 本文件定义跨平台共享的接入源、三类云资产和同步任务模型。
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
	// AssetStatusActive 表示资源在最近一次完整成功采集中存在。
	AssetStatusActive = "active"
	// AssetStatusLost 表示资源在最近一次完整成功采集中缺失但仍处于 24 小时保留期。
	AssetStatusLost = "lost"
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

// AssetBase 是三类资产共享的持久化字段，业务逻辑通过它保持一致。
type AssetBase struct {
	ID            uint64          `gorm:"primaryKey" json:"id"`
	ProjectID     uint64          `gorm:"not null;index" json:"project_id"`
	SourceID      uint64          `gorm:"not null;uniqueIndex:,composite:resource_identity" json:"source_id"`
	Provider      string          `gorm:"size:32;not null;index" json:"provider"`
	ResourceType  string          `gorm:"size:64;not null;uniqueIndex:,composite:resource_identity" json:"resource_type"`
	ExternalID    string          `gorm:"size:255;not null;uniqueIndex:,composite:resource_identity" json:"external_id"`
	Name          string          `gorm:"size:255;not null" json:"name"`
	Region        string          `gorm:"size:128;not null" json:"region"`
	Zone          string          `gorm:"size:128;not null" json:"zone"`
	CloudStatus   string          `gorm:"size:64;not null" json:"cloud_status"`
	AssetStatus   string          `gorm:"size:32;not null;index" json:"asset_status"`
	RawAttributes json.RawMessage `gorm:"type:json" json:"raw_attributes"`
	FirstSeenAt   time.Time       `json:"first_seen_at"`
	LastSeenAt    time.Time       `json:"last_seen_at"`
	MissingSince  *time.Time      `json:"missing_since"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// Server 保存 ECS 和 EC2，网卡内外网 IP 直接归属于服务器资产。
type Server struct {
	AssetBase
	PrivateIPs json.RawMessage `gorm:"type:json" json:"private_ips"`
	PublicIPs  json.RawMessage `gorm:"type:json" json:"public_ips"`
}

// TableName 将服务器映射到独立资产表。
func (Server) TableName() string { return "resources_servers" }

// Database 保存两家云平台的 RDS 及其原始访问端点。
type Database struct {
	AssetBase
	Engine        string          `gorm:"size:64;not null" json:"engine"`
	EngineVersion string          `gorm:"size:64;not null" json:"engine_version"`
	Endpoints     json.RawMessage `gorm:"type:json" json:"endpoints"`
}

// TableName 将数据库映射到独立资产表。
func (Database) TableName() string { return "resources_databases" }

// LoadBalancer 保存负载均衡及其原始域名、端口和最近解析 IP。
type LoadBalancer struct {
	AssetBase
	NetworkType string          `gorm:"size:32;not null" json:"network_type"`
	Endpoints   json.RawMessage `gorm:"type:json" json:"endpoints"`
}

// TableName 将负载均衡映射到独立资产表。
func (LoadBalancer) TableName() string { return "resources_load_balancers" }

// Resource 是跨三张资产表返回给 API 的统一只读视图，不对应数据库表。
type Resource struct {
	AssetBase
	Endpoints []EndpointSnapshot `gorm:"-" json:"endpoints"`
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
