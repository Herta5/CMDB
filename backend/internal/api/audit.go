// 本文件将生成查询参数连接到统一审计筛选，路径项目不能被查询正文覆盖。
package api

import (
	"cmdb/internal/api/generated"
	"cmdb/internal/audit"
	"context"
	"errors"
)

func auditFilter(p generated.ListGlobalAuditLogsParams, projectID *uint64) audit.Filter {
	return audit.Filter{ProjectID: projectID, Page: defaultValue(p.Page, 1), PageSize: defaultValue(p.PageSize, 20), Action: value(p.Action), ActorUsername: value(p.ActorUsername), ResourceType: value(p.ResourceType), ResourceID: value(p.ResourceId), SnapshotID: value(p.SnapshotId), StartAt: p.StartAt, EndAt: p.EndAt}
}

// ListGlobalAuditLogs 查询系统管理员授权范围内的全部审计。
func (h *Handler) ListGlobalAuditLogs(ctx context.Context, r generated.ListGlobalAuditLogsRequestObject) (generated.ListGlobalAuditLogsResponseObject, error) {
	page, err := h.audit.List(requestContext(ctx), auditFilter(r.Params, nil))
	if err != nil {
		return auditError(err), nil
	}
	return generated.ListGlobalAuditLogs200JSONResponse{Body: toAuditPage(page), Headers: generated.ListGlobalAuditLogs200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// ListProjectAuditLogs 只查询路径项目内的审计记录。
func (h *Handler) ListProjectAuditLogs(ctx context.Context, r generated.ListProjectAuditLogsRequestObject) (generated.ListProjectAuditLogsResponseObject, error) {
	page, err := h.audit.List(requestContext(ctx), auditFilter(generated.ListGlobalAuditLogsParams(r.Params), &r.Id))
	if err != nil {
		return auditError(err), nil
	}
	return generated.ListProjectAuditLogs200JSONResponse{Body: toAuditPage(page), Headers: generated.ListProjectAuditLogs200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}
func auditError(err error) *errorResponse {
	if errors.Is(err, audit.ErrInvalidFilter) {
		return failure(400, "AUDIT_INVALID_REQUEST", "审计查询参数无效")
	}
	return failure(500, "AUDIT_SERVICE_UNAVAILABLE", "审计服务暂不可用")
}
