// 本文件定义用户持久化边界，并以 GORM 实现新版 users 表的最小访问能力。
package identity

import (
	"context"

	"gorm.io/gorm"
)

// UserRepository 隔离身份域对存储实现的依赖，供后续认证与项目授权流程查询用户。
type UserRepository interface {
	Create(ctx context.Context, user *User) error
	FindByID(ctx context.Context, id uint64) (*User, error)
	FindByUsername(ctx context.Context, username string) (*User, error)
	List(ctx context.Context) ([]User, error)
	UpdateStatus(ctx context.Context, id uint64, status string) error
}

// List 按创建顺序返回用户，调用方必须在 HTTP 边界完成系统管理员授权。
func (r *gormUserRepository) List(ctx context.Context) ([]User, error) {
	var users []User
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&users).Error; err != nil {
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

// Create 写入新用户，由数据库唯一索引保证用户名在全局范围内唯一。
func (r *gormUserRepository) Create(ctx context.Context, user *User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

// FindByID 按全局用户 ID 查询用户，调用方应自行处理记录不存在的情形。
func (r *gormUserRepository) FindByID(ctx context.Context, id uint64) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// FindByUsername 按登录名查询用户，用于后续认证时取得密码哈希而不返回给接口层。
func (r *gormUserRepository) FindByUsername(ctx context.Context, username string) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}
