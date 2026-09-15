// 本文件验证生成器类型绑定失败仍返回安全契约错误，不能产生空正文或输入日志。
package httpserver_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestGeneratedBindingOverflowReturnsSafeError 覆盖 Schema 数字可表达但 Go uint64 无法绑定的边界。
func TestGeneratedBindingOverflowReturnsSafeError(t *testing.T) {
	server, password := integrationServer(t)
	token := loginUser(t, server, "operator", password)
	response := integrationRawRequest(t, server, token, http.MethodPost, "/api/v1/users", `{"username":"overflow","display_name":"溢出测试","password":"fictional-password","global_role":"user","status":"active","project_permissions":[{"project_id":18446744073709551616,"role":"member"}]}`, 400)
	if !strings.Contains(response.Body.String(), `"code":"USER_INVALID_REQUEST"`) {
		t.Fatal("绑定溢出必须返回安全参数错误")
	}
}

// TestGeneratedPathErrorsKeepDomainCodes 验证生成参数校验仍区分项目路径与查询错误。
func TestGeneratedPathErrorsKeepDomainCodes(t *testing.T) {
	server, password := integrationServer(t)
	token := loginUser(t, server, "operator", password)
	for _, tc := range []struct{ name, method, path, body, code string }{{"资源项目标识无效", http.MethodGet, "/api/v1/projects/0/resources", "", "RESOURCE_INVALID_REQUEST"}, {"成员用户名无效", http.MethodPut, "/api/v1/projects/1/members/invalid-name", `{"role":"member"}`, "PROJECT_MEMBER_INVALID_INPUT"}} {
		t.Run(tc.name, func(t *testing.T) {
			response := integrationRawRequest(t, server, token, tc.method, tc.path, tc.body, 400)
			if !strings.Contains(response.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatal("路径错误必须保留领域错误码")
			}
		})
	}
}
