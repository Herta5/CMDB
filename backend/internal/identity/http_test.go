// 本文件从 HTTP 边界验证新版身份认证流程，确保错误响应不会泄露账户存在性。
package identity_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github-cmdb/internal/identity"
	"github-cmdb/internal/platform/httpserver"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// TestLoginDoesNotRevealWhetherUserExists 防止攻击者通过登录响应枚举 CMDB 用户。
func TestLoginDoesNotRevealWhetherUserExists(t *testing.T) {
	response := performLogin(t, "missing", "wrong")
	if response.Code != http.StatusUnauthorized || response.Body.String() != `{"code":"AUTH_INVALID_CREDENTIALS","message":"用户名或密码错误"}` {
		t.Fatalf("登录失败响应不得泄露用户是否存在：%s", response.Body.String())
	}
}

// TestLoginIssuesJWTWithOnlyIdentityClaims 防止令牌携带用户名、密码哈希等不必要身份数据。
func TestLoginIssuesJWTWithOnlyIdentityClaims(t *testing.T) {
	hash, err := identity.HashPassword("correct-password")
	if err != nil {
		t.Fatal("准备登录测试用户失败")
	}
	user := &identity.User{
		ID:           7,
		Username:     "alice",
		PasswordHash: hash,
		DisplayName:  "Alice",
		Email:        "alice@example.invalid",
		GlobalRole:   identity.GlobalRoleSystemAdmin,
		Status:       "active",
	}
	server := newAuthenticationServer(t, user)
	response := requestLogin(t, server, user.Username, "correct-password")
	if response.Code != http.StatusOK {
		t.Fatalf("有效凭证登录状态码错误：got=%d want=%d", response.Code, http.StatusOK)
	}

	var payload struct {
		Token string `json:"token"`
		User  struct {
			ID         uint64 `json:"id"`
			GlobalRole string `json:"global_role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("登录成功响应不是有效 JSON：%v", err)
	}
	if payload.Token == "" || payload.User.ID != user.ID || payload.User.GlobalRole != identity.GlobalRoleSystemAdmin {
		t.Fatal("登录成功响应必须提供令牌和必要的公开用户信息")
	}

	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(payload.Token, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte("identity-test-signing-key"), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatal("登录签发的令牌必须可由服务签名密钥验证")
	}
	if len(claims) != 3 || claims["user_id"] != float64(user.ID) || claims["global_role"] != identity.GlobalRoleSystemAdmin {
		t.Fatal("JWT 只能包含用户标识、全局角色和过期时间")
	}
	if _, ok := claims["exp"].(float64); !ok {
		t.Fatal("JWT 必须包含过期时间")
	}
}

// TestCurrentUserRequiresJWTAndReturnsPublicIdentity 验证当前用户接口拒绝匿名访问且不会返回密码哈希。
func TestCurrentUserRequiresJWTAndReturnsPublicIdentity(t *testing.T) {
	hash, err := identity.HashPassword("correct-password")
	if err != nil {
		t.Fatal("准备当前用户测试数据失败")
	}
	user := &identity.User{
		ID:           9,
		Username:     "me-user",
		PasswordHash: hash,
		DisplayName:  "当前用户",
		Email:        "me@example.invalid",
		GlobalRole:   identity.GlobalRoleUser,
		Status:       "active",
	}
	server := newAuthenticationServer(t, user)

	anonymous := httptest.NewRecorder()
	server.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if anonymous.Code != http.StatusUnauthorized || anonymous.Body.String() != `{"code":"AUTH_UNAUTHORIZED","message":"身份认证已失效"}` {
		t.Fatalf("匿名请求必须得到稳定认证错误：status=%d body=%s", anonymous.Code, anonymous.Body.String())
	}

	login := requestLogin(t, server, user.Username, "correct-password")
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil || session.Token == "" {
		t.Fatal("当前用户测试未能取得有效会话")
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	request.Header.Set("Authorization", "Bearer "+session.Token)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "password_hash") {
		t.Fatalf("当前用户接口必须仅返回公开身份信息：status=%d", response.Code)
	}
}

// performLogin 使用完整 HTTP 路由发送登录请求，验证公开接口而非处理器内部细节。
func performLogin(t *testing.T, username, password string) *httptest.ResponseRecorder {
	t.Helper()

	server := newAuthenticationServer(t)
	return requestLogin(t, server, username, password)
}

// newAuthenticationServer 装配认证 HTTP 服务，并让测试可替换新版用户仓储。
func newAuthenticationServer(t *testing.T, users ...*identity.User) http.Handler {
	t.Helper()
	return httpserver.New(httpserver.Dependencies{
		UserRepository: inMemoryUserRepository{users: users},
		JWTSecret:      "identity-test-signing-key",
	})
}

// requestLogin 以 JSON 调用登录接口，避免测试直接绕过路由或认证处理器。
func requestLogin(t *testing.T, server http.Handler, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

// inMemoryUserRepository 是只用于 HTTP 边界测试的确定性用户存储替身。
type inMemoryUserRepository struct {
	users []*identity.User
}

// Create 不属于本认证测试覆盖范围，因此返回明确的未实现错误。
func (inMemoryUserRepository) Create(context.Context, *identity.User) error {
	return gorm.ErrInvalidDB
}

// FindByID 不属于本登录测试覆盖范围，因此返回未找到。
func (r inMemoryUserRepository) FindByID(_ context.Context, id uint64) (*identity.User, error) {
	for _, user := range r.users {
		if user.ID == id {
			return user, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// FindByUsername 按测试夹具中的用户名查找，以覆盖存在和不存在两种认证分支。
func (r inMemoryUserRepository) FindByUsername(_ context.Context, username string) (*identity.User, error) {
	for _, user := range r.users {
		if user.Username == username {
			return user, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}
