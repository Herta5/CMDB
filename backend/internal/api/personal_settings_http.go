// 本文件适配当前身份的个人设置契约，目标只来自认证中间件。
package api

import (
	"context"
	"errors"

	"cmdb/internal/api/generated"
	"cmdb/internal/identity"
)

// UpdateMyProfile 更新当前用户的显示名称，响应仍遵守公开身份字段边界。
func (h *Handler) UpdateMyProfile(ctx context.Context, r generated.UpdateMyProfileRequestObject) (generated.UpdateMyProfileResponseObject, error) {
	user, err := h.identity.UpdateMyProfile(requestContext(ctx), currentClaims(ctx), r.Body.DisplayName)
	if err != nil {
		return personalSettingsError(err), nil
	}
	return generated.UpdateMyProfile200JSONResponse{Body: toUser(user), Headers: generated.UpdateMyProfile200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// ChangeMyPassword 密码错误属于可纠正输入，只有身份本身失效才返回 401。
func (h *Handler) ChangeMyPassword(ctx context.Context, r generated.ChangeMyPasswordRequestObject) (generated.ChangeMyPasswordResponseObject, error) {
	err := h.identity.ChangeMyPassword(requestContext(ctx), currentClaims(ctx), value(r.Body.CurrentPassword), value(r.Body.NewPassword))
	if err != nil {
		return personalSettingsError(err), nil
	}
	return generated.ChangeMyPassword204Response{Headers: generated.ChangeMyPassword204ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}

// personalSettingsError 不透传存储、哈希或审计错误，不回显请求中的密码。
func personalSettingsError(err error) *errorResponse {
	switch {
	case errors.Is(err, identity.ErrAuthenticatedUserNotFound), errors.Is(err, identity.ErrInvalidSession):
		return failure(401, "AUTH_UNAUTHORIZED", "身份认证已失效")
	case errors.Is(err, identity.ErrCurrentPasswordInvalid):
		return failure(400, "PERSONAL_CURRENT_PASSWORD_INVALID", "当前密码不正确")
	case errors.Is(err, identity.ErrInvalidUserInput):
		return failure(400, "USER_INVALID_INPUT", "个人设置参数无效")
	default:
		return failure(500, "USER_SERVICE_UNAVAILABLE", "个人设置服务暂不可用")
	}
}
