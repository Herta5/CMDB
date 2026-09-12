// 本文件验证接入源 HTTP 错误边界，确保领域错误不会向公开接口泄露内部原因。
package resource

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestWriteVerifySourceIdentityError 验证专门身份确认的所有领域失败均有稳定的公开分类。
func TestWriteVerifySourceIdentityError(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "字段错误", err: ErrInvalidProviderCredential, status: http.StatusBadRequest, code: "SOURCE_INVALID_INPUT"},
		{name: "账号冲突", err: ErrCloudAccountConflict, status: http.StatusConflict, code: "CLOUD_ACCOUNT_CONFLICT"},
		{name: "身份不一致", err: ErrSourceIdentityMismatch, status: http.StatusConflict, code: "SOURCE_IDENTITY_MISMATCH"},
		{name: "待验证", err: ErrSourceIdentityPending, status: http.StatusConflict, code: "SOURCE_IDENTITY_PENDING"},
		{name: "项目停用", err: ErrProjectDisabled, status: http.StatusConflict, code: "PROJECT_DISABLED"},
		{name: "云认证失败", err: ErrCloudAuthentication, status: http.StatusBadGateway, code: "CLOUD_IDENTITY_UNAVAILABLE"},
		{name: "云权限失败", err: ErrCloudPermission, status: http.StatusBadGateway, code: "CLOUD_IDENTITY_UNAVAILABLE"},
		{name: "云网络失败", err: ErrCloudNetwork, status: http.StatusBadGateway, code: "CLOUD_IDENTITY_UNAVAILABLE"},
		{name: "内部失败", err: errors.New("上游内部错误正文"), status: http.StatusInternalServerError, code: "SOURCE_SERVICE_UNAVAILABLE"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			writeVerifySourceIdentityError(context, scenario.err)
			if response.Code != scenario.status {
				t.Fatal("身份验证错误状态码不符合公开契约")
			}
			if body := response.Body.String(); !strings.Contains(body, `"code":"`+scenario.code+`"`) || strings.Contains(body, "上游内部错误正文") {
				t.Fatal("身份验证错误必须使用稳定分类且不得泄露内部正文")
			}
		})
	}
}
