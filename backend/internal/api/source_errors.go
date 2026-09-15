// 本文件按资源领域错误身份映射稳定状态，禁止检查或回传底层错误字符串。
package api

import (
	"cmdb/internal/resource"
	"errors"
	"gorm.io/gorm"
)

func sourceIdentityConflict(err error) *errorResponse {
	switch {
	case errors.Is(err, resource.ErrCloudAccountConflict):
		return failure(409, "CLOUD_ACCOUNT_CONFLICT", "该云账号已接入 CMDB")
	case errors.Is(err, resource.ErrSourceIdentityMismatch):
		return failure(409, "SOURCE_IDENTITY_MISMATCH", "新凭证所属云账号与原接入源不一致")
	}
	return nil
}
func sourceIdentityError(err error) *errorResponse {
	if response := sourceIdentityConflict(err); response != nil {
		return response
	}
	switch {
	case errors.Is(err, resource.ErrCloudAuthentication):
		return failure(502, "CLOUD_IDENTITY_UNAVAILABLE", "AccessKey 无效或签名校验失败，请检查凭证")
	case errors.Is(err, resource.ErrCloudPermission):
		return failure(502, "CLOUD_IDENTITY_UNAVAILABLE", "云账号身份查询权限不足，请检查云账号授权")
	case errors.Is(err, resource.ErrCloudNetwork):
		return failure(502, "CLOUD_IDENTITY_UNAVAILABLE", "云账号身份服务连接失败，请检查服务端网络")
	}
	return nil
}
func sourceMutationError(err error) *errorResponse {
	if response := sourceIdentityError(err); response != nil {
		return response
	}
	if errors.Is(err, resource.ErrInvalidSourceInput) || errors.Is(err, resource.ErrInvalidProviderCredential) || errors.Is(err, resource.ErrInvalidProviderConfig) {
		return failure(400, "SOURCE_INVALID_INPUT", "接入源参数无效")
	}
	return failure(500, "SOURCE_SERVICE_UNAVAILABLE", "接入源服务暂不可用")
}
func sourceReadError(err error) *errorResponse {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return failure(404, "SOURCE_NOT_FOUND", "接入源不存在")
	}
	return failure(500, "SOURCE_SERVICE_UNAVAILABLE", "接入源服务暂不可用")
}
