// 本文件提供项目级角色中间件，所有按项目标识访问的后续领域路由必须复用该隔离边界。
package project

import (
	"errors"
	"net/http"

	"cmdb/internal/identity"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RequireRole 校验当前用户在路径项目中的成员角色；无权时统一返回 404，避免通过状态码枚举项目。
func RequireRole(repository Repository, roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := currentUserClaims(c)
		if !ok {
			abortProjectError(c, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "身份认证已失效")
			return
		}
		if isSystemAdmin(claims) {
			c.Next()
			return
		}
		projectID, ok := projectIDFromPath(c)
		if !ok {
			abortProjectNotFound(c)
			return
		}
		if repository == nil {
			abortProjectError(c, http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用")
			return
		}
		member, err := repository.FindMemberRole(c.Request.Context(), projectID, claims.UserID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				abortProjectNotFound(c)
				return
			}
			abortProjectError(c, http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用")
			return
		}
		if member == nil {
			abortProjectNotFound(c)
			return
		}
		if !hasProjectRole(member.Role, roles) {
			abortProjectNotFound(c)
			return
		}
		c.Next()
	}
}

// currentUserClaims 从认证中间件写入的上下文读取声明，项目模块不解析或信任客户端自行提交的身份信息。
func currentUserClaims(c *gin.Context) (identity.UserClaims, bool) {
	value, ok := c.Get(identity.UserClaimsContextKey)
	claims, claimsOK := value.(identity.UserClaims)
	return claims, ok && claimsOK
}

// hasProjectRole 判断成员角色是否位于当前接口允许集合；空集合按拒绝处理以避免路由装配失误放行。
func hasProjectRole(memberRole string, allowedRoles []string) bool {
	for _, role := range allowedRoles {
		if memberRole == role {
			return true
		}
	}
	return false
}

// abortProjectNotFound 输出不泄露授权失败原因的项目不存在响应。
func abortProjectNotFound(c *gin.Context) {
	abortProjectError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在")
}

// abortProjectError 终止后续处理器链，防止中间件已拒绝请求后仍发生资源读取或写入。
func abortProjectError(c *gin.Context, status int, code, message string) {
	c.Abort()
	writeProjectError(c, status, code, message)
}
