// 本文件实现实例独立的 JWT 认证，并在每次请求中用数据库当前身份建立统一授权边界。
package httpserver

import (
	"errors"
	"net/http"
	"strings"

	"cmdb/internal/audit"
	"cmdb/internal/identity"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Authenticator 只持有当前服务实例的校验密钥和身份服务，避免多个引擎互相改变认证结果。
type Authenticator struct {
	jwtSecret []byte
	identity  *identity.Service
}

// NewAuthenticator 在服务装配时创建认证依赖；密钥不暴露给领域模块、日志或响应。
func NewAuthenticator(service *identity.Service, secret string) *Authenticator {
	return &Authenticator{jwtSecret: []byte(secret), identity: service}
}

// RequireUser 验证签名后重新读取当前账户；令牌中的历史角色不能用于项目授权。
func (a *Authenticator) RequireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			writeAuthenticationError(c)
			return
		}

		claims := identity.UserClaims{}
		token, err := jwt.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (interface{}, error) {
			if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, errors.New("不支持的 JWT 签名算法")
			}
			return a.jwtSecret, nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
		if err != nil || !token.Valid || !identity.ValidUsername(claims.Username) {
			writeAuthenticationError(c)
			return
		}

		user, err := a.identity.CurrentUser(c.Request.Context(), claims)
		if errors.Is(err, identity.ErrAuthenticatedUserNotFound) {
			writeAuthenticationError(c)
			return
		}
		if err != nil {
			// 仓储故障必须拒绝授权但保留服务错误语义，不能伪装成已失效会话。
			c.AbortWithStatusJSON(http.StatusInternalServerError, authenticationErrorResponse{Code: "AUTH_SERVICE_UNAVAILABLE", Message: "认证服务暂不可用"})
			return
		}
		// 停用和删除已由身份服务拒绝；所有下游项目接口只消费数据库当前全局角色。
		claims.InternalUserID = user.ID
		claims.GlobalRole = user.GlobalRole
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
