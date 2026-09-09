// Package httpserver 负责创建 CMDB HTTP 服务基础实例。
package httpserver

import (
	"github-cmdb/internal/identity"
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
	if repository == nil && dependencies.Database != nil {
		repository = identity.NewUserRepository(dependencies.Database)
	}
	SetJWTSecret(dependencies.JWTSecret)
	handler := identity.NewHTTPHandler(identity.NewService(repository, dependencies.JWTSecret))
	engine.POST("/api/v1/auth/login", handler.Login)
	engine.GET("/api/v1/me", RequireUser(), func(c *gin.Context) {
		handler.Me(c, CurrentUser(c))
	})
	return engine
}
