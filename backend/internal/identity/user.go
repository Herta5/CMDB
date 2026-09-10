// 本文件定义 CMDB 新版身份域的用户模型和全局角色边界。
package identity

import "time"

const (
	// UserClaimsContextKey 是认证中间件保存已验证身份声明的上下文键，项目权限中间件据此读取当前用户。
	UserClaimsContextKey = "cmdb.identity.user_claims"

	// GlobalRoleSystemAdmin 表示拥有全局运维权限的系统管理员。
	GlobalRoleSystemAdmin = "system_admin"
	// GlobalRoleUser 表示仅能通过项目成员关系访问资源的普通用户。
	GlobalRoleUser = "user"
)

// User 表示全局身份；项目内权限由 project_members 单独约束，不能混入用户全局角色。
type User struct {
	ID           uint64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Username     string     `gorm:"column:username;size:64;not null" json:"username"`
	PasswordHash string     `gorm:"column:password_hash;size:255;not null" json:"-"`
	DisplayName  string     `gorm:"column:display_name;size:128;not null" json:"display_name"`
	Email        string     `gorm:"column:email;size:255" json:"email"`
	GlobalRole   string     `gorm:"column:global_role;size:32;not null" json:"global_role"`
	Status       string     `gorm:"column:status;size:32;not null" json:"status"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at" json:"last_login_at"`
	CreatedAt    time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
	// ProjectPermissions 由仓储通过共享成员表加载，不是 users 表的直接列。
	ProjectPermissions []ProjectPermission `gorm:"-" json:"project_permissions"`
}

// ProjectPermission 表示用户在某个业务项目中的角色，项目名称仅用于展示。
type ProjectPermission struct {
	ProjectID   uint64 `json:"project_id"`
	ProjectName string `json:"project_name"`
	Role        string `json:"role"`
}

// TableName 将身份域用户明确映射到 CMDB 的 users 表。
func (User) TableName() string {
	return "users"
}
