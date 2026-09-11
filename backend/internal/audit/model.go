// Package audit 统一管理 CMDB 的长期审计记录、动作契约和查询模型。
package audit

import (
	"encoding/json"
	"time"
)

const (
	// ActionUserCreated 表示系统管理员创建用户。
	ActionUserCreated = "user.created"
	// ActionUserUpdated 表示系统管理员修改用户资料、角色、授权或密码。
	ActionUserUpdated = "user.updated"
	// ActionUserStatusChanged 表示系统管理员启用或停用用户。
	ActionUserStatusChanged = "user.status_changed"
	// ActionUserDeleted 为后续用户删除分支提供稳定的统一动作名称。
	ActionUserDeleted = "user.deleted"
	// ActionProjectCreated 表示系统管理员创建业务项目。
	ActionProjectCreated = "project.created"
	// ActionProjectUpdated 表示系统管理员修改业务项目。
	ActionProjectUpdated = "project.updated"
	// ActionProjectDeleted 表示系统管理员删除业务项目。
	ActionProjectDeleted = "project.deleted"
	// ActionProjectMemberAdded 表示为项目添加成员关系。
	ActionProjectMemberAdded = "project_member.added"
	// ActionProjectMemberRoleChanged 表示修改项目成员角色。
	ActionProjectMemberRoleChanged = "project_member.role_changed"
	// ActionProjectMemberRemoved 表示移除项目成员关系。
	ActionProjectMemberRemoved = "project_member.removed"
	// ActionSourceCreated 表示创建云接入源。
	ActionSourceCreated = "source.created"
	// ActionSourceUpdated 表示修改云接入源。
	ActionSourceUpdated = "source.updated"
	// ActionSourceDeleted 表示删除云接入源。
	ActionSourceDeleted = "source.deleted"
	// ActionSourceConnectionTested 表示完成一次云连接测试。
	ActionSourceConnectionTested = "source.connection_tested"
	// ActionSourceSynced 表示完成一次云资源同步。
	ActionSourceSynced = "source.synced"
	// ActionResourceCreated 表示首次发现云资源。
	ActionResourceCreated = "resource.created"
	// ActionResourceUpdated 表示刷新已有云资源属性。
	ActionResourceUpdated = "resource.updated"
	// ActionResourceRestored 表示失联资源重新出现。
	ActionResourceRestored = "resource.restored"
	// ActionResourceLost 表示完整成功采集后未发现原资源。
	ActionResourceLost = "resource.lost"
	// ActionResourceDeleted 表示资源连续失联满 24 小时后被物理删除。
	ActionResourceDeleted = "resource.deleted"
)

// Log 映射长期保留的 audit_logs 表，并附带查询时得到的公开显示名称。
type Log struct {
	ID               uint64          `gorm:"primaryKey" json:"id"`
	ActorID          *uint64         `json:"actor_id"`
	ActorUsername    string          `gorm:"->" json:"actor_username"`
	ActorDisplayName string          `gorm:"->" json:"actor_display_name"`
	ProjectID        *uint64         `gorm:"index" json:"project_id"`
	ProjectName      string          `gorm:"->" json:"project_name"`
	Action           string          `gorm:"size:128;not null" json:"action"`
	ResourceType     string          `gorm:"size:64;not null" json:"resource_type"`
	ResourceID       string          `gorm:"size:255" json:"resource_id"`
	Detail           json.RawMessage `gorm:"type:json" json:"detail"`
	RequestIP        string          `gorm:"size:45" json:"request_ip"`
	CreatedAt        time.Time       `json:"created_at"`
}

// TableName 明确复用平台初始化脚本中的长期审计表。
func (Log) TableName() string { return "audit_logs" }

// Entry 是业务领域写入审计的白名单输入，不允许传入完整请求或领域模型。
type Entry struct {
	ActorID      *uint64
	ProjectID    *uint64
	Action       string
	ResourceType string
	ResourceID   string
	Detail       map[string]any
	RequestIP    string
}

// Filter 表示审计页面允许使用的精确筛选及分页边界。
type Filter struct {
	ProjectID    *uint64
	Action       string
	ActorID      *uint64
	ResourceType string
	ResourceID   string
	StartAt      *time.Time
	EndAt        *time.Time
	Page         int
	PageSize     int
	SnapshotID   uint64
}

// Page 是审计查询的稳定分页响应。
type Page struct {
	Items    []Log `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	// SnapshotID 固定首次查询可见的最大审计标识，后续翻页不会被新记录推移。
	SnapshotID uint64 `json:"snapshot_id"`
}
