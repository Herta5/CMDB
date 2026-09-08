package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func TestShouldCaptureAuditBodyRedactsSensitivePaths(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/api/v1/auth/login", want: false},
		{path: "/api/v1/profile/password", want: false},
		{path: "/api/v1/users/42/password", want: false},
		{path: "/api/v1/assets", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := ShouldCaptureAuditBody(tt.path); got != tt.want {
				t.Fatalf("ShouldCaptureAuditBody(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestCaptureAuditBodySkipsSensitiveRequestWithoutReadingIt(t *testing.T) {
	body := &readTrackingBody{Reader: strings.NewReader(`{"password":"do-not-read"}`)}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)

	if got := captureAuditBody(request); got != nil {
		t.Fatalf("captureAuditBody() = %q, want nil", *got)
	}
	if body.read {
		t.Fatal("captureAuditBody() read a sensitive request body")
	}
}

func TestCaptureAuditBodyLimitsNormalRequestTo4096Bytes(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/assets", bytes.NewBufferString(strings.Repeat("x", 4097)))

	body := captureAuditBody(request)
	if body == nil {
		t.Fatal("captureAuditBody() = nil, want a body")
	}
	if got := len(*body); got != 4096 {
		t.Fatalf("captured body length = %d, want 4096", got)
	}
	if got, err := io.ReadAll(request.Body); err != nil || len(got) != 4097 {
		t.Fatalf("request body after capture = %d bytes, %v; want restored 4097-byte body", len(got), err)
	}
}

func TestReadAuditIdentityPrefersAuthenticatedContext(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/assets", nil)
	context.Request.Header.Set("Authorization", "Bearer "+auditToken(t, 99, "token-user"))
	context.Set("user_id", uint64(42))
	context.Set("username", "authenticated-user")

	userID, username := ReadAuditIdentity(context)
	if userID != 42 || username != "authenticated-user" {
		t.Fatalf("ReadAuditIdentity() = (%d, %q), want (42, authenticated-user)", userID, username)
	}
}

func TestReadAuditIdentityFallsBackToJWTUserIDClaim(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public", nil)
	context.Request.Header.Set("Authorization", "Bearer "+auditToken(t, 7, "token-user"))

	userID, username := ReadAuditIdentity(context)
	if userID != 7 || username != "token-user" {
		t.Fatalf("ReadAuditIdentity() = (%d, %q), want (7, token-user)", userID, username)
	}
}

func TestReadAuditIdentityRejectsNonIntegralJWTUserID(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public", nil)
	context.Request.Header.Set("Authorization", "Bearer "+auditTokenWithClaims(t, jwt.MapClaims{
		"user_id": 7.5,
		"username": "token-user",
	}))

	userID, username := ReadAuditIdentity(context)
	if userID != 0 || username != "token-user" {
		t.Fatalf("ReadAuditIdentity() = (%d, %q), want (0, token-user)", userID, username)
	}
}

func auditToken(t *testing.T, userID uint64, username string) string {
	t.Helper()
	return auditTokenWithClaims(t, Claims{UserID: userID, Username: username})
}

func auditTokenWithClaims(t *testing.T, claims jwt.Claims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("audit-test-secret"))
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}
	return tokenString
}

type readTrackingBody struct {
	io.Reader
	read bool
}

func (b *readTrackingBody) Read(p []byte) (int, error) {
	b.read = true
	return b.Reader.Read(p)
}

func (b *readTrackingBody) Close() error { return nil }
