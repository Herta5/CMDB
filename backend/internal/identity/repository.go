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
