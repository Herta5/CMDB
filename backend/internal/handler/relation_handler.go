package handler

import (
	"strconv"
	"github-cmdb/internal/model"
	"github-cmdb/internal/service"
	"github-cmdb/pkg/response"
	"github.com/gin-gonic/gin"
)

type RelationHandler struct {
	svc *service.RelationSvc
}

func NewRelationHandler(svc *service.RelationSvc) *RelationHandler {
	return &RelationHandler{svc: svc}
}

func (h *RelationHandler) ListRules(c *gin.Context) {
	rules, err := h.svc.ListRules()
	if err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, rules)
}

func (h *RelationHandler) CreateRule(c *gin.Context) {
	var rule model.CIRelationRule
	if err := c.ShouldBindJSON(&rule); err != nil { response.BadRequest(c, err.Error()); return }
	if err := h.svc.CreateRule(&rule); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, rule)
}

func (h *RelationHandler) UpdateRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	var rule model.CIRelationRule
	if err := c.ShouldBindJSON(&rule); err != nil { response.BadRequest(c, err.Error()); return }
	rule.ID = id
	if err := h.svc.UpdateRule(&rule); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, rule)
}

func (h *RelationHandler) DeleteRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	if err := h.svc.DeleteRule(id); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, nil)
}

func (h *RelationHandler) ListInstances(c *gin.Context) {
	var filter service.RelationInstanceFilter
	if err := c.ShouldBindQuery(&filter); err != nil { response.BadRequest(c, err.Error()); return }
	instances, err := h.svc.ListInstances(filter)
	if err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, instances)
}

func (h *RelationHandler) CreateInstance(c *gin.Context) {
	var inst model.CIRelationInstance
	if err := c.ShouldBindJSON(&inst); err != nil { response.BadRequest(c, err.Error()); return }
	if err := h.svc.CreateInstance(&inst); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, inst)
}

func (h *RelationHandler) DeleteInstance(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	if err := h.svc.DeleteInstance(id); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, nil)
}

func (h *RelationHandler) Topology(c *gin.Context) {
	ciID, err := strconv.ParseUint(c.Query("ci_id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid ci_id"); return }
	depth := 3
	if d, err := strconv.Atoi(c.DefaultQuery("depth", "3")); err == nil { depth = d }
	instances, err := h.svc.Topology(ciID, depth)
	if err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, instances)
}

func (h *RelationHandler) ImpactAnalysis(c *gin.Context) {
	ciID, err := strconv.ParseUint(c.Query("ci_id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid ci_id"); return }
	depth := 5
	if d, err := strconv.Atoi(c.DefaultQuery("depth", "5")); err == nil { depth = d }
	nodes, err := h.svc.ImpactAnalysis(ciID, depth)
	if err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, nodes)
}

func (h *RelationHandler) MultiLevelTopology(c *gin.Context) {
	ciID, err := strconv.ParseUint(c.Query("ci_id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid ci_id"); return }
	depth := 3
	if d, err := strconv.Atoi(c.DefaultQuery("depth", "3")); err == nil { depth = d }
	graph, err := h.svc.MultiLevelTopology(ciID, depth)
	if err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, graph)
}