// Package httpserver 负责创建 CMDB HTTP 服务基础实例。
package httpserver

import (
	"net/http"

	"cmdb/internal/identity"
	"cmdb/internal/project"
	cloudresource "cmdb/internal/resource"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Dependencies 声明 HTTP 服务依赖，后续领域路由通过该边界接入共享基础设施。
type Dependencies struct {
	Database        *gorm.DB
	UserRepository  identity.UserRepository
	JWTSecret       string
	EncryptionKey   string
	Collectors      map[string]cloudresource.Collector
	ResourceService *cloudresource.Service
}

// New 创建启用恢复中间件的 Gin 引擎并装配公共身份路由。
// 用户仓储允许测试替换，生产环境则统一由平台层数据库创建，避免领域模块自行管理连接。
func New(dependencies Dependencies) *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Recovery())
	// 健康检查只报告进程存活，不暴露数据库配置，也不要求部署探针持有用户凭证。
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	repository := dependencies.UserRepository
	var projectRepository project.Repository
	if repository == nil && dependencies.Database != nil {
		repository = identity.NewUserRepository(dependencies.Database)
	}
	if dependencies.Database != nil {
		projectRepository = project.NewRepository(dependencies.Database)
	}
	identityService := identity.NewService(repository, dependencies.JWTSecret)
	authenticator := NewAuthenticator(identityService, dependencies.JWTSecret)
	handler := identity.NewHTTPHandler(identityService)
	projectHandler := project.NewHTTPHandler(project.NewService(projectRepository))
	resourceService := dependencies.ResourceService
	if resourceService == nil {
		resourceService = cloudresource.NewService(cloudresource.NewRepository(dependencies.Database), cloudresource.NewCredentialCipher(dependencies.EncryptionKey))
	}
	resourceHandler := cloudresource.NewHTTPHandler(resourceService, dependencies.Collectors)
	engine.POST("/api/v1/auth/login", handler.Login)
	engine.GET("/api/v1/me", authenticator.RequireUser(), func(c *gin.Context) {
		handler.Me(c, CurrentUser(c))
	})
	users := engine.Group("/api/v1/users", authenticator.RequireUser())
	users.GET("", func(c *gin.Context) { handler.ListUsers(c, CurrentUser(c)) })
	users.POST("", func(c *gin.Context) { handler.CreateUser(c, CurrentUser(c)) })
	users.PUT("/:id", func(c *gin.Context) { handler.UpdateUser(c, CurrentUser(c)) })
	users.PUT("/:id/status", func(c *gin.Context) { handler.UpdateUserStatus(c, CurrentUser(c)) })
	projects := engine.Group("/api/v1/projects")
	projects.Use(authenticator.RequireUser())
	projects.GET("", func(c *gin.Context) {
		projectHandler.List(c, CurrentUser(c))
	})
	projects.POST("", func(c *gin.Context) {
		projectHandler.Create(c, CurrentUser(c))
	})
	projects.PUT("/:id", func(c *gin.Context) {
		projectHandler.Update(c, CurrentUser(c))
	})
	projects.DELETE("/:id", func(c *gin.Context) {
		projectHandler.Delete(c, CurrentUser(c))
	})
	projectReadRoles := []string{project.MemberRoleProjectAdmin, project.MemberRoleMember, project.MemberRoleViewer}
	projects.GET("/:id", project.RequireRole(projectRepository, projectReadRoles...), projectHandler.Get)
	members := projects.Group("/:id/members", project.RequireRole(projectRepository, projectReadRoles...))
	members.GET("", projectHandler.ListMembers)
	members.POST("", project.RequireRole(projectRepository, project.MemberRoleProjectAdmin), projectHandler.AddMember)
	members.PUT("/:user_id", project.RequireRole(projectRepository, project.MemberRoleProjectAdmin), projectHandler.UpdateMemberRole)
	members.DELETE("/:user_id", project.RequireRole(projectRepository, project.MemberRoleProjectAdmin), projectHandler.RemoveMember)
	projects.GET("/:id/member-candidates", project.RequireRole(projectRepository, project.MemberRoleProjectAdmin), projectHandler.ListMemberCandidates)
	sources := projects.Group("/:id/sources", project.RequireRole(projectRepository, projectReadRoles...))
	sources.GET("", resourceHandler.ListSources)
	sources.POST("", project.RequireRole(projectRepository, project.MemberRoleProjectAdmin), resourceHandler.CreateSource)
	sources.PUT("/:sourceId", project.RequireRole(projectRepository, project.MemberRoleProjectAdmin), resourceHandler.UpdateSource)
	sources.DELETE("/:sourceId", project.RequireRole(projectRepository, project.MemberRoleProjectAdmin), resourceHandler.DeleteSource)
	sources.POST("/:sourceId/sync", project.RequireRole(projectRepository, project.MemberRoleProjectAdmin), resourceHandler.SyncSource)
	sources.POST("/:sourceId/test", project.RequireRole(projectRepository, project.MemberRoleProjectAdmin), resourceHandler.TestSourceConnection)
	projects.GET("/:id/resources", project.RequireRole(projectRepository, projectReadRoles...), resourceHandler.ListResources)
	projects.GET("/:id/sync-jobs", project.RequireRole(projectRepository, projectReadRoles...), resourceHandler.ListJobs)
	projects.POST("/:id/sync-jobs/:jobId/retry", project.RequireRole(projectRepository, project.MemberRoleProjectAdmin), resourceHandler.RetryJob)
	return engine
}
