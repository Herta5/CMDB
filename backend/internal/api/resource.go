// 本文件实现接入源、资源与任务的严格 HTTP 适配，不序列化领域模型。
package api

import (
	"cmdb/internal/api/generated"
	"cmdb/internal/resource"
	"context"
	"errors"
	"gorm.io/gorm"
	"net/http"
	"strings"
)

// UpdateSource 在已授权项目内调用资源核心并转换公开结果。
func (h *Handler) UpdateSource(ctx context.Context, r generated.UpdateSourceRequestObject) (generated.UpdateSourceResponseObject, error) {
	projectID, sourceID := r.Id, r.SourceId
	request := r.Body
	source, err := h.resource.UpdateSource(requestContext(ctx), projectID, sourceID, resource.UpdateSourceInput{Name: request.Name, Region: request.Region, Credential: request.Credential, Config: request.Config, Enabled: request.Enabled, SyncIntervalMinutes: request.SyncIntervalMinutes})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return failure(http.StatusNotFound, "SOURCE_NOT_FOUND", "接入源不存在"), nil
	}
	if err != nil {
		return sourceMutationError(err), nil
	}
	return generated.UpdateSource200JSONResponse{Body: toSource(source), Headers: generated.UpdateSource200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// DeleteSource 在已授权项目内调用资源核心并转换公开结果。
func (h *Handler) DeleteSource(ctx context.Context, r generated.DeleteSourceRequestObject) (generated.DeleteSourceResponseObject, error) {
	projectID, sourceID := r.Id, r.SourceId
	if err := h.resource.DeleteSource(requestContext(ctx), projectID, sourceID); errors.Is(err, gorm.ErrRecordNotFound) {
		return failure(http.StatusNotFound, "SOURCE_NOT_FOUND", "接入源不存在"), nil
	} else if errors.Is(err, resource.ErrDeleteDependencyConflict) {
		return failure(http.StatusConflict, "SOURCE_DELETE_CONFLICT", "接入源仍有资产或运行中的同步任务，暂不能删除"), nil
	} else if err != nil {
		if response := sourceIdentityError(err); response != nil {
			return response, nil
		}
		return failure(http.StatusInternalServerError, "SOURCE_DELETE_FAILED", "删除接入源失败"), nil
	}
	return generated.DeleteSource204Response{Headers: generated.DeleteSource204ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// CreateSource 在已授权项目内调用资源核心并转换公开结果。
func (h *Handler) CreateSource(ctx context.Context, r generated.CreateSourceRequestObject) (generated.CreateSourceResponseObject, error) {
	projectID := r.Id
	request := r.Body
	source, err := h.resource.CreateSource(requestContext(ctx), resource.CreateSourceInput{ProjectID: projectID, Provider: string(request.Provider), Name: request.Name, Region: request.Region, Credential: request.Credential, Config: request.Config, SyncIntervalMinutes: request.SyncIntervalMinutes})
	if err != nil {
		return sourceMutationError(err), nil
	}
	return generated.CreateSource201JSONResponse{Body: toSource(source), Headers: generated.CreateSource201ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// ListSources 在已授权项目内调用资源核心并转换公开结果。
func (h *Handler) ListSources(ctx context.Context, r generated.ListSourcesRequestObject) (generated.ListSourcesResponseObject, error) {
	projectID := r.Id
	values, err := h.resource.ListSources(requestContext(ctx), projectID, value(r.Params.Provider))
	if err != nil {
		return failure(http.StatusBadRequest, "SOURCE_INVALID_INPUT", "接入源查询参数无效"), nil
	}
	items := make([]generated.Source, 0, len(values))
	for i := range values {
		items = append(items, toSource(&values[i]))
	}
	return generated.ListSources200JSONResponse{Body: items, Headers: generated.ListSources200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// SyncSource 在已授权项目内调用资源核心并转换公开结果。
func (h *Handler) SyncSource(ctx context.Context, r generated.SyncSourceRequestObject) (generated.SyncSourceResponseObject, error) {
	projectID, sourceID := r.Id, r.SourceId
	job, err := h.resource.SyncSource(requestContext(ctx), projectID, sourceID)
	if errors.Is(err, resource.ErrSourceReadFailed) {
		return sourceReadError(err), nil
	}
	if errors.Is(err, resource.ErrCollectorUnavailable) {
		return failure(503, "SOURCE_COLLECTOR_UNAVAILABLE", "平台采集器暂不可用"), nil
	}
	if errors.Is(err, resource.ErrSyncAlreadyRunning) {
		return failure(http.StatusConflict, "SOURCE_SYNC_RUNNING", "接入源同步任务正在执行"), nil
	}
	if err != nil {
		if response := sourceIdentityError(err); response != nil {
			return response, nil
		}
		return failure(http.StatusInternalServerError, "SOURCE_SYNC_FAILED", "同步任务执行失败"), nil
	}
	return generated.SyncSource202JSONResponse{Body: toJob(job), Headers: generated.SyncSource202ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// TestSourceConnection 在已授权项目内调用资源核心并转换公开结果。
func (h *Handler) TestSourceConnection(ctx context.Context, r generated.TestSourceConnectionRequestObject) (generated.TestSourceConnectionResponseObject, error) {
	projectID, sourceID := r.Id, r.SourceId
	result, err := h.resource.ProbeSource(requestContext(ctx), projectID, sourceID)
	if errors.Is(err, resource.ErrSourceReadFailed) {
		return sourceReadError(err), nil
	}
	if errors.Is(err, resource.ErrCollectorUnavailable) {
		return failure(503, "SOURCE_COLLECTOR_UNAVAILABLE", "平台采集器暂不可用"), nil
	}
	if err != nil {
		if response := sourceIdentityConflict(err); response != nil {
			return response, nil
		}
		if errors.Is(err, resource.ErrPermissionDenied) {
			return failure(http.StatusForbidden, "SOURCE_PERMISSION_DENIED", "云账号权限不足，请授予 ECS、RDS 和负载均衡只读权限"), nil
		}
		if errors.Is(err, resource.ErrAuthenticationFailed) {
			return failure(http.StatusBadGateway, "SOURCE_AUTHENTICATION_FAILED", "AccessKey 无效或签名校验失败，请检查凭证"), nil
		}
		return failure(http.StatusBadGateway, "SOURCE_CONNECTION_FAILED", "连接测试失败，请检查区域和网络"), nil
	}
	reachable := resourceTypes(result.ReachableTypes)
	failed := resourceTypes(result.FailedTypes)
	return generated.TestSourceConnection200JSONResponse{Body: generated.ConnectionTestResult{ReachableTypes: &reachable, FailedTypes: &failed}, Headers: generated.TestSourceConnection200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// RetrySyncJob 在已授权项目内调用资源核心并转换公开结果。
func (h *Handler) RetrySyncJob(ctx context.Context, r generated.RetrySyncJobRequestObject) (generated.RetrySyncJobResponseObject, error) {
	projectID, jobID := r.Id, r.JobId
	job, err := h.resource.RetryJob(requestContext(ctx), projectID, jobID)
	if errors.Is(err, resource.ErrSyncAlreadyRunning) {
		return failure(http.StatusConflict, "SOURCE_SYNC_RUNNING", "接入源同步任务正在执行"), nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return failure(http.StatusNotFound, "SYNC_JOB_NOT_FOUND", "同步任务不存在"), nil
	}
	if err != nil {
		if response := sourceIdentityError(err); response != nil {
			return response, nil
		}
		return failure(http.StatusConflict, "SYNC_JOB_NOT_RETRYABLE", "同步任务当前不可重试"), nil
	}
	return generated.RetrySyncJob202JSONResponse{Body: toJob(job), Headers: generated.RetrySyncJob202ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// ListSyncJobs 在已授权项目内调用资源核心并转换公开结果。
func (h *Handler) ListSyncJobs(ctx context.Context, r generated.ListSyncJobsRequestObject) (generated.ListSyncJobsResponseObject, error) {
	projectID := r.Id
	sourceID := value(r.Params.SourceId)
	page := defaultValue(r.Params.Page, 1)
	pageSize := defaultValue(r.Params.PageSize, 20)
	values, total, err := h.resource.ListJobs(requestContext(ctx), projectID, sourceID, value(r.Params.Provider), page, pageSize)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return failure(http.StatusNotFound, "SOURCE_NOT_FOUND", "接入源不存在"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "SYNC_JOB_SERVICE_UNAVAILABLE", "同步任务服务暂不可用"), nil
	}
	var items *[]generated.SyncJob
	if values != nil {
		mapped := make([]generated.SyncJob, 0, len(values))
		for i := range values {
			mapped = append(mapped, toJob(&values[i]))
		}
		items = &mapped
	}
	return generated.ListSyncJobs200JSONResponse{Body: generated.SyncJobPage{Items: items, Total: total, Page: page, PageSize: pageSize}, Headers: generated.ListSyncJobs200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// ListProjectResources 在已授权项目内调用资源核心并转换公开结果。
func (h *Handler) ListProjectResources(ctx context.Context, r generated.ListProjectResourcesRequestObject) (generated.ListProjectResourcesResponseObject, error) {
	projectID := r.Id
	query := resourceQuery(generated.ListAllResourcesParams(r.Params))
	values, total, err := h.resource.ListResources(requestContext(ctx), projectID, query)
	if errors.Is(err, resource.ErrInvalidResourceQuery) {
		return failure(http.StatusBadRequest, "RESOURCE_INVALID_QUERY", "资源查询参数无效"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "RESOURCE_SERVICE_UNAVAILABLE", "资源服务暂不可用"), nil
	}
	items := make([]generated.Resource, 0, len(values))
	for _, v := range values {
		items = append(items, toResource(v))
	}
	return generated.ListProjectResources200JSONResponse{Body: generated.ResourcePage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, Headers: generated.ListProjectResources200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// ListAllResources 在已授权项目内调用资源核心并转换公开结果。
func (h *Handler) ListAllResources(ctx context.Context, r generated.ListAllResourcesRequestObject) (generated.ListAllResourcesResponseObject, error) {
	query := resourceQuery(generated.ListAllResourcesParams(r.Params))
	values, total, err := h.resource.ListAllResources(requestContext(ctx), query)
	if errors.Is(err, resource.ErrInvalidResourceQuery) {
		return failure(http.StatusBadRequest, "RESOURCE_INVALID_QUERY", "资源查询参数无效"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "RESOURCE_SERVICE_UNAVAILABLE", "资源服务暂不可用"), nil
	}
	items := make([]generated.Resource, 0, len(values))
	for _, v := range values {
		items = append(items, toResource(v))
	}
	return generated.ListAllResources200JSONResponse{Body: generated.ResourcePage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, Headers: generated.ListAllResources200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

func resourceTypes(values []string) []generated.ResourceType {
	result := make([]generated.ResourceType, 0, len(values))
	for _, v := range values {
		result = append(result, generated.ResourceType(v))
	}
	return result
}
func resourceQuery(p generated.ListAllResourcesParams) resource.ResourceListQuery {
	page, pageSize := defaultValue(p.Page, 1), defaultValue(p.PageSize, 20)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	return resource.ResourceListQuery{Providers: splitQueryValues(value(p.Provider)), ResourceTypes: splitQueryValues(value(p.ResourceType)), SourceID: value(p.SourceId), Keyword: value(p.Keyword), Engine: value(p.Engine), NetworkType: value(p.NetworkType), Region: value(p.Region), CloudStatus: value(p.CloudStatus), AssetStatus: value(p.AssetStatus), SortBy: value(p.SortBy), SortOrder: value(p.SortOrder), Page: page, PageSize: pageSize}
}
func splitQueryValues(v string) []string {
	r := make([]string, 0)
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			r = append(r, strings.ToLower(s))
		}
	}
	return r
}
