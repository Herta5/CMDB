package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github-cmdb/internal/collector"
	"github-cmdb/internal/config"
	"github-cmdb/internal/handler"
	"github-cmdb/internal/middleware"
	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
	"github-cmdb/internal/router"
	"github-cmdb/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	cfg := config.Load()

	db, err := gorm.Open(mysql.Open(cfg.Database.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}

	if err := db.AutoMigrate(
		&model.CIType{},
		&model.CIAttribute{},
		&model.CIInstance{},
		&model.CIRelationRule{},
		&model.CIRelationInstance{},
		&model.ConfigSnapshot{},
		&model.DiscoveryStrategy{},
		&model.DiscoveryHistory{},
		&model.ChangeTicket{},
		&model.AuditLog{},
		&model.WebhookRecord{},
	); err != nil {
		log.Fatalf("failed to migrate: %v", err)
	}
	log.Println("database migration completed")

	// --- Repositories ---
	ciTypeRepo := repository.NewCITypeRepo(db)
	ciInstanceRepo := repository.NewCIInstanceRepo(db)
	snapRepo := repository.NewConfigSnapshotRepo(db)
	relationRepo := repository.NewRelationRepo(db)
	discoveryRepo := repository.NewDiscoveryRepo(db)
	changeRepo := repository.NewChangeRepo(db)
	auditRepo := repository.NewAuditRepo(db)

	// --- Services ---
	ciTypeSvc := service.NewCITypeSvc(ciTypeRepo)
	ciInstanceSvc := service.NewCIInstanceSvc(ciInstanceRepo, ciTypeRepo, snapRepo)
	relationSvc := service.NewRelationSvc(relationRepo)
	discoverySvc := service.NewDiscoverySvc(discoveryRepo)
	changeSvc := service.NewChangeSvc(changeRepo, ciInstanceRepo, snapRepo)

	// --- Handlers ---
	ciTypeHandler := handler.NewCITypeHandler(ciTypeSvc)
	ciInstanceHandler := handler.NewCIInstanceHandler(ciInstanceSvc)
	relationHandler := handler.NewRelationHandler(relationSvc)
	dashboardHandler := handler.NewDashboardHandler(ciInstanceSvc)
	discoveryHandler := handler.NewDiscoveryHandler(discoverySvc, ciTypeRepo)
	snapshotHandler := handler.NewSnapshotHandler(snapRepo)
	changeHandler := handler.NewChangeHandler(changeSvc)
	batchHandler := handler.NewBatchHandler(ciInstanceSvc)
	integrationHandler := handler.NewIntegrationHandler(ciInstanceRepo, ciTypeRepo, auditRepo, changeSvc)
	auditHandler := handler.NewAuditHandler(auditRepo)

	// --- Discovery Executor & Scheduler ---
	exec := collector.NewDiscoveryExecutor(ciInstanceRepo, ciTypeRepo, snapRepo, relationRepo, discoveryRepo)
	scheduler := collector.NewScheduler(exec, discoveryRepo)
	scheduler.Start()
	defer scheduler.Stop()

	// --- JWT Secret ---
	middleware.SetJWTSecret(cfg.JWT.Secret)

	// --- Audit Middleware ---
	middleware.SetAuditRepo(auditRepo)

	// --- Gin Engine ---
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()

	router.Setup(r, &router.Handlers{
		CIType:      ciTypeHandler,
		CIInstance:  ciInstanceHandler,
		Relation:    relationHandler,
		Dashboard:   dashboardHandler,
		Discovery:   discoveryHandler,
		Snapshot:    snapshotHandler,
		Change:      changeHandler,
		Batch:       batchHandler,
		Integration: integrationHandler,
		Audit:       auditHandler,
	})

	// --- Graceful shutdown ---
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		log.Println("shutting down server...")
		scheduler.Stop()
		os.Exit(0)
	}()

	log.Printf("server starting on :%s", cfg.Server.Port)
	if err := r.Run(":" + cfg.Server.Port); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}