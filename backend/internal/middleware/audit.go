package middleware

import (
	"bytes"
	"io"
	"strings"
	"time"

	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var auditRepo *repository.AuditRepo

func SetAuditRepo(repo *repository.AuditRepo) {
	auditRepo = repo
}

func AuditLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		if auditRepo == nil {
			c.Next()
			return
		}

		start := time.Now()

		// Capture request body
		var bodyStr *string
		if c.Request.Body != nil && c.Request.ContentLength > 0 {
			bodyBytes, _ := io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			if len(bodyBytes) > 4096 { bodyBytes = bodyBytes[:4096] }
			s := string(bodyBytes)
			bodyStr = &s
		}

		c.Next()

		duration := time.Since(start).Milliseconds()

		// Extract user info from JWT
		username := "anonymous"
		userID := uint64(0)
		if tokenStr := c.GetHeader("Authorization"); strings.HasPrefix(tokenStr, "Bearer ") {
			tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")
			if token, _, err := jwt.NewParser().ParseUnverified(tokenStr, jwt.MapClaims{}); err == nil {
				if claims, ok := token.Claims.(jwt.MapClaims); ok {
					if u, ok := claims["username"].(string); ok { username = u }
					if id, ok := claims["sub"].(float64); ok { userID = uint64(id) }
				}
			}
		}

		// Skip logging for static files and health checks
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/assets") || path == "/health" {
			return
		}

		log := &model.AuditLog{
			UserID:         userID,
			Username:       username,
			Method:         c.Request.Method,
			Path:           path,
			QueryString:    c.Request.URL.RawQuery,
			RequestBody:    bodyStr,
			ResponseStatus: c.Writer.Status(),
			ClientIP:       c.ClientIP(),
			UserAgent:      c.Request.UserAgent(),
			DurationMs:     duration,
		}

		go func() { _ = auditRepo.Create(log) }()
	}
}