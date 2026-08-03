package handler

import (
	"strconv"

	"github-cmdb/internal/repository"
	"github-cmdb/pkg/response"
	"github.com/gin-gonic/gin"
)

type AuditHandler struct {
	repo *repository.AuditRepo
}

func NewAuditHandler(repo *repository.AuditRepo) *AuditHandler {
	return &AuditHandler{repo: repo}
}

func (h *AuditHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	filter := repository.AuditFilter{
		Username: c.Query("username"),
		Method:   c.Query("method"),
		Path:     c.Query("path"),
		Page:     page,
		PageSize: pageSize,
	}

	logs, total, err := h.repo.List(filter)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Page(c, logs, total, page, pageSize)
}