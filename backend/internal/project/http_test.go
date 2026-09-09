// 本文件从真实 HTTP 路由验证项目接口的身份边界，避免仅测试处理器内部调用。
package project_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github-cmdb/internal/identity"
	"github-cmdb/internal/platform/httpserver"
	"github-cmdb/internal/project"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestProjectHTTPRejectsNonAdministratorCreation 防止普通用户创建新的全局项目隔离边界。
func TestProjectHTTPRejectsNonAdministratorCreation(t *testing.T) {
	server := newProjectHTTPServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(`{"code":"cloud","name":"云平台"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+projectTestToken(t, 7, identity.GlobalRoleUser))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || response.Body.String() != `{"code":"PROJECT_FORBIDDEN","message":"无权执行该操作"}` {
		t.Fatalf("普通用户创建项目必须被稳定拒绝：status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestProjectHTTPRejectsNonAdministratorDeletion 防止普通用户删除其他成员仍需访问的项目隔离边界。
func TestProjectHTTPRejectsNonAdministratorDeletion(t *testing.T) {
	server, db := newProjectHTTPServerWithDatabase(t)
	created := createProjectThroughHTTP(t, server, `{"code":"cloud","name":"云平台"}`)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+strconv.FormatUint(created.ID, 10), nil)
	request.Header.Set("Authorization", "Bearer "+projectTestToken(t, 7, identity.GlobalRoleUser))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || response.Body.String() != `{"code":"PROJECT_FORBIDDEN","message":"无权执行该操作"}` {
		t.Fatalf("普通用户删除项目必须被稳定拒绝：status=%d body=%s", response.Code, response.Body.String())
	}
	var remaining int64
	if err := db.Model(&project.Project{}).Where("id = ?", created.ID).Count(&remaining).Error; err != nil || remaining != 1 {
		t.Fatalf("未授权删除不得改变项目记录：count=%d err=%v", remaining, err)
	}
}

// TestProjectHTTPAdministratorDeletesProject 验证系统管理员可删除项目本体，供后续资源与成员关系清理流程消费。
func TestProjectHTTPAdministratorDeletesProject(t *testing.T) {
	server, db := newProjectHTTPServerWithDatabase(t)
	created := createProjectThroughHTTP(t, server, `{"code":"cloud","name":"云平台"}`)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+strconv.FormatUint(created.ID, 10), nil)
	request.Header.Set("Authorization", "Bearer "+projectTestToken(t, 1, identity.GlobalRoleSystemAdmin))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("系统管理员删除项目状态码错误：status=%d body=%s", response.Code, response.Body.String())
	}
	var remaining int64
	if err := db.Model(&project.Project{}).Where("id = ?", created.ID).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("系统管理员删除必须移除项目记录：count=%d err=%v", remaining, err)
	}
}

// TestProjectHTTPUpdateKeepsCodeImmutable 防止管理员通过更新请求改写已用于资源归属的项目编码。
func TestProjectHTTPUpdateKeepsCodeImmutable(t *testing.T) {
	server := newProjectHTTPServer(t)
	created := createProjectThroughHTTP(t, server, `{"code":"cloud","name":"云平台"}`)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/projects/"+strconv.FormatUint(created.ID, 10), bytes.NewBufferString(`{"code":"changed","name":"云资源平台","status":"disabled"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+projectTestToken(t, 1, identity.GlobalRoleSystemAdmin))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("管理员更新项目状态码错误：status=%d body=%s", response.Code, response.Body.String())
	}
	var updated struct {
		Code   string `json:"code"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatalf("更新项目响应不是有效 JSON：%v", err)
	}
	if updated.Code != "cloud" || updated.Name != "云资源平台" || updated.Status != project.ProjectStatusDisabled {
		t.Fatalf("项目更新必须保留编码并更新可变资料：got=%+v", updated)
	}
}

// TestProjectHTTPListsOnlyCurrentUserMembership 防止普通用户通过公开项目列表读取不属于自己的项目。
func TestProjectHTTPListsOnlyCurrentUserMembership(t *testing.T) {
	server, db := newProjectHTTPServerWithDatabase(t)
	first := createProjectThroughHTTP(t, server, `{"code":"cloud","name":"云平台"}`)
	if err := db.Create(&identity.User{ID: 7, Username: "http-viewer", PasswordHash: "test-hash", DisplayName: "接口查看者", GlobalRole: identity.GlobalRoleUser, Status: "active"}).Error; err != nil {
		t.Fatalf("准备项目列表用户失败：%v", err)
	}
	if err := db.Create(&project.MemberRole{ProjectID: first.ID, UserID: 7, Role: project.MemberRoleViewer}).Error; err != nil {
		t.Fatalf("准备项目列表成员关系失败：%v", err)
	}
	if response := createProjectResponse(t, server, `{"code":"data","name":"数据平台"}`); response.Code != http.StatusCreated {
		t.Fatalf("准备第二个项目失败：status=%d body=%s", response.Code, response.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	request.Header.Set("Authorization", "Bearer "+projectTestToken(t, 7, identity.GlobalRoleUser))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("普通用户查询项目列表状态码错误：status=%d body=%s", response.Code, response.Body.String())
	}
	var projects []struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &projects); err != nil {
		t.Fatalf("项目列表响应不是有效 JSON：%v", err)
	}
	if len(projects) != 1 || projects[0].Code != "cloud" {
		t.Fatalf("项目列表不得泄露非成员项目：got=%+v", projects)
	}
}

// createdProject 是创建项目接口测试中需要继续操作的最小公开资料。
type createdProject struct {
	ID uint64 `json:"id"`
}

// createProjectThroughHTTP 使用管理员会话创建测试项目，确保后续测试复用真实的公开创建路径。
func createProjectThroughHTTP(t *testing.T, server http.Handler, body string) createdProject {
	t.Helper()
	response := createProjectResponse(t, server, body)
	if response.Code != http.StatusCreated {
		t.Fatalf("准备项目失败：status=%d body=%s", response.Code, response.Body.String())
	}
	var created createdProject
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.ID == 0 {
		t.Fatalf("创建项目响应缺少项目标识：err=%v body=%s", err, response.Body.String())
	}
	return created
}

// createProjectResponse 以管理员会话调用创建接口，使需要检查失败响应的测试能够复用真实请求构造。
func createProjectResponse(t *testing.T, server http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+projectTestToken(t, 1, identity.GlobalRoleSystemAdmin))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

// newProjectHTTPServer 装配使用真实 SQLite 持久化和认证中间件的项目 HTTP 服务。
func newProjectHTTPServer(t *testing.T) http.Handler {
	t.Helper()
	server, _ := newProjectHTTPServerWithDatabase(t)
	return server
}

// newProjectHTTPServerWithDatabase 返回 HTTP 服务及测试数据库，供路由测试准备真实项目成员关系。
func newProjectHTTPServerWithDatabase(t *testing.T) (http.Handler, *gorm.DB) {
	t.Helper()
	sqlDB, err := sql.Open("sqlite3", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("创建项目 HTTP 测试数据库失败：%v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(sqlite.Dialector{Conn: sqlDB}, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("打开项目 HTTP 测试数据库失败：%v", err)
	}
	if err := db.AutoMigrate(&identity.User{}, &project.Project{}, &project.MemberRole{}); err != nil {
		t.Fatalf("创建项目 HTTP 测试表失败：%v", err)
	}
	return httpserver.New(httpserver.Dependencies{Database: db, JWTSecret: "project-http-test-key"}), db
}

// projectTestToken 仅构造已签名的最小身份声明，避免测试中通过登录接口引入密码无关因素。
func projectTestToken(t *testing.T, userID uint64, globalRole string) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, identity.UserClaims{
		UserID:     userID,
		GlobalRole: globalRole,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}).SignedString([]byte("project-http-test-key"))
	if err != nil {
		t.Fatalf("签发项目 HTTP 测试令牌失败：%v", err)
	}
	return token
}
