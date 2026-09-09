// Command server 启动 CMDB 单体服务，并仅负责装配平台基础设施。
package main

import (
	"log"

	"github-cmdb/internal/platform/config"
	"github-cmdb/internal/platform/database"
	"github-cmdb/internal/platform/httpserver"
	"github.com/gin-gonic/gin"
)

func main() {
	configuration, err := config.Load()
	if err != nil {
		log.Fatalf("加载服务配置失败：%v", err)
	}

	if configuration.Server.Mode == gin.ReleaseMode {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := database.Open(configuration.Database)
	if err != nil {
		log.Fatalf("连接数据库失败：%v", err)
	}

	server := httpserver.New(httpserver.Dependencies{
		Database:  db,
		JWTSecret: configuration.JWTSecret,
	})
	if err := server.Run(":" + configuration.Server.Port); err != nil {
		log.Fatalf("启动 HTTP 服务失败：%v", err)
	}
}
