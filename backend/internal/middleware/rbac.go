package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HasAnyRole reports whether userRoles include a required role. A super_admin
// role is authorized for every protected role.
func HasAnyRole(userRoles []string, requiredRoles ...string) bool {
	if len(requiredRoles) == 0 {
		return false
	}
	for _, has := range userRoles {
		if has == "super_admin" {
			return true
		}
		for _, required := range requiredRoles {
			if has == required {
				return true
			}
		}
	}
	return false
}

func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if HasAnyRole(GetCurrentRoles(c), roles...) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": -1, "message": "insufficient permissions"})
	}
}
