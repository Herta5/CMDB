// 本文件统一安全错误输出；各操作共享同一错误结构，避免底层异常进入响应。
package api

import (
	"cmdb/internal/api/generated"
	"encoding/json"
	"net/http"
)

type errorResponse struct {
	status int
	body   generated.Error
}

func failure(status int, code, message string) *errorResponse {
	return &errorResponse{status, generated.Error{Code: code, Message: message}}
}
func (e *errorResponse) write(w http.ResponseWriter) error {
	data, err := json.Marshal(e.body)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.status)
	_, err = w.Write(data)
	return err
}

// VisitListGlobalAuditLogsResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitListGlobalAuditLogsResponse(w http.ResponseWriter) error {
	return e.write(w)
}

// VisitLoginResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitLoginResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitGetMeResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitGetMeResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitListProjectsResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitListProjectsResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitCreateProjectResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitCreateProjectResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitDeleteProjectResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitDeleteProjectResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitGetProjectResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitGetProjectResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitUpdateProjectResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitUpdateProjectResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitListProjectAuditLogsResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitListProjectAuditLogsResponse(w http.ResponseWriter) error {
	return e.write(w)
}

// VisitListProjectMemberCandidatesResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitListProjectMemberCandidatesResponse(w http.ResponseWriter) error {
	return e.write(w)
}

// VisitListProjectMembersResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitListProjectMembersResponse(w http.ResponseWriter) error {
	return e.write(w)
}

// VisitAddProjectMemberResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitAddProjectMemberResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitRemoveProjectMemberResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitRemoveProjectMemberResponse(w http.ResponseWriter) error {
	return e.write(w)
}

// VisitUpdateProjectMemberRoleResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitUpdateProjectMemberRoleResponse(w http.ResponseWriter) error {
	return e.write(w)
}

// VisitListProjectResourcesResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitListProjectResourcesResponse(w http.ResponseWriter) error {
	return e.write(w)
}

// VisitListSourcesResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitListSourcesResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitCreateSourceResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitCreateSourceResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitDeleteSourceResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitDeleteSourceResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitUpdateSourceResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitUpdateSourceResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitSyncSourceResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitSyncSourceResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitTestSourceConnectionResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitTestSourceConnectionResponse(w http.ResponseWriter) error {
	return e.write(w)
}

// VisitListSyncJobsResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitListSyncJobsResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitRetrySyncJobResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitRetrySyncJobResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitListAllResourcesResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitListAllResourcesResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitListUsersResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitListUsersResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitCreateUserResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitCreateUserResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitDeleteUserResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitDeleteUserResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitUpdateUserResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitUpdateUserResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitUpdateUserStatusResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitUpdateUserStatusResponse(w http.ResponseWriter) error { return e.write(w) }

// VisitHealthResponse 输出该操作的统一安全失败。
func (e *errorResponse) VisitHealthResponse(w http.ResponseWriter) error { return e.write(w) }
