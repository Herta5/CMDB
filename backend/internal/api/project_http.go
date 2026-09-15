// 本文件将生成的公开请求与领域服务显式连接，错误只返回稳定中文摘要。
package api

import (
	"cmdb/internal/api/generated"
	projectdomain "cmdb/internal/project"
	"cmdb/internal/resource"
	"context"
	"errors"
	"net/http"
)

// CreateProject 保持既有业务错误与公开身份边界。
func (h *Handler) CreateProject(ctx context.Context, r generated.CreateProjectRequestObject) (generated.CreateProjectResponseObject, error) {
	request := r.Body
	project, err := h.project.Create(requestContext(ctx), projectdomain.CreateInput{
		Code:          request.Code,
		Name:          request.Name,
		Description:   request.Description,
		OwnerUsername: request.OwnerUsername,
	})
	if errors.Is(err, projectdomain.ErrDuplicateCode) {
		return failure(http.StatusConflict, "PROJECT_DUPLICATE_CODE", "项目编码已存在"), nil
	}
	if errors.Is(err, projectdomain.ErrProjectOwnerNotFound) {
		return failure(http.StatusBadRequest, "PROJECT_OWNER_NOT_FOUND", "负责人用户不存在"), nil
	}
	if errors.Is(err, projectdomain.ErrInvalidProjectInput) {
		return failure(http.StatusBadRequest, "PROJECT_INVALID_INPUT", "项目参数无效"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用"), nil
	}
	return generated.CreateProject201JSONResponse{Body: toProject(project), Headers: generated.CreateProject201ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// UpdateProject 保持既有业务错误与公开身份边界。
func (h *Handler) UpdateProject(ctx context.Context, r generated.UpdateProjectRequestObject) (generated.UpdateProjectResponseObject, error) {
	projectID := r.Id
	request := r.Body
	project, err := h.project.Update(requestContext(ctx), projectID, projectdomain.UpdateInput{
		Name:          request.Name,
		Description:   request.Description,
		Status:        string(request.Status),
		OwnerUsername: request.OwnerUsername,
	})
	if errors.Is(err, projectdomain.ErrProjectNotFound) {
		return failure(http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在"), nil
	}
	if errors.Is(err, projectdomain.ErrInvalidProjectInput) || errors.Is(err, projectdomain.ErrInvalidProjectStatus) {
		return failure(http.StatusBadRequest, "PROJECT_INVALID_INPUT", "项目参数无效"), nil
	}
	if errors.Is(err, projectdomain.ErrProjectOwnerNotFound) {
		return failure(http.StatusBadRequest, "PROJECT_OWNER_NOT_FOUND", "负责人用户不存在"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用"), nil
	}
	return generated.UpdateProject200JSONResponse{Body: toProject(project), Headers: generated.UpdateProject200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// ListProjects 保持既有业务错误与公开身份边界。
func (h *Handler) ListProjects(ctx context.Context, r generated.ListProjectsRequestObject) (generated.ListProjectsResponseObject, error) {
	projects, err := h.project.ListForUser(requestContext(ctx), currentClaims(ctx).InternalUserID, currentClaims(ctx).GlobalRole)
	if err != nil {
		return failure(http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用"), nil
	}
	response := make([]generated.Project, 0, len(projects))
	for i := range projects {
		response = append(response, toProject(&projects[i]))
	}
	return generated.ListProjects200JSONResponse{Body: response, Headers: generated.ListProjects200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// GetProject 保持既有业务错误与公开身份边界。
func (h *Handler) GetProject(ctx context.Context, r generated.GetProjectRequestObject) (generated.GetProjectResponseObject, error) {
	projectID := r.Id
	project, err := h.project.Get(requestContext(ctx), projectID)
	if errors.Is(err, projectdomain.ErrProjectNotFound) {
		return failure(http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在"), nil
	}
	if errors.Is(err, projectdomain.ErrInvalidProjectInput) {
		return failure(http.StatusBadRequest, "PROJECT_INVALID_REQUEST", "请求格式错误"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用"), nil
	}
	return generated.GetProject200JSONResponse{Body: toProject(project), Headers: generated.GetProject200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// DeleteProject 保持既有业务错误与公开身份边界。
func (h *Handler) DeleteProject(ctx context.Context, r generated.DeleteProjectRequestObject) (generated.DeleteProjectResponseObject, error) {
	projectID := r.Id
	if err := h.project.Delete(requestContext(ctx), projectID); errors.Is(err, projectdomain.ErrProjectNotFound) {
		return failure(http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在"), nil
	} else if errors.Is(err, resource.ErrDeleteDependencyConflict) {
		return failure(http.StatusConflict, "PROJECT_DELETE_CONFLICT", "项目仍有资产或运行中的同步任务，暂不能删除"), nil
	} else if err != nil {
		return failure(http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用"), nil
	}
	return generated.DeleteProject204Response{Headers: generated.DeleteProject204ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}
