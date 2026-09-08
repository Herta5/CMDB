package handler

import (
	"net/http"
	"strconv"

	"github-cmdb/internal/middleware"
	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
	"github-cmdb/internal/service"
	"github-cmdb/pkg/response"
	"github.com/gin-gonic/gin"
)

type ChangeHandler struct {
	svc *service.ChangeSvc
}

func NewChangeHandler(svc *service.ChangeSvc) *ChangeHandler {
	return &ChangeHandler{svc: svc}
}

func (h *ChangeHandler) List(c *gin.Context) {
	var filter repository.ChangeFilter
	if err := c.ShouldBindQuery(&filter); err != nil { response.BadRequest(c, err.Error()); return }
	tickets, total, err := h.svc.List(filter)
	if err != nil { response.InternalError(c, err.Error()); return }
	response.Page(c, tickets, total, filter.Page, filter.PageSize)
}

func (h *ChangeHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	ticket, err := h.svc.Get(id)
	if err != nil { response.NotFound(c, "change ticket not found"); return }
	response.Success(c, ticket)
}

func (h *ChangeHandler) Create(c *gin.Context) {
	var ticket model.ChangeTicket
	if err := c.ShouldBindJSON(&ticket); err != nil { response.BadRequest(c, err.Error()); return }
	if err := h.svc.Create(&ticket); err != nil { response.BadRequest(c, err.Error()); return }
	response.Success(c, ticket)
}

func (h *ChangeHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	var ticket model.ChangeTicket
	if err := c.ShouldBindJSON(&ticket); err != nil { response.BadRequest(c, err.Error()); return }
	ticket.ID = id
	if err := h.svc.Update(&ticket); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, ticket)
}

func (h *ChangeHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	if err := h.svc.Delete(id); err != nil { response.InternalError(c, err.Error()); return }
	response.Success(c, nil)
}

func (h *ChangeHandler) Submit(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	if _, ok := requireChangeOperator(c); !ok { return }
	if err := h.svc.Submit(id); err != nil { response.BadRequest(c, err.Error()); return }
	response.Success(c, nil)
}

func (h *ChangeHandler) Approve(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	operator, ok := requireChangeOperator(c)
	if !ok { return }
	if err := h.svc.Approve(id, operator); err != nil { response.BadRequest(c, err.Error()); return }
	response.Success(c, nil)
}

func (h *ChangeHandler) Reject(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	operator, ok := requireChangeOperator(c)
	if !ok { return }
	if err := h.svc.Reject(id, operator); err != nil { response.BadRequest(c, err.Error()); return }
	response.Success(c, nil)
}

func (h *ChangeHandler) Execute(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	operator, ok := requireChangeOperator(c)
	if !ok { return }
	if err := h.svc.Execute(id, operator); err != nil { response.BadRequest(c, err.Error()); return }
	response.Success(c, nil)
}

func (h *ChangeHandler) Complete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	if _, ok := requireChangeOperator(c); !ok { return }
	if err := h.svc.Complete(id); err != nil { response.BadRequest(c, err.Error()); return }
	response.Success(c, nil)
}

func (h *ChangeHandler) Rollback(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	if _, ok := requireChangeOperator(c); !ok { return }
	if err := h.svc.Rollback(id); err != nil { response.BadRequest(c, err.Error()); return }
	response.Success(c, nil)
}

func (h *ChangeHandler) Fail(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil { response.BadRequest(c, "invalid id"); return }
	if _, ok := requireChangeOperator(c); !ok { return }
	if err := h.svc.Fail(id); err != nil { response.BadRequest(c, err.Error()); return }
	response.Success(c, nil)
}

func requireChangeOperator(c *gin.Context) (string, bool) {
	username := middleware.GetCurrentUsername(c)
	if username == "" {
		response.Error(c, http.StatusUnauthorized, "authenticated username required")
		return "", false
	}
	return username, true
}
