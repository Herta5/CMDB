package handler

import (
	"strconv"
	"github-cmdb/internal/model"
	"github-cmdb/internal/service"
	"github-cmdb/pkg/response"
	"github.com/gin-gonic/gin"
)

type CITypeHandler struct {
	svc *service.CITypeSvc
}

func NewCITypeHandler(svc *service.CITypeSvc) *CITypeHandler {
	return &CITypeHandler{svc: svc}
}

func (h *CITypeHandler) ListTree(c *gin.Context) {
	tree, err := h.svc.ListTree()
	if err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, tree)
}

func (h *CITypeHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	t, err := h.svc.Get(id)
	if err != nil { response.NotFound(c, "ci type not found"); return }
	response.Success(c, t)
}

func (h *CITypeHandler) Create(c *gin.Context) {
	var t model.CIType
	if err := c.ShouldBindJSON(&t); err != nil { response.BadRequest(c, err.Error()); return }
	if err := h.svc.Create(&t); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, t)
}

func (h *CITypeHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	var t model.CIType
	if err := c.ShouldBindJSON(&t); err != nil { response.BadRequest(c, err.Error()); return }
	t.ID = id
	if err := h.svc.Update(&t); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, t)
}

func (h *CITypeHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	if err := h.svc.Delete(id); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, nil)
}

func (h *CITypeHandler) ListAttributes(c *gin.Context) {
	typeID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid type id"); return }
	attrs, err := h.svc.ListAttributes(typeID)
	if err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, attrs)
}

func (h *CITypeHandler) CreateAttribute(c *gin.Context) {
	typeID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid type id"); return }
	var attr model.CIAttribute
	if err := c.ShouldBindJSON(&attr); err != nil { response.BadRequest(c, err.Error()); return }
	attr.CITypeID = typeID
	if err := h.svc.CreateAttribute(&attr); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, attr)
}

func (h *CITypeHandler) UpdateAttribute(c *gin.Context) {
	attrID, err := strconv.ParseUint(c.Param("attrId"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid attribute id"); return }
	var attr model.CIAttribute
	if err := c.ShouldBindJSON(&attr); err != nil { response.BadRequest(c, err.Error()); return }
	attr.ID = attrID
	if err := h.svc.UpdateAttribute(&attr); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, attr)
}

func (h *CITypeHandler) DeleteAttribute(c *gin.Context) {
	attrID, err := strconv.ParseUint(c.Param("attrId"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid attribute id"); return }
	if err := h.svc.DeleteAttribute(attrID); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, nil)
}