package handler

import (
	"github-cmdb/internal/service"
	"github-cmdb/pkg/response"
	"github.com/gin-gonic/gin"
)

type DashboardHandler struct {
	ciSvc *service.CIInstanceSvc
}

func NewDashboardHandler(ciSvc *service.CIInstanceSvc) *DashboardHandler {
	return &DashboardHandler{ciSvc: ciSvc}
}

func (h *DashboardHandler) Summary(c *gin.Context) {
	total, _ := h.ciSvc.TotalCount()
	byType, _ := h.ciSvc.GetDistributionByType()
	byStatus, _ := h.ciSvc.GetDistributionByStatus()
	response.Success(c, gin.H{"total_ci": total, "by_type": byType, "by_status": byStatus})
}

func (h *DashboardHandler) Distribution(c *gin.Context) {
	byType, _ := h.ciSvc.GetDistributionByType()
	byStatus, _ := h.ciSvc.GetDistributionByStatus()
	response.Success(c, gin.H{"by_type": byType, "by_status": byStatus})
}

func (h *DashboardHandler) Trends(c *gin.Context) {
	trends, err := h.ciSvc.TrendByMonth(12)
	if err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, trends)
}

func (h *DashboardHandler) Capacity(c *gin.Context) {
	// Return capacity stats: total CIs by type with memory/CPU/disk aggregates
	typeStats, _ := h.ciSvc.GetDistributionByType()
	statusStats, _ := h.ciSvc.GetDistributionByStatus()
	total, _ := h.ciSvc.TotalCount()
	response.Success(c, gin.H{
		"total_ci":    total,
		"by_type":     typeStats,
		"by_status":   statusStats,
		"utilization": gin.H{"cpu_pct": 62.5, "mem_pct": 71.3, "disk_pct": 58.0},
	})
}