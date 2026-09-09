// 本文件通过真实登录、项目 API 和数据库验证第一阶段可交付的身份及项目隔离流程。
package httpserver_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"cmdb/internal/identity"
	"cmdb/internal/platform/httpserver"
	"cmdb/internal/project"
	cloudresource "cmdb/internal/resource"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestProjectBoundaryEndToEnd 覆盖真实密码登录、授权、越权、成员撤销以及数据未被越权修改。
func TestProjectBoundaryEndToEnd(t *testing.T) {
	server, password := integrationServer(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member-a", password)
	first := integrationRequest(t, server, admin, "POST", "/api/v1/projects", map[string]any{"code": "cloud-a", "name": "云项目甲"}, 201)
	second := integrationRequest(t, server, admin, "POST", "/api/v1/projects", map[string]any{"code": "cloud-b", "name": "云项目乙"}, 201)
	var firstProject, secondProject project.Project
	decodeIntegration(t, first, &firstProject)
	decodeIntegration(t, second, &secondProject)
	firstPath := "/api/v1/projects/" + strconv.FormatUint(firstProject.ID, 10)
	secondPath := "/api/v1/projects/" + strconv.FormatUint(secondProject.ID, 10)
	integrationRequest(t, server, admin, "POST", firstPath+"/members", map[string]any{"user_id": 2, "role": "viewer"}, 201)

	var me map[string]any
	decodeIntegration(t, integrationRequest(t, server, member, "GET", "/api/v1/me", nil, 200), &me)
	if me["id"] != float64(2) || me["password_hash"] != nil || me["password"] != nil {
		t.Fatal("当前身份必须只包含已登录用户的公开资料")
	}
	var projects []project.Project
	decodeIntegration(t, integrationRequest(t, server, member, "GET", "/api/v1/projects", nil, 200), &projects)
	if len(projects) != 1 || projects[0].ID != firstProject.ID {
		t.Fatal("项目列表必须只返回当前用户所属项目")
	}
	integrationRequest(t, server, member, "GET", firstPath, nil, 200)
	integrationRequest(t, server, member, "GET", firstPath+"/members", nil, 200)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		denied := integrationRequest(t, server, member, method, secondPath, map[string]any{"name": "越权修改"}, 404)
		missing := integrationRequest(t, server, member, method, "/api/v1/projects/999999", map[string]any{"name": "越权修改"}, 404)
		if denied.Body.String() != missing.Body.String() {
			t.Fatalf("%s 不得泄露非成员项目是否存在", method)
		}
	}
	integrationRequest(t, server, member, "GET", secondPath+"/members", nil, 404)
	integrationRequest(t, server, member, "POST", firstPath+"/members", map[string]any{"user_id": 3, "role": "member"}, 404)
	integrationRequest(t, server, "", "GET", firstPath, nil, 401)
	var unchanged project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, "GET", secondPath, nil, 200), &unchanged)
	if unchanged.Name != "云项目乙" {
		t.Fatal("越权请求不得改变目标项目")
	}

	// 移除成员后继续使用原 JWT，权限必须立即反映数据库中的当前成员关系。
	integrationRequest(t, server, admin, "DELETE", firstPath+"/members/2", nil, 204)
	integrationRequest(t, server, member, "GET", firstPath, nil, 404)
	decodeIntegration(t, integrationRequest(t, server, member, "GET", "/api/v1/projects", nil, 200), &projects)
	if len(projects) != 0 {
		t.Fatal("移除成员后不得继续列出原项目")
	}
}

// TestHealthEndpoint 验证镜像健康探针在无需用户会话时可检测服务存活。
func TestHealthEndpoint(t *testing.T) {
	server, _ := integrationServer(t)
	response := integrationRequest(t, server, "", "GET", "/health", nil, 200)
	if response.Body.String() != `{"status":"ok"}` {
		t.Fatal("健康响应不得包含内部配置或凭证")
	}
}

// TestSystemAdministratorManagesUsers 验证用户管理仅暴露公开资料，并立即执行停用状态。
func TestSystemAdministratorManagesUsers(t *testing.T) {
	server, password := integrationServer(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member-a", password)
	created := integrationRequest(t, server, admin, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "cloud-user", "password": "secure-user-password", "display_name": "云资源用户", "email": "cloud@example.invalid",
	}, http.StatusCreated)
	if strings.Contains(created.Body.String(), "password") {
		t.Fatal("创建用户响应不得包含密码或密码哈希")
	}
	var user identity.User
	decodeIntegration(t, created, &user)
	if user.ID == 0 || user.Username != "cloud-user" || user.GlobalRole != identity.GlobalRoleUser || user.Status != "active" {
		t.Fatal("创建用户必须返回默认启用的普通用户公开资料")
	}
	integrationRequest(t, server, member, http.MethodGet, "/api/v1/users", nil, http.StatusForbidden)
	listed := integrationRequest(t, server, admin, http.MethodGet, "/api/v1/users", nil, http.StatusOK)
	if strings.Contains(listed.Body.String(), "password_hash") {
		t.Fatal("用户列表不得包含密码哈希")
	}
	integrationRequest(t, server, admin, http.MethodPut, "/api/v1/users/"+strconv.FormatUint(user.ID, 10)+"/status", map[string]any{"status": "disabled"}, http.StatusOK)
	integrationRequest(t, server, "", http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "cloud-user", "password": "secure-user-password"}, http.StatusUnauthorized)
}

// TestProjectAdministratorReadsCandidatesAndManagesMemberRoles 验证项目管理员只能在所属项目内查询候选身份并管理角色。
func TestProjectAdministratorReadsCandidatesAndManagesMemberRoles(t *testing.T) {
	server, password := integrationServer(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member-a", password)
	var created project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "members", "name": "成员项目"}, http.StatusCreated), &created)
	path := "/api/v1/projects/" + strconv.FormatUint(created.ID, 10)
	integrationRequest(t, server, admin, http.MethodPost, path+"/members", map[string]any{"user_id": 2, "role": "project_admin"}, http.StatusCreated)
	candidates := integrationRequest(t, server, member, http.MethodGet, path+"/member-candidates", nil, http.StatusOK)
	if strings.Contains(candidates.Body.String(), "email") || strings.Contains(candidates.Body.String(), "password") {
		t.Fatal("成员候选接口不得暴露邮箱或认证字段")
	}
	var values []struct {
		ID       uint64 `json:"id"`
		Username string `json:"username"`
	}
	decodeIntegration(t, candidates, &values)
	if len(values) != 2 || values[0].ID == 0 || values[0].Username == "" {
		t.Fatal("项目管理员必须能读取可添加用户的最小公开身份")
	}
	members := integrationRequest(t, server, member, http.MethodGet, path+"/members", nil, http.StatusOK)
	if !strings.Contains(members.Body.String(), "member-a") || strings.Contains(members.Body.String(), "password") {
		t.Fatal("成员列表必须包含公开用户名且不得包含认证字段")
	}
}

// TestProjectSourceAPINeverReturnsCredentials 验证接入源接口权限和凭证响应边界。
func TestProjectSourceAPINeverReturnsCredentials(t *testing.T) {
	server, password := integrationServer(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member-a", password)
	var created project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "sources", "name": "接入项目"}, http.StatusCreated), &created)
	path := "/api/v1/projects/" + strconv.FormatUint(created.ID, 10)
	integrationRequest(t, server, admin, http.MethodPost, path+"/members", map[string]any{"user_id": 2, "role": "viewer"}, http.StatusCreated)
	response := integrationRequest(t, server, admin, http.MethodPost, path+"/sources", map[string]any{"provider": "aws", "name": "AWS 生产账号", "region": "cn-north-1", "credential": map[string]any{"access_key_id": "example-id", "secret_access_key": "example-secret"}}, http.StatusCreated)
	if strings.Contains(response.Body.String(), "example") || strings.Contains(response.Body.String(), "encrypted") {
		t.Fatal("接入源响应不得暴露凭证明文或密文字段")
	}
	listed := integrationRequest(t, server, member, http.MethodGet, path+"/sources?provider=aws", nil, http.StatusOK)
	if strings.Contains(listed.Body.String(), "example") || !strings.Contains(listed.Body.String(), "AWS 生产账号") {
		t.Fatal("成员列表响应必须可用且不含凭证")
	}
	integrationRequest(t, server, member, http.MethodPost, path+"/sources", map[string]any{"provider": "aws", "name": "禁止创建", "credential": map[string]any{"token": "value"}}, http.StatusNotFound)
	var source cloudresource.Source
	decodeIntegration(t, response, &source)
	integrationRequest(t, server, admin, http.MethodPost, path+"/sources/"+strconv.FormatUint(source.ID, 10)+"/sync", nil, http.StatusOK)
	updated := integrationRequest(t, server, admin, http.MethodPut, path+"/sources/"+strconv.FormatUint(source.ID, 10), map[string]any{"name": "AWS 更新账号", "region": "ap-east-1", "config": map[string]any{"environment": "production"}, "enabled": true, "sync_interval_minutes": 120}, http.StatusOK)
	if strings.Contains(updated.Body.String(), "example") || !strings.Contains(updated.Body.String(), "AWS 更新账号") {
		t.Fatal("更新接入源必须保留凭证且响应不得暴露凭证")
	}
	jobs := integrationRequest(t, server, member, http.MethodGet, path+"/sync-jobs?source_id="+strconv.FormatUint(source.ID, 10), nil, http.StatusOK)
	if !strings.Contains(jobs.Body.String(), `"trigger":"manual"`) {
		t.Fatal("项目成员必须能查询当前项目的同步任务")
	}
	resources := integrationRequest(t, server, member, http.MethodGet, path+"/resources?provider=aws&resource_type=ec2", nil, http.StatusOK)
	if !strings.Contains(resources.Body.String(), "i-integration") || !strings.Contains(resources.Body.String(), "10.0.0.8") {
		t.Fatal("同步资源查询必须包含模拟采集器输出及端点")
	}
	integrationRequest(t, server, admin, http.MethodDelete, path+"/sources/"+strconv.FormatUint(source.ID, 10), nil, http.StatusNoContent)
	integrationRequest(t, server, member, http.MethodGet, path+"/sources", nil, http.StatusOK)
}

type integrationCollector struct{}

func (integrationCollector) Collect(context.Context, cloudresource.Source, []byte) ([]cloudresource.CollectionResult, error) {
	return []cloudresource.CollectionResult{{ResourceType: "ec2", Snapshots: []cloudresource.Snapshot{{ResourceType: "ec2", ExternalID: "i-integration", Name: "集成计算节点", CloudStatus: "running", Endpoints: []cloudresource.EndpointSnapshot{{Kind: "private", Address: "10.0.0.8"}}}}}}, nil
}

// TestIssuedAdminSessionUsesCurrentAccount 验证旧管理员 JWT 不能绕过数据库中的实时停用、删除或降权。
func TestIssuedAdminSessionUsesCurrentAccount(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*gorm.DB) error
		status int
	}{
		{"停用", func(db *gorm.DB) error {
			return db.Model(&identity.User{}).Where("id = ?", 1).Update("status", "disabled").Error
		}, 401},
		{"删除", func(db *gorm.DB) error { return db.Delete(&identity.User{}, 1).Error }, 401},
		{"降为普通用户", func(db *gorm.DB) error {
			return db.Model(&identity.User{}).Where("id = ?", 1).Update("global_role", "user").Error
		}, 404},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server, password, db := integrationServerWithDatabase(t)
			token := loginUser(t, server, "operator", password)
			var created project.Project
			decodeIntegration(t, integrationRequest(t, server, token, "POST", "/api/v1/projects", map[string]any{"code": "cloud", "name": "云项目"}, 201), &created)
			path := "/api/v1/projects/" + strconv.FormatUint(created.ID, 10)
			integrationRequest(t, server, token, "GET", path, nil, 200)
			if err := scenario.change(db); err != nil {
				t.Fatal("更新当前账户状态失败")
			}
			for _, method := range []string{"GET", "PUT", "DELETE"} {
				integrationRequest(t, server, token, method, path, map[string]any{"name": "禁止修改"}, scenario.status)
			}
			integrationRequest(t, server, token, "GET", path+"/members", nil, scenario.status)
			if scenario.status == 401 {
				response := integrationRequest(t, server, token, "GET", "/api/v1/projects", nil, 401)
				if response.Body.String() != `{"code":"AUTH_UNAUTHORIZED","message":"身份认证已失效"}` {
					t.Fatal("不可用账户必须使用统一认证错误")
				}
				integrationRequest(t, server, token, "POST", "/api/v1/projects", map[string]any{"code": "forbidden", "name": "禁止创建"}, 401)
			} else {
				var projects []project.Project
				decodeIntegration(t, integrationRequest(t, server, token, "GET", "/api/v1/projects", nil, 200), &projects)
				if len(projects) != 0 {
					t.Fatal("降权后的非成员不得列出项目")
				}
				integrationRequest(t, server, token, "POST", "/api/v1/projects", map[string]any{"code": "forbidden", "name": "禁止创建"}, 403)
			}
			var unchanged project.Project
			if err := db.First(&unchanged, created.ID).Error; err != nil || unchanged.Name != "云项目" {
				t.Fatal("失去权限后不能修改或删除原项目")
			}
		})
	}
}

// TestEnginesKeepIndependentSigningKeys 防止后创建的服务实例替换已有服务的 JWT 校验密钥。
func TestEnginesKeepIndependentSigningKeys(t *testing.T) {
	first, firstPassword := integrationServer(t)
	firstToken := loginUser(t, first, "operator", firstPassword)
	integrationRequest(t, first, firstToken, "GET", "/api/v1/me", nil, 200)
	second, secondPassword := integrationServer(t)
	secondToken := loginUser(t, second, "operator", secondPassword)
	integrationRequest(t, first, firstToken, "GET", "/api/v1/me", nil, 200)
	integrationRequest(t, second, secondToken, "GET", "/api/v1/me", nil, 200)
	integrationRequest(t, first, secondToken, "GET", "/api/v1/me", nil, 401)
	integrationRequest(t, second, firstToken, "GET", "/api/v1/me", nil, 401)
}

// integrationServer 为每个流程建立独立的真实数据库；凭证仅在运行时生成且不进入失败输出。
func integrationServer(t *testing.T) (http.Handler, string) {
	t.Helper()
	server, password, _ := integrationServerWithDatabase(t)
	return server, password
}

// integrationServerWithDatabase 允许通过真实持久化变更验证已签发会话的授权撤销。
func integrationServerWithDatabase(t *testing.T) (http.Handler, string, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("打开验收数据库失败：%v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("读取验收数据库连接失败")
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&identity.User{}, &project.Project{}, &project.MemberRole{}, &cloudresource.Source{}, &cloudresource.Resource{}, &cloudresource.Endpoint{}, &cloudresource.SyncJob{}, &cloudresource.AuditLog{}); err != nil {
		t.Fatalf("创建验收表失败：%v", err)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal("生成验收凭证失败")
	}
	password := hex.EncodeToString(secret)
	hash, err := identity.HashPassword(password)
	if err != nil {
		t.Fatal("生成验收密码哈希失败")
	}
	for _, user := range []identity.User{
		{ID: 1, Username: "operator", PasswordHash: hash, DisplayName: "系统管理员", GlobalRole: "system_admin", Status: "active"},
		{ID: 2, Username: "member-a", PasswordHash: hash, DisplayName: "项目查看者", GlobalRole: "user", Status: "active"},
	} {
		if err := db.Create(&user).Error; err != nil {
			t.Fatal("准备验收身份失败")
		}
	}
	if _, err := rand.Read(secret); err != nil {
		t.Fatal("生成签名密钥失败")
	}
	return httpserver.New(httpserver.Dependencies{Database: db, JWTSecret: hex.EncodeToString(secret), EncryptionKey: "integration-encryption-key", Collectors: map[string]cloudresource.Collector{"aws": integrationCollector{}}}), password, db
}

// loginUser 必须通过公开登录流程获取 JWT，不能以自行签发令牌跳过密码认证。
func loginUser(t *testing.T, server http.Handler, username, password string) string {
	t.Helper()
	response := integrationRequest(t, server, "", "POST", "/api/v1/auth/login", map[string]any{"username": username, "password": password}, 200)
	var session struct {
		Token string `json:"token"`
	}
	decodeIntegration(t, response, &session)
	if session.Token == "" {
		t.Fatal("登录响应缺少会话")
	}
	return session.Token
}

// integrationRequest 只报告方法、路径与状态码，避免登录响应中的令牌进入测试日志。
func integrationRequest(t *testing.T, server http.Handler, token, method, path string, body any, status int) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal("编码验收请求失败")
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != status {
		t.Fatalf("%s %s 返回 %d，期望 %d", method, path, response.Code, status)
	}
	return response
}

// decodeIntegration 不回显响应内容，确保解析失败时也不会输出认证材料。
func decodeIntegration(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatal("验收响应不是有效 JSON")
	}
}
