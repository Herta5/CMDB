// Command server 启动 CMDB 单体服务，并仅负责装配平台基础设施。
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	aliyuncollector "cmdb/internal/aliyun"
	"cmdb/internal/audit"
	awscollector "cmdb/internal/aws"
	"cmdb/internal/platform/config"
	"cmdb/internal/platform/database"
	"cmdb/internal/platform/httpserver"
	cloudresource "cmdb/internal/resource"
	"cmdb/internal/web"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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
	if err := serve(configuration, db); err != nil {
		log.Fatalf("启动服务失败：%v", err)
	}
}

// serve 在数据库连接建立后装配并启动调度器与 HTTP 服务。
func serve(configuration config.Config, db *gorm.DB) error {
	adapters := map[string]cloudresource.ProviderAdapter{
		cloudresource.ProviderAliyun: aliyuncollector.NewCollector(),
		cloudresource.ProviderAWS:    awscollector.NewCollector(),
	}
	// 调度器与 HTTP 操作共享统一审计仓储，人工同步保留操作者，定时同步保持系统任务语义。
	resourceService := cloudresource.NewService(cloudresource.NewRepository(db), cloudresource.NewCredentialCipher(configuration.EncryptionKey), adapters, audit.NewRepository(db))
	// 调度器和 HTTP 必须共享同一服务实例，才能统一执行同源互斥与生命周期规则。
	schedulerContext, stopScheduler := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopScheduler()
	if err := resourceService.StartScheduler(schedulerContext); err != nil {
		return err
	}

	server, err := buildServer(httpserver.Dependencies{
		Database:        db,
		JWTSecret:       configuration.JWTSecret,
		EncryptionKey:   configuration.EncryptionKey,
		Adapters:        adapters,
		ResourceService: resourceService,
	}, os.Getenv("STATIC_DIR"))
	if err != nil {
		return err
	}
	return server.Run(":" + configuration.Server.Port)
}

// buildServer 在同一服务中装配 API 与可选静态页面；显式静态目录损坏时拒绝带缺失页面启动。
func buildServer(dependencies httpserver.Dependencies, staticDir string) (*gin.Engine, error) {
	server := httpserver.New(dependencies)
	if err := web.Mount(server, staticDir); err != nil {
		return nil, err
	}
	return server, nil
}
