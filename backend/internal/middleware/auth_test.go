package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func TestHasAnyRole(t *testing.T) {
	tests := []struct {
		name     string
		user     []string
		required []string
		want     bool
	}{
		{
			name:     "super admin satisfies every protected role",
			user:     []string{"super_admin"},
			required: []string{"asset_admin"},
			want:     true,
		},
		{
			name:     "ordinary role satisfies an exact required role",
			user:     []string{"asset_admin"},
			required: []string{"asset_admin"},
			want:     true,
		},
		{
			name:     "ordinary role does not satisfy a different required role",
			user:     []string{"asset_admin"},
			required: []string{"user_admin"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasAnyRole(tt.user, tt.required...); got != tt.want {
				t.Fatalf("HasAnyRole(%v, %v) = %v, want %v", tt.user, tt.required, got, tt.want)
			}
		})
	}
}

func TestGenerateTokenPreservesConfiguredExpiry(t *testing.T) {
	SetJWTSecret("test-secret")

	tokenString, err := GenerateToken(42, "alice", []string{"asset_admin"}, 3)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	parsed, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte("test-secret"), nil
	})
	if err != nil {
		t.Fatalf("ParseWithClaims() error = %v", err)
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok {
		t.Fatalf("token claims type = %T, want *Claims", parsed.Claims)
	}
	if claims.ExpiresAt == nil {
		t.Fatal("token expiry is nil")
	}
	if claims.IssuedAt == nil {
		t.Fatal("token issue time is nil")
	}
	if claims.UserID != 42 || claims.Username != "alice" {
		t.Fatalf("token identity = (%d, %q), want (42, alice)", claims.UserID, claims.Username)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "asset_admin" {
		t.Fatalf("token roles = %v, want [asset_admin]", claims.Roles)
	}
	if got := claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time); got != 3*time.Hour {
		t.Fatalf("token lifetime = %v, want %v", got, 3*time.Hour)
	}
}

func TestAuthRequiredPopulatesIdentityContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetJWTSecret("test-secret")
	tokenString := signedToken(t, Claims{
		UserID:      42,
		Username:    "alice",
		Roles:       []string{"asset_admin"},
		Departments: []uint64{7, 9},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	router := gin.New()
	router.GET("/protected", AuthRequired(), func(c *gin.Context) {
		if got := GetCurrentUserID(c); got != 42 {
			t.Fatalf("user_id = %d, want 42", got)
		}
		if got := GetCurrentUsername(c); got != "alice" {
			t.Fatalf("username = %q, want alice", got)
		}
		if got := GetCurrentRoles(c); len(got) != 1 || got[0] != "asset_admin" {
			t.Fatalf("roles = %v, want [asset_admin]", got)
		}
		departments, ok := c.Get("departments")
		if !ok {
			t.Fatal("departments missing from context")
		}
		gotDepartments, ok := departments.([]uint64)
		if !ok || len(gotDepartments) != 2 || gotDepartments[0] != 7 || gotDepartments[1] != 9 {
			t.Fatalf("departments = %v, want [7 9]", departments)
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer "+tokenString)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func signedToken(t *testing.T, claims Claims) string {
	t.Helper()
	tokenString, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}
	return tokenString
}
