// 本文件让生成路由成为唯一注册来源，并在生成参数绑定之前完成全部授权。
package api

import (
	"cmdb/internal/api/generated"
	"cmdb/internal/identity"
	"cmdb/internal/project"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gin-gonic/gin"
	"strings"
)

// AuthorizationPolicies 显式声明每个契约操作的权限，新操作遗漏策略时拒绝启动。
var AuthorizationPolicies = map[string]string{
	"health": "public", "login": "public", "getMe": "authenticated", "listProjects": "authenticated",
	"listUsers": "system_admin_users", "createUser": "system_admin_users", "updateUser": "system_admin_users", "deleteUser": "system_admin_users", "updateUserStatus": "system_admin_users",
	"createProject": "system_admin_project_create", "updateProject": "system_admin_project_write", "deleteProject": "system_admin_project_write",
	"listGlobalAuditLogs": "system_admin_audit", "listAllResources": "system_admin_resources",
	"getProject": "project_read", "listProjectMembers": "project_read", "listProjectResources": "project_read", "listSources": "project_read", "listSyncJobs": "project_read",
	"addProjectMember": "project_admin", "updateProjectMemberRole": "project_admin", "removeProjectMember": "project_admin", "listProjectMemberCandidates": "project_admin", "listProjectAuditLogs": "project_admin", "createSource": "project_admin", "updateSource": "project_admin", "deleteSource": "project_admin", "syncSource": "project_admin", "testSourceConnection": "project_admin", "retrySyncJob": "project_admin",
}

// Register 先注册认证、授权和契约校验，再使用生成器注册所有公开操作。
func Register(engine *gin.Engine, handler *Handler, authenticate gin.HandlerFunc, repository project.Repository) {
	swagger, err := generated.GetSwagger()
	if err != nil {
		panic("接口契约不可用")
	}
	routes := map[string]*openapi3.Operation{}
	seen := map[string]bool{}
	for path, item := range swagger.Paths.Map() {
		for method, op := range item.Operations() {
			name := operationName(op.OperationID)
			if _, ok := AuthorizationPolicies[name]; !ok {
				panic("接口缺少授权策略")
			}
			seen[name] = true
			ginPath := path
			for _, p := range strings.Split(path, "/") {
				if strings.HasPrefix(p, "{") {
					ginPath = strings.ReplaceAll(ginPath, p, ":"+strings.Trim(p, "{}"))
				}
			}
			routes[method+" "+ginPath] = op
		}
	}
	if len(seen) != len(AuthorizationPolicies) {
		panic("接口授权策略与契约不一致")
	}
	lookup := func(c *gin.Context) *openapi3.Operation { return routes[c.Request.Method+" "+c.FullPath()] }
	engine.Use(func(c *gin.Context) {
		op := lookup(c)
		if op == nil || AuthorizationPolicies[operationName(op.OperationID)] == "public" {
			c.Next()
			return
		}
		authenticate(c)
	})
	engine.Use(func(c *gin.Context) {
		op := lookup(c)
		if op == nil {
			c.Next()
			return
		}
		policy := AuthorizationPolicies[operationName(op.OperationID)]
		claims := currentClaims(c)
		switch policy {
		case "public", "authenticated":
			c.Next()
		case "project_read":
			project.RequireRole(repository, project.MemberRoleMember, project.MemberRoleProjectAdmin)(c)
		case "project_admin":
			project.RequireRole(repository, project.MemberRoleProjectAdmin)(c)
		default:
			if claims.GlobalRole == identity.GlobalRoleSystemAdmin {
				c.Next()
				return
			}
			var response *errorResponse
			switch policy {
			case "system_admin_users":
				response = failure(403, "USER_FORBIDDEN", "无权执行该操作")
			case "system_admin_project_create":
				response = failure(403, "PROJECT_FORBIDDEN", "无权执行该操作")
			case "system_admin_project_write":
				response = failure(404, "PROJECT_NOT_FOUND", "项目不存在")
			case "system_admin_audit":
				response = failure(403, "AUDIT_FORBIDDEN", "无权查看审计日志")
			case "system_admin_resources":
				response = failure(403, "RESOURCE_FORBIDDEN", "无权查看全部项目资源")
			default:
				response = failure(500, "INTERNAL_ERROR", "服务暂时不可用")
			}
			c.Abort()
			_ = response.write(c.Writer)
		}
	})
	engine.Use(requestValidation(swagger, lookup))
	generated.RegisterHandlersWithOptions(engine, generated.NewStrictHandler(handler, nil), generated.GinServerOptions{ErrorHandler: func(c *gin.Context, _ error, _ int) {
		c.Abort()
		_ = invalidRequest(operationName(lookup(c).OperationID), false).write(c.Writer)
	}})
}
func operationName(v string) string {
	if v == "" {
		return ""
	}
	return strings.ToLower(v[:1]) + v[1:]
}
