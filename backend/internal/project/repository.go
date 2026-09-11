// 本文件定义项目领域的持久化边界，并提供唯一的 GORM 数据访问实现。
package project

import (
	"context"
	"time"

	"cmdb/internal/audit"
	"cmdb/internal/identity"
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
	FindUserByUsername(ctx context.Context, username string) (*identity.User, error)
	FindMemberRole(ctx context.Context, projectID, userID uint64) (*MemberRole, error)
	ListMembers(ctx context.Context, projectID uint64) ([]MemberRole, error)
	ListMemberCandidates(ctx context.Context) ([]identity.User, error)
	CreateMember(ctx context.Context, member *MemberRole) error
	UpdateMemberRole(ctx context.Context, projectID, userID uint64, role string) error
	DeleteMember(ctx context.Context, projectID, userID uint64) error
}

// auditTransactionRepository 是启用审计时项目仓储必须提供的共享事务能力。
type auditTransactionRepository interface {
	WithAuditTransaction(ctx context.Context, operation func(Repository, audit.Recorder) error) error
}

// gormRepository 是 Repository 的 GORM 实现，所有查询都明确落在新版项目表。
type gormRepository struct {
	db *gorm.DB
}

// NewRepository 创建项目仓储，数据库连接必须由平台层统一装配。
func NewRepository(db *gorm.DB) Repository {
	return &gormRepository{db: db}
}

// WithAuditTransaction 将项目、成员关系和审计日志绑定到同一数据库事务。
func (r *gormRepository) WithAuditTransaction(ctx context.Context, operation func(Repository, audit.Recorder) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return operation(&gormRepository{db: tx}, audit.NewRepository(tx))
	})
}

// Create 写入项目，数据库唯一索引用于并发场景下最终保障项目编码唯一。
func (r *gormRepository) Create(ctx context.Context, project *Project) error {
	return r.db.WithContext(ctx).Omit("OwnerUser").Create(project).Error
}

// FindByID 按项目主键查询，调用方负责将未找到转换为项目领域错误。
func (r *gormRepository) FindByID(ctx context.Context, id uint64) (*Project, error) {
	var project Project
	if err := r.db.WithContext(ctx).Preload("OwnerUser").First(&project, id).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

// FindByCode 按全局唯一项目编码查询，用于在创建前提供稳定的重复编码错误。
func (r *gormRepository) FindByCode(ctx context.Context, code string) (*Project, error) {
	var project Project
	if err := r.db.WithContext(ctx).Preload("OwnerUser").Where("code = ?", code).First(&project).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

// Update 只更新项目可变属性，并在零行受影响时返回未找到，绝不使用 Save 复活已被并发删除的项目。
func (r *gormRepository) Update(ctx context.Context, project *Project) error {
	updatedAt := time.Now()
	result := r.db.WithContext(ctx).Model(&Project{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"name":          project.Name,
		"description":   project.Description,
		"status":        project.Status,
		"owner_user_id": project.OwnerUserID,
		"updated_at":    updatedAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	project.UpdatedAt = updatedAt
	return nil
}

// Delete 物理删除项目，关联成员关系由数据库外键级联清理，审计记录由外键外的数值保留。
func (r *gormRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&Project{}, id).Error
}

// List 返回全部项目，仅供系统管理员的全局项目视图使用。
func (r *gormRepository) List(ctx context.Context) ([]Project, error) {
	var projects []Project
	if err := r.db.WithContext(ctx).Preload("OwnerUser").Order("id ASC").Find(&projects).Error; err != nil {
		return nil, err
	}
	return projects, nil
}

// ListForUser 仅返回用户确实拥有成员关系的项目，避免普通用户枚举其他项目。
func (r *gormRepository) ListForUser(ctx context.Context, userID uint64) ([]Project, error) {
	var projects []Project
	if err := r.db.WithContext(ctx).
		Preload("OwnerUser").
		Select("projects.*, project_members.role AS current_role").
		Joins("JOIN project_members ON project_members.project_id = projects.id").
		Where("project_members.user_id = ?", userID).
		Order("projects.id ASC").
		Find(&projects).Error; err != nil {
		return nil, err
	}
	return projects, nil
}

// FindUserByUsername 将公开用户名转换为内部关联，禁止回退到数字主键查询。
func (r *gormRepository) FindUserByUsername(ctx context.Context, username string) (*identity.User, error) {
	var user identity.User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// FindMemberRole 按项目和用户查询唯一成员关系，权限中间件不得改为先查询项目以免泄露项目存在性。
func (r *gormRepository) FindMemberRole(ctx context.Context, projectID, userID uint64) (*MemberRole, error) {
	var member MemberRole
	if err := r.db.WithContext(ctx).Preload("User").Where("project_id = ? AND user_id = ?", projectID, userID).First(&member).Error; err != nil {
		return nil, err
	}
	return &member, nil
}

// ListMembers 返回项目内全部成员关系，仅应由已完成项目权限校验的处理器调用。
func (r *gormRepository) ListMembers(ctx context.Context, projectID uint64) ([]MemberRole, error) {
	var members []MemberRole
	if err := r.db.WithContext(ctx).Preload("User").Where("project_id = ?", projectID).Order("user_id ASC").Find(&members).Error; err != nil {
		return nil, err
	}
	return members, nil
}

// ListMemberCandidates 返回可加入项目的启用用户，接口层只输出必要的公开身份字段。
func (r *gormRepository) ListMemberCandidates(ctx context.Context) ([]identity.User, error) {
	var users []identity.User
	if err := r.db.WithContext(ctx).Where("status = ?", "active").Order("username ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// CreateMember 写入项目成员关系，联合唯一索引负责并发情况下的一人一角色约束。
func (r *gormRepository) CreateMember(ctx context.Context, member *MemberRole) error {
	return r.db.WithContext(ctx).Omit("User").Create(member).Error
}

// UpdateMemberRole 只更新成员角色，零行受影响代表成员已被并发移除。
func (r *gormRepository) UpdateMemberRole(ctx context.Context, projectID, userID uint64, role string) error {
	result := r.db.WithContext(ctx).Model(&MemberRole{}).Where("project_id = ? AND user_id = ?", projectID, userID).Update("role", role)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteMember 删除指定成员关系；未命中也返回未找到，避免把并发删除误报为成功。
func (r *gormRepository) DeleteMember(ctx context.Context, projectID, userID uint64) error {
	result := r.db.WithContext(ctx).Where("project_id = ? AND user_id = ?", projectID, userID).Delete(&MemberRole{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
