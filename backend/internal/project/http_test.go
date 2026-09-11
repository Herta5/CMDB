// 本文件从真实 HTTP 路由验证项目接口的身份边界，避免仅测试处理器内部调用。
package project_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"cmdb/internal/audit"
	"cmdb/internal/identity"
	"cmdb/internal/platform/httpserver"
	"cmdb/internal/project"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestProjectHTTPRejectsNonAdministratorCreation 防止普通用户创建新的全局项目隔离边界。
func TestProjectHTTPRejectsNonAdministratorCreation(t *testing.T) {
	server, db := newProjectHTTPServerWithDatabase(t)
	createProjectUser(t, db, 7)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(`{"code":"cloud","name":"云平台"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+projectTestToken(t, 7, identity.GlobalRoleUser))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || response.Body.String() != `{"code":"PROJECT_FORBIDDEN","message":"无权执行该操作"}` {
		t.Fatalf("普通用户创建项目必须被稳定拒绝：status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestProjectHTTPHidesProjectFromNonAdministratorDeletion 防止普通用户通过删除接口枚举项目存在性。
func TestProjectHTTPHidesProjectFromNonAdministratorDeletion(t *testing.T) {
	server, db := newProjectHTTPServerWithDatabase(t)
	createProjectUser(t, db, 7)
	created := createProjectThroughHTTP(t, server, `{"code":"cloud","name":"云平台"}`)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+strconv.FormatUint(created.ID, 10), nil)
	request.Header.Set("Authorization", "Bearer "+projectTestToken(t, 7, identity.GlobalRoleUser))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || response.Body.String() != `{"code":"PROJECT_NOT_FOUND","message":"项目不存在"}` {
		t.Fatalf("普通用户删除项目必须隐藏目标存在性：status=%d body=%s", response.Code, response.Body.String())
	}
	var remaining int64
	if err := db.Model(&project.Project{}).Where("id = ?", created.ID).Count(&remaining).Error; err != nil || remaining != 1 {
		t.Fatalf("未授权删除不得改变项目记录：count=%d err=%v", remaining, err)
	}
}

// TestProjectHTTPHidesExistingAndMissingProjectWrites 防止普通用户从项目更新或删除响应区分项目是否存在。
func TestProjectHTTPHidesExistingAndMissingProjectWrites(t *testing.T) {
	server, db := newProjectHTTPServerWithDatabase(t)
	createProjectUser(t, db, 7)
	created := createProjectThroughHTTP(t, server, `{"code":"cloud","name":"云平台"}`)
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			for _, targetID := range []uint64{created.ID, 999} {
				request := httptest.NewRequest(method, "/api/v1/projects/"+strconv.FormatUint(targetID, 10), bytes.NewBufferString(`{"name":"越权修改","status":"disabled"}`))
				if method == http.MethodPut {
					request.Header.Set("Content-Type", "application/json")
				}
				request.Header.Set("Authorization", "Bearer "+projectTestToken(t, 7, identity.GlobalRoleUser))
				response := httptest.NewRecorder()
				server.ServeHTTP(response, request)
				if response.Code != http.StatusNotFound || response.Body.String() != `{"code":"PROJECT_NOT_FOUND","message":"项目不存在"}` {
					t.Fatalf("普通用户%s必须隐藏目标存在性：target=%d status=%d body=%s", method, targetID, response.Code, response.Body.String())
				}
			}
		})
	}
	var persisted project.Project
	if err := db.First(&persisted, created.ID).Error; err != nil {
		t.Fatalf("读取项目记录失败：%v", err)
	}
	if persisted.Name != "云平台" || persisted.Status != project.ProjectStatusEnabled {
		t.Fatalf("普通用户项目写请求不得改变项目：project=%+v", persisted)
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
	if err := db.Create(&identity.User{ID: 7, Username: "member_7", PasswordHash: "test-hash", DisplayName: "接口成员", GlobalRole: identity.GlobalRoleUser, Status: "active"}).Error; err != nil {
		t.Fatalf("准备项目列表用户失败：%v", err)
	}
	if err := db.Create(&project.MemberRole{ProjectID: first.ID, UserID: 7, Role: project.MemberRoleMember}).Error; err != nil {
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

// TestProjectOwnerUsesUsername 验证负责人解析、清空和读取均只公开用户名。
func TestProjectOwnerUsesUsername(t *testing.T) {
	server, db := newProjectHTTPServerWithDatabase(t)
	createProjectUser(t, db, 7)
	created := createProjectResponse(t, server, `{"code":"owner","name":"负责人项目","owner_username":"member_7"}`)
	var payload map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil || created.Code != http.StatusCreated || payload["owner_username"] != "member_7" {
		t.Fatalf("创建项目必须返回负责人用户名：%s", created.Body.String())
	}
	if _, exists := payload["owner_user_id"]; exists {
		t.Fatal("项目不得暴露负责人内部 ID")
	}
	id := uint64(payload["id"].(float64))
	var persisted project.Project
	if err := db.First(&persisted, id).Error; err != nil || persisted.OwnerUserID == nil || *persisted.OwnerUserID != 7 {
		t.Fatal("负责人用户名必须解析为内部关联")
	}
	for _, path := range []string{"/api/v1/projects", "/api/v1/projects/" + strconv.FormatUint(id, 10)} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+projectTestToken(t, 1, identity.GlobalRoleSystemAdmin))
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"owner_username":"member_7"`)) || bytes.Contains(response.Body.Bytes(), []byte(`"owner_user_id"`)) {
			t.Fatalf("读取项目必须返回负责人用户名：%s", response.Body.String())
		}
	}
	for _, owner := range []string{`"project_admin"`, `null`} {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/projects/"+strconv.FormatUint(id, 10), bytes.NewBufferString(`{"name":"负责人项目","status":"enabled","owner_username":`+owner+`}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+projectTestToken(t, 1, identity.GlobalRoleSystemAdmin))
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"owner_username":`+owner)) || bytes.Contains(response.Body.Bytes(), []byte(`"owner_user_id"`)) {
			t.Fatalf("负责人更新或清空失败：%s", response.Body.String())
		}
	}
	for _, owner := range []string{`"missing_user"`, `"invalid-name"`} {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/projects/"+strconv.FormatUint(id, 10), bytes.NewBufferString(`{"name":"错误更新","status":"enabled","owner_username":`+owner+`}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+projectTestToken(t, 1, identity.GlobalRoleSystemAdmin))
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("无效负责人更新必须失败：%s", response.Body.String())
		}
	}
	if err := db.First(&persisted, id).Error; err != nil || persisted.Name != "负责人项目" || persisted.OwnerUserID != nil {
		t.Fatal("负责人清空必须持久化，无效更新不得改变项目")
	}
	var logs []audit.Log
	if err := db.Where("project_id = ?", id).Order("id ASC").Find(&logs).Error; err != nil || len(logs) != 3 {
		t.Fatal("负责人变更应产生三条成功审计")
	}
	if !bytes.Contains(logs[0].Detail, []byte(`"owner_username":"member_7"`)) || !bytes.Contains(logs[1].Detail, []byte(`"previous_owner_username":"member_7"`)) || !bytes.Contains(logs[1].Detail, []byte(`"owner_username":"project_admin"`)) {
		t.Fatal("负责人审计必须保留用户名变化")
	}
}

// TestProjectOwnerRejectsUnknownAndLegacyInput 防止不存在的负责人和旧 ID 请求被静默接受。
func TestProjectOwnerRejectsUnknownAndLegacyInput(t *testing.T) {
	for _, test := range []struct{ name, body, code string }{
		{"负责人不存在", `{"code":"owner","name":"项目","owner_username":"missing_user"}`, "PROJECT_OWNER_NOT_FOUND"},
		{"旧负责人字段", `{"code":"owner","name":"项目","owner_user_id":1}`, "PROJECT_INVALID_REQUEST"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := createProjectResponse(t, newProjectHTTPServer(t), test.body)
			if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte(test.code)) {
				t.Fatalf("无效负责人必须被拒绝：%s", response.Body.String())
			}
		})
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
	if err := db.AutoMigrate(&identity.User{}, &project.Project{}, &project.MemberRole{}, &audit.Log{}); err != nil {
		t.Fatalf("创建项目 HTTP 测试表失败：%v", err)
	}
	// 所有项目接口必须验证当前账户，测试管理员也必须是真实持久化的有效身份。
	if err := db.Create(&identity.User{ID: 1, Username: "project_admin", DisplayName: "系统管理员", GlobalRole: identity.GlobalRoleSystemAdmin, Status: "active"}).Error; err != nil {
		t.Fatal("准备项目管理员失败")
	}
	return httpserver.New(httpserver.Dependencies{Database: db, JWTSecret: "project-http-test-key"}), db
}

// projectTestToken 仅构造已签名的最小身份声明，避免测试中通过登录接口引入密码无关因素。
func projectTestToken(t *testing.T, userID uint64, globalRole string) string {
	t.Helper()
	username := "project_admin"
	if userID != 1 {
		username = "member_" + strconv.FormatUint(userID, 10)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, identity.UserClaims{
		Username:   username,
		GlobalRole: globalRole,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}).SignedString(projectTestSigningKey(userID))
	if err != nil {
		t.Fatalf("签发项目 HTTP 测试令牌失败：%v", err)
	}
	return token
}

// projectTestSigningKey 将项目 HTTP 夹具绑定到数据库实际账号，保持真实认证中间件参与权限验收。
func projectTestSigningKey(userID uint64) []byte {
	mac := hmac.New(sha256.New, []byte("project-http-test-key"))
	mac.Write([]byte("cmdb.jwt.account.v1:" + strconv.FormatUint(userID, 10)))
	return mac.Sum(nil)
}
