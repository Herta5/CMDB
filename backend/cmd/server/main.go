// Command server 启动 CMDB 单体服务，并仅负责装配平台基础设施。
package main

import (
	"cmdb/internal/platform/diagnostics"
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	slog.SetDefault(diagnostics.New(os.Stderr, slog.LevelInfo))
	configuration, err := config.Load()
	if err != nil {
		slog.Error("加载服务配置失败，请检查必需变量与运行参数", "event", "startup_config_failed")
		os.Exit(1)
	}

	if configuration.Server.Mode == gin.ReleaseMode {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := database.Open(configuration.Database)
	if err != nil {
		slog.Error("连接数据库失败，请检查数据库配置与可达性", "event", "startup_database_failed")
		os.Exit(1)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := serve(configuration, db); err != nil {
		slog.Error("服务启动或关闭失败，请检查运行诊断", "event", "server_failed")
		os.Exit(1)
	}
}

// serve 在数据库连接建立后装配并启动调度器与 HTTP 服务。
func serve(configuration config.Config, db *gorm.DB) error {
	adapters := map[string]cloudresource.ProviderAdapter{
		cloudresource.ProviderAliyun: aliyuncollector.NewCollector(),
		cloudresource.ProviderAWS:    awscollector.NewCollector(),
	}
	// 调度器与 HTTP 操作共享统一审计仓储，人工同步保留操作者，定时同步保持系统任务语义。
	limits := cloudresource.ExecutionConfig{MaxConcurrent: configuration.Sync.MaxConcurrent, Timeout: configuration.Sync.Timeout}
	// 内部装配测试可省略配置，实际启动参数已由 config.Load 完整校验。
	if limits.MaxConcurrent == 0 && limits.Timeout == 0 {
		limits = cloudresource.DefaultExecutionConfig()
	}
	resourceService, err := cloudresource.NewServiceWithExecution(cloudresource.NewRepository(db), cloudresource.NewCredentialCipher(configuration.EncryptionKey), adapters, limits, audit.NewRepository(db))
	if err != nil {
		return err
	}
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
		if stopErr := stopAndWait(resourceService); stopErr != nil {
			slog.Error("工作器退出等待超时", "event", "worker_shutdown_timeout")
		}
		return err
	}
	slog.Info("服务配置已就绪", "event", "server_ready", "sync_max_concurrent", limits.MaxConcurrent, "sync_timeout_seconds", int64(limits.Timeout.Seconds()), "db_max_open_conns", configuration.Database.MaxOpenConns, "db_max_idle_conns", configuration.Database.MaxIdleConns)
	return runHTTP(schedulerContext, &http.Server{Addr: ":" + configuration.Server.Port, Handler: server, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}, resourceService)
}

// buildServer 在同一服务中装配 API 与可选静态页面；显式静态目录损坏时拒绝带缺失页面启动。
func buildServer(dependencies httpserver.Dependencies, staticDir string) (*gin.Engine, error) {
	server := httpserver.New(dependencies)
	if err := web.Mount(server, staticDir); err != nil {
		return nil, err
	}
	return server, nil
}
