package middleware

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRoles := GetCurrentRoles(c)
		for _, required := range roles {
			for _, has := range userRoles {
				if has == required || has == "super_admin" {
					c.Next()
					return
				}
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": -1, "message": "insufficient permissions"})
	}
}