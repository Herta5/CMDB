// 本文件通过真实登录、项目 API 和数据库验证第一阶段可交付的身份及项目隔离流程。
package httpserver_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"cmdb/internal/audit"
	awscollector "cmdb/internal/aws"
	"cmdb/internal/identity"
	"cmdb/internal/platform/httpserver"
	"cmdb/internal/project"
	cloudresource "cmdb/internal/resource"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestDeletionConflictHTTPContract 验证管理员不能绕过依赖保护，拒绝响应不泄露依赖或跨项目对象信息。
func TestDeletionConflictHTTPContract(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member_a", password)
	parent := project.Project{Code: "删除保护", Name: "删除保护", Status: "enabled"}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal("准备项目失败")
	}
	if err := db.Create(&project.MemberRole{ProjectID: parent.ID, UserID: 2, Role: "project_admin"}).Error; err != nil {
		t.Fatal("准备项目管理员失败")
	}
	now := time.Now().UTC()
	source := cloudresource.Source{ProjectID: parent.ID, Name: "保留来源", Provider: "aws", CloudAccountID: "虚构账号", IdentityVerifiedAt: &now}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal("准备来源失败")
	}
	asset := cloudresource.Database{AssetBase: cloudresource.AssetBase{ProjectID: parent.ID, SourceID: source.ID, Provider: "aws", ResourceType: "rds", ExternalID: "保留资产", AssetStatus: "lost"}, Endpoints: json.RawMessage(`[{"address":"private.example.invalid"}]`)}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal("准备资产失败")
	}
	path := fmt.Sprintf("/api/v1/projects/%d", parent.ID)
	sourcePath := fmt.Sprintf("%s/sources/%d", path, source.ID)
	for _, test := range []struct{ name, token, path, code, message string }{
		{"系统管理员删除项目", admin, path, "PROJECT_DELETE_CONFLICT", "项目仍有资产或运行中的同步任务，暂不能删除"},
		{"系统管理员删除来源", admin, sourcePath, "SOURCE_DELETE_CONFLICT", "接入源仍有资产或运行中的同步任务，暂不能删除"},
		{"项目管理员删除来源", member, sourcePath, "SOURCE_DELETE_CONFLICT", "接入源仍有资产或运行中的同步任务，暂不能删除"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := integrationRequest(t, server, test.token, http.MethodDelete, test.path, nil, http.StatusConflict)
			var payload map[string]any
			decodeIntegration(t, response, &payload)
			if len(payload) != 2 || payload["code"] != test.code || payload["message"] != test.message {
				t.Fatal("依赖冲突必须只返回固定中文提示和错误码")
			}
		})
	}
	other := project.Project{Code: "其他删除项目", Name: "其他项目", Status: "enabled"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal("准备其他项目失败")
	}
	denied := integrationRequest(t, server, member, http.MethodDelete, path, nil, 404)
	missing := integrationRequest(t, server, member, http.MethodDelete, "/api/v1/projects/999999", nil, 404)
	if denied.Body.String() != missing.Body.String() {
		t.Fatal("项目删除必须先授权，不得枚举依赖")
	}
	denied = integrationRequest(t, server, admin, http.MethodDelete, fmt.Sprintf("/api/v1/projects/%d/sources/%d", other.ID, source.ID), nil, 404)
	missing = integrationRequest(t, server, admin, http.MethodDelete, fmt.Sprintf("/api/v1/projects/%d/sources/999999", other.ID), nil, 404)
	if denied.Body.String() != missing.Body.String() {
		t.Fatal("来源跨项目和不存在必须返回一致响应")
	}
	var count int64
	if err := db.Model(&audit.Log{}).Where("action IN ?", []string{audit.ActionProjectDeleted, audit.ActionSourceDeleted}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("冲突和越权不得产生成功删除审计")
	}
}

// TestPublicUsernameContractEndToEnd 验证公开身份链路始终以用户名传递，内部用户主键不会穿透 HTTP 边界。
func TestPublicUsernameContractEndToEnd(t *testing.T) {
	server, password := integrationServer(t)
	login := integrationRequest(t, server, "", http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "operator", "password": password}, http.StatusOK)
	var loginPayload any
	decodeIntegration(t, login, &loginPayload)
	assertNoPublicUserNumericIdentifiers(t, loginPayload)
	loginObject := integrationObject(t, loginPayload)
	token, _ := loginObject["token"].(string)
	if token == "" {
		t.Fatal("登录公开响应必须包含会话令牌")
	}

	const username = "contract_user"
	createdUser := integrationRequest(t, server, token, http.MethodPost, "/api/v1/users", map[string]any{
		"username": username, "password": password, "display_name": "公开契约用户", "global_role": "user", "status": "active", "project_permissions": []any{},
	}, http.StatusCreated)
	var createdUserPayload any
	decodeIntegration(t, createdUser, &createdUserPayload)
	assertNoPublicUserNumericIdentifiers(t, createdUserPayload)
	if integrationObject(t, createdUserPayload)["username"] != username {
		t.Fatal("创建用户响应必须以用户名标识新用户")
	}

	createdProject := integrationRequest(t, server, token, http.MethodPost, "/api/v1/projects", map[string]any{"code": "contract", "name": "公开契约项目"}, http.StatusCreated)
	var createdProjectPayload any
	decodeIntegration(t, createdProject, &createdProjectPayload)
	assertNoPublicUserNumericIdentifiers(t, createdProjectPayload)
	projectID := integrationNumericID(t, integrationObject(t, createdProjectPayload)["id"])
	projectPath := "/api/v1/projects/" + strconv.FormatUint(projectID, 10)

	updatedProject := integrationRequest(t, server, token, http.MethodPut, projectPath, map[string]any{"name": "公开契约项目", "description": "", "status": "enabled", "owner_username": username}, http.StatusOK)
	var updatedProjectPayload any
	decodeIntegration(t, updatedProject, &updatedProjectPayload)
	assertNoPublicUserNumericIdentifiers(t, updatedProjectPayload)
	if integrationObject(t, updatedProjectPayload)["owner_username"] != username {
		t.Fatal("项目负责人必须以用户名公开")
	}
	otherActorToken := loginUser(t, server, "member_a", password)
	otherActorMember := integrationRequest(t, server, token, http.MethodPost, projectPath+"/members", map[string]any{"username": "member_a", "role": "project_admin"}, http.StatusCreated)
	var otherActorMemberPayload any
	decodeIntegration(t, otherActorMember, &otherActorMemberPayload)
	assertNoPublicUserNumericIdentifiers(t, otherActorMemberPayload)
	createdByOtherActor := integrationRequest(t, server, otherActorToken, http.MethodPost, projectPath+"/members", map[string]any{"username": username, "role": "member"}, http.StatusCreated)
	var createdByOtherActorPayload any
	decodeIntegration(t, createdByOtherActor, &createdByOtherActorPayload)
	assertNoPublicUserNumericIdentifiers(t, createdByOtherActorPayload)
	if integrationObject(t, createdByOtherActorPayload)["username"] != username {
		t.Fatal("另一操作人添加成员时必须以用户名标识目标")
	}

	for _, operation := range []struct {
		method string
		path   string
		body   any
		status int
	}{
		{http.MethodPut, projectPath + "/members/" + username, map[string]any{"role": "project_admin"}, http.StatusOK},
		{http.MethodDelete, projectPath + "/members/" + username, nil, http.StatusNoContent},
	} {
		response := integrationRequest(t, server, token, operation.method, operation.path, operation.body, operation.status)
		if operation.method == http.MethodDelete {
			continue
		}
		var payload any
		decodeIntegration(t, response, &payload)
		assertNoPublicUserNumericIdentifiers(t, payload)
		if integrationObject(t, payload)["username"] != username {
			t.Fatalf("成员 %s 响应必须以用户名标识目标", operation.method)
		}
	}

	auditResponse := integrationRequest(t, server, token, http.MethodGet, "/api/v1/audit-logs?actor_username=operator&page=1&page_size=100", nil, http.StatusOK)
	var auditPayload any
	decodeIntegration(t, auditResponse, &auditPayload)
	assertNoPublicUserNumericIdentifiers(t, auditPayload)
	assertIntegrationAuditActors(t, auditPayload, "operator", "member_a")
	for _, action := range []string{audit.ActionUserCreated, audit.ActionProjectMemberRoleChanged, audit.ActionProjectMemberRemoved} {
		assertIntegrationAuditResourceID(t, auditPayload, action, username)
	}
	assertIntegrationAuditResourceID(t, auditPayload, audit.ActionProjectMemberAdded, "member_a")

	otherActorAuditResponse := integrationRequest(t, server, token, http.MethodGet, "/api/v1/audit-logs?actor_username=member_a&page=1&page_size=100", nil, http.StatusOK)
	var otherActorAuditPayload any
	decodeIntegration(t, otherActorAuditResponse, &otherActorAuditPayload)
	assertNoPublicUserNumericIdentifiers(t, otherActorAuditPayload)
	assertIntegrationAuditActors(t, otherActorAuditPayload, "member_a", "operator")
	assertIntegrationAuditResourceID(t, otherActorAuditPayload, audit.ActionProjectMemberAdded, username)
}

// TestAuditQueryPermissionsAndProjectIsolation 验证系统管理员、项目管理员和成员使用不同审计边界。
func TestAuditQueryPermissionsAndProjectIsolation(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	projectAdmin := loginUser(t, server, "member_a", password)
	integrationRequest(t, server, admin, http.MethodPost, "/api/v1/users", map[string]any{"username": "member_b", "password": password, "display_name": "项目成员", "global_role": "user", "status": "active", "project_permissions": []any{}}, http.StatusCreated)
	projectMember := loginUser(t, server, "member_b", password)
	var firstProject, secondProject project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "audit-a", "name": "审计项目甲"}, http.StatusCreated), &firstProject)
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "audit-b", "name": "审计项目乙"}, http.StatusCreated), &secondProject)
	firstPath := "/api/v1/projects/" + strconv.FormatUint(firstProject.ID, 10)
	integrationRequest(t, server, admin, http.MethodPost, firstPath+"/members", map[string]any{"username": "member_a", "role": "project_admin"}, http.StatusCreated)
	integrationRequest(t, server, admin, http.MethodPost, firstPath+"/members", map[string]any{"username": "member_b", "role": "member"}, http.StatusCreated)
	if err := db.Create(&[]audit.Log{
		{ProjectID: &firstProject.ID, Action: audit.ActionProjectUpdated, ResourceType: "project", ResourceID: strconv.FormatUint(firstProject.ID, 10), Detail: json.RawMessage(`{}`)},
		{ProjectID: &secondProject.ID, Action: audit.ActionProjectUpdated, ResourceType: "project", ResourceID: strconv.FormatUint(secondProject.ID, 10), Detail: json.RawMessage(`{}`)},
		{Action: audit.ActionUserUpdated, ResourceType: "user", ResourceID: "member_a", Detail: json.RawMessage(`{}`)},
	}).Error; err != nil {
		t.Fatalf("准备审计查询数据失败：%v", err)
	}

	global := integrationRequest(t, server, admin, http.MethodGet, "/api/v1/audit-logs?page=1&page_size=20", nil, http.StatusOK)
	var globalPage audit.Page
	decodeIntegration(t, global, &globalPage)
	if globalPage.Total < 3 {
		t.Fatalf("系统管理员必须看到全局和全部项目审计：%+v", globalPage)
	}
	projectPageResponse := integrationRequest(t, server, projectAdmin, http.MethodGet, firstPath+"/audit-logs?page=1&page_size=20", nil, http.StatusOK)
	var projectPage audit.Page
	decodeIntegration(t, projectPageResponse, &projectPage)
	for _, item := range projectPage.Items {
		if item.ProjectID == nil || *item.ProjectID != firstProject.ID {
			t.Fatalf("项目管理员只能看到当前项目审计：%+v", item)
		}
	}
	integrationRequest(t, server, projectAdmin, http.MethodGet, "/api/v1/audit-logs", nil, http.StatusForbidden)
	integrationRequest(t, server, projectMember, http.MethodGet, firstPath+"/audit-logs", nil, http.StatusNotFound)
	denied := integrationRequest(t, server, projectAdmin, http.MethodGet, "/api/v1/projects/"+strconv.FormatUint(secondProject.ID, 10)+"/audit-logs", nil, http.StatusNotFound)
	missing := integrationRequest(t, server, projectAdmin, http.MethodGet, "/api/v1/projects/999999/audit-logs", nil, http.StatusNotFound)
	if denied.Body.String() != missing.Body.String() {
		t.Fatal("跨项目审计查询不得泄露项目是否存在")
	}
	integrationRequest(t, server, admin, http.MethodGet, "/api/v1/audit-logs?page=0", nil, http.StatusBadRequest)
}

// TestManagementMutationsWriteActorAudit 验证用户、项目和成员管理动作都进入统一审计且不含密码。
func TestManagementMutationsWriteActorAudit(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	var managedProject project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "audit-actions", "name": "审计动作项目"}, http.StatusCreated), &managedProject)
	projectPath := "/api/v1/projects/" + strconv.FormatUint(managedProject.ID, 10)
	integrationRequest(t, server, admin, http.MethodPut, projectPath, map[string]any{"name": "审计动作项目新版", "description": "审计详情", "status": "enabled"}, http.StatusOK)
	var managedUser identity.User
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/users", map[string]any{"username": "audit_user", "password": "never-persist-this-password", "display_name": "审计用户", "global_role": "user", "status": "active", "project_permissions": []any{}}, http.StatusCreated), &managedUser)
	if err := db.Where("username = ?", "audit_user").First(&managedUser).Error; err != nil {
		t.Fatalf("读取刚创建的用户失败：%v", err)
	}
	userPath := "/api/v1/users/" + managedUser.Username
	integrationRequest(t, server, admin, http.MethodPut, userPath, map[string]any{"display_name": "审计用户新版", "email": "audit@example.invalid", "global_role": "user", "status": "active", "password": "another-never-persist-password", "project_permissions": []any{}}, http.StatusOK)
	integrationRequest(t, server, admin, http.MethodPut, userPath+"/status", map[string]any{"status": "disabled"}, http.StatusOK)
	integrationRequest(t, server, admin, http.MethodPost, projectPath+"/members", map[string]any{"username": managedUser.Username, "role": "member"}, http.StatusCreated)
	integrationRequest(t, server, admin, http.MethodPut, projectPath+"/members/"+managedUser.Username, map[string]any{"role": "project_admin"}, http.StatusOK)
	integrationRequest(t, server, admin, http.MethodDelete, projectPath+"/members/"+managedUser.Username, nil, http.StatusNoContent)
	integrationRequest(t, server, admin, http.MethodDelete, projectPath, nil, http.StatusNoContent)

	expectedActions := []string{
		audit.ActionProjectCreated, audit.ActionProjectUpdated, audit.ActionUserCreated,
		audit.ActionUserUpdated, audit.ActionUserStatusChanged, audit.ActionProjectMemberAdded,
		audit.ActionProjectMemberRoleChanged, audit.ActionProjectMemberRemoved, audit.ActionProjectDeleted,
	}
	for _, action := range expectedActions {
		var value audit.Log
		if err := db.Where("action = ?", action).Order("id DESC").First(&value).Error; err != nil {
			t.Fatalf("管理动作必须写入统一审计：action=%s err=%v", action, err)
		}
		if value.ActorID == nil || *value.ActorID != 1 || value.RequestIP != "192.0.2.1" {
			t.Fatalf("人工管理审计必须记录真实操作者和来源 IP：action=%s value=%+v", action, value)
		}
		encoded := string(value.Detail)
		if strings.Contains(encoded, "user_id") {
			t.Fatalf("项目与成员审计不得记录用户数字 ID：%s", encoded)
		}
		if value.ResourceType == "project_member" && (value.ResourceID != "audit_user" || !strings.Contains(encoded, `"target_username":"audit_user"`)) {
			t.Fatalf("成员审计必须以用户名标识目标：%+v", value)
		}
		if strings.Contains(encoded, "never-persist") || strings.Contains(encoded, "password") {
			t.Fatalf("管理审计不得保存密码或密码字段：action=%s detail=%s", action, encoded)
		}
	}
}

// TestUserPermissionReplacementWritesProjectAudit 验证用户管理中的授权变化也对受影响项目管理员可见。
func TestUserPermissionReplacementWritesProjectAudit(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	var firstProject, secondProject project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "permission-a", "name": "授权项目甲"}, http.StatusCreated), &firstProject)
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "permission-b", "name": "授权项目乙"}, http.StatusCreated), &secondProject)
	var managedUser identity.User
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "permission_user", "password": "permission-user-password", "display_name": "授权用户", "global_role": "user", "status": "active",
		"project_permissions": []map[string]any{{"project_id": firstProject.ID, "role": "member"}},
	}, http.StatusCreated), &managedUser)
	integrationRequest(t, server, admin, http.MethodPut, "/api/v1/users/"+managedUser.Username, map[string]any{
		"display_name": "授权用户", "email": "", "global_role": "user", "status": "active",
		"project_permissions": []map[string]any{{"project_id": secondProject.ID, "role": "project_admin"}},
	}, http.StatusOK)

	assertProjectMemberAudit := func(projectID uint64, action string) {
		t.Helper()
		var value audit.Log
		if err := db.Where("project_id = ? AND action = ? AND resource_id = ?", projectID, action, managedUser.Username).First(&value).Error; err != nil {
			t.Fatalf("用户权限变化必须产生项目审计：project=%d action=%s err=%v", projectID, action, err)
		}
	}
	assertProjectMemberAudit(firstProject.ID, audit.ActionProjectMemberAdded)
	assertProjectMemberAudit(firstProject.ID, audit.ActionProjectMemberRemoved)
	assertProjectMemberAudit(secondProject.ID, audit.ActionProjectMemberAdded)
}

// TestProjectBoundaryEndToEnd 覆盖真实密码登录、授权、越权、成员撤销以及数据未被越权修改。
func TestProjectBoundaryEndToEnd(t *testing.T) {
	server, password := integrationServer(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member_a", password)
	first := integrationRequest(t, server, admin, "POST", "/api/v1/projects", map[string]any{"code": "cloud-a", "name": "云项目甲"}, 201)
	second := integrationRequest(t, server, admin, "POST", "/api/v1/projects", map[string]any{"code": "cloud-b", "name": "云项目乙"}, 201)
	var firstProject, secondProject project.Project
	decodeIntegration(t, first, &firstProject)
	decodeIntegration(t, second, &secondProject)
	firstPath := "/api/v1/projects/" + strconv.FormatUint(firstProject.ID, 10)
	secondPath := "/api/v1/projects/" + strconv.FormatUint(secondProject.ID, 10)
	integrationRequest(t, server, admin, "POST", firstPath+"/members", map[string]any{"username": "member_a", "role": "member"}, 201)

	var me map[string]any
	decodeIntegration(t, integrationRequest(t, server, member, "GET", "/api/v1/me", nil, 200), &me)
	if me["username"] != "member_a" || me["id"] != nil || me["user_id"] != nil || me["password_hash"] != nil || me["password"] != nil {
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
	integrationRequest(t, server, member, "POST", firstPath+"/members", map[string]any{"username": "member_b", "role": "member"}, 404)
	integrationRequest(t, server, "", "GET", firstPath, nil, 401)
	var unchanged project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, "GET", secondPath, nil, 200), &unchanged)
	if unchanged.Name != "云项目乙" {
		t.Fatal("越权请求不得改变目标项目")
	}

	// 移除成员后继续使用原 JWT，权限必须立即反映数据库中的当前成员关系。
	integrationRequest(t, server, admin, "DELETE", firstPath+"/members/member_a", nil, 204)
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

// TestSystemAdministratorManagesUsers 验证创建用户可原子授予多个项目角色，列表仅暴露公开资料。
func TestSystemAdministratorManagesUsers(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member_a", password)
	var firstProject, secondProject project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "user-auth-a", "name": "用户授权甲"}, http.StatusCreated), &firstProject)
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "user-auth-b", "name": "用户授权乙"}, http.StatusCreated), &secondProject)
	created := integrationRequest(t, server, admin, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "cloud_user", "password": "secure-user-password", "display_name": "云资源用户", "email": "cloud@example.invalid", "global_role": "user", "status": "active",
		"project_permissions": []map[string]any{{"project_id": firstProject.ID, "role": "project_admin"}, {"project_id": secondProject.ID, "role": "member"}},
	}, http.StatusCreated)
	if strings.Contains(created.Body.String(), "password") {
		t.Fatal("创建用户响应不得包含密码或密码哈希")
	}
	var user identity.User
	decodeIntegration(t, created, &user)
	if user.Username != "cloud_user" || user.GlobalRole != identity.GlobalRoleUser || user.Status != "active" {
		t.Fatal("创建用户必须返回指定的全局角色和状态")
	}
	if err := db.Where("username = ?", user.Username).First(&user).Error; err != nil {
		t.Fatalf("读取刚创建的用户失败：%v", err)
	}
	var memberships int64
	if err := db.Table("project_members").Where("user_id = ?", user.ID).Count(&memberships).Error; err != nil || memberships != 2 {
		t.Fatalf("创建用户必须在同一事务内保存两个项目权限：count=%d err=%v", memberships, err)
	}
	integrationRequest(t, server, member, http.MethodGet, "/api/v1/users", nil, http.StatusForbidden)
	listed := integrationRequest(t, server, admin, http.MethodGet, "/api/v1/users", nil, http.StatusOK)
	if strings.Contains(listed.Body.String(), "password_hash") || !strings.Contains(listed.Body.String(), "用户授权甲") || !strings.Contains(listed.Body.String(), "project_admin") {
		t.Fatal("用户列表必须返回项目权限和项目名称，且不得包含密码哈希")
	}
	integrationRequest(t, server, admin, http.MethodPut, "/api/v1/users/"+user.Username, map[string]any{"display_name": "云资源用户", "email": "cloud@example.invalid", "global_role": "user", "status": "disabled", "project_permissions": []map[string]any{{"project_id": secondProject.ID, "role": "member"}}}, http.StatusOK)
	integrationRequest(t, server, "", http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "cloud_user", "password": "secure-user-password"}, http.StatusUnauthorized)
}

// TestSystemAdministratorDeletesUser 验证系统管理员可删除其他用户，但不能删除当前登录身份。
func TestSystemAdministratorDeletesUser(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member_a", password)
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatalf("启用删除副作用校验失败：%v", err)
	}
	ownerID := uint64(2)
	ownedProject := project.Project{Code: "deleted-user-project", Name: "待清理负责人项目", Description: "", Status: project.ProjectStatusEnabled, OwnerUserID: &ownerID}
	if err := db.Create(&ownedProject).Error; err != nil {
		t.Fatalf("准备用户负责项目失败：%v", err)
	}
	if err := db.Create(&project.MemberRole{ProjectID: ownedProject.ID, UserID: ownerID, Role: project.MemberRoleMember}).Error; err != nil {
		t.Fatalf("准备用户项目权限失败：%v", err)
	}
	fixtureAudit := audit.Log{ActorID: &ownerID, ProjectID: &ownedProject.ID, Action: "user.fixture", ResourceType: "user", ResourceID: "member_a", Detail: json.RawMessage(`{}`)}
	if err := db.Create(&fixtureAudit).Error; err != nil {
		t.Fatalf("准备独立审计记录失败：%v", err)
	}

	forbidden := integrationRequest(t, server, member, http.MethodDelete, "/api/v1/users/member_a", nil, http.StatusForbidden)
	if forbidden.Body.String() != `{"code":"USER_FORBIDDEN","message":"无权执行该操作"}` {
		t.Fatalf("普通用户删除响应契约错误：%s", forbidden.Body.String())
	}
	protected := integrationRequest(t, server, admin, http.MethodDelete, "/api/v1/users/operator", nil, http.StatusConflict)
	if protected.Body.String() != `{"code":"USER_SELF_PROTECTED","message":"不能删除当前管理员"}` {
		t.Fatalf("管理员自删保护响应契约错误：%s", protected.Body.String())
	}
	invalid := integrationRequest(t, server, admin, http.MethodDelete, "/api/v1/users/not-a-number", nil, http.StatusBadRequest)
	if invalid.Body.String() != `{"code":"USER_INVALID_REQUEST","message":"请求格式错误"}` {
		t.Fatalf("无效用户标识响应契约错误：%s", invalid.Body.String())
	}
	integrationRequest(t, server, admin, http.MethodDelete, "/api/v1/users/member_a", nil, http.StatusNoContent)

	var remaining, memberships, audits int64
	if err := db.Model(&identity.User{}).Where("id = ?", 2).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("删除成功后用户记录必须消失：count=%d err=%v", remaining, err)
	}
	if err := db.Model(&project.MemberRole{}).Where("user_id = ?", 2).Count(&memberships).Error; err != nil || memberships != 0 {
		t.Fatalf("删除用户必须级联清理项目成员关系：count=%d err=%v", memberships, err)
	}
	if err := db.First(&ownedProject, ownedProject.ID).Error; err != nil || ownedProject.OwnerUserID != nil {
		t.Fatalf("删除用户必须把项目负责人置空：owner=%v err=%v", ownedProject.OwnerUserID, err)
	}
	if err := db.Model(&audit.Log{}).Where("id = ?", fixtureAudit.ID).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatalf("删除用户后必须保留独立审计记录：count=%d err=%v", audits, err)
	}
	for _, action := range []string{audit.ActionUserDeleted, audit.ActionProjectMemberRemoved} {
		var count int64
		if err := db.Model(&audit.Log{}).Where("action = ? AND resource_id = ?", action, "member_a").Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("删除用户必须生成动作 %s：count=%d err=%v", action, count, err)
		}
	}
	notFound := integrationRequest(t, server, admin, http.MethodDelete, "/api/v1/users/member_a", nil, http.StatusNotFound)
	if notFound.Body.String() != `{"code":"USER_NOT_FOUND","message":"用户不存在"}` {
		t.Fatalf("重复删除响应契约错误：%s", notFound.Body.String())
	}
}

// TestSystemAdministratorEditsUserAndCannotLockSelfOut 验证用户资料、角色和密码可维护，同时保护当前管理员权限。
func TestSystemAdministratorEditsUserAndCannotLockSelfOut(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member_a", password)
	var firstProject, secondProject project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "replace-a", "name": "替换前项目"}, http.StatusCreated), &firstProject)
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "replace-b", "name": "替换后项目"}, http.StatusCreated), &secondProject)
	created := integrationRequest(t, server, admin, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "editable_user", "password": "initial-user-password", "display_name": "待编辑用户", "email": "old@example.invalid", "global_role": "user", "status": "active", "project_permissions": []map[string]any{{"project_id": firstProject.ID, "role": "member"}},
	}, http.StatusCreated)
	var user identity.User
	decodeIntegration(t, created, &user)
	path := "/api/v1/users/" + user.Username
	updated := integrationRequest(t, server, admin, http.MethodPut, path, map[string]any{
		"display_name": "已编辑用户", "email": "new@example.invalid", "global_role": "system_admin", "status": "active", "password": "replacement-password", "project_permissions": []map[string]any{{"project_id": secondProject.ID, "role": "member"}},
	}, http.StatusOK)
	if strings.Contains(updated.Body.String(), "password") || !strings.Contains(updated.Body.String(), "已编辑用户") || !strings.Contains(updated.Body.String(), "system_admin") {
		t.Fatal("编辑响应必须返回更新后的公开资料且不得包含密码")
	}
	if err := db.Where("username = ?", user.Username).First(&user).Error; err != nil {
		t.Fatalf("读取编辑用户失败：%v", err)
	}
	var roles []project.MemberRole
	if err := db.Where("user_id = ?", user.ID).Find(&roles).Error; err != nil || len(roles) != 1 || roles[0].ProjectID != secondProject.ID || roles[0].Role != project.MemberRoleMember {
		t.Fatalf("编辑用户必须全量替换项目权限：roles=%+v err=%v", roles, err)
	}
	integrationRequest(t, server, "", http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "editable_user", "password": "initial-user-password"}, http.StatusUnauthorized)
	integrationRequest(t, server, "", http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "editable_user", "password": "replacement-password"}, http.StatusOK)
	integrationRequest(t, server, member, http.MethodPut, path, map[string]any{"display_name": "越权修改", "email": "", "global_role": "user", "status": "active"}, http.StatusForbidden)
	integrationRequest(t, server, admin, http.MethodPut, "/api/v1/users/operator", map[string]any{"display_name": "当前管理员", "email": "", "global_role": "user", "status": "active"}, http.StatusConflict)
	integrationRequest(t, server, admin, http.MethodPut, "/api/v1/users/operator/status", map[string]any{"status": "disabled"}, http.StatusConflict)
}

// TestCreatingUserWithMissingProjectRollsBack 验证无效项目授权不会留下孤立用户或部分成员关系。
func TestCreatingUserWithMissingProjectRollsBack(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	integrationRequest(t, server, admin, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "rollback_user", "password": "rollback-user-password", "display_name": "回滚用户", "global_role": "user", "status": "active",
		"project_permissions": []map[string]any{{"project_id": 999999, "role": "member"}},
	}, http.StatusBadRequest)
	var users, memberships int64
	if err := db.Table("users").Where("username = ?", "rollback_user").Count(&users).Error; err != nil {
		t.Fatalf("查询回滚用户失败：%v", err)
	}
	if err := db.Table("project_members").Where("user_id NOT IN ?", []uint64{1, 2, 3}).Count(&memberships).Error; err != nil {
		t.Fatalf("查询回滚权限失败：%v", err)
	}
	if users != 0 || memberships != 0 {
		t.Fatalf("无效授权必须整体回滚：users=%d memberships=%d", users, memberships)
	}
}

// TestProjectAdministratorReadsCandidatesAndManagesMemberRoles 验证项目管理员只能在所属项目内查询候选身份并管理角色。
func TestProjectAdministratorReadsCandidatesAndManagesMemberRoles(t *testing.T) {
	server, password := integrationServer(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member_a", password)
	var created project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "members", "name": "成员项目"}, http.StatusCreated), &created)
	path := "/api/v1/projects/" + strconv.FormatUint(created.ID, 10)
	integrationRequest(t, server, admin, http.MethodPost, path+"/members", map[string]any{"username": "member_a", "role": "project_admin"}, http.StatusCreated)
	candidates := integrationRequest(t, server, member, http.MethodGet, path+"/member-candidates", nil, http.StatusOK)
	if strings.Contains(candidates.Body.String(), "email") || strings.Contains(candidates.Body.String(), "password") || strings.Contains(candidates.Body.String(), `"id"`) || strings.Contains(candidates.Body.String(), `"user_id"`) {
		t.Fatal("成员候选接口不得暴露邮箱或认证字段")
	}
	var values []struct {
		Username string `json:"username"`
	}
	decodeIntegration(t, candidates, &values)
	if len(values) != 2 || values[0].Username != "member_a" || values[1].Username != "operator" {
		t.Fatal("项目管理员必须能读取可添加用户的最小公开身份")
	}
	members := integrationRequest(t, server, member, http.MethodGet, path+"/members", nil, http.StatusOK)
	if !strings.Contains(members.Body.String(), "member_a") || strings.Contains(members.Body.String(), "password") || strings.Contains(members.Body.String(), `"id"`) || strings.Contains(members.Body.String(), `"user_id"`) {
		t.Fatal("成员列表必须包含公开用户名且不得包含认证字段")
	}
}

// TestProjectSourceAPINeverReturnsCredentials 验证接入源接口权限和凭证响应边界。
func TestProjectSourceAPINeverReturnsCredentials(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member_a", password)
	var created project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "sources", "name": "接入项目"}, http.StatusCreated), &created)
	path := "/api/v1/projects/" + strconv.FormatUint(created.ID, 10)
	integrationRequest(t, server, admin, http.MethodPost, path+"/members", map[string]any{"username": "member_a", "role": "member"}, http.StatusCreated)
	response := integrationRequest(t, server, admin, http.MethodPost, path+"/sources", map[string]any{"provider": "aws", "name": "AWS 生产账号", "region": "cn-north-1", "credential": map[string]any{"access_key_id": "example-id", "secret_access_key": "example-secret"}}, http.StatusCreated)
	if strings.Contains(response.Body.String(), "123456789012") || strings.Contains(response.Body.String(), "identity_verified_at") || strings.Contains(response.Body.String(), "identity_status") {
		t.Fatal("HTTP 新建来源不得公开账号、验证时间或不存在的身份状态")
	}
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
	integrationRequest(t, server, admin, http.MethodPost, path+"/sources/"+strconv.FormatUint(source.ID, 10)+"/test", nil, http.StatusOK)
	var probeCount int64
	_ = db.Model(&cloudresource.Server{}).Count(&probeCount).Error
	if probeCount != 0 {
		t.Fatal("连接测试不得写入资源")
	}
	integrationRequest(t, server, admin, http.MethodPost, path+"/sources/"+strconv.FormatUint(source.ID, 10)+"/sync", nil, http.StatusAccepted)
	// 后台任务完成后再检查资源，避免把调度速度当成接口契约。
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		var job cloudresource.SyncJob
		_ = db.Order("id DESC").First(&job).Error
		if job.Status == "success" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	updated := integrationRequest(t, server, admin, http.MethodPut, path+"/sources/"+strconv.FormatUint(source.ID, 10), map[string]any{"name": "AWS 更新账号", "region": "ap-east-1", "config": map[string]any{}, "enabled": true, "sync_interval_minutes": 120}, http.StatusOK)
	if strings.Contains(updated.Body.String(), "example") || !strings.Contains(updated.Body.String(), "AWS 更新账号") {
		t.Fatal("更新接入源必须保留凭证且响应不得暴露凭证")
	}
	jobs := integrationRequest(t, server, member, http.MethodGet, path+"/sync-jobs?source_id="+strconv.FormatUint(source.ID, 10), nil, http.StatusOK)
	if !strings.Contains(jobs.Body.String(), `"trigger":"manual"`) {
		t.Fatal("项目成员必须能查询当前项目的同步任务")
	}
	failedJob := cloudresource.SyncJob{ProjectID: created.ID, SourceID: source.ID, Status: "failed", Trigger: "manual", StartedAt: time.Now(), ErrorSummary: "采集失败"}
	_ = db.Create(&failedJob).Error
	retried := integrationRequest(t, server, admin, http.MethodPost, path+"/sync-jobs/"+strconv.FormatUint(failedJob.ID, 10)+"/retry", nil, http.StatusAccepted)
	if !strings.Contains(retried.Body.String(), `"previous_job_id":`+strconv.FormatUint(failedJob.ID, 10)) {
		t.Fatal("重试任务必须引用原失败任务")
	}
	resources := integrationRequest(t, server, member, http.MethodGet, path+"/resources?provider=aws&resource_type=ec2", nil, http.StatusOK)
	if !strings.Contains(resources.Body.String(), "i-integration") || !strings.Contains(resources.Body.String(), "10.0.0.8") {
		t.Fatal("同步资源查询必须包含模拟采集器输出及端点")
	}
	integrationRequest(t, server, admin, http.MethodDelete, path+"/sources/"+strconv.FormatUint(source.ID, 10), nil, http.StatusConflict)
	var unsuccessfulDeleteAudit int64
	if err := db.Model(&audit.Log{}).Where("action = ? AND resource_id = ?", audit.ActionSourceDeleted, strconv.FormatUint(source.ID, 10)).Count(&unsuccessfulDeleteAudit).Error; err != nil || unsuccessfulDeleteAudit != 0 {
		t.Fatal("资产阻止来源删除时不得产生成功审计")
	}
	// 空来源用于独立验收删除能力，不能把已同步资产的来源当作级联清理入口。
	// 公开响应不会携带密文和身份验证时间，测试夹具必须从持久化记录复制完整的服务端字段。
	var emptySource cloudresource.Source
	if err := db.First(&emptySource, source.ID).Error; err != nil {
		t.Fatalf("读取来源夹具失败：%v", err)
	}
	emptySource.ID, emptySource.CloudAccountID = 0, "虚构空账号"
	emptySource.Name = "无依赖来源"
	if err := db.Create(&emptySource).Error; err != nil {
		t.Fatalf("准备无依赖来源失败：%v", err)
	}
	integrationRequest(t, server, admin, http.MethodDelete, path+"/sources/"+strconv.FormatUint(emptySource.ID, 10), nil, http.StatusNoContent)
	integrationRequest(t, server, member, http.MethodGet, path+"/sources", nil, http.StatusOK)
	for _, action := range []string{audit.ActionSourceCreated, audit.ActionSourceConnectionTested, audit.ActionSourceSynced, audit.ActionSourceUpdated, audit.ActionSourceDeleted} {
		var value audit.Log
		auditSourceID := source.ID
		if action == audit.ActionSourceDeleted {
			auditSourceID = emptySource.ID
		}
		if err := db.Where("action = ? AND resource_id = ?", action, strconv.FormatUint(auditSourceID, 10)).Order("id DESC").First(&value).Error; err != nil {
			t.Fatalf("接入源动作必须写入统一审计：action=%s err=%v", action, err)
		}
		if value.ActorID == nil || *value.ActorID != 1 || value.RequestIP != "192.0.2.1" {
			t.Fatalf("人工接入源审计必须保留请求操作者：action=%s value=%+v", action, value)
		}
		if strings.Contains(string(value.Detail), "example-id") || strings.Contains(string(value.Detail), "example-secret") {
			t.Fatalf("接入源审计不得保存云凭证：action=%s", action)
		}
	}
}

func TestSourceOperationReadFailureIsInternal(t *testing.T) {
	for _, operation := range []struct{ name, suffix string }{{"同步", "sync"}, {"连接测试", "test"}} {
		t.Run(operation.name, func(t *testing.T) {
			server, password, db := integrationServerWithDatabase(t)
			admin := loginUser(t, server, "operator", password)
			var parent, other project.Project
			decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "operation-read", "name": "运行读取项目"}, http.StatusCreated), &parent)
			decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "other-operation-read", "name": "另一个项目"}, http.StatusCreated), &other)
			source := integrationIdentitySource(t, db, parent.ID, "111111111111", "identity-ok")
			missing := integrationRequest(t, server, admin, http.MethodPost, fmt.Sprintf("/api/v1/projects/%d/sources/999999/%s", parent.ID, operation.suffix), nil, http.StatusNotFound)
			crossProject := integrationRequest(t, server, admin, http.MethodPost, fmt.Sprintf("/api/v1/projects/%d/sources/%d/%s", other.ID, source.ID, operation.suffix), nil, http.StatusNotFound)
			if missing.Body.String() != crossProject.Body.String() {
				t.Fatal("不存在与跨项目来源必须保持相同响应")
			}
			assertIdentityVerificationError(t, missing.Body.String(), "SOURCE_NOT_FOUND")
			var beforeAudits int64
			if err := db.Model(&audit.Log{}).Count(&beforeAudits).Error; err != nil {
				t.Fatal("读取原审计数量失败")
			}
			if err := db.Callback().Query().Before("gorm:query").Register("integration:operation_read_failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "resource_sources" {
					tx.AddError(fmt.Errorf("虚构运行读取数据库原文及敏感参数"))
				}
			}); err != nil {
				t.Fatal("安装运行读取故障失败")
			}
			response := integrationRequest(t, server, admin, http.MethodPost, fmt.Sprintf("/api/v1/projects/%d/sources/%d/%s", parent.ID, source.ID, operation.suffix), nil, http.StatusInternalServerError)
			assertIdentityVerificationError(t, response.Body.String(), "SOURCE_SERVICE_UNAVAILABLE")
			if strings.Contains(response.Body.String(), "虚构运行读取数据库") {
				t.Fatal("运行读取故障不得公开底层错误")
			}
			if err := db.Callback().Query().Remove("integration:operation_read_failure"); err != nil {
				t.Fatal("撤销运行读取故障失败")
			}
			for _, model := range []any{&cloudresource.SyncJob{}, &cloudresource.Server{}, &cloudresource.Database{}, &cloudresource.LoadBalancer{}} {
				var count int64
				if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
					t.Fatal("来源读取失败不得创建任务或资源")
				}
			}
			var afterAudits int64
			if err := db.Model(&audit.Log{}).Count(&afterAudits).Error; err != nil || afterAudits != beforeAudits {
				t.Fatal("来源读取失败不得产生操作审计")
			}
			var current cloudresource.Source
			if err := db.First(&current, source.ID).Error; err != nil || current.EncryptedCredential != source.EncryptedCredential || current.LastSyncAt != nil || current.NextSyncAt != nil {
				t.Fatal("来源读取失败不得修改凭证或同步计划")
			}
		})
	}
}

// TestSourceUpdateReadFailureIsInternal 验证编辑前数据库读取故障不能伪装成来源不存在。
func TestSourceUpdateReadFailureIsInternal(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	var parent project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "read-failure", "name": "读取故障项目"}, http.StatusCreated), &parent)
	source := integrationIdentitySource(t, db, parent.ID, "111111111111", "identity-ok")
	if err := db.Callback().Query().Before("gorm:query").Register("integration:source_read_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "resource_sources" {
			tx.AddError(fmt.Errorf("虚构来源数据库连接原文"))
		}
	}); err != nil {
		t.Fatal("安装读取失败夹具失败")
	}
	response := integrationRequest(t, server, admin, http.MethodPut, fmt.Sprintf("/api/v1/projects/%d/sources/%d", parent.ID, source.ID), map[string]any{"name": "新名称", "sync_interval_minutes": 60}, http.StatusInternalServerError)
	assertIdentityVerificationError(t, response.Body.String(), "SOURCE_SERVICE_UNAVAILABLE")
	if strings.Contains(response.Body.String(), "虚构来源数据库") {
		t.Fatal("数据库连接故障不得公开原文")
	}
	if err := db.Callback().Query().Remove("integration:source_read_failure"); err != nil {
		t.Fatal("撤销读取故障失败")
	}
	var current cloudresource.Source
	if err := db.First(&current, source.ID).Error; err != nil || current.Name != source.Name || current.EncryptedCredential != source.EncryptedCredential {
		t.Fatal("读取失败不得影响已保存来源")
	}
}

// TestSourceProbeErrorsKeepConnectionClassification 验证 Probe 认证与权限失败不会误用身份识别错误分类。
func TestSourceProbeErrorsKeepConnectionClassification(t *testing.T) {
	for _, scenario := range []struct {
		name, code string
		status     int
	}{
		{"集成探测权限失败", "SOURCE_PERMISSION_DENIED", http.StatusForbidden},
		{"集成探测认证失败", "SOURCE_AUTHENTICATION_FAILED", http.StatusBadGateway},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server, password, db := integrationServerWithDatabase(t)
			admin := loginUser(t, server, "operator", password)
			var parent project.Project
			decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "probe-errors", "name": "探测错误项目"}, http.StatusCreated), &parent)
			source := integrationIdentitySource(t, db, parent.ID, "111111111111", "identity-ok")
			if err := db.Model(&source).Update("name", scenario.name).Error; err != nil {
				t.Fatal("准备探测场景失败")
			}
			response := integrationRequest(t, server, admin, http.MethodPost, fmt.Sprintf("/api/v1/projects/%d/sources/%d/test", parent.ID, source.ID), nil, scenario.status)
			assertIdentityVerificationError(t, response.Body.String(), scenario.code)
			if strings.Contains(response.Body.String(), "虚构探测原文") {
				t.Fatal("连接测试不得回显云原文")
			}
			var audits []audit.Log
			if err := db.Where("action = ?", audit.ActionSourceConnectionTested).Find(&audits).Error; err != nil || len(audits) != 1 {
				t.Fatal("连接失败必须记录一次安全审计")
			}
			if strings.Contains(string(audits[0].Detail), "虚构探测原文") {
				t.Fatal("失败审计不得保存云原文")
			}
		})
	}
}

// TestSourceMutationInternalFailureIsSafeAndAtomic 验证真实来源和审计写入后内部失败会一起回滚并返回安全 500。
func TestSourceMutationInternalFailureIsSafeAndAtomic(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		t.Run(map[string]string{http.MethodPost: "创建", http.MethodPut: "编辑"}[method], func(t *testing.T) {
			server, password, db := integrationServerWithDatabase(t)
			admin := loginUser(t, server, "operator", password)
			var parent project.Project
			decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "internal-errors", "name": "内部错误项目"}, http.StatusCreated), &parent)
			path := fmt.Sprintf("/api/v1/projects/%d/sources", parent.ID)
			var original cloudresource.Source
			if method == http.MethodPut {
				original = integrationIdentitySource(t, db, parent.ID, "111111111111", "identity-ok")
				path += fmt.Sprintf("/%d", original.ID)
			}
			// 故障位于真实审计 INSERT 之后，必须回滚已发生的来源与审计写入。
			if err := db.Callback().Create().After("gorm:create").Register("integration:source_audit_failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "audit_logs" {
					tx.AddError(fmt.Errorf("虚构数据库审计正文及敏感参数"))
				}
			}); err != nil {
				t.Fatal("安装审计失败夹具失败")
			}
			response := integrationRequest(t, server, admin, method, path, map[string]any{"provider": "aws", "name": "未提交的新名称", "region": "ap-east-1", "credential": map[string]string{"access_key_id": "identity-ok", "secret_access_key": "identity-test-secret"}, "config": map[string]any{}, "enabled": true, "sync_interval_minutes": 60}, http.StatusInternalServerError)
			assertIdentityVerificationError(t, response.Body.String(), "SOURCE_SERVICE_UNAVAILABLE")
			if strings.Contains(response.Body.String(), "虚构数据库") {
				t.Fatal("内部故障不得暴露数据库原文")
			}
			var count int64
			if err := db.Model(&audit.Log{}).Where("action IN ?", []string{audit.ActionSourceCreated, audit.ActionSourceUpdated}).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("失败必须回滚来源成功审计")
			}
			var sources []cloudresource.Source
			if err := db.Find(&sources).Error; err != nil {
				t.Fatal("读取回滚结果失败")
			}
			if method == http.MethodPost {
				if len(sources) != 0 {
					t.Fatal("失败创建不得保留来源")
				}
			} else {
				if len(sources) != 1 || sources[0].Name != original.Name || sources[0].Region != original.Region || sources[0].EncryptedCredential != original.EncryptedCredential || sources[0].CloudAccountID != original.CloudAccountID || !sources[0].IdentityVerifiedAt.Equal(*original.IdentityVerifiedAt) || string(sources[0].Config) != string(original.Config) {
					t.Fatal("失败编辑必须保留名称、配置、凭证和身份")
				}
			}
		})
	}
}

// TestSourceIdentityErrorsUseStableHTTPContract 验证普通接入源入口不会把身份领域错误降级为泛化失败。
func TestSourceIdentityErrorsUseStableHTTPContract(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	var parent project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "identity-errors", "name": "身份错误分类项目"}, http.StatusCreated), &parent)
	projectPath := "/api/v1/projects/" + strconv.FormatUint(parent.ID, 10)
	_ = integrationIdentitySource(t, db, parent.ID, "333333333333", "identity-occupied")

	for _, scenario := range []struct {
		name    string
		body    map[string]any
		status  int
		code    string
		message string
	}{
		{name: "创建基础字段无效", body: map[string]any{"provider": "aws", "name": "", "credential": map[string]string{"access_key_id": "identity-ok", "secret_access_key": "identity-secret"}}, status: http.StatusBadRequest, code: "SOURCE_INVALID_INPUT"},
		{name: "创建周期无效", body: map[string]any{"provider": "aws", "name": "周期无效", "sync_interval_minutes": 1, "credential": map[string]string{"access_key_id": "identity-ok", "secret_access_key": "identity-secret"}}, status: http.StatusBadRequest, code: "SOURCE_INVALID_INPUT"},
		{name: "创建账号冲突", body: map[string]any{"provider": "aws", "name": "重复账号", "region": "ap-east-1", "credential": map[string]any{"access_key_id": "identity-conflict", "secret_access_key": "identity-secret"}}, status: http.StatusConflict, code: "CLOUD_ACCOUNT_CONFLICT"},
		{name: "创建时云认证失败", body: map[string]any{"provider": "aws", "name": "云认证失败账号", "region": "ap-east-1", "credential": map[string]any{"access_key_id": "identity-authentication", "secret_access_key": "identity-secret"}}, status: http.StatusBadGateway, code: "CLOUD_IDENTITY_UNAVAILABLE", message: "AccessKey 无效或签名校验失败，请检查凭证"},
		{name: "创建时云权限不足", body: map[string]any{"provider": "aws", "name": "云权限不足账号", "region": "ap-east-1", "credential": map[string]any{"access_key_id": "identity-permission", "secret_access_key": "identity-secret"}}, status: http.StatusBadGateway, code: "CLOUD_IDENTITY_UNAVAILABLE", message: "云账号身份查询权限不足，请检查云账号授权"},
		{name: "创建时云网络失败", body: map[string]any{"provider": "aws", "name": "云网络失败账号", "region": "ap-east-1", "credential": map[string]any{"access_key_id": "identity-network", "secret_access_key": "identity-secret"}}, status: http.StatusBadGateway, code: "CLOUD_IDENTITY_UNAVAILABLE", message: "云账号身份服务连接失败，请检查服务端网络"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			response := integrationRequest(t, server, admin, http.MethodPost, projectPath+"/sources", scenario.body, scenario.status)
			assertIdentityVerificationError(t, response.Body.String(), scenario.code)
			if scenario.message != "" && !strings.Contains(response.Body.String(), scenario.message) {
				t.Fatal("创建凭证身份失败必须返回对应的安全处理提示")
			}
		})
	}

	verified := integrationIdentitySource(t, db, parent.ID, "111111111111", "identity-ok")
	invalidUpdate := integrationRequest(t, server, admin, http.MethodPut, projectPath+"/sources/"+strconv.FormatUint(verified.ID, 10), map[string]any{"name": "", "sync_interval_minutes": 60}, http.StatusBadRequest)
	assertIdentityVerificationError(t, invalidUpdate.Body.String(), "SOURCE_INVALID_INPUT")
	updateResponse := integrationRequest(t, server, admin, http.MethodPut, projectPath+"/sources/"+strconv.FormatUint(verified.ID, 10), map[string]any{
		"name": "替换凭证", "region": "ap-east-1", "credential": map[string]any{"access_key_id": "identity-object", "secret_access_key": "identity-secret"}, "config": map[string]any{}, "enabled": true, "sync_interval_minutes": 60,
	}, http.StatusConflict)
	assertIdentityVerificationError(t, updateResponse.Body.String(), "SOURCE_IDENTITY_MISMATCH")
	for _, scenario := range []struct {
		name, accessKeyID, message string
	}{
		{name: "替换时云认证失败", accessKeyID: "identity-authentication", message: "AccessKey 无效或签名校验失败，请检查凭证"},
		{name: "替换时云权限不足", accessKeyID: "identity-permission", message: "云账号身份查询权限不足，请检查云账号授权"},
		{name: "替换时云网络失败", accessKeyID: "identity-network", message: "云账号身份服务连接失败，请检查服务端网络"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			response := integrationRequest(t, server, admin, http.MethodPut, projectPath+"/sources/"+strconv.FormatUint(verified.ID, 10), map[string]any{
				"name": "替换凭证失败", "region": "ap-east-1", "credential": map[string]any{"access_key_id": scenario.accessKeyID, "secret_access_key": "identity-secret"}, "config": map[string]any{}, "enabled": true, "sync_interval_minutes": 60,
			}, http.StatusBadGateway)
			assertIdentityVerificationError(t, response.Body.String(), "CLOUD_IDENTITY_UNAVAILABLE")
			if !strings.Contains(response.Body.String(), scenario.message) {
				t.Fatal("替换凭证身份失败必须返回对应的安全处理提示")
			}
		})
	}

}

// integrationIdentitySource 准备已经在创建阶段完成云账号身份确认的接入源。
func integrationIdentitySource(t *testing.T, db *gorm.DB, projectID uint64, accountID, accessKeyID string) cloudresource.Source {
	t.Helper()
	credential, err := json.Marshal(map[string]string{"access_key_id": accessKeyID, "secret_access_key": "identity-test-secret"})
	if err != nil {
		t.Fatal("编码虚构身份凭证失败")
	}
	encrypted, err := cloudresource.NewCredentialCipher("integration-encryption-key").Encrypt(credential)
	if err != nil {
		t.Fatal("加密虚构身份凭证失败")
	}
	verifiedAt := time.Now()
	source := cloudresource.Source{ProjectID: projectID, Provider: cloudresource.ProviderAWS, CloudAccountID: accountID, IdentityVerifiedAt: &verifiedAt, Name: "已验证接入源", Region: "cn-test-1", EncryptedCredential: encrypted, CredentialHint: "已安全配置", Config: json.RawMessage(`{}`), Enabled: true, SyncIntervalMinutes: 60}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal("准备已验证接入源失败")
	}
	return source
}

// assertIdentityVerificationResponseSafe 防止身份接口响应泄露云账号、凭证、密文或内部验证时间。
func assertIdentityVerificationResponseSafe(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{"111111111111", "222222222222", "333333333333", "444444444444", "identity-test-secret", "identity-object-secret", "identity-ok", "identity-object", "identity-authentication", "identity-permission", "identity-network", "identity-conflict", "云端原始身份错误", "encrypted_credential", "cloud_account_id", "identity_verified_at", "user_id", "actor_id"} {
		if strings.Contains(body, forbidden) {
			t.Fatal("身份验证响应不得泄露账号、凭证、密文或内部验证字段")
		}
	}
}

// assertIdentityVerificationError 只验证公开错误分类，不将响应体或上游错误正文回显到测试日志。
func assertIdentityVerificationError(t *testing.T, body, wantCode string) {
	t.Helper()
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil || payload.Code != wantCode {
		t.Fatal("身份验证错误必须使用稳定公开分类")
	}
	assertIdentityVerificationResponseSafe(t, body)
}

// TestSyncFailureHTTPContract 验证异步同步和任务查询公开真实终态，失败不更新资产和自动计划。
func TestSyncFailureHTTPContract(t *testing.T) {
	for _, scenario := range []struct{ name, status string }{
		{"集成全部失败", "failed"}, {"集成空结果", "failed"}, {"集成缺少类型", "failed"}, {"集成重复类型", "failed"}, {"集成部分成功", "partial_success"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server, password, db := integrationServerWithDatabase(t)
			admin := loginUser(t, server, "operator", password)
			var parent project.Project
			decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "同步终态", "name": "同步验收项目"}, http.StatusCreated), &parent)
			path := fmt.Sprintf("/api/v1/projects/%d", parent.ID)
			var source cloudresource.Source
			decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, path+"/sources", map[string]any{"provider": "aws", "name": scenario.name, "region": "cn-north-1", "credential": map[string]any{"access_key_id": "example-id", "secret_access_key": "example-secret"}}, http.StatusCreated), &source)
			last, next := time.Now().UTC().Add(-2*time.Hour), time.Now().UTC().Add(time.Hour)
			if err := db.Model(&source).Updates(map[string]any{"last_sync_at": last, "next_sync_at": next}).Error; err != nil {
				t.Fatal("准备自动计划失败")
			}
			var queued cloudresource.SyncJob
			decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, fmt.Sprintf("%s/sources/%d/sync", path, source.ID), nil, http.StatusAccepted), &queued)
			if queued.Status != "queued" {
				t.Fatal("手工同步必须先返回排队任务")
			}
			var completed cloudresource.SyncJob
			for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
				if err := db.First(&completed, queued.ID).Error; err != nil {
					t.Fatal("读取异步任务失败")
				}
				if completed.Status != "queued" && completed.Status != "running" {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if completed.Status != scenario.status || completed.FinishedAt == nil {
				t.Fatalf("异步任务终态错误：%s", completed.Status)
			}
			response := integrationRequest(t, server, admin, http.MethodGet, path+"/sync-jobs", nil, http.StatusOK)
			if !strings.Contains(response.Body.String(), `"status":"`+scenario.status+`"`) || strings.Contains(response.Body.String(), "虚构原始") || strings.Contains(response.Body.String(), "example-secret") {
				t.Fatal("任务查询必须返回安全且准确的同步终态")
			}
			var current cloudresource.Source
			if err := db.First(&current, source.ID).Error; err != nil {
				t.Fatal("读取同步后计划失败")
			}
			var assets int64
			if err := db.Model(&cloudresource.Server{}).Count(&assets).Error; err != nil {
				t.Fatal("查询同步资产失败")
			}
			if scenario.status == "failed" {
				if assets != 0 || current.LastSyncAt == nil || !current.LastSyncAt.Equal(last) || current.NextSyncAt == nil || !current.NextSyncAt.Equal(next) {
					t.Fatal("手工失败不得写入资产、最近同步时间或自动计划")
				}
			} else if assets != 1 || current.LastSyncAt == nil || !current.LastSyncAt.Equal(*completed.FinishedAt) || current.NextSyncAt == nil || !current.NextSyncAt.Equal(completed.FinishedAt.Add(time.Hour)) {
				t.Fatal("部分成功必须一并提交成功类型资产、终态和调度时间")
			}
		})
	}
}

// integrationCollector 保留真实 AWS 输入校验，只替换会访问云端的适配器边界。
type integrationCollector struct{ *awscollector.Collector }

// ResolveCloudAccountID 返回虚构稳定账号，HTTP 验收不访问真实 AWS。
func (integrationCollector) ResolveCloudAccountID(_ context.Context, _ cloudresource.Source, plain []byte) (string, error) {
	var credential struct {
		AccessKeyID string `json:"access_key_id"`
	}
	if err := json.Unmarshal(plain, &credential); err != nil {
		return "", cloudresource.ErrCloudAuthentication
	}
	switch credential.AccessKeyID {
	case "identity-conflict", "identity-occupied":
		return "333333333333", nil
	case "identity-object":
		return "444444444444", nil
	case "identity-authentication":
		return "", fmt.Errorf("云端原始身份错误：%w", cloudresource.ErrCloudAuthentication)
	case "identity-permission":
		return "", fmt.Errorf("云端原始身份错误：%w", cloudresource.ErrCloudPermission)
	case "identity-network":
		return "", fmt.Errorf("云端原始身份错误：%w", cloudresource.ErrCloudNetwork)
	default:
		return "111111111111", nil
	}
}

func (integrationCollector) Collect(_ context.Context, source cloudresource.Source, _ []byte) ([]cloudresource.CollectionResult, error) {
	switch source.Name {
	case "集成全部失败":
		return []cloudresource.CollectionResult{{ResourceType: "ec2", Err: fmt.Errorf("虚构原始认证响应：%w", cloudresource.ErrCloudAuthentication)}, {ResourceType: "rds", Err: cloudresource.ErrCloudNetwork}, {ResourceType: "elb", Err: cloudresource.ErrCloudPermission}}, nil
	case "集成空结果":
		return nil, nil
	case "集成缺少类型":
		return []cloudresource.CollectionResult{{ResourceType: "ec2"}}, nil
	case "集成重复类型":
		return []cloudresource.CollectionResult{{ResourceType: "ec2"}, {ResourceType: "ec2"}, {ResourceType: "elb"}}, nil
	case "集成部分成功":
		return []cloudresource.CollectionResult{{ResourceType: "ec2", Snapshots: []cloudresource.Snapshot{{ExternalID: "i-partial", Name: "成功类型实例"}}}, {ResourceType: "rds", Err: cloudresource.ErrCloudNetwork}, {ResourceType: "elb", Err: cloudresource.ErrCloudPermission}}, nil
	}
	return []cloudresource.CollectionResult{{ResourceType: "ec2", Snapshots: []cloudresource.Snapshot{{ResourceType: "ec2", ExternalID: "i-integration", Name: "集成计算节点", CloudStatus: "running", Endpoints: []cloudresource.EndpointSnapshot{{Kind: "private", Address: "10.0.0.8"}}}}}, {ResourceType: "rds"}, {ResourceType: "elb"}}, nil
}

// Probe 为集成测试提供不含快照的轻量连接结果，避免连接测试与同步行为混淆。
func (integrationCollector) Probe(_ context.Context, source cloudresource.Source, _ []byte) ([]cloudresource.CollectionResult, error) {
	if source.Name == "集成探测权限失败" {
		return nil, fmt.Errorf("虚构探测原文：%w", cloudresource.ErrPermissionDenied)
	}
	if source.Name == "集成探测认证失败" {
		return nil, fmt.Errorf("虚构探测原文：%w", cloudresource.ErrAuthenticationFailed)
	}
	return []cloudresource.CollectionResult{{ResourceType: "ec2"}}, nil
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

// TestRecreatedUsernameCannotReviveIssuedTokens 防止删除后重建同名账号把新权限赋给旧 JWT。
func TestRecreatedUsernameCannotReviveIssuedTokens(t *testing.T) {
	server, password := integrationServer(t)
	admin := loginUser(t, server, "operator", password)
	original := loginUser(t, server, "member_a", password)
	var managedProject project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "account-generation", "name": "账号代际项目"}, http.StatusCreated), &managedProject)
	projectPath := "/api/v1/projects/" + strconv.FormatUint(managedProject.ID, 10)
	integrationRequest(t, server, admin, http.MethodPost, projectPath+"/members", map[string]any{"username": "member_a", "role": "member"}, http.StatusCreated)
	integrationRequest(t, server, original, http.MethodGet, projectPath, nil, http.StatusOK)
	integrationRequest(t, server, original, http.MethodGet, "/api/v1/users", nil, http.StatusForbidden)

	oldTokens := []string{original}
	for _, role := range []string{identity.GlobalRoleSystemAdmin, identity.GlobalRoleUser} {
		integrationRequest(t, server, admin, http.MethodDelete, "/api/v1/users/member_a", nil, http.StatusNoContent)
		integrationRequest(t, server, original, http.MethodGet, "/api/v1/me", nil, http.StatusUnauthorized)
		integrationRequest(t, server, admin, http.MethodPost, "/api/v1/users", map[string]any{
			"username": "member_a", "password": password, "display_name": "重建账号", "global_role": role, "status": "active",
			"project_permissions": []map[string]any{{"project_id": managedProject.ID, "role": "member"}},
		}, http.StatusCreated)
		for _, oldToken := range oldTokens {
			for _, path := range []string{"/api/v1/me", "/api/v1/users", projectPath} {
				t.Run("重建为"+role+path, func(t *testing.T) {
					response := integrationRequest(t, server, oldToken, http.MethodGet, path, nil, http.StatusUnauthorized)
					if response.Body.String() != `{"code":"AUTH_UNAUTHORIZED","message":"身份认证已失效"}` {
						t.Fatal("历史账号令牌必须统一返回认证失效")
					}
				})
			}
		}
		current := loginUser(t, server, "member_a", password)
		integrationRequest(t, server, current, http.MethodGet, "/api/v1/me", nil, http.StatusOK)
		integrationRequest(t, server, current, http.MethodGet, projectPath, nil, http.StatusOK)
		wantUsersStatus := http.StatusForbidden
		if role == identity.GlobalRoleSystemAdmin {
			wantUsersStatus = http.StatusOK
		}
		integrationRequest(t, server, current, http.MethodGet, "/api/v1/users", nil, wantUsersStatus)
		oldTokens = append(oldTokens, current)
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
	if err := db.AutoMigrate(&identity.User{}, &project.Project{}, &project.MemberRole{}, &cloudresource.Source{}, &cloudresource.Server{}, &cloudresource.Database{}, &cloudresource.LoadBalancer{}, &cloudresource.SyncJob{}, &audit.Log{}); err != nil {
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
		{ID: 2, Username: "member_a", PasswordHash: hash, DisplayName: "项目查看者", GlobalRole: "user", Status: "active"},
	} {
		if err := db.Create(&user).Error; err != nil {
			t.Fatal("准备验收身份失败")
		}
	}
	if _, err := rand.Read(secret); err != nil {
		t.Fatal("生成签名密钥失败")
	}
	return httpserver.New(httpserver.Dependencies{Database: db, JWTSecret: hex.EncodeToString(secret), EncryptionKey: "integration-encryption-key", Adapters: map[string]cloudresource.ProviderAdapter{"aws": integrationCollector{Collector: awscollector.NewCollector()}}}), password, db
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
	// 公网客户端可自行伪造转发头，审计来源 IP 必须使用直连地址而不是默认信任该值。
	request.Header.Set("X-Forwarded-For", "198.51.100.99")
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

// integrationRawRequest 用于验证 HTTP JSON 边界，不经测试侧 JSON 编码器修正原始输入。
func integrationRawRequest(t *testing.T, server http.Handler, token, method, path, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
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

// assertNoPublicUserNumericIdentifiers 递归检查公开 JSON，不允许任何层级暴露用户内部数字标识。
func assertNoPublicUserNumericIdentifiers(t *testing.T, value any) {
	t.Helper()
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			if key == "user_id" || key == "owner_user_id" || key == "actor_id" {
				t.Fatalf("公开响应不得包含用户数字标识字段 %q", key)
			}
			assertNoPublicUserNumericIdentifiers(t, child)
		}
	case []any:
		for _, child := range current {
			assertNoPublicUserNumericIdentifiers(t, child)
		}
	}
}

// integrationObject 将已解码的公开 JSON 对象收窄为映射，避免测试重新使用领域模型绕开传输契约。
func integrationObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("公开响应必须是 JSON 对象，实际为 %T", value)
	}
	return object
}

// integrationNumericID 提取允许公开的项目标识，项目 ID 不属于用户内部标识。
func integrationNumericID(t *testing.T, value any) uint64 {
	t.Helper()
	number, ok := value.(float64)
	if !ok || number <= 0 || number != float64(uint64(number)) {
		t.Fatalf("公开项目响应缺少有效项目标识：%v", value)
	}
	return uint64(number)
}

// assertIntegrationAuditResourceID 验证用户及成员审计资源标识沿用公开用户名而非数据库主键。
func assertIntegrationAuditResourceID(t *testing.T, payload any, action, username string) {
	t.Helper()
	items, ok := integrationObject(t, payload)["items"].([]any)
	if !ok {
		t.Fatal("审计公开响应必须包含项目数组")
	}
	for _, item := range items {
		entry := integrationObject(t, item)
		if entry["action"] == action {
			if entry["resource_id"] != username {
				t.Fatalf("审计动作 %s 的资源标识必须为用户名 %q，实际为 %v", action, username, entry["resource_id"])
			}
			return
		}
	}
	t.Fatalf("审计查询必须返回动作 %s", action)
}

// assertIntegrationAuditActors 验证按操作人用户名筛选后的每条审计都属于预期身份，并排除已知的另一操作人。
func assertIntegrationAuditActors(t *testing.T, payload any, expectedActor, excludedActor string) {
	t.Helper()
	items, ok := integrationObject(t, payload)["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatal("按操作人用户名筛选必须返回至少一条审计")
	}
	for _, item := range items {
		actorUsername, _ := integrationObject(t, item)["actor_username"].(string)
		if actorUsername == excludedActor {
			t.Fatalf("操作人筛选不得返回已知另一操作人 %q", excludedActor)
		}
		if actorUsername != expectedActor {
			t.Fatalf("操作人筛选只应返回 %q，实际为 %q", expectedActor, actorUsername)
		}
	}
}
