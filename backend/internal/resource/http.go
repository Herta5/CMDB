// 本文件提供项目边界内的接入源 HTTP 接口，所有响应排除凭证明文和密文。
package resource

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// HTTPHandler 将资源服务适配为项目级 HTTP 接口。
type HTTPHandler struct {
	service *Service
}

// writeSourceIdentityError 为创建与替换凭证时的云身份识别提供同一组稳定公开分类。
func writeSourceIdentityError(c *gin.Context, err error) bool {
	if writeSourceIdentityConflict(c, err) {
		return true
	}
	switch {
	case errors.Is(err, ErrCloudAuthentication):
		c.JSON(http.StatusBadGateway, gin.H{"code": "CLOUD_IDENTITY_UNAVAILABLE", "message": "AccessKey 无效或签名校验失败，请检查凭证"})
		return true
	case errors.Is(err, ErrCloudPermission):
		c.JSON(http.StatusBadGateway, gin.H{"code": "CLOUD_IDENTITY_UNAVAILABLE", "message": "云账号身份查询权限不足，请检查云账号授权"})
		return true
	case errors.Is(err, ErrCloudNetwork):
		c.JSON(http.StatusBadGateway, gin.H{"code": "CLOUD_IDENTITY_UNAVAILABLE", "message": "云账号身份服务连接失败，请检查服务端网络"})
		return true
	}
	return false
}

// writeSourceIdentityConflict 只处理账号归属和替换身份不一致，不把连接 Probe 的错误当作身份识别故障。
func writeSourceIdentityConflict(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, ErrCloudAccountConflict):
		c.JSON(http.StatusConflict, gin.H{"code": "CLOUD_ACCOUNT_CONFLICT", "message": "该云账号已接入 CMDB"})
	case errors.Is(err, ErrSourceIdentityMismatch):
		c.JSON(http.StatusConflict, gin.H{"code": "SOURCE_IDENTITY_MISMATCH", "message": "新凭证所属云账号与原接入源不一致"})
	default:
		return false
	}
	return true
}

// writeSourceMutationError 仅有限输入领域错误属于 400，未知事务、加密和依赖失败统一安全 500。
func writeSourceMutationError(c *gin.Context, err error) {
	if writeSourceIdentityError(c, err) {
		return
	}
	if errors.Is(err, ErrInvalidSourceInput) || errors.Is(err, ErrInvalidProviderCredential) || errors.Is(err, ErrInvalidProviderConfig) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "SOURCE_INVALID_INPUT", "message": "接入源参数无效"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"code": "SOURCE_SERVICE_UNAVAILABLE", "message": "接入源服务暂不可用"})
}

// UpdateSource 更新接入源非敏感配置，并允许调用方选择性替换凭证。
func (h *HTTPHandler) UpdateSource(c *gin.Context) {
	projectID, ok := projectID(c)
	sourceID, err := strconv.ParseUint(c.Param("sourceId"), 10, 64)
	if !ok || err != nil || sourceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "SOURCE_INVALID_REQUEST", "message": "请求格式错误"})
		return
	}
	var request struct {
		Name                string          `json:"name"`
		Region              string          `json:"region"`
		Credential          json.RawMessage `json:"credential"`
		Config              json.RawMessage `json:"config"`
		Enabled             bool            `json:"enabled"`
		SyncIntervalMinutes int             `json:"sync_interval_minutes"`
	}
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "SOURCE_INVALID_REQUEST", "message": "请求格式错误"})
		return
	}
	source, err := h.service.UpdateSource(c.Request.Context(), projectID, sourceID, UpdateSourceInput{Name: request.Name, Region: request.Region, Credential: request.Credential, Config: request.Config, Enabled: request.Enabled, SyncIntervalMinutes: request.SyncIntervalMinutes})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "SOURCE_NOT_FOUND", "message": "接入源不存在"})
		return
	}
	if err != nil {
		writeSourceMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, source)
}

// DeleteSource 删除当前项目内的接入源。
func (h *HTTPHandler) DeleteSource(c *gin.Context) {
	projectID, ok := projectID(c)
	sourceID, err := strconv.ParseUint(c.Param("sourceId"), 10, 64)
	if !ok || err != nil || sourceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "SOURCE_INVALID_REQUEST", "message": "请求格式错误"})
		return
	}
	if err := h.service.DeleteSource(c.Request.Context(), projectID, sourceID); errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "SOURCE_NOT_FOUND", "message": "接入源不存在"})
		return
	} else if errors.Is(err, ErrDeleteDependencyConflict) {
		c.JSON(http.StatusConflict, gin.H{"code": "SOURCE_DELETE_CONFLICT", "message": "接入源仍有资产或运行中的同步任务，暂不能删除"})
		return
	} else if err != nil {
		if writeSourceIdentityError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "SOURCE_DELETE_FAILED", "message": "删除接入源失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// NewHTTPHandler 创建统一资源 HTTP 处理器。
func NewHTTPHandler(service *Service) *HTTPHandler {
	return &HTTPHandler{service: service}
}

// CreateSource 创建已加密凭证的接入源，项目权限由前置中间件验证。
func (h *HTTPHandler) CreateSource(c *gin.Context) {
	projectID, ok := projectID(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "SOURCE_INVALID_REQUEST", "message": "请求格式错误"})
		return
	}
	var request struct {
		Provider            string          `json:"provider"`
		Name                string          `json:"name"`
		Region              string          `json:"region"`
		Credential          json.RawMessage `json:"credential"`
		Config              json.RawMessage `json:"config"`
		SyncIntervalMinutes int             `json:"sync_interval_minutes"`
	}
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "SOURCE_INVALID_REQUEST", "message": "请求格式错误"})
		return
	}
	source, err := h.service.CreateSource(c.Request.Context(), CreateSourceInput{ProjectID: projectID, Provider: request.Provider, Name: request.Name, Region: request.Region, Credential: request.Credential, Config: request.Config, SyncIntervalMinutes: request.SyncIntervalMinutes})
	if err != nil {
		writeSourceMutationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, source)
}

// ListSources 返回当前项目的接入源，可按平台过滤。
func (h *HTTPHandler) ListSources(c *gin.Context) {
	projectID, ok := projectID(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "SOURCE_INVALID_REQUEST", "message": "请求格式错误"})
		return
	}
	values, err := h.service.ListSources(c.Request.Context(), projectID, c.Query("provider"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "SOURCE_INVALID_INPUT", "message": "接入源查询参数无效"})
		return
	}
	c.JSON(http.StatusOK, values)
}

// writeSourceReadError 保持不存在与跨项目防枚举语义，同时把内部读取故障收敛为安全 500。
func writeSourceReadError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "SOURCE_NOT_FOUND", "message": "接入源不存在"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"code": "SOURCE_SERVICE_UNAVAILABLE", "message": "接入源服务暂不可用"})
}

// SyncSource 手工同步属于当前项目的接入源。
func (h *HTTPHandler) SyncSource(c *gin.Context) {
	projectID, ok := projectID(c)
	sourceID, err := strconv.ParseUint(c.Param("sourceId"), 10, 64)
	if !ok || err != nil || sourceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "SOURCE_INVALID_REQUEST", "message": "请求格式错误"})
		return
	}
	source, err := h.service.FindSourceForProject(c.Request.Context(), projectID, sourceID)
	if err != nil {
		writeSourceReadError(c, err)
		return
	}
	collector := h.service.adapters[source.Provider]
	if collector == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SOURCE_COLLECTOR_UNAVAILABLE", "message": "平台采集器暂不可用"})
		return
	}
	job, err := h.service.EnqueueSync(c.Request.Context(), source.ID, "manual", collector)
	if errors.Is(err, ErrSyncAlreadyRunning) {
		c.JSON(http.StatusConflict, gin.H{"code": "SOURCE_SYNC_RUNNING", "message": "接入源同步任务正在执行"})
		return
	}
	if err != nil {
		if writeSourceIdentityError(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "SOURCE_SYNC_FAILED", "message": "同步任务执行失败"})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

// TestSourceConnection 验证接入源访问能力，但不会写入资源或触发生命周期变化。
func (h *HTTPHandler) TestSourceConnection(c *gin.Context) {
	projectID, ok := projectID(c)
	sourceID, err := strconv.ParseUint(c.Param("sourceId"), 10, 64)
	if !ok || err != nil || sourceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "SOURCE_INVALID_REQUEST", "message": "请求格式错误"})
		return
	}
	source, err := h.service.FindSourceForProject(c.Request.Context(), projectID, sourceID)
	if err != nil {
		writeSourceReadError(c, err)
		return
	}
	collector := h.service.adapters[source.Provider]
	if collector == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SOURCE_COLLECTOR_UNAVAILABLE", "message": "平台采集器暂不可用"})
		return
	}
	result, err := h.service.TestConnection(c.Request.Context(), projectID, sourceID, collector)
	if err != nil {
		if writeSourceIdentityConflict(c, err) {
			return
		}
		if errors.Is(err, ErrPermissionDenied) {
			c.JSON(http.StatusForbidden, gin.H{"code": "SOURCE_PERMISSION_DENIED", "message": "云账号权限不足，请授予 ECS、RDS 和负载均衡只读权限"})
			return
		}
		if errors.Is(err, ErrAuthenticationFailed) {
			c.JSON(http.StatusBadGateway, gin.H{"code": "SOURCE_AUTHENTICATION_FAILED", "message": "AccessKey 无效或签名校验失败，请检查凭证"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"code": "SOURCE_CONNECTION_FAILED", "message": "连接测试失败，请检查区域和网络"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// RetryJob 为当前项目内的失败任务创建新排队任务，原任务保持不变。
func (h *HTTPHandler) RetryJob(c *gin.Context) {
	projectID, ok := projectID(c)
	jobID, err := strconv.ParseUint(c.Param("jobId"), 10, 64)
	if !ok || err != nil || jobID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "SYNC_JOB_INVALID_REQUEST", "message": "请求格式错误"})
		return
	}
	job, err := h.service.RetryJob(c.Request.Context(), projectID, jobID)
	if errors.Is(err, ErrSyncAlreadyRunning) {
		c.JSON(http.StatusConflict, gin.H{"code": "SOURCE_SYNC_RUNNING", "message": "接入源同步任务正在执行"})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "SYNC_JOB_NOT_FOUND", "message": "同步任务不存在"})
		return
	}
	if err != nil {
		if writeSourceIdentityError(c, err) {
			return
		}
		c.JSON(http.StatusConflict, gin.H{"code": "SYNC_JOB_NOT_RETRYABLE", "message": "同步任务当前不可重试"})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

// ListJobs 按项目与可选接入源分页返回同步任务。
func (h *HTTPHandler) ListJobs(c *gin.Context) {
	projectID, ok := projectID(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "SYNC_JOB_INVALID_REQUEST", "message": "请求格式错误"})
		return
	}
	sourceID, _ := strconv.ParseUint(c.Query("source_id"), 10, 64)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	values, total, err := h.service.ListJobs(c.Request.Context(), projectID, sourceID, c.Query("provider"), page, pageSize)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "SOURCE_NOT_FOUND", "message": "接入源不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "SYNC_JOB_SERVICE_UNAVAILABLE", "message": "同步任务服务暂不可用"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": values, "total": total, "page": page, "page_size": pageSize})
}

// ListResources 按项目与筛选条件分页返回三类资产的统一视图。
func (h *HTTPHandler) ListResources(c *gin.Context) {
	projectID, ok := projectID(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "RESOURCE_INVALID_REQUEST", "message": "请求格式错误"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	values, total, err := h.service.ListResources(c.Request.Context(), projectID, c.Query("provider"), c.Query("resource_type"), c.Query("asset_status"), page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "RESOURCE_SERVICE_UNAVAILABLE", "message": "资源服务暂不可用"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": values, "total": total, "page": page, "page_size": pageSize})
}

// projectID 严格解析项目路径标识，拒绝零值和非数字。
func projectID(c *gin.Context) (uint64, bool) {
	value, err := strconv.ParseUint(c.Param("id"), 10, 64)
	return value, err == nil && value > 0
}
