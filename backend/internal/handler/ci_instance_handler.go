package handler

import (
	"strconv"
	"github-cmdb/internal/middleware"
	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
	"github-cmdb/internal/service"
	"github-cmdb/pkg/response"
	"github.com/gin-gonic/gin"
)

type CIInstanceHandler struct {
	svc *service.CIInstanceSvc
}

func NewCIInstanceHandler(svc *service.CIInstanceSvc) *CIInstanceHandler {
	return &CIInstanceHandler{svc: svc}
}

func (h *CIInstanceHandler) List(c *gin.Context) {
	var filter repository.CIInstanceFilter
	if err := c.ShouldBindQuery(&filter); err != nil { response.BadRequest(c, err.Error()); return }
	instances, total, err := h.svc.List(filter)
	if err != nil { response.InternalError(c, err.Error()); return }
	page := filter.Page
	if page < 1 { page = 1 }
	size := filter.PageSize
	if size < 1 || size > 100 { size = 20 }
	response.Page(c, instances, total, page, size)
}

func (h *CIInstanceHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	ci, err := h.svc.Get(id)
	if err != nil { response.NotFound(c, "ci instance not found"); return }
	response.Success(c, ci)
}

func (h *CIInstanceHandler) Create(c *gin.Context) {
	var ci model.CIInstance
	if err := c.ShouldBindJSON(&ci); err != nil { response.BadRequest(c, err.Error()); return }
	username := middleware.GetCurrentUsername(c)
	ci.CreatedBy = &username
	ci.UpdatedBy = &username
	if err := h.svc.Create(&ci); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, ci)
}

func (h *CIInstanceHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	var ci model.CIInstance
	if err := c.ShouldBindJSON(&ci); err != nil { response.BadRequest(c, err.Error()); return }
	ci.ID = id
	username := middleware.GetCurrentUsername(c)
	ci.UpdatedBy = &username
	if err := h.svc.Update(&ci); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, ci)
}

func (h *CIInstanceHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	if err := h.svc.Delete(id); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, nil)
}

func (h *CIInstanceHandler) UpdateStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	var body struct { Status string `json:"status"` }
	if err := c.ShouldBindJSON(&body); err != nil { response.BadRequest(c, err.Error()); return }
	ci, err := h.svc.Get(id)
	if err != nil { response.NotFound(c, "ci instance not found"); return }
	ci.Status = body.Status
	username := middleware.GetCurrentUsername(c)
	ci.UpdatedBy = &username
	if err := h.svc.Update(ci); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, ci)
}