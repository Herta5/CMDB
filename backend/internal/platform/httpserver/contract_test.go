// 本文件将真实 HTTP 回归响应与公开契约交叉验证，避免生成代码和接口实现各自正确却互不一致。
package httpserver_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"cmdb/internal/api/generated"
	"cmdb/internal/platform/httpserver"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/gin-gonic/gin"
)

var contractOnce sync.Once
var contractRouter routers.Router
var contractDocument *openapi3.T
var contractLoadError error

// loadHTTPContract 复用只读契约；不把加载器或校验器的原始错误写入测试输出。
func loadHTTPContract() (*openapi3.T, routers.Router, error) {
	contractOnce.Do(func() {
		contractDocument, contractLoadError = generated.GetSwagger()
		if contractLoadError != nil {
			return
		}
		contractLoadError = contractDocument.Validate(context.Background())
		if contractLoadError != nil {
			return
		}
		contractRouter, contractLoadError = legacy.NewRouter(contractDocument)
	})
	return contractDocument, contractRouter, contractLoadError
}

func responseContractError(request *http.Request, response *httptest.ResponseRecorder) error {
	_, router, err := loadHTTPContract()
	if err != nil {
		return err
	}
	route, params, err := router.FindRoute(request)
	if err != nil {
		return err
	}
	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: request, PathParams: params, Route: route},
		Status:                 response.Code, Header: response.Header(),
		Options: &openapi3filter.Options{IncludeResponseStatus: true},
	}
	input.SetBodyBytes(response.Body.Bytes())
	return openapi3filter.ValidateResponse(context.Background(), input)
}

// assertContractResponse 只报告契约位置，校验失败也不得打印含令牌或凭证的实际正文。
func assertContractResponse(t *testing.T, request *http.Request, response *httptest.ResponseRecorder) {
	t.Helper()
	if err := responseContractError(request, response); err != nil {
		var schemaError *openapi3.SchemaError
		if errors.As(err, &schemaError) {
			t.Fatalf("HTTP 响应不符合契约：%s %s，状态 %d，字段 %s", request.Method, request.URL.Path, response.Code, strings.Join(schemaError.JSONPointer(), "/"))
		}
		t.Fatalf("HTTP 响应或状态未满足契约：%s %s，状态 %d", request.Method, request.URL.Path, response.Code)
	}
}

// TestOpenAPIRouteCoverage 确保新增、移除和改名操作不能绕过公开契约。
func TestOpenAPIRouteCoverage(t *testing.T) {
	document, _, err := loadHTTPContract()
	if err != nil {
		t.Fatal("公开接口契约必须能加载并通过校验")
	}
	server := httpserver.New(httpserver.Dependencies{})
	expected := map[string]bool{}
	operationIDs := map[string]bool{}
	for path, item := range document.Paths.Map() {
		for method, operation := range item.Operations() {
			if operation.OperationID == "" || operationIDs[operation.OperationID] {
				t.Fatal("操作标识必须非空且唯一")
			}
			operationIDs[operation.OperationID] = true
			expected[method+" "+path] = false
		}
	}
	for _, route := range server.Routes() {
		path := route.Path
		for _, part := range strings.Split(path, "/") {
			if strings.HasPrefix(part, ":") {
				path = strings.ReplaceAll(path, part, "{"+part[1:]+"}")
			}
		}
		key := route.Method + " " + path
		if _, exists := expected[key]; !exists {
			t.Fatalf("实际路由缺少契约：%s", key)
		}
		expected[key] = true
	}
	for key, seen := range expected {
		if !seen {
			t.Errorf("契约操作未注册：%s", key)
		}
	}
}

// TestContractRejectsResponseDrift 用真实健康响应证明字段、状态和额外字段漂移能被门禁发现。
func TestContractRejectsResponseDrift(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httpserver.New(httpserver.Dependencies{})
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	assertContractResponse(t, request, response)
	for _, scenario := range []struct {
		name, body string
		status     int
	}{
		{"字段类型变化", `{"status":123}`, 200},
		{"必需字段缺失", `{}`, 200},
		{"意外公开字段", `{"status":"ok","password":"虚构禁止字段"}`, 200},
		{"未声明成功状态", `{"status":"ok"}`, 201},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			changed := httptest.NewRecorder()
			changed.Header().Set("Content-Type", "application/json")
			changed.Header().Set("X-Request-ID", response.Header().Get("X-Request-ID"))
			changed.WriteHeader(scenario.status)
			_, _ = changed.WriteString(scenario.body)
			if responseContractError(request, changed) == nil {
				t.Fatal("响应漂移必须导致契约验证失败")
			}
		})
	}
}

// TestAllContractOperationsRequireAuthentication 用畸形正文验证每个受保护操作先认证，新增操作默认不能成为匿名入口。
func TestAllContractOperationsRequireAuthentication(t *testing.T) {
	document, _, err := loadHTTPContract()
	if err != nil {
		t.Fatal("公开接口契约必须能加载")
	}
	server := httpserver.New(httpserver.Dependencies{})
	for path, item := range document.Paths.Map() {
		for method, operation := range item.Operations() {
			if (method == http.MethodGet && path == "/health") || (method == http.MethodPost && path == "/api/v1/auth/login") {
				continue
			}
			t.Run(operation.OperationID, func(t *testing.T) {
				concretePath := path
				for _, name := range []string{"id", "sourceId", "jobId"} {
					concretePath = strings.ReplaceAll(concretePath, "{"+name+"}", "1")
				}
				concretePath = strings.ReplaceAll(concretePath, "{username}", "contract_user")
				request := httptest.NewRequest(method, concretePath, strings.NewReader("{"))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				server.ServeHTTP(response, request)
				if response.Code != http.StatusUnauthorized {
					t.Fatalf("未认证操作必须先拒绝身份，实际状态 %d", response.Code)
				}
				assertContractResponse(t, request, response)
				if response.Header().Get("X-Request-ID") == "" {
					t.Fatal("认证失败也必须有可信请求关联")
				}
			})
		}
	}
}
