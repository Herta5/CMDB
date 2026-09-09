// 本文件通过真实 HTTP 路由验证项目成员角色与跨项目隔离，避免只覆盖中间件的内部实现。
package project_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github-cmdb/internal/identity"
	"github-cmdb/internal/project"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TestMemberCannotReadAnotherProject 防止普通成员借助项目 ID 枚举或读取未加入的项目。
func TestMemberCannotReadAnotherProject(t *testing.T) {
	server, db := newProjectHTTPServerWithDatabase(t)
	projectA := createProjectThroughHTTP(t, server, `{"code":"platform","name":"平台项目"}`)
	projectB := createProjectThroughHTTP(t, server, `{"code":"data","name":"数据项目"}`)
	memberOfProjectA := createProjectMember(t, db, projectA.ID, 7, project.MemberRoleMember)

	response := requestProjectAsUser(t, server, projectB.ID, memberOfProjectA.ID)
	if response.Code != http.StatusNotFound || response.Body.String() != `{"code":"PROJECT_NOT_FOUND","message":"项目不存在"}` {
		t.Fatalf("跨项目访问必须隐藏资源存在性：status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestMemberCanReadOwnProject 确保成员关系是项目详情访问的必要且充分条件。
func TestMemberCanReadOwnProject(t *testing.T) {
	server, db := newProjectHTTPServerWithDatabase(t)
	ownedProject := createProjectThroughHTTP(t, server, `{"code":"platform","name":"平台项目"}`)
	member := createProjectMember(t, db, ownedProject.ID, 7, project.MemberRoleMember)

	response := requestProjectAsUser(t, server, ownedProject.ID, member.ID)
	if response.Code != http.StatusOK {
		t.Fatalf("项目成员必须能读取自己的项目：status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestProjectAdminManagesMembers 验证项目管理员可以新增、查询、修改和移除本项目成员。
func TestProjectAdminManagesMembers(t *testing.T) {
	server, db := newProjectHTTPServerWithDatabase(t)
	managedProject := createProjectThroughHTTP(t, server, `{"code":"platform","name":"平台项目"}`)
	admin := createProjectMember(t, db, managedProject.ID, 7, project.MemberRoleProjectAdmin)
	createProjectUser(t, db, 8)

	created := requestProjectMember(t, server, http.MethodPost, managedProject.ID, "", `{"user_id":8,"role":"viewer"}`, admin.ID, identity.GlobalRoleUser)
	if created.Code != http.StatusCreated || created.Body.String() == "" {
		t.Fatalf("项目管理员新增成员失败：status=%d body=%s", created.Code, created.Body.String())
	}

	listed := requestProjectMember(t, server, http.MethodGet, managedProject.ID, "", "", admin.ID, identity.GlobalRoleUser)
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte(`"user_id":8`)) {
		t.Fatalf("项目管理员必须能查看成员：status=%d body=%s", listed.Code, listed.Body.String())
	}

	updated := requestProjectMember(t, server, http.MethodPut, managedProject.ID, "/8", `{"role":"member"}`, admin.ID, identity.GlobalRoleUser)
	if updated.Code != http.StatusOK || !bytes.Contains(updated.Body.Bytes(), []byte(`"role":"member"`)) {
		t.Fatalf("项目管理员修改成员角色失败：status=%d body=%s", updated.Code, updated.Body.String())
	}

	deleted := requestProjectMember(t, server, http.MethodDelete, managedProject.ID, "/8", "", admin.ID, identity.GlobalRoleUser)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("项目管理员移除成员失败：status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	var remaining int64
	if err := db.Model(&project.MemberRole{}).Where("project_id = ? AND user_id = ?", managedProject.ID, 8).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("移除成员必须删除成员关系：count=%d err=%v", remaining, err)
	}
}

// TestReadOnlyRolesCannotManageMembers 防止普通成员或只读成员利用成员接口修改项目权限边界。
func TestReadOnlyRolesCannotManageMembers(t *testing.T) {
	for _, memberRole := range []string{project.MemberRoleMember, project.MemberRoleViewer} {
		t.Run(memberRole, func(t *testing.T) {
			server, db := newProjectHTTPServerWithDatabase(t)
			managedProject := createProjectThroughHTTP(t, server, `{"code":"platform","name":"平台项目"}`)
			member := createProjectMember(t, db, managedProject.ID, 7, memberRole)
			createProjectUser(t, db, 8)

			response := requestProjectMember(t, server, http.MethodPost, managedProject.ID, "", `{"user_id":8,"role":"member"}`, member.ID, identity.GlobalRoleUser)
			if response.Code != http.StatusNotFound || response.Body.String() != `{"code":"PROJECT_NOT_FOUND","message":"项目不存在"}` {
				t.Fatalf("只读角色管理权限必须隐藏项目存在性：status=%d body=%s", response.Code, response.Body.String())
			}
			var created int64
			if err := db.Model(&project.MemberRole{}).Where("project_id = ? AND user_id = ?", managedProject.ID, 8).Count(&created).Error; err != nil || created != 0 {
				t.Fatalf("未授权成员管理不得写入成员关系：count=%d err=%v", created, err)
			}
		})
	}
}

// TestSystemAdminBypassesMembership 验证系统管理员不需要预先建立成员关系即可处理项目级操作。
func TestSystemAdminBypassesMembership(t *testing.T) {
	server := newProjectHTTPServer(t)
	target := createProjectThroughHTTP(t, server, `{"code":"platform","name":"平台项目"}`)

	response := requestProjectAsRole(t, server, target.ID, 99, identity.GlobalRoleSystemAdmin)
	if response.Code != http.StatusOK {
		t.Fatalf("系统管理员必须绕过项目成员检查：status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestRequireRolePreservesRepositoryFailure 防止成员关系存储异常被误判为无权限，从而掩盖服务故障。
func TestRequireRolePreservesRepositoryFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repositoryFailure := &memberLookupFailureRepository{}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.UserClaimsContextKey, identity.UserClaims{UserID: 7, GlobalRole: identity.GlobalRoleUser})
	})
	router.GET("/projects/:id", project.RequireRole(repositoryFailure, project.MemberRoleMember), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/projects/1", nil))
	if response.Code != http.StatusInternalServerError || response.Body.String() != `{"code":"PROJECT_SERVICE_UNAVAILABLE","message":"项目服务暂不可用"}` {
		t.Fatalf("成员仓储故障必须保持服务错误：status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestRequireRoleSupportsProjectIDPath 防止后续项目级资源路由使用 projectId 参数时意外绕过成员关系校验。
func TestRequireRoleSupportsProjectIDPath(t *testing.T) {
	_, db := newProjectHTTPServerWithDatabase(t)
	ownedProject := &project.Project{Code: "platform", Name: "平台项目", Status: project.ProjectStatusEnabled}
	if err := db.Create(ownedProject).Error; err != nil {
		t.Fatalf("准备项目失败：%v", err)
	}
	member := createProjectMember(t, db, ownedProject.ID, 7, project.MemberRoleMember)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.UserClaimsContextKey, identity.UserClaims{UserID: member.ID, GlobalRole: identity.GlobalRoleUser})
	})
	router.GET("/projects/:projectId/resources", project.RequireRole(project.NewRepository(db), project.MemberRoleMember), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/projects/"+strconv.FormatUint(ownedProject.ID, 10)+"/resources", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("projectId 参数的项目路由必须正确完成成员授权：status=%d body=%s", response.Code, response.Body.String())
	}
}

// memberLookupFailureRepository 仅让成员查询返回基础设施错误，以验证权限中间件不将其转换为项目不存在。
type memberLookupFailureRepository struct {
	project.Repository
}

// FindMemberRole 模拟成员关系查询的基础设施故障。
func (r *memberLookupFailureRepository) FindMemberRole(_ context.Context, _, _ uint64) (*project.MemberRole, error) {
	return nil, errors.New("项目成员仓储不可用")
}

// createProjectMember 使用真实持久化成员关系准备权限场景，避免测试依赖中间件实现细节。
func createProjectMember(t *testing.T, db *gorm.DB, projectID, userID uint64, role string) identity.User {
	t.Helper()
	member := createProjectUser(t, db, userID)
	if err := db.Create(&project.MemberRole{ProjectID: projectID, UserID: userID, Role: role}).Error; err != nil {
		t.Fatalf("准备项目成员关系失败：%v", err)
	}
	return member
}

// createProjectUser 创建可被成员关系外键引用的普通用户。
func createProjectUser(t *testing.T, db *gorm.DB, userID uint64) identity.User {
	t.Helper()
	member := identity.User{ID: userID, Username: "member-" + strconv.FormatUint(userID, 10), PasswordHash: "test-hash", DisplayName: "项目成员", GlobalRole: identity.GlobalRoleUser, Status: "active"}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("准备成员用户失败：%v", err)
	}
	return member
}

// requestProjectAsUser 构造真实认证后的项目详情请求，使断言覆盖路由和项目权限中间件。
func requestProjectAsUser(t *testing.T, server http.Handler, projectID, userID uint64) *httptest.ResponseRecorder {
	t.Helper()
	return requestProjectAsRole(t, server, projectID, userID, identity.GlobalRoleUser)
}

// requestProjectAsRole 构造指定全局角色的项目详情请求，用于验证系统管理员例外规则。
func requestProjectAsRole(t *testing.T, server http.Handler, projectID, userID uint64, globalRole string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+strconv.FormatUint(projectID, 10), nil)
	request.Header.Set("Authorization", "Bearer "+projectTestToken(t, userID, globalRole))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

// requestProjectMember 调用项目成员接口，路径片段仅用于附加成员用户标识。
func requestProjectMember(t *testing.T, server http.Handler, method string, projectID uint64, userPath, body string, userID uint64, globalRole string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/v1/projects/"+strconv.FormatUint(projectID, 10)+"/members"+userPath, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+projectTestToken(t, userID, globalRole))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}
