package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
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

// ShouldCaptureAuditBody reports whether a request path may have its body
// recorded. Credentials and password updates must never be read for audit
// logging.
func ShouldCaptureAuditBody(path string) bool {
	path = strings.TrimSuffix(path, "/")
	if path == "/api/v1/auth/login" || path == "/api/v1/profile/password" {
		return false
	}

	return !strings.HasPrefix(path, "/api/v1/users/") || !strings.HasSuffix(path, "/password")
}

func captureAuditBody(request *http.Request) *string {
	if request.Body == nil || request.ContentLength == 0 || !ShouldCaptureAuditBody(request.URL.Path) {
		return nil
	}

	const maxAuditBodyBytes = 4096
	readBytes, err := io.ReadAll(io.LimitReader(request.Body, maxAuditBodyBytes+1))
	request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(readBytes), request.Body))
	if err != nil {
		return nil
	}
	if len(readBytes) > maxAuditBodyBytes {
		readBytes = readBytes[:maxAuditBodyBytes]
	}
	body := string(readBytes)
	return &body
}

// ReadAuditIdentity uses the identity established by AuthRequired whenever it
// is present. Public and pre-auth requests have no trusted context identity,
// so their audit entry may use an unverified JWT only as a best-effort label.
func ReadAuditIdentity(c *gin.Context) (uint64, string) {
	contextUserID, hasUserID := c.Get("user_id")
	contextUsername, hasUsername := c.Get("username")
	if hasUserID || hasUsername {
		userID, _ := contextUserID.(uint64)
		username, _ := contextUsername.(string)
		return userID, username
	}

	username := "anonymous"
	authHeader := c.GetHeader("Authorization")
	parts := strings.Fields(authHeader)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return 0, username
	}

	token, _, err := jwt.NewParser().ParseUnverified(parts[1], jwt.MapClaims{})
	if err != nil {
		return 0, username
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, username
	}
	if claimUsername, ok := claims["username"].(string); ok && claimUsername != "" {
		username = claimUsername
	}
	return readAuditUserID(claims["user_id"]), username
}

func readAuditUserID(value interface{}) uint64 {
	switch id := value.(type) {
	case float64:
		if math.IsNaN(id) || math.IsInf(id, 0) || id < 0 || math.Trunc(id) != id || id >= 1<<64 {
			return 0
		}
		return uint64(id)
	case json.Number:
		parsed, err := strconv.ParseUint(string(id), 10, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func AuditLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		if auditRepo == nil {
			c.Next()
			return
		}

		start := time.Now()

		bodyStr := captureAuditBody(c.Request)

		c.Next()

		duration := time.Since(start).Milliseconds()

		userID, username := ReadAuditIdentity(c)

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
