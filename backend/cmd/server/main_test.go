// 本文件验证生产启动装配同时提供页面、健康检查和受保护的新版 API。
package main

import (
	"cmdb/internal/audit"
	"cmdb/internal/resource"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		{http.MethodPost, "/api/v1/projects/1/sources/1/verify-identity", 404, "接口不存在"},
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

// TestRunHTTPStopsWorkersOnShutdown 验证服务关闭会停止新同步受理并等待工作器退出。
func TestRunHTTPStopsWorkersOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := resource.NewService(nil, nil, nil)
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	if err := runHTTP(ctx, server, service); err != nil {
		t.Fatal("正常关闭不得报告启动失败")
	}
	if _, err := service.EnqueueSync(context.Background(), 1, "manual", nil); !errors.Is(err, resource.ErrSyncStopping) {
		t.Fatal("HTTP停止后不得受理新同步")
	}
}

// shutdownCollector 在实际工作器内等待取消，确保退出测试覆盖真实运行中任务。
type shutdownCollector struct{ entered chan struct{} }

func (c shutdownCollector) Collect(ctx context.Context, _ resource.Source, _ []byte) ([]resource.CollectionResult, error) {
	close(c.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}
func (c shutdownCollector) Probe(context.Context, resource.Source, []byte) ([]resource.CollectionResult, error) {
	return nil, nil
}

func TestShutdownWaitsForRunningJobFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "shutdown.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("创建关闭测试数据库失败")
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	if err := db.AutoMigrate(&resource.Source{}, &resource.SyncJob{}, &audit.Log{}); err != nil {
		t.Fatal("创建关闭测试表失败")
	}
	if err := db.Exec("CREATE TABLE projects (id integer primary key, name text, status text)").Error; err != nil {
		t.Fatal("准备项目表失败")
	}
	db.Exec("INSERT INTO projects VALUES (1,'关闭测试项目','enabled')")
	cipher := resource.NewCredentialCipher("虚构关闭测试密钥")
	encrypted, _ := cipher.Encrypt([]byte(`{}`))
	now := time.Now()
	source := &resource.Source{ProjectID: 1, Provider: resource.ProviderAWS, Name: "关闭测试来源", Region: "test", EncryptedCredential: encrypted, CloudAccountID: "123456789012", IdentityVerifiedAt: &now, SyncIntervalMinutes: 60}
	if err := db.Create(source).Error; err != nil {
		t.Fatal("准备关闭测试来源失败")
	}
	service := resource.NewService(resource.NewRepository(db), cipher, nil, audit.NewRepository(db))
	collector := shutdownCollector{entered: make(chan struct{})}
	job, err := service.EnqueueSync(context.Background(), source.ID, "manual", collector)
	if err != nil {
		t.Fatal("启动测试任务失败")
	}
	select {
	case <-collector.entered:
	case <-time.After(time.Second):
		t.Fatal("工作器未开始运行")
	}
	if err := stopAndWait(service); err != nil {
		t.Fatal("停止服务必须等待运行任务收敛")
	}
	var stored resource.SyncJob
	if err := db.First(&stored, job.ID).Error; err != nil || stored.Status != "failed" {
		t.Fatal("退出前必须完成运行任务失败落库")
	}
}
