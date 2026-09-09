// 本文件验证生产启动装配同时提供页面、健康检查和受保护的新版 API。
package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmdb/internal/platform/httpserver"
)

// TestBuildServerServesConsoleAndAPI 防止镜像启动后仅有 API、刷新详情页面失败或未知 API 被首页掩盖。
func TestBuildServerServesConsoleAndAPI(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html>CMDB 验收页面</html>"), 0600); err != nil {
		t.Fatal("准备页面失败")
	}
	server, err := buildServer(httpserver.Dependencies{}, staticDir)
	if err != nil {
		t.Fatalf("装配服务失败：%v", err)
	}
	for _, test := range []struct {
		path   string
		status int
		body   string
	}{
		{"/", 200, "CMDB 验收页面"},
		{"/projects/1", 200, "CMDB 验收页面"},
		{"/health", 200, `"status":"ok"`},
		{"/api/v1/projects", 401, "身份认证已失效"},
		{"/api/v1/missing", 404, "接口不存在"},
	} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.body) {
			t.Fatalf("%s 装配结果错误：状态=%d", test.path, response.Code)
		}
	}
	if _, err := buildServer(httpserver.Dependencies{}, t.TempDir()); err == nil {
		t.Fatal("显式配置的静态目录缺少首页时必须拒绝启动")
	}
}
