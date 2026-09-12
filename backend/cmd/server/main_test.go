// 本文件验证生产启动装配同时提供页面、健康检查和受保护的新版 API。
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmdb/internal/platform/config"
	"cmdb/internal/platform/httpserver"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestServeRejectsIncompleteSchedulerRecovery 防止来源恢复失败后仍装配或开放 HTTP 服务。
func TestServeRejectsIncompleteSchedulerRecovery(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("创建恢复启动门禁数据库失败")
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	// 缺少恢复任务表会使恢复查询失败；静态目录和端口均设为不可启动，避免回归时真监听端口。
	t.Setenv("STATIC_DIR", t.TempDir())
	err = serve(config.Config{EncryptionKey: "虚构启动测试密钥", Server: config.Server{Port: "invalid-port"}}, db)
	if err == nil || err.Error() != "恢复同步任务失败，服务未启动" {
		t.Fatal("恢复失败必须在 HTTP 装配前返回中文安全启动错误")
	}
}

// TestStartAfterSchemaCheckRequiresCurrentVersion 防止旧版或超前数据库先启动调度器或 HTTP 服务。
func TestStartAfterSchemaCheckRequiresCurrentVersion(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("结构版本%d", version), func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:server_schema_%d?mode=memory&cache=shared", version)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal("打开启动门禁测试数据库失败")
			}
			sqlDB, _ := db.DB()
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = sqlDB.Close() })
			if err := db.Exec("CREATE TABLE schema_migrations (version INTEGER NOT NULL)").Error; err != nil {
				t.Fatal("创建启动门禁版本表失败")
			}
			if err := db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version).Error; err != nil {
				t.Fatal("写入启动门禁版本失败")
			}

			started := false
			err = startAfterSchemaCheck(context.Background(), db, func() error {
				started = true
				return nil
			})
			if version == 2 && (err != nil || !started) {
				t.Fatalf("当前结构版本应允许启动，错误=%v", err)
			}
			if version != 2 && (err == nil || started) {
				t.Fatalf("结构版本 %d 必须在服务组件启动前阻断", version)
			}
		})
	}
}

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
		method string
		path   string
		status int
		body   string
	}{
		{http.MethodGet, "/", 200, "CMDB 验收页面"},
		{http.MethodGet, "/projects/1", 200, "CMDB 验收页面"},
		{http.MethodGet, "/health", 200, `"status":"ok"`},
		{http.MethodGet, "/api/v1/projects", 401, "身份认证已失效"},
		{http.MethodPost, "/api/v1/projects/1/sources/1/verify-identity", 401, "身份认证已失效"},
		{http.MethodGet, "/api/v1/missing", 404, "接口不存在"},
	} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.body) {
			t.Fatalf("%s 装配结果错误：状态=%d", test.path, response.Code)
		}
	}
	if _, err := buildServer(httpserver.Dependencies{}, t.TempDir()); err == nil {
		t.Fatal("显式配置的静态目录缺少首页时必须拒绝启动")
	}
}
