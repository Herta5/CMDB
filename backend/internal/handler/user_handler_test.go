package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github-cmdb/internal/config"
	"github-cmdb/internal/middleware"
	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
	"github-cmdb/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestLoginSignsTokenUsingLoadedExpiry(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{{"3", 3 * time.Hour}, {"invalid", 24 * time.Hour}, {"0", 24 * time.Hour}} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("JWT_EXPIRE_HOUR", tc.value)
			cfg := config.Load()
			middleware.SetJWTSecret(cfg.JWT.Secret)
			user := model.User{ID: 42, Username: "alice", DisplayName: "Alice", Status: "active", Roles: model.JSONArray{"viewer"}}
			if err := user.SetPassword("correct-password"); err != nil {
				t.Fatal(err)
			}
			// Replace only database I/O; the real service verifies the password,
			// the real handler binds the request and signs the returned token.
			db, err := gorm.Open(mysql.New(mysql.Config{DSN: "unused:unused@tcp(localhost:3306)/test", SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				*(tx.Statement.Dest.(*model.User)) = user
				tx.RowsAffected = 1
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {}); err != nil {
				t.Fatal(err)
			}
			h := NewUserHandler(service.NewUserSvc(repository.NewUserRepo(db)), cfg.JWT)
			router := gin.New()
			router.POST("/login", h.Login)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"alice","password":"correct-password"}`)))
			if response.Code != http.StatusOK {
				t.Fatalf("login = %d: %s", response.Code, response.Body.String())
			}
			var result struct {
				Data struct {
					Token string `json:"token"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			claims := &middleware.Claims{}
			if _, err := jwt.ParseWithClaims(result.Data.Token, claims, func(*jwt.Token) (interface{}, error) { return []byte(cfg.JWT.Secret), nil }); err != nil {
				t.Fatal(err)
			}
			if got := claims.ExpiresAt.Sub(claims.IssuedAt.Time); got != tc.want {
				t.Fatalf("login token lifetime = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestLoginResponseIncludesAuthenticatedUserIdentity(t *testing.T) {
	user := &model.User{
		ID:          42,
		Username:    "alice",
		DisplayName: "Alice Chen",
		Roles:       model.JSONArray{"asset_mgr"},
	}

	response := loginResponse(user, "token-123")

	if response["token"] != "token-123" {
		t.Fatalf("token = %v, want token-123", response["token"])
	}
	if response["user_id"] != uint64(42) {
		t.Fatalf("user_id = %v, want 42", response["user_id"])
	}
	if response["username"] != "alice" {
		t.Fatalf("username = %v, want alice", response["username"])
	}
	if response["display_name"] != "Alice Chen" {
		t.Fatalf("display_name = %v, want Alice Chen", response["display_name"])
	}
	if roles, ok := response["roles"].(model.JSONArray); !ok || len(roles) != 1 || roles[0] != "asset_mgr" {
		t.Fatalf("roles = %v, want [asset_mgr]", response["roles"])
	}
}
