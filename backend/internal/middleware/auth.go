package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID      uint64   `json:"user_id"`
	Username    string   `json:"username"`
	Roles       []string `json:"roles"`
	Departments []uint64 `json:"departments"`
	Scope       []string `json:"scope"`
	jwt.RegisteredClaims
}

var jwtSecret []byte

func SetJWTSecret(secret string) { jwtSecret = []byte(secret) }

func GenerateToken(userID uint64, username string, roles []string, expireHour int) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID, Username: username, Roles: roles,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expireHour) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": -1, "message": "missing authorization header"})
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": -1, "message": "invalid authorization format"})
			return
		}
		token, err := jwt.ParseWithClaims(parts[1], &Claims{}, func(t *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": -1, "message": "invalid or expired token"})
			return
		}
		claims, ok := token.Claims.(*Claims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": -1, "message": "invalid token claims"})
			return
		}
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("roles", claims.Roles)
		c.Set("departments", claims.Departments)
		c.Next()
	}
}

func GetCurrentUserID(c *gin.Context) uint64 {
	id, _ := c.Get("user_id")
	if v, ok := id.(uint64); ok { return v }
	return 0
}

func GetCurrentRoles(c *gin.Context) []string {
	roles, _ := c.Get("roles")
	if v, ok := roles.([]string); ok { return v }
	return nil
}

func GetCurrentUsername(c *gin.Context) string {
	username, _ := c.Get("username")
	if v, ok := username.(string); ok { return v }
	return ""
}
