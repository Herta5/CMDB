// 本文件承载项目创建、更新、查询和删除的领域规则，不允许调用方绕过项目隔离边界。
package project

import (
	"context"
	"errors"
	"strings"

	"github-cmdb/internal/identity"
	"gorm.io/gorm"
)

var (
	// ErrDuplicateCode 表示项目编码已被占用，客户端可据此提示用户使用其他稳定编码。
	ErrDuplicateCode = errors.New("项目编码已存在")
	// ErrProjectNotFound 表示目标项目不存在；HTTP 层必须先完成权限检查以免对普通用户泄露存在性。
	ErrProjectNotFound = errors.New("项目不存在")
	// ErrInvalidProjectInput 表示名称或编码为空等不满足项目最小业务约束的输入。
	ErrInvalidProjectInput = errors.New("项目参数无效")
	// ErrInvalidProjectStatus 表示项目状态不属于已定义的启用或停用集合。
	ErrInvalidProjectStatus = errors.New("项目状态无效")
	// ErrProjectRepositoryUnavailable 表示服务未被正确装配，不能继续执行项目操作。
	ErrProjectRepositoryUnavailable = errors.New("项目仓储不可用")
)

// CreateInput 是创建项目所需的可写字段；Code 只在此处出现，以保证创建后不可修改。
type CreateInput struct {
	Code        string
	Name        string
	Description string
	OwnerUserID *uint64
}

// UpdateInput 仅包含允许修改的项目资料，故意不提供 Code 字段以维持资源归属标识稳定。
type UpdateInput struct {
	Name        string
	Description string
	Status      string
	OwnerUserID *uint64
}

// Service 协调项目仓储与领域规则，HTTP 授权仍由边界层根据当前 JWT 声明执行。
type Service struct {
	repository Repository
}

// NewService 创建项目领域服务。
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// Create 创建默认启用的业务项目，并在写入前返回稳定的重复编码错误。
func (s *Service) Create(ctx context.Context, input CreateInput) (*Project, error) {
	if s.repository == nil {
		return nil, ErrProjectRepositoryUnavailable
	}
	if strings.TrimSpace(input.Code) == "" || strings.TrimSpace(input.Name) == "" {
		return nil, ErrInvalidProjectInput
	}
	if _, err := s.repository.FindByCode(ctx, input.Code); err == nil {
		return nil, ErrDuplicateCode
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	project := &Project{
		Code:        input.Code,
		Name:        input.Name,
		Description: input.Description,
		Status:      ProjectStatusEnabled,
		OwnerUserID: input.OwnerUserID,
	}
	if err := s.repository.Create(ctx, project); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, ErrDuplicateCode
		}
		return nil, err
	}
	return project, nil
}

// Update 修改项目的可变资料；项目编码不参与输入，因此不会因普通更新破坏资源归属。
func (s *Service) Update(ctx context.Context, id uint64, input UpdateInput) (*Project, error) {
	if s.repository == nil {
		return nil, ErrProjectRepositoryUnavailable
	}
	if id == 0 || strings.TrimSpace(input.Name) == "" {
		return nil, ErrInvalidProjectInput
	}
	if !validProjectStatus(input.Status) {
		return nil, ErrInvalidProjectStatus
	}
	project, err := s.repository.FindByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) || project == nil {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	project.Name = input.Name
	project.Description = input.Description
	project.Status = input.Status
	project.OwnerUserID = input.OwnerUserID
	if err := s.repository.Update(ctx, project); err != nil {
		return nil, err
	}
	return project, nil
}

// ListForUser 返回当前用户可见项目；系统管理员可管理全局项目，普通用户只能看到自己的成员项目。
func (s *Service) ListForUser(ctx context.Context, userID uint64, globalRole string) ([]Project, error) {
	if s.repository == nil {
		return nil, ErrProjectRepositoryUnavailable
	}
	if globalRole == identity.GlobalRoleSystemAdmin {
		return s.repository.List(ctx)
	}
	return s.repository.ListForUser(ctx, userID)
}

// Delete 删除项目本体；调用方必须在调用前确认当前用户具有系统管理员权限。
func (s *Service) Delete(ctx context.Context, id uint64) error {
	if s.repository == nil {
		return ErrProjectRepositoryUnavailable
	}
	if id == 0 {
		return ErrInvalidProjectInput
	}
	project, err := s.repository.FindByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) || project == nil {
		return ErrProjectNotFound
	}
	if err != nil {
		return err
	}
	return s.repository.Delete(ctx, id)
}

// validProjectStatus 集中维护项目允许的生命周期状态，避免接口写入未定义状态。
func validProjectStatus(status string) bool {
	return status == ProjectStatusEnabled || status == ProjectStatusDisabled
}
