// 本文件将生成的公开请求与领域服务显式连接，错误只返回稳定中文摘要。
package api

import (
	"cmdb/internal/api/generated"
	"cmdb/internal/identity"
	"context"
	"errors"
	"net/http"
)

// Login 保持既有业务错误与公开身份边界。
func (h *Handler) Login(ctx context.Context, r generated.LoginRequestObject) (generated.LoginResponseObject, error) {
	if r.Body.Username == "" || value(r.Body.Password) == "" {
		return failure(400, "AUTH_INVALID_REQUEST", "请求格式错误"), nil
	}
	request := r.Body

	user, token, err := h.identity.Login(requestContext(ctx), request.Username, value(request.Password))
	if errors.Is(err, identity.ErrInvalidCredentials) {
		return failure(http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "用户名或密码错误"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "AUTH_SERVICE_UNAVAILABLE", "认证服务暂不可用"), nil
	}
	return generated.Login200JSONResponse{Body: generated.LoginResponse{Token: token, User: toUser(user)}, Headers: generated.Login200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// GetMe 保持既有业务错误与公开身份边界。
func (h *Handler) GetMe(ctx context.Context, r generated.GetMeRequestObject) (generated.GetMeResponseObject, error) {
	user, err := h.identity.CurrentUser(requestContext(ctx), currentClaims(ctx))
	if errors.Is(err, identity.ErrAuthenticatedUserNotFound) {
		return failure(http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "身份认证已失效"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "AUTH_SERVICE_UNAVAILABLE", "认证服务暂不可用"), nil
	}
	return generated.GetMe200JSONResponse{Body: toUser(user), Headers: generated.GetMe200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// ListUsers 保持既有业务错误与公开身份边界。
func (h *Handler) ListUsers(ctx context.Context, r generated.ListUsersRequestObject) (generated.ListUsersResponseObject, error) {
	users, err := h.identity.ListUsers(requestContext(ctx))
	if err != nil {
		return failure(http.StatusInternalServerError, "USER_SERVICE_UNAVAILABLE", "用户服务暂不可用"), nil
	}
	response := make([]generated.PublicUser, 0, len(users))
	for index := range users {
		response = append(response, toUser(&users[index]))
	}
	return generated.ListUsers200JSONResponse{Body: response, Headers: generated.ListUsers200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// CreateUser 保持既有业务错误与公开身份边界。
func (h *Handler) CreateUser(ctx context.Context, r generated.CreateUserRequestObject) (generated.CreateUserResponseObject, error) {
	request := r.Body
	user, err := h.identity.CreateUser(requestContext(ctx), identity.CreateUserInput{Username: request.Username, Password: value(request.Password), DisplayName: request.DisplayName, Email: request.Email, GlobalRole: string(request.GlobalRole), Status: string(request.Status), ProjectPermissions: toPermissions(request.ProjectPermissions)})
	if errors.Is(err, identity.ErrInvalidUserInput) {
		return failure(http.StatusBadRequest, "USER_INVALID_INPUT", "用户参数无效"), nil
	}
	if errors.Is(err, identity.ErrDuplicateUsername) {
		return failure(http.StatusConflict, "USER_DUPLICATE_USERNAME", "用户名已存在"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "USER_SERVICE_UNAVAILABLE", "用户服务暂不可用"), nil
	}
	return generated.CreateUser201JSONResponse{Body: toUser(user), Headers: generated.CreateUser201ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// UpdateUser 保持既有业务错误与公开身份边界。
func (h *Handler) UpdateUser(ctx context.Context, r generated.UpdateUserRequestObject) (generated.UpdateUserResponseObject, error) {
	targetUsername := r.Username
	request := r.Body
	if !identity.ValidUsername(targetUsername) {
		return failure(http.StatusBadRequest, "USER_INVALID_REQUEST", "请求格式错误"), nil
	}
	user, err := h.identity.UpdateUser(requestContext(ctx), currentClaims(ctx).Username, targetUsername, identity.UpdateUserInput{DisplayName: request.DisplayName, Email: request.Email, GlobalRole: string(request.GlobalRole), Status: string(request.Status), Password: request.Password, ProjectPermissions: toPermissions(request.ProjectPermissions)})
	if errors.Is(err, identity.ErrInvalidUserInput) {
		return failure(http.StatusBadRequest, "USER_INVALID_INPUT", "用户参数无效"), nil
	}
	if errors.Is(err, identity.ErrSelfProtection) {
		return failure(http.StatusConflict, "USER_SELF_PROTECTED", "不能停用或降级当前管理员"), nil
	}
	if errors.Is(err, identity.ErrUserNotFound) {
		return failure(http.StatusNotFound, "USER_NOT_FOUND", "用户不存在"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "USER_SERVICE_UNAVAILABLE", "用户服务暂不可用"), nil
	}
	return generated.UpdateUser200JSONResponse{Body: toUser(user), Headers: generated.UpdateUser200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// DeleteUser 保持既有业务错误与公开身份边界。
func (h *Handler) DeleteUser(ctx context.Context, r generated.DeleteUserRequestObject) (generated.DeleteUserResponseObject, error) {
	targetUsername := r.Username
	if !identity.ValidUsername(targetUsername) {
		return failure(http.StatusBadRequest, "USER_INVALID_REQUEST", "请求格式错误"), nil
	}
	err := h.identity.DeleteUser(requestContext(ctx), currentClaims(ctx).Username, targetUsername)
	if errors.Is(err, identity.ErrSelfProtection) {
		return failure(http.StatusConflict, "USER_SELF_PROTECTED", "不能删除当前管理员"), nil
	}
	if errors.Is(err, identity.ErrUserNotFound) {
		return failure(http.StatusNotFound, "USER_NOT_FOUND", "用户不存在"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "USER_SERVICE_UNAVAILABLE", "用户服务暂不可用"), nil
	}
	return generated.DeleteUser204Response{Headers: generated.DeleteUser204ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// UpdateUserStatus 保持既有业务错误与公开身份边界。
func (h *Handler) UpdateUserStatus(ctx context.Context, r generated.UpdateUserStatusRequestObject) (generated.UpdateUserStatusResponseObject, error) {
	targetUsername := r.Username
	request := r.Body
	if !identity.ValidUsername(targetUsername) {
		return failure(http.StatusBadRequest, "USER_INVALID_REQUEST", "请求格式错误"), nil
	}
	user, err := h.identity.UpdateUserStatus(requestContext(ctx), currentClaims(ctx).Username, targetUsername, string(request.Status))
	if errors.Is(err, identity.ErrInvalidUserInput) {
		return failure(http.StatusBadRequest, "USER_INVALID_INPUT", "用户参数无效"), nil
	}
	if errors.Is(err, identity.ErrSelfProtection) {
		return failure(http.StatusConflict, "USER_SELF_PROTECTED", "不能停用当前管理员"), nil
	}
	if errors.Is(err, identity.ErrUserNotFound) {
		return failure(http.StatusNotFound, "USER_NOT_FOUND", "用户不存在"), nil
	}
	if err != nil {
		return failure(http.StatusInternalServerError, "USER_SERVICE_UNAVAILABLE", "用户服务暂不可用"), nil
	}
	return generated.UpdateUserStatus200JSONResponse{Body: toUser(user), Headers: generated.UpdateUserStatus200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}
