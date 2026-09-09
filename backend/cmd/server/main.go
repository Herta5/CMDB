// Command server 启动 CMDB 单体服务，并仅负责装配平台基础设施。
package main

import (
	"log"
	"os"

	"cmdb/internal/platform/config"
	"cmdb/internal/platform/database"
	"cmdb/internal/platform/httpserver"
	"cmdb/internal/web"
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

	server, err := buildServer(httpserver.Dependencies{
		Database:  db,
		JWTSecret: configuration.JWTSecret,
	}, os.Getenv("STATIC_DIR"))
	if err != nil {
		log.Fatalf("装配 HTTP 服务失败：%v", err)
	}
	if err := server.Run(":" + configuration.Server.Port); err != nil {
		log.Fatalf("启动 HTTP 服务失败：%v", err)
	}
}

// buildServer 在同一服务中装配 API 与可选静态页面；显式静态目录损坏时拒绝带缺失页面启动。
func buildServer(dependencies httpserver.Dependencies, staticDir string) (*gin.Engine, error) {
	server := httpserver.New(dependencies)
	if err := web.Mount(server, staticDir); err != nil {
		return nil, err
	}
	return server, nil
}
