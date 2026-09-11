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
	"time"

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
	integrationRequest(t, server, admin, "POST", firstPath+"/members", map[string]any{"user_id": 2, "role": "member"}, 201)

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

// TestSystemAdministratorManagesUsers 验证创建用户可原子授予多个项目角色，列表仅暴露公开资料。
func TestSystemAdministratorManagesUsers(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member-a", password)
	var firstProject, secondProject project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "user-auth-a", "name": "用户授权甲"}, http.StatusCreated), &firstProject)
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "user-auth-b", "name": "用户授权乙"}, http.StatusCreated), &secondProject)
	created := integrationRequest(t, server, admin, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "cloud-user", "password": "secure-user-password", "display_name": "云资源用户", "email": "cloud@example.invalid", "global_role": "user", "status": "active",
		"project_permissions": []map[string]any{{"project_id": firstProject.ID, "role": "project_admin"}, {"project_id": secondProject.ID, "role": "member"}},
	}, http.StatusCreated)
	if strings.Contains(created.Body.String(), "password") {
		t.Fatal("创建用户响应不得包含密码或密码哈希")
	}
	var user identity.User
	decodeIntegration(t, created, &user)
	if user.ID == 0 || user.Username != "cloud-user" || user.GlobalRole != identity.GlobalRoleUser || user.Status != "active" {
		t.Fatal("创建用户必须返回指定的全局角色和状态")
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
	integrationRequest(t, server, admin, http.MethodPut, "/api/v1/users/"+strconv.FormatUint(user.ID, 10), map[string]any{"display_name": "云资源用户", "email": "cloud@example.invalid", "global_role": "user", "status": "disabled", "project_permissions": []map[string]any{{"project_id": secondProject.ID, "role": "member"}}}, http.StatusOK)
	integrationRequest(t, server, "", http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "cloud-user", "password": "secure-user-password"}, http.StatusUnauthorized)
}

// TestSystemAdministratorDeletesUser 验证系统管理员可删除其他用户，但不能删除当前登录身份。
func TestSystemAdministratorDeletesUser(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member-a", password)
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
	audit := cloudresource.AuditLog{ActorID: &ownerID, ProjectID: &ownedProject.ID, Action: "user.fixture", ResourceType: "user", ResourceID: "2", Detail: []byte(`{}`)}
	if err := db.Create(&audit).Error; err != nil {
		t.Fatalf("准备独立审计记录失败：%v", err)
	}

	forbidden := integrationRequest(t, server, member, http.MethodDelete, "/api/v1/users/2", nil, http.StatusForbidden)
	if forbidden.Body.String() != `{"code":"USER_FORBIDDEN","message":"无权执行该操作"}` {
		t.Fatalf("普通用户删除响应契约错误：%s", forbidden.Body.String())
	}
	protected := integrationRequest(t, server, admin, http.MethodDelete, "/api/v1/users/1", nil, http.StatusConflict)
	if protected.Body.String() != `{"code":"USER_SELF_PROTECTED","message":"不能删除当前管理员"}` {
		t.Fatalf("管理员自删保护响应契约错误：%s", protected.Body.String())
	}
	invalid := integrationRequest(t, server, admin, http.MethodDelete, "/api/v1/users/not-a-number", nil, http.StatusBadRequest)
	if invalid.Body.String() != `{"code":"USER_INVALID_REQUEST","message":"请求格式错误"}` {
		t.Fatalf("无效用户标识响应契约错误：%s", invalid.Body.String())
	}
	integrationRequest(t, server, admin, http.MethodDelete, "/api/v1/users/2", nil, http.StatusNoContent)

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
	if err := db.Model(&cloudresource.AuditLog{}).Where("id = ?", audit.ID).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatalf("删除用户后必须保留独立审计记录：count=%d err=%v", audits, err)
	}
	notFound := integrationRequest(t, server, admin, http.MethodDelete, "/api/v1/users/2", nil, http.StatusNotFound)
	if notFound.Body.String() != `{"code":"USER_NOT_FOUND","message":"用户不存在"}` {
		t.Fatalf("重复删除响应契约错误：%s", notFound.Body.String())
	}
}

// TestSystemAdministratorEditsUserAndCannotLockSelfOut 验证用户资料、角色和密码可维护，同时保护当前管理员权限。
func TestSystemAdministratorEditsUserAndCannotLockSelfOut(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member-a", password)
	var firstProject, secondProject project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "replace-a", "name": "替换前项目"}, http.StatusCreated), &firstProject)
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "replace-b", "name": "替换后项目"}, http.StatusCreated), &secondProject)
	created := integrationRequest(t, server, admin, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "editable-user", "password": "initial-user-password", "display_name": "待编辑用户", "email": "old@example.invalid", "global_role": "user", "status": "active", "project_permissions": []map[string]any{{"project_id": firstProject.ID, "role": "member"}},
	}, http.StatusCreated)
	var user identity.User
	decodeIntegration(t, created, &user)
	path := "/api/v1/users/" + strconv.FormatUint(user.ID, 10)
	updated := integrationRequest(t, server, admin, http.MethodPut, path, map[string]any{
		"display_name": "已编辑用户", "email": "new@example.invalid", "global_role": "system_admin", "status": "active", "password": "replacement-password", "project_permissions": []map[string]any{{"project_id": secondProject.ID, "role": "member"}},
	}, http.StatusOK)
	if strings.Contains(updated.Body.String(), "password") || !strings.Contains(updated.Body.String(), "已编辑用户") || !strings.Contains(updated.Body.String(), "system_admin") {
		t.Fatal("编辑响应必须返回更新后的公开资料且不得包含密码")
	}
	var roles []project.MemberRole
	if err := db.Where("user_id = ?", user.ID).Find(&roles).Error; err != nil || len(roles) != 1 || roles[0].ProjectID != secondProject.ID || roles[0].Role != project.MemberRoleMember {
		t.Fatalf("编辑用户必须全量替换项目权限：roles=%+v err=%v", roles, err)
	}
	integrationRequest(t, server, "", http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "editable-user", "password": "initial-user-password"}, http.StatusUnauthorized)
	integrationRequest(t, server, "", http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "editable-user", "password": "replacement-password"}, http.StatusOK)
	integrationRequest(t, server, member, http.MethodPut, path, map[string]any{"display_name": "越权修改", "email": "", "global_role": "user", "status": "active"}, http.StatusForbidden)
	integrationRequest(t, server, admin, http.MethodPut, "/api/v1/users/1", map[string]any{"display_name": "当前管理员", "email": "", "global_role": "user", "status": "active"}, http.StatusConflict)
	integrationRequest(t, server, admin, http.MethodPut, "/api/v1/users/1/status", map[string]any{"status": "disabled"}, http.StatusConflict)
}

// TestCreatingUserWithMissingProjectRollsBack 验证无效项目授权不会留下孤立用户或部分成员关系。
func TestCreatingUserWithMissingProjectRollsBack(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	integrationRequest(t, server, admin, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "rollback-user", "password": "rollback-user-password", "display_name": "回滚用户", "global_role": "user", "status": "active",
		"project_permissions": []map[string]any{{"project_id": 999999, "role": "member"}},
	}, http.StatusBadRequest)
	var users, memberships int64
	if err := db.Table("users").Where("username = ?", "rollback-user").Count(&users).Error; err != nil {
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
	server, password, db := integrationServerWithDatabase(t)
	admin := loginUser(t, server, "operator", password)
	member := loginUser(t, server, "member-a", password)
	var created project.Project
	decodeIntegration(t, integrationRequest(t, server, admin, http.MethodPost, "/api/v1/projects", map[string]any{"code": "sources", "name": "接入项目"}, http.StatusCreated), &created)
	path := "/api/v1/projects/" + strconv.FormatUint(created.ID, 10)
	integrationRequest(t, server, admin, http.MethodPost, path+"/members", map[string]any{"user_id": 2, "role": "member"}, http.StatusCreated)
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
	updated := integrationRequest(t, server, admin, http.MethodPut, path+"/sources/"+strconv.FormatUint(source.ID, 10), map[string]any{"name": "AWS 更新账号", "region": "ap-east-1", "config": map[string]any{"environment": "production"}, "enabled": true, "sync_interval_minutes": 120}, http.StatusOK)
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
	integrationRequest(t, server, admin, http.MethodDelete, path+"/sources/"+strconv.FormatUint(source.ID, 10), nil, http.StatusNoContent)
	integrationRequest(t, server, member, http.MethodGet, path+"/sources", nil, http.StatusOK)
}

type integrationCollector struct{}

func (integrationCollector) Collect(context.Context, cloudresource.Source, []byte) ([]cloudresource.CollectionResult, error) {
	return []cloudresource.CollectionResult{{ResourceType: "ec2", Snapshots: []cloudresource.Snapshot{{ResourceType: "ec2", ExternalID: "i-integration", Name: "集成计算节点", CloudStatus: "running", Endpoints: []cloudresource.EndpointSnapshot{{Kind: "private", Address: "10.0.0.8"}}}}}}, nil
}

// Probe 为集成测试提供不含快照的轻量连接结果，避免连接测试与同步行为混淆。
func (integrationCollector) Probe(context.Context, cloudresource.Source, []byte) ([]cloudresource.CollectionResult, error) {
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
	if err := db.AutoMigrate(&identity.User{}, &project.Project{}, &project.MemberRole{}, &cloudresource.Source{}, &cloudresource.Server{}, &cloudresource.Database{}, &cloudresource.LoadBalancer{}, &cloudresource.SyncJob{}, &cloudresource.AuditLog{}); err != nil {
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
