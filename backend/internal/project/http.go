// 本文件提供项目领域的 HTTP 接口，并在进入服务前执行全局管理员授权边界。
package project

import (
	"errors"
	"net/http"
	"strconv"

	"github-cmdb/internal/identity"
	"github.com/gin-gonic/gin"
)

// HTTPHandler 将项目领域服务适配为 HTTP 路由处理器，不直接操作数据库。
type HTTPHandler struct {
	service *Service
}

// NewHTTPHandler 创建项目 HTTP 处理器。
func NewHTTPHandler(service *Service) *HTTPHandler {
	return &HTTPHandler{service: service}
}

// Create 仅允许系统管理员创建全局项目边界，普通用户不得扩大可见资源范围。
func (h *HTTPHandler) Create(c *gin.Context, claims identity.UserClaims) {
	if !isSystemAdmin(claims) {
		writeProjectError(c, http.StatusForbidden, "PROJECT_FORBIDDEN", "无权执行该操作")
		return
	}
	var request struct {
		Code        string  `json:"code"`
		Name        string  `json:"name"`
		Description string  `json:"description"`
		OwnerUserID *uint64 `json:"owner_user_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	project, err := h.service.Create(c.Request.Context(), CreateInput{
		Code:        request.Code,
		Name:        request.Name,
		Description: request.Description,
		OwnerUserID: request.OwnerUserID,
	})
	if errors.Is(err, ErrDuplicateCode) {
		writeProjectError(c, http.StatusConflict, "PROJECT_DUPLICATE_CODE", "项目编码已存在")
		return
	}
	if errors.Is(err, ErrInvalidProjectInput) {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_INPUT", "项目参数无效")
		return
	}
	if err != nil {
		writeProjectError(c, http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用")
		return
	}
	c.JSON(http.StatusCreated, project)
}

// Update 暂仅允许系统管理员修改项目资料；成员角色授权将在独立的项目成员能力中统一收紧。
func (h *HTTPHandler) Update(c *gin.Context, claims identity.UserClaims) {
	if !isSystemAdmin(claims) {
		writeProjectError(c, http.StatusForbidden, "PROJECT_FORBIDDEN", "无权执行该操作")
		return
	}
	projectID, ok := projectIDFromPath(c)
	if !ok {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	var request struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		Status      string  `json:"status"`
		OwnerUserID *uint64 `json:"owner_user_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	project, err := h.service.Update(c.Request.Context(), projectID, UpdateInput{
		Name:        request.Name,
		Description: request.Description,
		Status:      request.Status,
		OwnerUserID: request.OwnerUserID,
	})
	if errors.Is(err, ErrProjectNotFound) {
		writeProjectError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在")
		return
	}
	if errors.Is(err, ErrInvalidProjectInput) || errors.Is(err, ErrInvalidProjectStatus) {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_INPUT", "项目参数无效")
		return
	}
	if err != nil {
		writeProjectError(c, http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用")
		return
	}
	c.JSON(http.StatusOK, project)
}

// List 返回当前用户可见的项目集合；普通用户只能得到成员关系允许的结果。
func (h *HTTPHandler) List(c *gin.Context, claims identity.UserClaims) {
	projects, err := h.service.ListForUser(c.Request.Context(), claims.UserID, claims.GlobalRole)
	if err != nil {
		writeProjectError(c, http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用")
		return
	}
	c.JSON(http.StatusOK, projects)
}

// Delete 仅允许系统管理员移除项目，避免普通用户破坏其他项目成员的数据归属边界。
func (h *HTTPHandler) Delete(c *gin.Context, claims identity.UserClaims) {
	if !isSystemAdmin(claims) {
		writeProjectError(c, http.StatusForbidden, "PROJECT_FORBIDDEN", "无权执行该操作")
		return
	}
	projectID, ok := projectIDFromPath(c)
	if !ok {
		writeProjectError(c, http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误")
		return
	}
	if err := h.service.Delete(c.Request.Context(), projectID); errors.Is(err, ErrProjectNotFound) {
		writeProjectError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在")
		return
	} else if err != nil {
		writeProjectError(c, http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用")
		return
	}
	c.Status(http.StatusNoContent)
}

// projectIDFromPath 严格解析正整数项目标识，避免零值或非数字路径进入项目服务。
func projectIDFromPath(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	return id, err == nil && id > 0
}

// isSystemAdmin 统一维护项目全局管理授权条件，避免把普通用户权限误提升为项目创建权限。
func isSystemAdmin(claims identity.UserClaims) bool {
	return claims.GlobalRole == identity.GlobalRoleSystemAdmin
}

// projectErrorResponse 固定项目接口的错误结构，避免向客户端暴露数据库或授权实现细节。
type projectErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeProjectError 输出稳定且不含内部错误详情的项目接口错误。
func writeProjectError(c *gin.Context, status int, code, message string) {
	c.JSON(status, projectErrorResponse{Code: code, Message: message})
}
