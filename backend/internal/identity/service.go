// 本文件承载新版身份域的登录校验、最小 JWT 签发和当前用户查询逻辑。
package identity

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"cmdb/internal/audit"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

var (
	// ErrInvalidCredentials 统一表示账户不存在、密码错误或账号不可用，防止枚举用户名。
	ErrInvalidCredentials = errors.New("无效的登录凭证")
	// ErrAuthenticatedUserNotFound 表示令牌所指向的用户已不存在，当前会话应立即失效。
	ErrAuthenticatedUserNotFound = errors.New("认证用户不存在")
	// ErrIdentityRepositoryUnavailable 表示服务装配错误，不得向外暴露具体存储原因。
	ErrIdentityRepositoryUnavailable = errors.New("身份仓储不可用")
	// ErrInvalidUserInput 表示用户资料、密码长度或状态不符合身份域约束。
	ErrInvalidUserInput = errors.New("用户参数无效")
	// ErrDuplicateUsername 表示用户名已被其他身份占用。
	ErrDuplicateUsername = errors.New("用户名已存在")
	// ErrUserNotFound 表示管理接口指定的用户不存在。
	ErrUserNotFound = errors.New("用户不存在")
	// ErrSelfProtection 表示当前管理员试图删除、停用自己或移除自己的系统管理权限。
	ErrSelfProtection = errors.New("不能删除、停用或降级当前管理员")
)

// UpdateUserInput 是系统管理员可维护的用户字段；空密码表示保持原密码。
type UpdateUserInput struct {
	DisplayName        string
	Email              string
	GlobalRole         string
	Status             string
	Password           string
	ProjectPermissions []ProjectPermission
}

// CreateUserInput 是系统管理员创建身份时可同时设置的全局与项目权限。
type CreateUserInput struct {
	Username, Password, DisplayName, Email, GlobalRole, Status string
	ProjectPermissions                                         []ProjectPermission
}

// defaultTokenLifetime 限制会话可被盗用的时间窗口，同时保证每个 JWT 都带有到期时间。
const defaultTokenLifetime = 24 * time.Hour

// missingUserPasswordHash 是仅用于补齐 bcrypt 工作量的固定有效哈希，绝不对应可登录用户或写入响应、日志。
const missingUserPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// UserClaims 是 JWT 中唯一允许保存的身份信息；不要向其中添加用户名或任何敏感字段。
type UserClaims struct {
	UserID     uint64 `json:"user_id"`
	GlobalRole string `json:"global_role"`
	jwt.RegisteredClaims
}

// Service 协调用户仓储与 JWT 签名，避免 HTTP 层直接处理密码哈希或签名密钥。
type Service struct {
	repository    UserRepository
	jwtSecret     []byte
	now           func() time.Time
	auditRecorder audit.Recorder
}

// NewService 创建身份服务；令牌时限固定为一天，后续如需配置化必须保持过期声明存在。
func NewService(repository UserRepository, jwtSecret string, recorders ...audit.Recorder) *Service {
	service := &Service{
		repository: repository,
		jwtSecret:  []byte(jwtSecret),
		now:        time.Now,
	}
	if len(recorders) > 0 {
		service.auditRecorder = recorders[0]
	}
	return service
}

// Login 验证凭证并签发会话令牌；仓储未找到和密码不匹配都返回同一业务错误。
func (s *Service) Login(ctx context.Context, username, password string) (*User, string, error) {
	if s.repository == nil {
		return nil, "", ErrIdentityRepositoryUnavailable
	}

	user, err := s.repository.FindByUsername(ctx, username)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 不存在用户也执行一次与真实用户同成本的 bcrypt 比较，避免由耗时枚举登录名。
		_ = VerifyPassword(missingUserPasswordHash, password)
		return nil, "", ErrInvalidCredentials
	}
	if err != nil {
		return nil, "", err
	}
	if user == nil {
		// 异常仓储结果同样不能降低认证工作量或泄露内部状态。
		_ = VerifyPassword(missingUserPasswordHash, password)
		return nil, "", ErrInvalidCredentials
	}
	passwordMatches := VerifyPassword(user.PasswordHash, password)
	if user.Status != "active" || !passwordMatches {
		return nil, "", ErrInvalidCredentials
	}

	token, err := s.sign(user)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

// CurrentUser 以受验证的声明读取当前用户，避免把 JWT 中不应携带的公开资料复制到令牌内。
func (s *Service) CurrentUser(ctx context.Context, claims UserClaims) (*User, error) {
	if s.repository == nil {
		return nil, ErrIdentityRepositoryUnavailable
	}

	user, err := s.repository.FindByID(ctx, claims.UserID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAuthenticatedUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if user == nil || user.Status != "active" {
		return nil, ErrAuthenticatedUserNotFound
	}
	return user, nil
}

// CreateUser 按管理员选择创建用户并授权，明文密码只在此调用链中用于生成不可逆哈希。
func (s *Service) CreateUser(ctx context.Context, input CreateUserInput) (*User, error) {
	if s.repository == nil {
		return nil, ErrIdentityRepositoryUnavailable
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.TrimSpace(input.Email)
	if !ValidUsername(input.Username) || input.DisplayName == "" || len(input.Password) < 12 || len(input.Password) > 72 || !validRoleAndStatus(input.GlobalRole, input.Status) || !validProjectPermissions(input.ProjectPermissions) {
		return nil, ErrInvalidUserInput
	}
	if _, err := s.repository.FindByUsername(ctx, input.Username); err == nil {
		return nil, ErrDuplicateUsername
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	hash, err := HashPassword(input.Password)
	if err != nil {
		return nil, err
	}
	user := &User{Username: input.Username, PasswordHash: hash, DisplayName: input.DisplayName, Email: input.Email, GlobalRole: input.GlobalRole, Status: input.Status}
	if err := s.withAuditTransaction(ctx, func(repository UserRepository, recorder audit.Recorder) error {
		if err := repository.CreateWithPermissions(ctx, user, input.ProjectPermissions); err != nil {
			return err
		}
		if err := recordAuditWith(ctx, recorder, audit.Entry{Action: audit.ActionUserCreated, ResourceType: "user", ResourceID: strconv.FormatUint(user.ID, 10), Detail: map[string]any{
			"target_username": user.Username, "target_display_name": user.DisplayName, "global_role": user.GlobalRole,
			"status": user.Status, "project_permissions": permissionAuditValues(input.ProjectPermissions),
		}}); err != nil {
			return err
		}
		return recordPermissionChanges(ctx, recorder, *user, nil, input.ProjectPermissions)
	}); err != nil {
		if errors.Is(err, ErrProjectPermissionInvalid) {
			return nil, ErrInvalidUserInput
		}
		return nil, err
	}
	return user, nil
}

// ListUsers 返回用户公开资料的数据来源，HTTP 层负责过滤密码哈希字段。
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	if s.repository == nil {
		return nil, ErrIdentityRepositoryUnavailable
	}
	return s.repository.List(ctx)
}

// DeleteUser 删除指定用户，并保护当前管理员不会删除自己的登录身份。
func (s *Service) DeleteUser(ctx context.Context, actorID, id uint64) error {
	if s.repository == nil {
		return ErrIdentityRepositoryUnavailable
	}
	if actorID == 0 || id == 0 {
		return ErrInvalidUserInput
	}
	if actorID == id {
		return ErrSelfProtection
	}
	err := s.withAuditTransaction(ctx, func(repository UserRepository, recorder audit.Recorder) error {
		user, err := repository.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if user == nil {
			return gorm.ErrRecordNotFound
		}
		actorIDCopy := actorID
		if err := recordAuditWith(ctx, recorder, audit.Entry{ActorID: &actorIDCopy, Action: audit.ActionUserDeleted, ResourceType: "user", ResourceID: strconv.FormatUint(id, 10), Detail: map[string]any{
			"target_username": user.Username, "target_display_name": user.DisplayName, "global_role": user.GlobalRole, "status": user.Status,
		}}); err != nil {
			return err
		}
		// 用户删除会级联移除全部项目成员关系，逐项目审计让对应项目管理员也能追溯。
		if err := recordPermissionChanges(ctx, recorder, *user, user.ProjectPermissions, nil); err != nil {
			return err
		}
		return repository.Delete(ctx, id)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	return nil
}

// UpdateUserStatus 启停用户；停用后认证中间件会在下一次请求立即使其会话失效。
func (s *Service) UpdateUserStatus(ctx context.Context, actorID, id uint64, status string) (*User, error) {
	if s.repository == nil {
		return nil, ErrIdentityRepositoryUnavailable
	}
	if actorID == 0 || id == 0 || (status != "active" && status != "disabled") {
		return nil, ErrInvalidUserInput
	}
	if actorID == id && status != "active" {
		return nil, ErrSelfProtection
	}
	var updated *User
	err := s.withAuditTransaction(ctx, func(repository UserRepository, recorder audit.Recorder) error {
		// 旧状态、状态更新、审计及响应读取必须处于同一事务，避免并发变化造成错误快照。
		previous, err := repository.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if previous == nil {
			return gorm.ErrRecordNotFound
		}
		if err := repository.UpdateStatus(ctx, id, status); err != nil {
			return err
		}
		if err := recordAuditWith(ctx, recorder, audit.Entry{Action: audit.ActionUserStatusChanged, ResourceType: "user", ResourceID: strconv.FormatUint(id, 10), Detail: map[string]any{
			"target_username": previous.Username, "previous_status": previous.Status, "status": status,
		}}); err != nil {
			return err
		}
		updated, err = repository.FindByID(ctx, id)
		return err
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return updated, nil
}

// UpdateUser 更新公开资料、全局角色、状态及可选密码，并保护当前管理员不会锁定自己。
func (s *Service) UpdateUser(ctx context.Context, actorID, id uint64, input UpdateUserInput) (*User, error) {
	if s.repository == nil {
		return nil, ErrIdentityRepositoryUnavailable
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.TrimSpace(input.Email)
	if actorID == 0 || id == 0 || input.DisplayName == "" || !validRoleAndStatus(input.GlobalRole, input.Status) || !validProjectPermissions(input.ProjectPermissions) || (input.Password != "" && (len(input.Password) < 12 || len(input.Password) > 72)) {
		return nil, ErrInvalidUserInput
	}
	if actorID == id && (input.GlobalRole != GlobalRoleSystemAdmin || input.Status != "active") {
		return nil, ErrSelfProtection
	}
	user, err := s.repository.FindByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	previous := *user
	previousPermissions := append([]ProjectPermission(nil), user.ProjectPermissions...)
	user.DisplayName = input.DisplayName
	user.Email = input.Email
	user.GlobalRole = input.GlobalRole
	user.Status = input.Status
	if input.Password != "" {
		hash, hashErr := HashPassword(input.Password)
		if hashErr != nil {
			return nil, hashErr
		}
		user.PasswordHash = hash
	}
	changedFields := changedUserFields(previous, previousPermissions, input)
	if err := s.withAuditTransaction(ctx, func(repository UserRepository, recorder audit.Recorder) error {
		if err := repository.UpdateWithPermissions(ctx, user, input.ProjectPermissions); err != nil {
			return err
		}
		if err := recordAuditWith(ctx, recorder, audit.Entry{Action: audit.ActionUserUpdated, ResourceType: "user", ResourceID: strconv.FormatUint(id, 10), Detail: map[string]any{
			"target_username": previous.Username, "target_display_name": input.DisplayName,
			"changed_fields": changedFields, "project_permissions": permissionAuditValues(input.ProjectPermissions),
		}}); err != nil {
			return err
		}
		return recordPermissionChanges(ctx, recorder, previous, previousPermissions, input.ProjectPermissions)
	}); err != nil {
		if errors.Is(err, ErrProjectPermissionInvalid) {
			return nil, ErrInvalidUserInput
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return s.repository.FindByID(ctx, id)
}

// withAuditTransaction 在审计启用时强制使用仓储提供的共享事务，禁止降级为两个独立提交。
func (s *Service) withAuditTransaction(ctx context.Context, operation func(UserRepository, audit.Recorder) error) error {
	if s.auditRecorder == nil {
		return operation(s.repository, nil)
	}
	repository, ok := s.repository.(auditTransactionUserRepository)
	if !ok {
		return ErrIdentityRepositoryUnavailable
	}
	return repository.WithAuditTransaction(ctx, operation)
}

// recordAuditWith 将 nil 记录器视为未启用审计，仅供不装配数据库的轻量领域测试使用。
func recordAuditWith(ctx context.Context, recorder audit.Recorder, entry audit.Entry) error {
	if recorder == nil {
		return nil
	}
	return recorder.Record(ctx, entry)
}

// permissionAuditValues 只保留项目标识和角色，不把项目或用户模型整体写入审计。
func permissionAuditValues(values []ProjectPermission) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		result = append(result, map[string]any{"project_id": value.ProjectID, "role": value.Role})
	}
	return result
}

// recordPermissionChanges 把用户管理中的授权替换拆成项目级动作，让受影响项目管理员也能审计。
func recordPermissionChanges(ctx context.Context, recorder audit.Recorder, user User, previous, next []ProjectPermission) error {
	before := permissionMap(previous)
	after := permissionMap(next)
	for projectID, previousRole := range before {
		nextRole, exists := after[projectID]
		projectIDCopy := projectID
		if !exists {
			if err := recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectIDCopy, Action: audit.ActionProjectMemberRemoved, ResourceType: "project_member", ResourceID: strconv.FormatUint(user.ID, 10), Detail: map[string]any{"target_user_id": user.ID, "target_username": user.Username, "previous_role": previousRole, "source": "user_management"}}); err != nil {
				return err
			}
		} else if nextRole != previousRole {
			if err := recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectIDCopy, Action: audit.ActionProjectMemberRoleChanged, ResourceType: "project_member", ResourceID: strconv.FormatUint(user.ID, 10), Detail: map[string]any{"target_user_id": user.ID, "target_username": user.Username, "previous_role": previousRole, "role": nextRole, "source": "user_management"}}); err != nil {
				return err
			}
		}
	}
	for projectID, role := range after {
		if _, exists := before[projectID]; exists {
			continue
		}
		projectIDCopy := projectID
		if err := recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectIDCopy, Action: audit.ActionProjectMemberAdded, ResourceType: "project_member", ResourceID: strconv.FormatUint(user.ID, 10), Detail: map[string]any{"target_user_id": user.ID, "target_username": user.Username, "role": role, "source": "user_management"}}); err != nil {
			return err
		}
	}
	return nil
}

// permissionMap 用项目标识和角色比较授权，忽略仅供页面显示的项目名称。
func permissionMap(values []ProjectPermission) map[uint64]string {
	result := make(map[uint64]string, len(values))
	for _, value := range values {
		result[value.ProjectID] = value.Role
	}
	return result
}

// changedUserFields 只记录发生变化的字段名称；密码内容及哈希永远不进入详情。
func changedUserFields(previous User, previousPermissions []ProjectPermission, input UpdateUserInput) []string {
	fields := make([]string, 0, 6)
	if previous.DisplayName != input.DisplayName {
		fields = append(fields, "display_name")
	}
	if previous.Email != input.Email {
		fields = append(fields, "email")
	}
	if previous.GlobalRole != input.GlobalRole {
		fields = append(fields, "global_role")
	}
	if previous.Status != input.Status {
		fields = append(fields, "status")
	}
	if !permissionMapsEqual(permissionMap(previousPermissions), permissionMap(input.ProjectPermissions)) {
		fields = append(fields, "project_permissions")
	}
	if input.Password != "" {
		// 只表达认证凭据发生替换，字段名也不复用敏感请求键，避免审计扫描产生歧义。
		fields = append(fields, "login_secret_replaced")
	}
	return fields
}

// permissionMapsEqual 判断项目角色集合是否相同，顺序变化不应被误报为授权变更。
func permissionMapsEqual(left, right map[uint64]string) bool {
	if len(left) != len(right) {
		return false
	}
	for projectID, role := range left {
		if right[projectID] != role {
			return false
		}
	}
	return true
}

// validRoleAndStatus 统一校验全局角色和账号状态，创建与编辑保持同一契约。
func validRoleAndStatus(role, status string) bool {
	return (role == GlobalRoleSystemAdmin || role == GlobalRoleUser) && (status == "active" || status == "disabled")
}

// validProjectPermissions 拒绝重复项目和非法角色，避免全量替换出现歧义。
func validProjectPermissions(values []ProjectPermission) bool {
	seen := make(map[uint64]struct{}, len(values))
	for _, value := range values {
		if value.ProjectID == 0 || (value.Role != "project_admin" && value.Role != "member") {
			return false
		}
		if _, exists := seen[value.ProjectID]; exists {
			return false
		}
		seen[value.ProjectID] = struct{}{}
	}
	return true
}

// sign 使用 HS256 签发仅含最小身份声明的 JWT，令牌内容不可替代数据库中的用户资料。
func (s *Service) sign(user *User) (string, error) {
	claims := UserClaims{
		UserID:     user.ID,
		GlobalRole: user.GlobalRole,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(s.now().Add(defaultTokenLifetime)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}
