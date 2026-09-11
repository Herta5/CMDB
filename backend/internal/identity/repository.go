// 本文件定义用户持久化边界，并以 GORM 实现新版 users 表的最小访问能力。
package identity

import (
	"context"
	"errors"
	"time"

	"cmdb/internal/audit"
	"gorm.io/gorm"
)

var (
	// ErrProjectPermissionInvalid 表示授权指向不存在的项目，整个用户事务必须回滚。
	ErrProjectPermissionInvalid = errors.New("项目权限无效")
)

// UserRepository 隔离身份域对存储实现的依赖，供后续认证与项目授权流程查询用户。
type UserRepository interface {
	Create(ctx context.Context, user *User) error
	CreateWithPermissions(ctx context.Context, user *User, permissions []ProjectPermission) error
	Delete(ctx context.Context, id uint64) error
	FindByID(ctx context.Context, id uint64) (*User, error)
	FindByUsername(ctx context.Context, username string) (*User, error)
	List(ctx context.Context) ([]User, error)
	Update(ctx context.Context, user *User) error
	UpdateWithPermissions(ctx context.Context, user *User, permissions []ProjectPermission) error
	UpdateStatus(ctx context.Context, id uint64, status string) error
}

// auditTransactionUserRepository 是启用审计时仓储必须实现的原子写入能力。
// 事务回调获得绑定同一数据库事务的用户仓储和审计记录器。
type auditTransactionUserRepository interface {
	WithAuditTransaction(ctx context.Context, operation func(UserRepository, audit.Recorder) error) error
}

// Update 只写入系统管理员允许维护的资料、角色、状态和密码哈希，用户名保持不可变。
func (r *gormUserRepository) Update(ctx context.Context, user *User) error {
	return updateUser(r.db.WithContext(ctx), user)
}

// updateUser 在指定数据库会话中更新用户，便于复用同一事务边界。
func updateUser(db *gorm.DB, user *User) error {
	result := db.Model(&User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"display_name":  user.DisplayName,
		"email":         user.Email,
		"global_role":   user.GlobalRole,
		"status":        user.Status,
		"password_hash": user.PasswordHash,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// MySQL 对值未变化的更新可能报告零行，需再查存在性，避免把幂等保存误判为用户不存在。
		var count int64
		if err := db.Model(&User{}).Where("id = ?", user.ID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return gorm.ErrRecordNotFound
		}
	}
	return nil
}

// List 按创建顺序返回用户，调用方必须在 HTTP 边界完成系统管理员授权。
func (r *gormUserRepository) List(ctx context.Context) ([]User, error) {
	var users []User
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	if err := r.loadPermissions(ctx, users); err != nil {
		return nil, err
	}
	return users, nil
}

// UpdateStatus 只修改用户启停状态，不允许借此变更角色或认证资料。
func (r *gormUserRepository) UpdateStatus(ctx context.Context, id uint64, status string) error {
	result := r.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// gormUserRepository 是 UserRepository 的 GORM 实现，只操作新版 users 表。
type gormUserRepository struct {
	db *gorm.DB
}

// NewUserRepository 创建用户仓储；传入的数据库连接由平台层统一管理。
func NewUserRepository(db *gorm.DB) UserRepository {
	return &gormUserRepository{db: db}
}

// WithAuditTransaction 确保用户、项目权限和审计日志在同一事务中提交或回滚。
func (r *gormUserRepository) WithAuditTransaction(ctx context.Context, operation func(UserRepository, audit.Recorder) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return operation(&gormUserRepository{db: tx}, audit.NewRepository(tx))
	})
}

// Create 写入新用户，由数据库唯一索引保证用户名在全局范围内唯一。
func (r *gormUserRepository) Create(ctx context.Context, user *User) error {
	return normalizeUserWriteError(r.db.WithContext(ctx).Create(user).Error)
}

// CreateWithPermissions 在单一事务中创建用户和全部项目成员关系。
func (r *gormUserRepository) CreateWithPermissions(ctx context.Context, user *User, permissions []ProjectPermission) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return normalizeUserWriteError(err)
		}
		return replacePermissions(tx, user.ID, permissions)
	})
}

// normalizeUserWriteError 将数据库唯一约束转换为稳定业务错误，覆盖并发创建绕过预检查的时序。
func normalizeUserWriteError(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrDuplicateUsername
	}
	return err
}

// Delete 物理删除用户；项目成员关系由数据库外键级联清理，项目负责人自动置空。
func (r *gormUserRepository) Delete(ctx context.Context, id uint64) error {
	result := r.db.WithContext(ctx).Delete(&User{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateWithPermissions 使用全量替换语义原子保存用户和项目权限。
func (r *gormUserRepository) UpdateWithPermissions(ctx context.Context, user *User, permissions []ProjectPermission) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := updateUser(tx, user); err != nil {
			return err
		}
		return replacePermissions(tx, user.ID, permissions)
	})
}

// replacePermissions 先验证所有项目再替换关系，防止无效项目留下部分写入。
func replacePermissions(tx *gorm.DB, userID uint64, permissions []ProjectPermission) error {
	if len(permissions) > 0 {
		ids := make([]uint64, 0, len(permissions))
		for _, permission := range permissions {
			ids = append(ids, permission.ProjectID)
		}
		var count int64
		if err := tx.Table("projects").Where("id IN ?", ids).Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(ids)) {
			return ErrProjectPermissionInvalid
		}
	}
	if err := tx.Table("project_members").Where("user_id = ?", userID).Delete(nil).Error; err != nil {
		return err
	}
	// 成员表的时间字段为非空，使用同一时刻保持一次授权的审计语义一致。
	now := time.Now()
	for _, permission := range permissions {
		row := map[string]any{"project_id": permission.ProjectID, "user_id": userID, "role": permission.Role, "created_at": now, "updated_at": now}
		if err := tx.Table("project_members").Create(row).Error; err != nil {
			return err
		}
	}
	return nil
}

// loadPermissions 批量加载用户的项目角色和项目名称，避免列表 N+1 查询。
func (r *gormUserRepository) loadPermissions(ctx context.Context, users []User) error {
	if len(users) == 0 {
		return nil
	}
	ids := make([]uint64, 0, len(users))
	indexes := make(map[uint64]int, len(users))
	for index := range users {
		ids = append(ids, users[index].ID)
		indexes[users[index].ID] = index
	}
	var rows []struct {
		UserID      uint64 `gorm:"column:user_id"`
		ProjectID   uint64 `gorm:"column:project_id"`
		ProjectName string `gorm:"column:project_name"`
		Role        string `gorm:"column:role"`
	}
	if err := r.db.WithContext(ctx).Table("project_members AS pm").Select("pm.user_id, pm.project_id, projects.name AS project_name, pm.role").Joins("JOIN projects ON projects.id = pm.project_id").Where("pm.user_id IN ?", ids).Order("pm.project_id ASC").Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		index := indexes[row.UserID]
		users[index].ProjectPermissions = append(users[index].ProjectPermissions, ProjectPermission{ProjectID: row.ProjectID, ProjectName: row.ProjectName, Role: row.Role})
	}
	return nil
}

// FindByID 按全局用户 ID 查询用户，调用方应自行处理记录不存在的情形。
func (r *gormUserRepository) FindByID(ctx context.Context, id uint64) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, err
	}
	users := []User{user}
	if err := r.loadPermissions(ctx, users); err != nil {
		return nil, err
	}
	return &users[0], nil
}

// FindByUsername 按登录名查询完整用户资料，认证和公开资料读取均需保留项目授权。
func (r *gormUserRepository) FindByUsername(ctx context.Context, username string) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	users := []User{user}
	if err := r.loadPermissions(ctx, users); err != nil {
		return nil, err
	}
	return &users[0], nil
}
