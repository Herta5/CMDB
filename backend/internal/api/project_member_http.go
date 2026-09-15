// 本文件将生成的公开请求与领域服务显式连接，错误只返回稳定中文摘要。
package api

import (
	"cmdb/internal/api/generated"
	"cmdb/internal/identity"
	"cmdb/internal/project"
	"context"
	"errors"
	"net/http"
)

// ListProjectMembers 保持既有业务错误与公开身份边界。
func (h *Handler) ListProjectMembers(ctx context.Context, r generated.ListProjectMembersRequestObject) (generated.ListProjectMembersResponseObject, error) {
	projectID := r.Id
	members, err := h.project.ListMembers(requestContext(ctx), projectID)
	if errors.Is(err, project.ErrInvalidMemberInput) {
		return failure(http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用"), nil
	}
	response := make([]generated.ProjectMember, 0, len(members))
	for _, member := range members {
		response = append(response, toMember(&member))
	}
	return generated.ListProjectMembers200JSONResponse{Body: response, Headers: generated.ListProjectMembers200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// ListProjectMemberCandidates 保持既有业务错误与公开身份边界。
func (h *Handler) ListProjectMemberCandidates(ctx context.Context, r generated.ListProjectMemberCandidatesRequestObject) (generated.ListProjectMemberCandidatesResponseObject, error) {
	users, err := h.project.ListMemberCandidates(requestContext(ctx))
	if err != nil {
		return failure(http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用"), nil
	}
	response := make([]generated.MemberCandidate, 0, len(users))
	for _, user := range users {
		response = append(response, generated.MemberCandidate{Username: user.Username, DisplayName: user.DisplayName})
	}
	return generated.ListProjectMemberCandidates200JSONResponse{Body: response, Headers: generated.ListProjectMemberCandidates200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// AddProjectMember 保持既有业务错误与公开身份边界。
func (h *Handler) AddProjectMember(ctx context.Context, r generated.AddProjectMemberRequestObject) (generated.AddProjectMemberResponseObject, error) {
	projectID := r.Id
	request := r.Body
	member, err := h.project.AddMember(requestContext(ctx), projectID, request.Username, string(request.Role))
	if errors.Is(err, project.ErrInvalidMemberInput) {
		return failure(http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效"), nil
	}
	if errors.Is(err, project.ErrMemberAlreadyExists) {
		return failure(http.StatusConflict, "PROJECT_MEMBER_EXISTS", "项目成员已存在"), nil
	}
	if errors.Is(err, project.ErrMemberNotFound) {
		return failure(http.StatusNotFound, "PROJECT_MEMBER_NOT_FOUND", "项目成员不存在"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用"), nil
	}
	return generated.AddProjectMember201JSONResponse{Body: toMember(member), Headers: generated.AddProjectMember201ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// UpdateProjectMemberRole 保持既有业务错误与公开身份边界。
func (h *Handler) UpdateProjectMemberRole(ctx context.Context, r generated.UpdateProjectMemberRoleRequestObject) (generated.UpdateProjectMemberRoleResponseObject, error) {
	projectID := r.Id
	username, ok := r.Username, identity.ValidUsername(r.Username)
	if !ok {
		return failure(http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效"), nil
	}
	request := r.Body
	member, err := h.project.UpdateMemberRole(requestContext(ctx), projectID, username, string(request.Role))
	if errors.Is(err, project.ErrInvalidMemberInput) {
		return failure(http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效"), nil
	}
	if errors.Is(err, project.ErrMemberNotFound) {
		return failure(http.StatusNotFound, "PROJECT_MEMBER_NOT_FOUND", "项目成员不存在"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用"), nil
	}
	return generated.UpdateProjectMemberRole200JSONResponse{Body: toMember(member), Headers: generated.UpdateProjectMemberRole200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// RemoveProjectMember 保持既有业务错误与公开身份边界。
func (h *Handler) RemoveProjectMember(ctx context.Context, r generated.RemoveProjectMemberRequestObject) (generated.RemoveProjectMemberResponseObject, error) {
	projectID := r.Id
	username, ok := r.Username, identity.ValidUsername(r.Username)
	if !ok {
		return failure(http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效"), nil
	}
	claims := currentClaims(ctx)
	authenticated := claims.Username != ""
	if !authenticated {
		return failure(http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "身份认证已失效"), nil
	}
	// 项目管理员不能删除自己的成员关系；系统管理员仍可执行全局纠正操作。
	if claims.GlobalRole != identity.GlobalRoleSystemAdmin && claims.Username == username {
		return failure(http.StatusConflict, "PROJECT_MEMBER_SELF_REMOVE", "项目管理员不能移除自己"), nil
	}
	if err := h.project.RemoveMember(requestContext(ctx), projectID, username); errors.Is(err, project.ErrInvalidMemberInput) {
		return failure(http.StatusBadRequest, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效"), nil
	} else if errors.Is(err, project.ErrMemberNotFound) {
		return failure(http.StatusNotFound, "PROJECT_MEMBER_NOT_FOUND", "项目成员不存在"), nil
	} else if err != nil {
		return failure(http.StatusInternalServerError, "PROJECT_SERVICE_UNAVAILABLE", "项目服务暂不可用"), nil
	}
	return generated.RemoveProjectMember204Response{Headers: generated.RemoveProjectMember204ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}
