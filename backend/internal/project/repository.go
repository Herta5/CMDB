// 本文件定义项目领域的持久化边界，并提供唯一的 GORM 数据访问实现。
package project

import (
	"context"

	"gorm.io/gorm"
)

// Repository 隔离项目领域对数据库的依赖，避免 HTTP 层直接读写项目和成员关系。
type Repository interface {
	Create(ctx context.Context, project *Project) error
	FindByID(ctx context.Context, id uint64) (*Project, error)
	FindByCode(ctx context.Context, code string) (*Project, error)
	Update(ctx context.Context, project *Project) error
	Delete(ctx context.Context, id uint64) error
	List(ctx context.Context) ([]Project, error)
	ListForUser(ctx context.Context, userID uint64) ([]Project, error)
}

// gormRepository 是 Repository 的 GORM 实现，所有查询都明确落在新版项目表。
type gormRepository struct {
	db *gorm.DB
}

// NewRepository 创建项目仓储，数据库连接必须由平台层统一装配。
func NewRepository(db *gorm.DB) Repository {
	return &gormRepository{db: db}
}

// Create 写入项目，数据库唯一索引用于并发场景下最终保障项目编码唯一。
func (r *gormRepository) Create(ctx context.Context, project *Project) error {
	return r.db.WithContext(ctx).Create(project).Error
}

// FindByID 按项目主键查询，调用方负责将未找到转换为项目领域错误。
func (r *gormRepository) FindByID(ctx context.Context, id uint64) (*Project, error) {
	var project Project
	if err := r.db.WithContext(ctx).First(&project, id).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

// FindByCode 按全局唯一项目编码查询，用于在创建前提供稳定的重复编码错误。
func (r *gormRepository) FindByCode(ctx context.Context, code string) (*Project, error) {
	var project Project
	if err := r.db.WithContext(ctx).Where("code = ?", code).First(&project).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

// Update 保存项目可变属性；编码不在更新输入中，因此不能被此仓储路径变更。
func (r *gormRepository) Update(ctx context.Context, project *Project) error {
	return r.db.WithContext(ctx).Save(project).Error
}

// Delete 物理删除项目，关联成员关系由数据库外键级联清理，审计记录由外键外的数值保留。
func (r *gormRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&Project{}, id).Error
}

// List 返回全部项目，仅供系统管理员的全局项目视图使用。
func (r *gormRepository) List(ctx context.Context) ([]Project, error) {
	var projects []Project
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&projects).Error; err != nil {
		return nil, err
	}
	return projects, nil
}

// ListForUser 仅返回用户确实拥有成员关系的项目，避免普通用户枚举其他项目。
func (r *gormRepository) ListForUser(ctx context.Context, userID uint64) ([]Project, error) {
	var projects []Project
	if err := r.db.WithContext(ctx).
		Joins("JOIN project_members ON project_members.project_id = projects.id").
		Where("project_members.user_id = ?", userID).
		Order("projects.id ASC").
		Find(&projects).Error; err != nil {
		return nil, err
	}
	return projects, nil
}
