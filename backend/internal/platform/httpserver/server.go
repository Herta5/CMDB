// Package httpserver 负责创建 CMDB HTTP 服务基础实例。
package httpserver

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Dependencies 声明 HTTP 服务依赖，后续领域路由通过该边界接入共享基础设施。
type Dependencies struct {
	Database *gorm.DB
}

// New 创建启用恢复中间件的 Gin 引擎。
// 路由注册由各业务模块完成，避免平台构造器承担项目、资源或云平台领域职责。
func New(_ Dependencies) *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Recovery())
	return engine
}
