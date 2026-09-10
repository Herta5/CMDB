// 本文件定义 CMDB 业务项目及其成员角色模型，项目是资源归属和权限隔离的最高边界。
package project

import (
	"time"

	"cmdb/internal/identity"
)

const (
	// ProjectStatusEnabled 表示项目可接入云资源并供其成员访问。
	ProjectStatusEnabled = "enabled"
	// ProjectStatusDisabled 表示项目暂时不可用，但保留其资源归属和审计边界。
	ProjectStatusDisabled = "disabled"

	// MemberRoleProjectAdmin 表示可管理本项目成员关系的项目管理员。
	MemberRoleProjectAdmin = "project_admin"
	// MemberRoleMember 表示项目普通成员可按后续授权访问项目资源。
	MemberRoleMember = "member"
)

// Project 表示业务项目；Code 是创建后永久稳定的归属标识，禁止通过更新接口变更。
type Project struct {
	ID          uint64         `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Code        string         `gorm:"column:code;size:64;not null;uniqueIndex:uk_projects_code" json:"code"`
	Name        string         `gorm:"column:name;size:128;not null" json:"name"`
	Description string         `gorm:"column:description;size:500;not null" json:"description"`
	Status      string         `gorm:"column:status;size:32;not null" json:"status"`
	OwnerUserID *uint64        `gorm:"column:owner_user_id" json:"owner_user_id"`
	OwnerUser   *identity.User `gorm:"foreignKey:OwnerUserID;constraint:OnDelete:SET NULL" json:"-"`
	CreatedAt   time.Time      `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;not null" json:"updated_at"`
	// CurrentRole 是项目列表针对当前普通用户附加的角色，不属于项目持久化字段。
	CurrentRole string `gorm:"column:current_role;->;-:migration" json:"current_role,omitempty"`
}

// TableName 将业务项目明确映射到 CMDB 的 projects 表。
func (Project) TableName() string {
	return "projects"
}

// MemberRole 表示用户在业务项目中的成员关系；项目内角色不能混入用户全局角色。
type MemberRole struct {
	ID        uint64         `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ProjectID uint64         `gorm:"column:project_id;not null;uniqueIndex:uk_project_members_project_user" json:"project_id"`
	UserID    uint64         `gorm:"column:user_id;not null;uniqueIndex:uk_project_members_project_user" json:"user_id"`
	Role      string         `gorm:"column:role;size:32;not null" json:"role"`
	Project   *Project       `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE" json:"-"`
	User      *identity.User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	CreatedAt time.Time      `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"column:updated_at;not null" json:"updated_at"`
}

// TableName 将成员关系映射到共享的 project_members 表，为后续项目角色鉴权提供唯一数据源。
func (MemberRole) TableName() string {
	return "project_members"
}
