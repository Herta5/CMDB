// 本文件承载新版身份域的登录校验、最小 JWT 签发和当前用户查询逻辑。
package identity

import (
	"context"
	"errors"
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
)

// defaultTokenLifetime 限制会话可被盗用的时间窗口，同时保证每个 JWT 都带有到期时间。
const defaultTokenLifetime = 24 * time.Hour

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
		return nil, "", ErrInvalidCredentials
	}
	if err != nil {
		return nil, "", err
	}
	if user == nil || user.Status != "active" || !VerifyPassword(user.PasswordHash, password) {
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
	if user == nil {
		return nil, ErrAuthenticatedUserNotFound
	}
	return user, nil
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
