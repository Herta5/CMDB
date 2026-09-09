// 本文件承载新版身份域的登录校验、最小 JWT 签发和当前用户查询逻辑。
package identity

import (
	"context"
	"errors"
	"strings"
	"time"

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
)

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
	repository UserRepository
	jwtSecret  []byte
	now        func() time.Time
}

// NewService 创建身份服务；令牌时限固定为一天，后续如需配置化必须保持过期声明存在。
func NewService(repository UserRepository, jwtSecret string) *Service {
	return &Service{
		repository: repository,
		jwtSecret:  []byte(jwtSecret),
		now:        time.Now,
	}
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

// CreateUser 创建默认启用的普通用户，明文密码只在此调用链中用于生成不可逆哈希。
func (s *Service) CreateUser(ctx context.Context, username, password, displayName, email string) (*User, error) {
	if s.repository == nil {
		return nil, ErrIdentityRepositoryUnavailable
	}
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	if username == "" || displayName == "" || len(password) < 12 || len(password) > 72 {
		return nil, ErrInvalidUserInput
	}
	if _, err := s.repository.FindByUsername(ctx, username); err == nil {
		return nil, ErrDuplicateUsername
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	user := &User{Username: username, PasswordHash: hash, DisplayName: displayName, Email: strings.TrimSpace(email), GlobalRole: GlobalRoleUser, Status: "active"}
	if err := s.repository.Create(ctx, user); err != nil {
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

// UpdateUserStatus 启停用户；停用后认证中间件会在下一次请求立即使其会话失效。
func (s *Service) UpdateUserStatus(ctx context.Context, id uint64, status string) (*User, error) {
	if s.repository == nil {
		return nil, ErrIdentityRepositoryUnavailable
	}
	if id == 0 || (status != "active" && status != "disabled") {
		return nil, ErrInvalidUserInput
	}
	if err := s.repository.UpdateStatus(ctx, id, status); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return s.repository.FindByID(ctx, id)
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
