package router

import (
	"github-cmdb/internal/handler"
	"github-cmdb/internal/middleware"
	"github.com/gin-gonic/gin"
)

type Handlers struct {
	CIType      *handler.CITypeHandler
	CIInstance  *handler.CIInstanceHandler
	Relation    *handler.RelationHandler
	Dashboard   *handler.DashboardHandler
	Discovery   *handler.DiscoveryHandler
	Snapshot    *handler.SnapshotHandler
	Change      *handler.ChangeHandler
	Batch       *handler.BatchHandler
	Integration *handler.IntegrationHandler
	Audit       *handler.AuditHandler
}

func Setup(r *gin.Engine, h *Handlers) {
	r.Use(middleware.CORS())
	r.Use(middleware.AuditLog())

	// --- Public integration endpoints (no auth) ---
	r.GET("/api/v1/integration/prometheus/targets", h.Integration.PrometheusTargets)
	r.GET("/api/v1/integration/ansible/inventory", h.Integration.AnsibleInventory)
	r.POST("/api/v1/integration/webhook/alertmanager", h.Integration.AlertmanagerWebhook)
	r.POST("/api/v1/integration/webhook/generic", h.Integration.GenericWebhook)

	api := r.Group("/api/v1")
	api.Use(middleware.AuthRequired())
	{
		// --- CI Types ---
		ciTypes := api.Group("/ci-types")
		{
			ciTypes.GET("", h.CIType.ListTree)
			ciTypes.POST("", middleware.RequireRole("cmdb_admin"), h.CIType.Create)
			ciTypes.GET("/:id", h.CIType.Get)
			ciTypes.PUT("/:id", middleware.RequireRole("cmdb_admin"), h.CIType.Update)
			ciTypes.DELETE("/:id", middleware.RequireRole("cmdb_admin"), h.CIType.Delete)
			ciTypes.GET("/:id/attributes", h.CIType.ListAttributes)
			ciTypes.POST("/:id/attributes", middleware.RequireRole("cmdb_admin"), h.CIType.CreateAttribute)
			ciTypes.PUT("/:id/attributes/:attrId", middleware.RequireRole("cmdb_admin"), h.CIType.UpdateAttribute)
			ciTypes.DELETE("/:id/attributes/:attrId", middleware.RequireRole("cmdb_admin"), h.CIType.DeleteAttribute)
		}

		// --- CI Instances ---
		ciInstances := api.Group("/ci-instances")
		{
			ciInstances.GET("", h.CIInstance.List)
			ciInstances.POST("", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.CIInstance.Create)
			ciInstances.GET("/:id", h.CIInstance.Get)
			ciInstances.PUT("/:id", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.CIInstance.Update)
			ciInstances.DELETE("/:id", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.CIInstance.Delete)
			ciInstances.PATCH("/:id/status", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.CIInstance.UpdateStatus)
			// Batch
			ciInstances.POST("/import", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.Batch.ImportCSV)
			ciInstances.GET("/export", h.Batch.ExportCSV)
		}

		// --- Relations ---
		relations := api.Group("/relations")
		{
			relations.GET("/rules", h.Relation.ListRules)
			relations.POST("/rules", middleware.RequireRole("cmdb_admin"), h.Relation.CreateRule)
			relations.PUT("/rules/:id", middleware.RequireRole("cmdb_admin"), h.Relation.UpdateRule)
			relations.DELETE("/rules/:id", middleware.RequireRole("cmdb_admin"), h.Relation.DeleteRule)

			relations.GET("/instances", h.Relation.ListInstances)
			relations.POST("/instances", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.Relation.CreateInstance)
			relations.DELETE("/instances/:id", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.Relation.DeleteInstance)

			relations.GET("/topology", h.Relation.MultiLevelTopology)
			relations.GET("/impact", h.Relation.ImpactAnalysis)
		}

		// --- Changes ---
		changes := api.Group("/changes")
		{
			changes.GET("", h.Change.List)
			changes.POST("", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.Change.Create)
			changes.GET("/:id", h.Change.Get)
			changes.PUT("/:id", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.Change.Update)
			changes.DELETE("/:id", middleware.RequireRole("cmdb_admin"), h.Change.Delete)
			changes.POST("/:id/submit", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.Change.Submit)
			changes.POST("/:id/approve", middleware.RequireRole("cmdb_admin"), h.Change.Approve)
			changes.POST("/:id/reject", middleware.RequireRole("cmdb_admin"), h.Change.Reject)
			changes.POST("/:id/execute", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.Change.Execute)
			changes.POST("/:id/complete", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.Change.Complete)
			changes.POST("/:id/rollback", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.Change.Rollback)
			changes.POST("/:id/fail", middleware.RequireRole("asset_mgr", "cmdb_admin"), h.Change.Fail)
		}

		// --- Discovery ---
		discovery := api.Group("/discovery")
		{
			discovery.GET("/collectors", h.Discovery.CollectorTypes)

			strategies := discovery.Group("/strategies")
			{
				strategies.GET("", h.Discovery.ListStrategies)
				strategies.POST("", middleware.RequireRole("cmdb_admin"), h.Discovery.CreateStrategy)
				strategies.GET("/:id", h.Discovery.GetStrategy)
				strategies.PUT("/:id", middleware.RequireRole("cmdb_admin"), h.Discovery.UpdateStrategy)
				strategies.DELETE("/:id", middleware.RequireRole("cmdb_admin"), h.Discovery.DeleteStrategy)
				strategies.GET("/:id/history", h.Discovery.ListHistory)
			}
		}

		// --- Snapshots ---
		snapshots := api.Group("/snapshots")
		{
			snapshots.GET("", h.Snapshot.List)
			snapshots.GET("/:id", h.Snapshot.Get)
			snapshots.GET("/diff", h.Snapshot.Diff)
		}

		// --- Dashboard ---
		dashboard := api.Group("/dashboard")
		{
			dashboard.GET("/summary", h.Dashboard.Summary)
			dashboard.GET("/distribution", h.Dashboard.Distribution)
			dashboard.GET("/trends", h.Dashboard.Trends)
			dashboard.GET("/capacity", h.Dashboard.Capacity)
		}

		// --- Integration ---
		integration := api.Group("/integration")
		{
			integration.GET("/webhooks", h.Integration.WebhookHistory)
		}

		// --- Audit Logs ---
		audit := api.Group("/audit")
		audit.Use(middleware.RequireRole("cmdb_admin"))
		{
			audit.GET("/logs", h.Audit.List)
		}
	}

	// --- Auth (public) ---
	r.POST("/api/v1/auth/login", func(c *gin.Context) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"code": -1, "message": err.Error()})
			return
		}
		if req.Username != "admin" || req.Password != "admin123" {
			c.JSON(401, gin.H{"code": -1, "message": "invalid credentials"})
			return
		}
		token, err := middleware.GenerateToken(1, "admin", []string{"super_admin"}, 24)
		if err != nil {
			c.JSON(500, gin.H{"code": -1, "message": "failed to generate token"})
			return
		}
		c.JSON(200, gin.H{"code": 0, "message": "ok", "data": gin.H{"token": token}})
	})
}