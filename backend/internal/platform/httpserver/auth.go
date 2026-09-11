// 本文件实现实例独立的 JWT 认证，并在每次请求中用数据库当前身份建立统一授权边界。
package httpserver

import (
	"net/http"
	"strings"

	"cmdb/internal/audit"
	"cmdb/internal/identity"
	"github.com/gin-gonic/gin"
)

// Authenticator 只使用当前服务实例的身份服务，避免多个引擎互相改变认证结果。
type Authenticator struct {
	identity *identity.Service
}

// NewAuthenticator 在服务装配时创建认证依赖；签名密钥始终由身份服务保管。
func NewAuthenticator(service *identity.Service) *Authenticator {
	return &Authenticator{identity: service}
}

// RequireUser 完整验证账号代际绑定的令牌；授权只使用数据库当前身份与角色。
func (a *Authenticator) RequireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			writeAuthenticationError(c)
			return
		}

		user, err := a.identity.Authenticate(c.Request.Context(), tokenString)
		if err != nil {
			writeAuthenticationError(c)
			return
		}

		// 仅从通过完整认证的账号建立内部关联，绝不采用未验证或历史声明中的角色。
		claims := identity.UserClaims{Username: user.Username, InternalUserID: user.ID, GlobalRole: user.GlobalRole}
		c.Set(identity.UserClaimsContextKey, claims)
		// 只有完成实时账户校验的请求才能写入操作者审计上下文，令牌和用户资料不进入其中。
		c.Request = c.Request.WithContext(audit.WithActorProfile(c.Request.Context(), claims.InternalUserID, c.ClientIP(), user.Username, user.DisplayName))
		c.Next()
	}
}

// CurrentUser 读取签名和当前账户均已验证的声明；未经过中间件时返回零值声明。
func CurrentUser(c *gin.Context) identity.UserClaims {
	claims, _ := c.Get(identity.UserClaimsContextKey)
	if value, ok := claims.(identity.UserClaims); ok {
		return value
	}
	return identity.UserClaims{}
}

// bearerToken 严格接受单个 Bearer 值，拒绝额外分段以避免认证头歧义。
func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

// writeAuthenticationError 将令牌无效、账户停用和账户删除统一为稳定错误响应。
func writeAuthenticationError(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, authenticationErrorResponse{
		Code:    "AUTH_UNAUTHORIZED",
		Message: "身份认证已失效",
	})
}

// authenticationErrorResponse 是中间件专用的认证失败响应结构。
type authenticationErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
