// 本文件实现 HTTP 服务的 JWT 身份中间件，并将经过验证的声明存入请求上下文。
package httpserver

import (
	"errors"
	"net/http"
	"strings"

	"github-cmdb/internal/identity"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// jwtSecret 仅在服务启动装配时设置，运行中只用于验证客户端提交的 JWT 签名。
var jwtSecret []byte

// SetJWTSecret 在服务装配阶段设置 JWT 校验密钥；密钥只保留在进程内，绝不写入日志或响应。
func SetJWTSecret(secret string) {
	jwtSecret = []byte(secret)
}

// RequireUser 验证 Bearer JWT 并建立当前用户声明；失败时统一返回不泄露令牌细节的认证错误。
func RequireUser() gin.HandlerFunc {
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
			return jwtSecret, nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
		if err != nil || !token.Valid {
			writeAuthenticationError(c)
			return
		}

		c.Set(identity.UserClaimsContextKey, claims)
		c.Next()
	}
}

// CurrentUser 读取已由 RequireUser 验证的用户声明；未经过中间件时返回零值声明。
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

// writeAuthenticationError 将所有 JWT 解析失败统一为稳定错误响应，避免泄露令牌状态。
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
