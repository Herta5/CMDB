// 本文件验证 HTTP 诊断仅记录安全元数据且 panic 不泄露敏感上下文。
package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cmdb/internal/platform/diagnostics"
	"github.com/gin-gonic/gin"
)

func TestHTTPDiagnosticsSensitiveInputAndPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, method, path, route string
		panic                     bool
		status                    int
	}{
		{"匹配路由仅记录模板", "POST", "/diagnostic/虚构路径秘密?token=虚构查询秘密", "/diagnostic/:value", false, 204},
		{"未知路径不记录原文", "GET", "/虚构路径秘密?token=虚构查询秘密", "unmatched", false, 404},
		{"未知方法归一化", "虚构方法秘密", "/unknown", "unmatched", false, 404},
		{"异常恢复不记录原文和堆栈", "POST", "/diagnostic/虚构路径秘密", "/diagnostic/:value", true, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output, fallback bytes.Buffer
			oldWriter := gin.DefaultErrorWriter
			gin.DefaultErrorWriter = &fallback
			t.Cleanup(func() { gin.DefaultErrorWriter = oldWriter })
			engine := New(Dependencies{Logger: diagnostics.New(&output, slog.LevelDebug)})
			var contextID string
			engine.POST("/diagnostic/:value", func(c *gin.Context) {
				contextID = diagnostics.RequestID(c.Request.Context())
				if tc.panic {
					panic("虚构panic秘密")
				}
				c.Status(http.StatusNoContent)
			})
			req := httptest.NewRequest("POST", tc.path, strings.NewReader("虚构正文秘密"))
			req.Method = tc.method
			req.Header.Set("Authorization", "Bearer 虚构认证秘密")
			req.Header.Set("Cookie", "session=虚构Cookie秘密")
			req.Header.Set("X-Request-ID", "虚构关联秘密")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("状态码不正确：%d", response.Code)
			}
			id := response.Header().Get("X-Request-ID")
			if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
				t.Fatalf("请求必须获得服务端生成的标识：%q", id)
			}
			if contextID != "" && contextID != id {
				t.Fatal("处理链中的请求关联必须一致")
			}
			if strings.Contains(output.String()+fallback.String()+response.Body.String(), "秘密") || fallback.Len() != 0 {
				t.Fatal("请求或 panic 敏感信息泄露")
			}
			decoder := json.NewDecoder(&output)
			seenRequest, seenPanic := false, false
			for {
				var event map[string]any
				if err := decoder.Decode(&event); err == io.EOF {
					break
				} else if err != nil {
					t.Fatalf("日志必须是 JSON：%v", err)
				}
				if event["request_id"] != id {
					t.Fatalf("日志缺少请求关联：%v", event)
				}
				if event["event"] == "http_request" {
					seenRequest = true
					method := tc.method
					if method == "虚构方法秘密" {
						method = "OTHER"
					}
					if event["route"] != tc.route || event["method"] != method || event["status"] != float64(tc.status) || event["duration_ms"] == nil {
						t.Fatalf("HTTP 诊断字段不正确：%v", event)
					}
				}
				if event["event"] == "http_panic" {
					seenPanic = true
				}
			}
			if !seenRequest || seenPanic != tc.panic {
				t.Fatal("请求结束或异常诊断缺失")
			}
		})
	}
}

func TestHTTPDiagnosticsHealthyProbeIsQuiet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	engine := New(Dependencies{Logger: diagnostics.New(&output, slog.LevelInfo)})
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest("GET", "/health", nil))
	if response.Code != 200 || output.Len() != 0 {
		t.Fatal("成功的健康检查应保持静默")
	}
}

// TestHTTPDiagnosticsDebugRedirectDoesNotLeakURL 覆盖 Gin 在中间件之前输出自动重定向 URL 的旁路。
func TestHTTPDiagnosticsDebugRedirectDoesNotLeakURL(t *testing.T) {
	previousMode := gin.Mode()
	previousWriter := gin.DefaultWriter
	var framework, output bytes.Buffer
	gin.SetMode(gin.DebugMode)
	gin.DefaultWriter = &framework
	t.Cleanup(func() {
		gin.SetMode(previousMode)
		gin.DefaultWriter = previousWriter
	})
	engine := New(Dependencies{Logger: diagnostics.New(&output, slog.LevelInfo)})
	framework.Reset()
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/users/?token=fictional-url-secret", nil))
	if strings.Contains(framework.String()+output.String(), "fictional-url-secret") {
		t.Fatal("自动重定向不得绕过安全日志并输出完整 URL")
	}
	if response.Code != http.StatusNotFound || response.Header().Get("X-Request-ID") == "" {
		t.Fatal("不匹配的尾斜杠路径必须进入安全请求中间件并返回未找到")
	}
	var event map[string]any
	if err := json.Unmarshal(output.Bytes(), &event); err != nil || event["route"] != "unmatched" || event["status"] != float64(404) {
		t.Fatal("尾斜杠请求必须记录安全的未匹配路由结果")
	}
}
