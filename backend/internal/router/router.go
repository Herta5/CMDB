package router

import (
	"github-cmdb/internal/handler"
	"github-cmdb/internal/middleware"
	"github.com/gin-gonic/gin"
)

type Handlers struct {
	CIType     *handler.CITypeHandler
	CIInstance *handler.CIInstanceHandler
	Relation   *handler.RelationHandler
	Dashboard  *handler.DashboardHandler
	Discovery  *handler.DiscoveryHandler
	Snapshot   *handler.SnapshotHandler
}

func Setup(r *gin.Engine, h *Handlers) {
	r.Use(middleware.CORS())

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

			relations.GET("/topology", h.Relation.Topology)
			relations.GET("/impact", h.Relation.ImpactAnalysis)
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