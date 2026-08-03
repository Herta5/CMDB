package handler

import (
	"strconv"

	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
	"github-cmdb/internal/service"
	"github-cmdb/pkg/response"
	"github.com/gin-gonic/gin"
)

// DiscoveryHandler handles discovery strategy and history endpoints.
type DiscoveryHandler struct {
	svc      *service.DiscoverySvc
	typeRepo *repository.CITypeRepo
}

func NewDiscoveryHandler(svc *service.DiscoverySvc, typeRepo *repository.CITypeRepo) *DiscoveryHandler {
	return &DiscoveryHandler{svc: svc, typeRepo: typeRepo}
}

// --- Strategies ---

func (h *DiscoveryHandler) ListStrategies(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	list, total, err := h.svc.ListStrategies(page, size)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Page(c, list, total, page, size)
}

func (h *DiscoveryHandler) GetStrategy(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	st, err := h.svc.GetStrategy(id)
	if err != nil {
		response.NotFound(c, "strategy not found")
		return
	}
	response.Success(c, st)
}

func (h *DiscoveryHandler) CreateStrategy(c *gin.Context) {
	var st model.DiscoveryStrategy
	if err := c.ShouldBindJSON(&st); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := h.svc.CreateStrategy(&st); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, st)
}

func (h *DiscoveryHandler) UpdateStrategy(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	var st model.DiscoveryStrategy
	if err := c.ShouldBindJSON(&st); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	st.ID = id
	if err := h.svc.UpdateStrategy(&st); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, st)
}

func (h *DiscoveryHandler) DeleteStrategy(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid id")
		return
	}
	if err := h.svc.DeleteStrategy(id); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, nil)
}

// --- History ---

func (h *DiscoveryHandler) ListHistory(c *gin.Context) {
	strategyID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid strategy id")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	list, total, err := h.svc.ListHistory(strategyID, page, size)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Page(c, list, total, page, size)
}

// CollectorTypes returns the list of registered collector names.
func (h *DiscoveryHandler) CollectorTypes(c *gin.Context) {
	// import cycle avoided by using a string list; we hardcode the known types.
	types := []map[string]string{
		{"name": "ssh", "label": "SSH (Agentless)", "description": "Discover Linux/Windows hosts via SSH"},
		{"name": "k8s_api", "label": "Kubernetes API", "description": "Sync K8s resources (Nodes, Pods, Services)"},
		{"name": "agent", "label": "Agent", "description": "Lightweight agent for host discovery"},
	}
	response.Success(c, types)
}