// Package httpserver 负责创建 CMDB HTTP 服务基础实例。
package httpserver

import (
	"github-cmdb/internal/identity"
	"github-cmdb/internal/project"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Dependencies 声明 HTTP 服务依赖，后续领域路由通过该边界接入共享基础设施。
type Dependencies struct {
	Database       *gorm.DB
	UserRepository identity.UserRepository
	JWTSecret      string
}

// New 创建启用恢复中间件的 Gin 引擎并装配公共身份路由。
// 用户仓储允许测试替换，生产环境则统一由平台层数据库创建，避免领域模块自行管理连接。
func New(dependencies Dependencies) *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Recovery())

	repository := dependencies.UserRepository
	var projectRepository project.Repository
	if repository == nil && dependencies.Database != nil {
		repository = identity.NewUserRepository(dependencies.Database)
	}
	if dependencies.Database != nil {
		projectRepository = project.NewRepository(dependencies.Database)
	}
	SetJWTSecret(dependencies.JWTSecret)
	handler := identity.NewHTTPHandler(identity.NewService(repository, dependencies.JWTSecret))
	projectHandler := project.NewHTTPHandler(project.NewService(projectRepository))
	engine.POST("/api/v1/auth/login", handler.Login)
	engine.GET("/api/v1/me", RequireUser(), func(c *gin.Context) {
		handler.Me(c, CurrentUser(c))
	})
	projects := engine.Group("/api/v1/projects")
	projects.Use(RequireUser())
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
	return engine
}
