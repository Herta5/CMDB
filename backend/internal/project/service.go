// 本文件承载项目创建、更新、查询和删除的领域规则，不允许调用方绕过项目隔离边界。
package project

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"cmdb/internal/audit"
	"cmdb/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
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
	// ErrInvalidMemberInput 表示成员用户或项目内角色不满足最小权限约束。
	ErrInvalidMemberInput = errors.New("项目成员参数无效")
	// ErrMemberAlreadyExists 表示同一用户已在目标项目拥有唯一成员关系。
	ErrMemberAlreadyExists = errors.New("项目成员已存在")
	// ErrMemberNotFound 表示目标项目内不存在指定成员关系。
	ErrMemberNotFound = errors.New("项目成员不存在")
	// ErrProjectOwnerNotFound 表示负责人用户名未对应任何用户。
	ErrProjectOwnerNotFound = errors.New("负责人用户不存在")
)

// CreateInput 是创建项目所需的可写字段；Code 只在此处出现，以保证创建后不可修改。
type CreateInput struct {
	Code          string
	Name          string
	Description   string
	OwnerUsername *string
}

// UpdateInput 仅包含允许修改的项目资料，故意不提供 Code 字段以维持资源归属标识稳定。
type UpdateInput struct {
	Name          string
	Description   string
	Status        string
	OwnerUsername *string
}

// Service 协调项目仓储与领域规则，HTTP 授权仍由边界层根据当前 JWT 声明执行。
type Service struct {
	repository    Repository
	auditRecorder audit.Recorder
}

// NewService 创建项目领域服务。
func NewService(repository Repository, recorders ...audit.Recorder) *Service {
	service := &Service{repository: repository}
	if len(recorders) > 0 {
		service.auditRecorder = recorders[0]
	}
	return service
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

	owner, err := s.resolveOwner(ctx, input.OwnerUsername)
	if err != nil {
		return nil, err
	}
	project := &Project{
		Code:        input.Code,
		Name:        input.Name,
		Description: input.Description,
		Status:      ProjectStatusEnabled,
		OwnerUser:   owner,
	}
	if owner != nil {
		project.OwnerUserID = &owner.ID
	}
	if err := s.withAuditTransaction(ctx, func(repository Repository, recorder audit.Recorder) error {
		if err := repository.Create(ctx, project); err != nil {
			return err
		}
		projectID := project.ID
		return recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectID, Action: audit.ActionProjectCreated, ResourceType: "project", ResourceID: strconv.FormatUint(project.ID, 10), Detail: map[string]any{
			"project_code": project.Code, "project_name": project.Name, "owner_username": ownerUsername(project),
		}})
	}); err != nil {
		if isDuplicateCodeError(err) {
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
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProjectNotFound
		}
		return nil, err
	}
	if project == nil {
		return nil, ErrProjectNotFound
	}
	owner, err := s.resolveOwner(ctx, input.OwnerUsername)
	if err != nil {
		return nil, err
	}
	previousName, previousStatus, previousOwner := project.Name, project.Status, ownerUsername(project)
	project.Name = input.Name
	project.Description = input.Description
	project.Status = input.Status
	project.OwnerUser = owner
	project.OwnerUserID = nil
	if owner != nil {
		project.OwnerUserID = &owner.ID
	}
	if err := s.withAuditTransaction(ctx, func(repository Repository, recorder audit.Recorder) error {
		if err := repository.Update(ctx, project); err != nil {
			return err
		}
		projectID := project.ID
		return recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectID, Action: audit.ActionProjectUpdated, ResourceType: "project", ResourceID: strconv.FormatUint(project.ID, 10), Detail: map[string]any{
			"project_code": project.Code, "project_name": project.Name, "previous_name": previousName,
			"previous_status": previousStatus, "status": project.Status, "previous_owner_username": previousOwner, "owner_username": ownerUsername(project),
		}})
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProjectNotFound
		}
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

// Get 返回单个项目资料；权限中间件必须先于该方法执行，普通用户不得借此判断项目存在性。
func (s *Service) Get(ctx context.Context, id uint64) (*Project, error) {
	if s.repository == nil {
		return nil, ErrProjectRepositoryUnavailable
	}
	if id == 0 {
		return nil, ErrInvalidProjectInput
	}
	project, err := s.repository.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProjectNotFound
		}
		return nil, err
	}
	if project == nil {
		return nil, ErrProjectNotFound
	}
	return project, nil
}

// ListMembers 返回项目成员关系；调用方必须已验证对目标项目拥有读取权限。
func (s *Service) ListMembers(ctx context.Context, projectID uint64) ([]MemberRole, error) {
	if s.repository == nil {
		return nil, ErrProjectRepositoryUnavailable
	}
	if projectID == 0 {
		return nil, ErrInvalidMemberInput
	}
	return s.repository.ListMembers(ctx, projectID)
}

// ListMemberCandidates 返回项目成员管理可选择的启用用户，调用方必须先验证项目管理权限。
func (s *Service) ListMemberCandidates(ctx context.Context) ([]identity.User, error) {
	if s.repository == nil {
		return nil, ErrProjectRepositoryUnavailable
	}
	return s.repository.ListMemberCandidates(ctx)
}

// AddMember 为已有用户建立项目内唯一角色，角色集合受统一校验以避免写入未定义权限。
func (s *Service) AddMember(ctx context.Context, projectID uint64, username, role string) (*MemberRole, error) {
	if s.repository == nil {
		return nil, ErrProjectRepositoryUnavailable
	}
	if projectID == 0 || !identity.ValidUsername(username) || !validMemberRole(role) {
		return nil, ErrInvalidMemberInput
	}
	user, err := s.resolveMember(ctx, username)
	if err != nil {
		return nil, err
	}
	userID := user.ID
	if _, err := s.repository.FindMemberRole(ctx, projectID, userID); err == nil {
		return nil, ErrMemberAlreadyExists
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	member := &MemberRole{ProjectID: projectID, UserID: userID, User: user, Role: role}
	if err := s.withAuditTransaction(ctx, func(repository Repository, recorder audit.Recorder) error {
		if err := repository.CreateMember(ctx, member); err != nil {
			return err
		}
		projectIDCopy := projectID
		return recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectIDCopy, Action: audit.ActionProjectMemberAdded, ResourceType: "project_member", ResourceID: username, Detail: map[string]any{
			"target_username": username, "role": role,
		}})
	}); err != nil {
		if isDuplicateCodeError(err) {
			return nil, ErrMemberAlreadyExists
		}
		return nil, err
	}
	return member, nil
}

// UpdateMemberRole 修改已有成员的项目内角色，不存在的成员关系不应被隐式创建。
func (s *Service) UpdateMemberRole(ctx context.Context, projectID uint64, username, role string) (*MemberRole, error) {
	if s.repository == nil {
		return nil, ErrProjectRepositoryUnavailable
	}
	if projectID == 0 || !identity.ValidUsername(username) || !validMemberRole(role) {
		return nil, ErrInvalidMemberInput
	}
	user, err := s.resolveMember(ctx, username)
	if err != nil {
		return nil, err
	}
	userID := user.ID
	previous, previousErr := s.repository.FindMemberRole(ctx, projectID, userID)
	if previousErr != nil {
		if errors.Is(previousErr, gorm.ErrRecordNotFound) {
			return nil, ErrMemberNotFound
		}
		return nil, previousErr
	}
	var member *MemberRole
	if err := s.withAuditTransaction(ctx, func(repository Repository, recorder audit.Recorder) error {
		if err := repository.UpdateMemberRole(ctx, projectID, userID, role); err != nil {
			return err
		}
		// 更新后在同一事务内读取，响应和审计必须对应已提交的同一成员版本。
		var err error
		member, err = repository.FindMemberRole(ctx, projectID, userID)
		if err != nil {
			return err
		}
		if member == nil {
			return gorm.ErrRecordNotFound
		}
		projectIDCopy := projectID
		return recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectIDCopy, Action: audit.ActionProjectMemberRoleChanged, ResourceType: "project_member", ResourceID: username, Detail: map[string]any{
			"target_username": username, "previous_role": previous.Role, "role": role,
		}})
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMemberNotFound
		}
		return nil, err
	}
	return member, nil
}

// RemoveMember 移除项目成员关系；调用方必须先确认当前用户具备项目管理员或系统管理员权限。
func (s *Service) RemoveMember(ctx context.Context, projectID uint64, username string) error {
	if s.repository == nil {
		return ErrProjectRepositoryUnavailable
	}
	if projectID == 0 || !identity.ValidUsername(username) {
		return ErrInvalidMemberInput
	}
	user, err := s.resolveMember(ctx, username)
	if err != nil {
		return err
	}
	userID := user.ID
	previous, previousErr := s.repository.FindMemberRole(ctx, projectID, userID)
	if previousErr != nil {
		if errors.Is(previousErr, gorm.ErrRecordNotFound) {
			return ErrMemberNotFound
		}
		return previousErr
	}
	if err := s.withAuditTransaction(ctx, func(repository Repository, recorder audit.Recorder) error {
		if err := repository.DeleteMember(ctx, projectID, userID); err != nil {
			return err
		}
		projectIDCopy := projectID
		return recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectIDCopy, Action: audit.ActionProjectMemberRemoved, ResourceType: "project_member", ResourceID: username, Detail: map[string]any{
			"target_username": username, "previous_role": previous.Role,
		}})
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrMemberNotFound
		}
		return err
	}
	return nil
}

// resolveOwner 保留空负责人语义，并在任何写入前校验公开用户名。
func (s *Service) resolveOwner(ctx context.Context, username *string) (*identity.User, error) {
	if username == nil {
		return nil, nil
	}
	if !identity.ValidUsername(*username) {
		return nil, ErrInvalidProjectInput
	}
	user, err := s.repository.FindUserByUsername(ctx, *username)
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && user == nil) {
		return nil, ErrProjectOwnerNotFound
	}
	return user, err
}

// ownerUsername 为响应和审计提供统一的可空负责人身份，数字关联仅在内部保留。
func ownerUsername(project *Project) *string {
	if project.OwnerUser == nil {
		return nil
	}
	return &project.OwnerUser.Username
}

// resolveMember 将成员用户名严格映射至用户记录，未知身份使用稳定领域错误。
func (s *Service) resolveMember(ctx context.Context, username string) (*identity.User, error) {
	user, err := s.repository.FindUserByUsername(ctx, username)
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && user == nil) {
		return nil, ErrMemberNotFound
	}
	return user, err
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
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrProjectNotFound
		}
		return err
	}
	if project == nil {
		return ErrProjectNotFound
	}
	projectID := project.ID
	return s.withAuditTransaction(ctx, func(repository Repository, recorder audit.Recorder) error {
		// 删除审计先在事务内读取项目名称快照；后续删除失败时整个事务仍会回滚。
		if err := recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectID, Action: audit.ActionProjectDeleted, ResourceType: "project", ResourceID: strconv.FormatUint(project.ID, 10), Detail: map[string]any{
			"project_code": project.Code, "project_name": project.Name,
		}}); err != nil {
			return err
		}
		return repository.Delete(ctx, id)
	})
}

// withAuditTransaction 在审计启用时强制使用项目仓储提供的共享事务能力。
func (s *Service) withAuditTransaction(ctx context.Context, operation func(Repository, audit.Recorder) error) error {
	if s.auditRecorder == nil {
		return operation(s.repository, nil)
	}
	repository, ok := s.repository.(auditTransactionRepository)
	if !ok {
		return ErrProjectRepositoryUnavailable
	}
	return repository.WithAuditTransaction(ctx, operation)
}

// recordAuditWith 将 nil 记录器视为轻量测试未启用审计，生产路径始终收到事务记录器。
func recordAuditWith(ctx context.Context, recorder audit.Recorder, entry audit.Entry) error {
	if recorder == nil {
		return nil
	}
	return recorder.Record(ctx, entry)
}

// isDuplicateCodeError 兼容 GORM 已翻译错误和未启用 TranslateError 时原样返回的 PostgreSQL 23505。
func isDuplicateCodeError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

// validProjectStatus 集中维护项目允许的生命周期状态，避免接口写入未定义状态。
func validProjectStatus(status string) bool {
	return status == ProjectStatusEnabled || status == ProjectStatusDisabled
}

// validMemberRole 集中维护首期项目内角色，避免成员接口写入无法被权限中间件识别的值。
func validMemberRole(role string) bool {
	return role == MemberRoleProjectAdmin || role == MemberRoleMember
}
