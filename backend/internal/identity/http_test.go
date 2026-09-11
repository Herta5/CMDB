// 本文件从 HTTP 边界验证新版身份认证流程，确保错误响应不会泄露账户存在性。
package identity_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cmdb/internal/identity"
	"cmdb/internal/platform/httpserver"
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

// TestLoginRejectsWrongPassword 防止已存在用户的错误密码被错误地接受或获得不同错误响应。
func TestLoginRejectsWrongPassword(t *testing.T) {
	user := newAuthenticationFixtureUser(t, 11, "wrong-password-user", "active")
	response := requestLogin(t, newAuthenticationServer(t, user), user.Username, "incorrect-password")
	assertAuthenticationFailure(t, response)
}

// TestLoginRejectsInactiveUser 防止已停用账户以正确凭证取得新的 JWT 会话。
func TestLoginRejectsInactiveUser(t *testing.T) {
	user := newAuthenticationFixtureUser(t, 12, "inactive-login-user", "disabled")
	response := requestLogin(t, newAuthenticationServer(t, user), user.Username, "correct-password")
	assertAuthenticationFailure(t, response)
}

// TestLoginMissingUserPerformsComparableBcryptWork 防止不存在用户绕过 bcrypt 造成可观测的用户名枚举时差。
func TestLoginMissingUserPerformsComparableBcryptWork(t *testing.T) {
	user := newAuthenticationFixtureUser(t, 13, "timing-user", "active")
	inactiveUser := newAuthenticationFixtureUser(t, 16, "inactive-timing-user", "disabled")
	server := newAuthenticationServer(t, user, inactiveUser)

	var missingElapsed, wrongPasswordElapsed, inactiveElapsed time.Duration
	for range 2 {
		missingStartedAt := time.Now()
		assertAuthenticationFailure(t, requestLogin(t, server, "missing-timing-user", "incorrect-password"))
		missingElapsed += time.Since(missingStartedAt)

		wrongPasswordStartedAt := time.Now()
		assertAuthenticationFailure(t, requestLogin(t, server, user.Username, "incorrect-password"))
		wrongPasswordElapsed += time.Since(wrongPasswordStartedAt)

		inactiveStartedAt := time.Now()
		assertAuthenticationFailure(t, requestLogin(t, server, inactiveUser.Username, "correct-password"))
		inactiveElapsed += time.Since(inactiveStartedAt)
	}
	if missingElapsed < wrongPasswordElapsed/3 {
		t.Fatalf("不存在用户的认证耗时明显低于错误密码分支：missing=%s wrong=%s", missingElapsed, wrongPasswordElapsed)
	}
	if inactiveElapsed < wrongPasswordElapsed/3 {
		t.Fatalf("失活用户的认证耗时明显低于错误密码分支：inactive=%s wrong=%s", inactiveElapsed, wrongPasswordElapsed)
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

// TestCurrentUserRejectsUserDisabledAfterTokenIssued 防止用户停用后仍可继续使用先前签发的 JWT。
func TestCurrentUserRejectsUserDisabledAfterTokenIssued(t *testing.T) {
	user := newAuthenticationFixtureUser(t, 14, "disabled-session-user", "active")
	server := newAuthenticationServer(t, user)
	login := requestLogin(t, server, user.Username, "correct-password")
	token := sessionToken(t, login)

	user.Status = "disabled"
	response := requestCurrentUser(t, server, token)
	assertUnauthorized(t, response)
}

// TestCurrentUserRejectsExpiredAndWrongSignatureTokens 验证中间件拒绝已过期及非本服务签发的 JWT。
func TestCurrentUserRejectsExpiredAndWrongSignatureTokens(t *testing.T) {
	user := newAuthenticationFixtureUser(t, 15, "invalid-token-user", "active")
	server := newAuthenticationServer(t, user)

	for _, testCase := range []struct {
		name       string
		expiresAt  time.Time
		signingKey string
	}{
		{name: "过期令牌", expiresAt: time.Now().Add(-time.Minute), signingKey: "identity-test-signing-key"},
		{name: "错误签名", expiresAt: time.Now().Add(time.Hour), signingKey: "another-test-signing-key"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := requestCurrentUser(t, server, signedTestToken(t, user, testCase.expiresAt, testCase.signingKey))
			assertUnauthorized(t, response)
		})
	}
}

// TestDeleteUserReportsRepositoryFailure 验证删除存储失败使用稳定服务错误，不能误报为用户不存在。
func TestDeleteUserReportsRepositoryFailure(t *testing.T) {
	admin := newAuthenticationFixtureUser(t, 1, "delete-admin", "active")
	admin.GlobalRole = identity.GlobalRoleSystemAdmin
	server := newAuthenticationServer(t, admin)
	token := sessionToken(t, requestLogin(t, server, admin.Username, "correct-password"))
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/users/2", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || response.Body.String() != `{"code":"USER_SERVICE_UNAVAILABLE","message":"用户服务暂不可用"}` {
		t.Fatalf("删除存储失败响应契约错误：status=%d body=%s", response.Code, response.Body.String())
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

// requestCurrentUser 以 Bearer JWT 请求当前用户接口，保持认证中间件位于真实调用路径中。
func requestCurrentUser(t *testing.T, server http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

// newAuthenticationFixtureUser 创建可用于认证场景的用户，测试失败信息不会输出其密码哈希。
func newAuthenticationFixtureUser(t *testing.T, id uint64, username, status string) *identity.User {
	t.Helper()
	hash, err := identity.HashPassword("correct-password")
	if err != nil {
		t.Fatal("准备认证测试用户失败")
	}
	return &identity.User{
		ID:           id,
		Username:     username,
		PasswordHash: hash,
		DisplayName:  "认证测试用户",
		Email:        "authentication@example.invalid",
		GlobalRole:   identity.GlobalRoleUser,
		Status:       status,
	}
}

// sessionToken 从成功登录响应中提取会话令牌，但绝不在失败诊断中输出令牌内容。
func sessionToken(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var session struct {
		Token string `json:"token"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &session) != nil || session.Token == "" {
		t.Fatal("认证测试未能取得有效会话")
	}
	return session.Token
}

// signedTestToken 仅为中间件边界测试构造指定过期时间和签名密钥的 JWT。
func signedTestToken(t *testing.T, user *identity.User, expiresAt time.Time, signingKey string) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, identity.UserClaims{
		UserID:     user.ID,
		GlobalRole: user.GlobalRole,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}).SignedString([]byte(signingKey))
	if err != nil {
		t.Fatal("构造认证测试令牌失败")
	}
	return token
}

// assertAuthenticationFailure 验证登录失败始终使用反枚举的统一错误边界。
func assertAuthenticationFailure(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusUnauthorized || response.Body.String() != `{"code":"AUTH_INVALID_CREDENTIALS","message":"用户名或密码错误"}` {
		t.Fatalf("登录失败响应必须保持统一：status=%d body=%s", response.Code, response.Body.String())
	}
}

// assertUnauthorized 验证认证中间件或当前用户检查返回稳定的失效会话响应。
func assertUnauthorized(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusUnauthorized || response.Body.String() != `{"code":"AUTH_UNAUTHORIZED","message":"身份认证已失效"}` {
		t.Fatalf("认证失败响应必须保持统一：status=%d body=%s", response.Code, response.Body.String())
	}
}

// inMemoryUserRepository 是只用于 HTTP 边界测试的确定性用户存储替身。
type inMemoryUserRepository struct {
	users []*identity.User
}

// Create 不属于本认证测试覆盖范围，因此返回明确的未实现错误。
func (inMemoryUserRepository) Create(context.Context, *identity.User) error {
	return gorm.ErrInvalidDB
}

// CreateWithPermissions 不属于认证测试范围，返回明确的未实现错误。
func (inMemoryUserRepository) CreateWithPermissions(context.Context, *identity.User, []identity.ProjectPermission) error {
	return gorm.ErrInvalidDB
}

// Delete 不属于认证接口测试范围，返回明确的未实现错误。
func (inMemoryUserRepository) Delete(context.Context, uint64) error {
	return gorm.ErrInvalidDB
}

// Update 不属于认证接口测试范围，返回明确的未实现错误。
func (inMemoryUserRepository) Update(context.Context, *identity.User) error {
	return gorm.ErrInvalidDB
}

// UpdateWithPermissions 不属于认证测试范围，返回明确的未实现错误。
func (inMemoryUserRepository) UpdateWithPermissions(context.Context, *identity.User, []identity.ProjectPermission) error {
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

// List 不属于认证接口测试范围，返回夹具副本以满足完整仓储契约。
func (r inMemoryUserRepository) List(context.Context) ([]identity.User, error) {
	users := make([]identity.User, 0, len(r.users))
	for _, user := range r.users {
		users = append(users, *user)
	}
	return users, nil
}

// UpdateStatus 不属于认证接口测试范围，返回明确的未实现错误。
func (inMemoryUserRepository) UpdateStatus(context.Context, uint64, string) error {
	return gorm.ErrInvalidDB
}
