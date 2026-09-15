// 本文件验证后台同步的诊断信息可关联且不会泄露来源名称、凭证和底层异常。
package resource

import (
	"bytes"
	"cmdb/internal/platform/diagnostics"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestSyncDiagnosticsUsesSafeCorrelatedEvents(t *testing.T) {
	service, _, source, _ := newResourceServiceTest(t)
	var output bytes.Buffer
	service.logger = diagnostics.New(&output, slog.LevelInfo)
	ctx := diagnostics.WithRequestID(context.Background())
	_, err := service.Sync(ctx, source.ID, "manual", collectorStub{err: errors.New("虚构云凭证原始错误不可输出")})
	if err == nil {
		t.Fatal("采集错误应返回安全失败")
	}
	logged := output.String()
	if !strings.Contains(logged, "sync_started") || !strings.Contains(logged, "sync_finished") || !strings.Contains(logged, diagnostics.RequestID(ctx)) {
		t.Fatal("同步运行日志必须记录开始、结束与请求关联")
	}
	if strings.Contains(logged, "虚构云凭证原始错误不可输出") || strings.Contains(logged, source.EncryptedCredential) || strings.Contains(logged, source.Name) {
		t.Fatal("同步日志泄露敏感数据或原始错误")
	}
}

func TestSchedulerReadFailureProducesSafeDiagnostic(t *testing.T) {
	service, db, _, _ := newResourceServiceTest(t)
	var output bytes.Buffer
	service.logger = diagnostics.New(&output, slog.LevelInfo)
	if err := db.Migrator().DropTable(&Source{}); err != nil {
		t.Fatal("准备读取故障失败")
	}
	service.SyncDueSources(context.Background())
	logged := output.String()
	if !strings.Contains(logged, "scheduler_scan_failed") || !strings.Contains(logged, "ERROR") {
		t.Fatal("自动任务扫描失败必须有错误诊断")
	}
	if strings.Contains(logged, "SELECT") || strings.Contains(logged, "no such table") {
		t.Fatal("后台诊断不得包含SQL或底层错误")
	}
}

// panicCollector 模拟 SDK 异常，调用方不得让 panic 穿透到进程或保留明文凭证。
type panicCollector struct {
	collectorStub
	plain []byte
}

func (c *panicCollector) Collect(ctx context.Context, source Source, plain []byte) ([]CollectionResult, error) {
	c.plain = plain
	panic("测试采集异常")
}

func TestSyncDiagnosticsContainsCollectorPanic(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	var output bytes.Buffer
	service.logger = diagnostics.New(&output, slog.LevelInfo)
	collector := &panicCollector{}
	job, err := service.Sync(context.Background(), source.ID, "manual", collector)
	if err == nil || job == nil || job.Status != "failed" {
		t.Fatal("采集异常必须收敛为失败任务")
	}
	awaitJob(t, db, job.ID, "failed")
	if !strings.Contains(output.String(), "sync_panicked") || strings.Contains(output.String(), "测试采集异常") {
		t.Fatal("采集异常只能记录固定安全事件")
	}
	for _, value := range collector.plain {
		if value != 0 {
			t.Fatal("采集异常后必须清理明文凭证")
		}
	}
}

// TestFailureLookupProducesPersistenceError 验证失败收敛尚未进入事务时的来源读取故障也可定位。
func TestFailureLookupProducesPersistenceError(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	var output bytes.Buffer
	service.logger = diagnostics.New(&output, slog.LevelInfo)
	if err := db.Migrator().DropTable(&Source{}); err != nil {
		t.Fatal("准备失败收敛读取故障失败")
	}
	_, err := service.finishFailed(context.Background(), &SyncJob{SourceID: source.ID, Status: "running"}, "同步执行超时，可重新执行", context.DeadlineExceeded)
	if err == nil || !strings.Contains(output.String(), "sync_failure_persist_failed") || !strings.Contains(output.String(), "ERROR") {
		t.Fatal("失败收敛来源读取错误必须有固定错误诊断")
	}
	if strings.Contains(output.String(), "SELECT") || strings.Contains(output.String(), "no such table") {
		t.Fatal("失败读取诊断不得输出SQL或原错误")
	}
}
