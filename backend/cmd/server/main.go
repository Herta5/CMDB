package main

import (
	"log"
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
	if err != nil { log.Fatalf("failed to connect database: %v", err) }

	if err := db.AutoMigrate(
		&model.CIType{},
		&model.CIAttribute{},
		&model.CIInstance{},
		&model.CIRelationRule{},
		&model.CIRelationInstance{},
		&model.ConfigSnapshot{},
	); err != nil { log.Fatalf("failed to migrate: %v", err) }
	log.Println("database migration completed")

	ciTypeRepo := repository.NewCITypeRepo(db)
	ciInstanceRepo := repository.NewCIInstanceRepo(db)
	snapRepo := repository.NewConfigSnapshotRepo(db)
	relationRepo := repository.NewRelationRepo(db)

	ciTypeSvc := service.NewCITypeSvc(ciTypeRepo)
	ciInstanceSvc := service.NewCIInstanceSvc(ciInstanceRepo, ciTypeRepo, snapRepo)
	relationSvc := service.NewRelationSvc(relationRepo)

	ciTypeHandler := handler.NewCITypeHandler(ciTypeSvc)
	ciInstanceHandler := handler.NewCIInstanceHandler(ciInstanceSvc)
	relationHandler := handler.NewRelationHandler(relationSvc)
	dashboardHandler := handler.NewDashboardHandler(ciInstanceSvc)

	middleware.SetJWTSecret(cfg.JWT.Secret)

	if cfg.Server.Mode == "release" { gin.SetMode(gin.ReleaseMode) }
	r := gin.Default()

	router.Setup(r, &router.Handlers{
		CIType:     ciTypeHandler,
		CIInstance: ciInstanceHandler,
		Relation:   relationHandler,
		Dashboard:  dashboardHandler,
	})

	log.Printf("server starting on :%s", cfg.Server.Port)
	if err := r.Run(":" + cfg.Server.Port); err != nil { log.Fatalf("server failed: %v", err) }
}