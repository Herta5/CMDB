// 本文件承载新版身份域的登录校验、最小 JWT 签发和当前用户查询逻辑。
package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
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
	// ErrInvalidSession 统一表示令牌或当前账号无法通过完整认证，不暴露失败步骤。
	ErrInvalidSession = errors.New("身份认证已失效")
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

// UserClaims 是 JWT 中允许保存的最小身份信息；内部用户 ID 只能在认证中间件实时补全。
type UserClaims struct {
	Username       string `json:"username"`
	InternalUserID uint64 `json:"-"`
	GlobalRole     string `json:"global_role"`
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

// Authenticate 只在签名、算法、到期时间与当前账号均有效后返回可用于授权的身份。
func (s *Service) Authenticate(ctx context.Context, tokenString string) (*User, error) {
	if s.repository == nil {
		return nil, ErrInvalidSession
	}
	claims := UserClaims{}
	var user *User
	token, err := jwt.ParseWithClaims(tokenString, &claims, func(_ *jwt.Token) (interface{}, error) {
		// 此时声明尚未验证，用户名只能选择候选账号的密钥，不能建立权限或审计上下文。
		if !ValidUsername(claims.Username) {
			return nil, ErrInvalidSession
		}
		var err error
		user, err = s.repository.FindByUsername(ctx, claims.Username)
		if err != nil || user == nil || user.ID == 0 || user.Status != "active" {
			return nil, ErrInvalidSession
		}
		return s.accountSigningKey(user.ID), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired(), jwt.WithTimeFunc(s.now))
	if err != nil || token == nil || !token.Valid {
		return nil, ErrInvalidSession
	}
	return user, nil
}

// CurrentUser 重新读取受验证账号的资料；二次查询也不得把同名新账号绑定到旧会话。
func (s *Service) CurrentUser(ctx context.Context, claims UserClaims) (*User, error) {
	if s.repository == nil {
		return nil, ErrIdentityRepositoryUnavailable
	}

	user, err := s.repository.FindByUsername(ctx, claims.Username)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAuthenticatedUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if user == nil || user.Status != "active" || user.ID != claims.InternalUserID {
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
		if err := recordAuditWith(ctx, recorder, audit.Entry{Action: audit.ActionUserCreated, ResourceType: "user", ResourceID: user.Username, Detail: map[string]any{
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
func (s *Service) DeleteUser(ctx context.Context, actorUsername, targetUsername string) error {
	if s.repository == nil {
		return ErrIdentityRepositoryUnavailable
	}
	if !ValidUsername(actorUsername) || !ValidUsername(targetUsername) {
		return ErrInvalidUserInput
	}
	err := s.withAuditTransaction(ctx, func(repository UserRepository, recorder audit.Recorder) error {
		user, err := repository.FindByUsername(ctx, targetUsername)
		if err != nil {
			return err
		}
		if user == nil {
			return gorm.ErrRecordNotFound
		}
		if actorUsername == user.Username {
			return ErrSelfProtection
		}
		// 按用户名定位后重新读取完整资料，删除时必须同时审计其项目成员关系。
		user, err = repository.FindByID(ctx, user.ID)
		if err != nil {
			return err
		}
		if err := recordAuditWith(ctx, recorder, audit.Entry{Action: audit.ActionUserDeleted, ResourceType: "user", ResourceID: user.Username, Detail: map[string]any{
			"target_username": user.Username, "target_display_name": user.DisplayName, "global_role": user.GlobalRole, "status": user.Status,
		}}); err != nil {
			return err
		}
		// 用户删除会级联移除全部项目成员关系，逐项目审计让对应项目管理员也能追溯。
		if err := recordPermissionChanges(ctx, recorder, *user, user.ProjectPermissions, nil); err != nil {
			return err
		}
		return repository.Delete(ctx, user.ID)
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
func (s *Service) UpdateUserStatus(ctx context.Context, actorUsername, targetUsername, status string) (*User, error) {
	if s.repository == nil {
		return nil, ErrIdentityRepositoryUnavailable
	}
	if !ValidUsername(actorUsername) || !ValidUsername(targetUsername) || (status != "active" && status != "disabled") {
		return nil, ErrInvalidUserInput
	}
	var updated *User
	err := s.withAuditTransaction(ctx, func(repository UserRepository, recorder audit.Recorder) error {
		// 旧状态、状态更新、审计及响应读取必须处于同一事务，避免并发变化造成错误快照。
		previous, err := repository.FindByUsername(ctx, targetUsername)
		if err != nil {
			return err
		}
		if previous == nil {
			return gorm.ErrRecordNotFound
		}
		if actorUsername == previous.Username && status != "active" {
			return ErrSelfProtection
		}
		if err := repository.UpdateStatus(ctx, previous.ID, status); err != nil {
			return err
		}
		if err := recordAuditWith(ctx, recorder, audit.Entry{Action: audit.ActionUserStatusChanged, ResourceType: "user", ResourceID: previous.Username, Detail: map[string]any{
			"target_username": previous.Username, "previous_status": previous.Status, "status": status,
		}}); err != nil {
			return err
		}
		updated, err = repository.FindByID(ctx, previous.ID)
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
func (s *Service) UpdateUser(ctx context.Context, actorUsername, targetUsername string, input UpdateUserInput) (*User, error) {
	if s.repository == nil {
		return nil, ErrIdentityRepositoryUnavailable
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.TrimSpace(input.Email)
	if !ValidUsername(actorUsername) || !ValidUsername(targetUsername) || input.DisplayName == "" || !validRoleAndStatus(input.GlobalRole, input.Status) || !validProjectPermissions(input.ProjectPermissions) || (input.Password != "" && (len(input.Password) < 12 || len(input.Password) > 72)) {
		return nil, ErrInvalidUserInput
	}
	user, err := s.repository.FindByUsername(ctx, targetUsername)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	// 用户名只用于对外定位；编辑权限快照仍通过内部 ID 读取，避免覆盖既有项目授权。
	user, err = s.repository.FindByID(ctx, user.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if actorUsername == user.Username && (input.GlobalRole != GlobalRoleSystemAdmin || input.Status != "active") {
		return nil, ErrSelfProtection
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
		if err := recordAuditWith(ctx, recorder, audit.Entry{Action: audit.ActionUserUpdated, ResourceType: "user", ResourceID: previous.Username, Detail: map[string]any{
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
	return s.repository.FindByID(ctx, user.ID)
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
			if err := recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectIDCopy, Action: audit.ActionProjectMemberRemoved, ResourceType: "project_member", ResourceID: user.Username, Detail: map[string]any{"target_username": user.Username, "previous_role": previousRole, "source": "user_management"}}); err != nil {
				return err
			}
		} else if nextRole != previousRole {
			if err := recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectIDCopy, Action: audit.ActionProjectMemberRoleChanged, ResourceType: "project_member", ResourceID: user.Username, Detail: map[string]any{"target_username": user.Username, "previous_role": previousRole, "role": nextRole, "source": "user_management"}}); err != nil {
				return err
			}
		}
	}
	for projectID, role := range after {
		if _, exists := before[projectID]; exists {
			continue
		}
		projectIDCopy := projectID
		if err := recordAuditWith(ctx, recorder, audit.Entry{ProjectID: &projectIDCopy, Action: audit.ActionProjectMemberAdded, ResourceType: "project_member", ResourceID: user.Username, Detail: map[string]any{"target_username": user.Username, "role": role, "source": "user_management"}}); err != nil {
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
		Username:   user.Username,
		GlobalRole: user.GlobalRole,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(s.now().Add(defaultTokenLifetime)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.accountSigningKey(user.ID))
}

// accountSigningKey 将不可复用的数据库主键绑定到签名，用户名重建不会恢复旧账号令牌。
// 域前缀隔离此派生用途；内部主键只参与服务端 HMAC，绝不进入 JWT 载荷或公开输出。
func (s *Service) accountSigningKey(userID uint64) []byte {
	mac := hmac.New(sha256.New, s.jwtSecret)
	mac.Write([]byte("cmdb.jwt.account.v1:" + strconv.FormatUint(userID, 10)))
	return mac.Sum(nil)
}
