// 本文件将全局与项目审计查询适配为 Gin HTTP 接口，授权由路由中间件先行完成。
package audit

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// HTTPHandler 只处理审计查询参数和稳定响应，不直接执行身份授权。
type HTTPHandler struct {
	service *Service
}

// NewHTTPHandler 创建审计查询处理器。
func NewHTTPHandler(service *Service) *HTTPHandler { return &HTTPHandler{service: service} }

// ListGlobal 返回系统管理员可见的全局及全部项目审计。
func (h *HTTPHandler) ListGlobal(c *gin.Context) { h.list(c, nil) }

// ListProject 强制使用路径项目标识，查询参数不能覆盖权限中间件检查过的边界。
func (h *HTTPHandler) ListProject(c *gin.Context) {
	projectID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || projectID == 0 {
		writeError(c, http.StatusBadRequest, "AUDIT_INVALID_REQUEST", "审计查询参数无效")
		return
	}
	h.list(c, &projectID)
}

// list 解析允许的精确筛选并输出统一分页结构。
func (h *HTTPHandler) list(c *gin.Context, projectID *uint64) {
	filter, err := filterFromQuery(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "AUDIT_INVALID_REQUEST", "审计查询参数无效")
		return
	}
	filter.ProjectID = projectID
	page, err := h.service.List(c.Request.Context(), filter)
	if errors.Is(err, ErrInvalidFilter) {
		writeError(c, http.StatusBadRequest, "AUDIT_INVALID_REQUEST", "审计查询参数无效")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "AUDIT_SERVICE_UNAVAILABLE", "审计服务暂不可用")
		return
	}
	c.JSON(http.StatusOK, page)
}

// filterFromQuery 严格解析数字与 RFC3339 时间，非法值不能悄悄退化为无筛选查询。
func filterFromQuery(c *gin.Context) (Filter, error) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil {
		return Filter{}, err
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil {
		return Filter{}, err
	}
	filter := Filter{Action: c.Query("action"), ResourceType: c.Query("resource_type"), ResourceID: c.Query("resource_id"), Page: page, PageSize: pageSize}
	if value := c.Query("snapshot_id"); value != "" {
		snapshotID, parseErr := strconv.ParseUint(value, 10, 64)
		if parseErr != nil || snapshotID == 0 {
			return Filter{}, ErrInvalidFilter
		}
		filter.SnapshotID = snapshotID
	}
	if value := c.Query("actor_id"); value != "" {
		actorID, parseErr := strconv.ParseUint(value, 10, 64)
		if parseErr != nil || actorID == 0 {
			return Filter{}, ErrInvalidFilter
		}
		filter.ActorID = &actorID
	}
	if value := c.Query("start_at"); value != "" {
		parsed, parseErr := time.Parse(time.RFC3339, value)
		if parseErr != nil {
			return Filter{}, parseErr
		}
		filter.StartAt = &parsed
	}
	if value := c.Query("end_at"); value != "" {
		parsed, parseErr := time.Parse(time.RFC3339, value)
		if parseErr != nil {
			return Filter{}, parseErr
		}
		filter.EndAt = &parsed
	}
	return filter, nil
}

// writeError 输出审计接口稳定错误，不暴露 SQL、用户或项目是否存在。
func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"code": code, "message": message})
}
