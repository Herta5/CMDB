// 本文件验证诊断日志的等级过滤、请求关联与数据库敏感信息边界。
package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestJSONLevelAndRequestCorrelation(t *testing.T) {
	var output bytes.Buffer
	log := New(&output, slog.LevelWarn)
	log.Info("不应输出")
	log.Warn("安全事件", "category", "test")
	var event map[string]any
	if err := json.Unmarshal(output.Bytes(), &event); err != nil || event["level"] != "WARN" || event["category"] != "test" {
		t.Fatalf("应按等级输出单条有效 JSON：%v，%s", err, output.String())
	}
	ctx := WithRequestID(context.Background())
	id := RequestID(ctx)
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" || RequestID(WithRequestID(ctx)) == id || RequestID(context.Background()) != "" {
		t.Fatal("关联标识必须由服务端独立生成，不继承外来标识")
	}
	type secretKey struct{}
	source := context.WithValue(ctx, secretKey{}, "虚构敏感上下文")
	copied := CopyRequestID(context.Background(), source)
	if RequestID(copied) != id || copied.Value(secretKey{}) != nil {
		t.Fatal("后台任务只允许保留已生成的关联标识，不得传播请求其他上下文")
	}
}

func TestDatabaseLogsNeverEvaluateSQLOrExposeErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		age      time.Duration
		category string
	}{
		{"失败只记录固定分类", errors.New("虚构数据库密码及 SQL 秘密"), 0, "database_error"},
		{"超时只记录固定分类", context.DeadlineExceeded, 0, "timeout"},
		{"取消只记录固定分类", context.Canceled, 0, "canceled"},
		{"慢查询不记录 SQL", nil, time.Second, "slow_query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			ctx := WithRequestID(context.Background())
			dbLog := NewDatabaseLogger(New(&output, slog.LevelDebug))
			dbLog.Trace(ctx, time.Now().Add(-tc.age), func() (string, int64) {
				t.Fatal("安全日志禁止执行 SQL 生成闭包")
				return "虚构 SQL 密文", 3
			}, tc.err)
			var event map[string]any
			if err := json.Unmarshal(output.Bytes(), &event); err != nil {
				t.Fatalf("数据库诊断必须输出 JSON：%v", err)
			}
			if event["category"] != tc.category || event["request_id"] != RequestID(ctx) || event["duration_ms"] == nil || event["rows"] != float64(-1) {
				t.Fatalf("数据库诊断缺少安全关联与执行摘要：%v", event)
			}
			if strings.Contains(output.String(), "秘密") || strings.Contains(output.String(), "密文") {
				t.Fatal("数据库诊断泄露敏感信息")
			}
		})
	}
}

func TestDatabaseLoggerDropsFreeTextAndNormalQueries(t *testing.T) {
	var output bytes.Buffer
	dbLog := NewDatabaseLogger(New(&output, slog.LevelDebug))
	ctx := context.Background()
	dbLog.Info(ctx, "虚构秘密 %s", "密码")
	dbLog.Warn(ctx, "虚构秘密 %s", "密码")
	dbLog.Error(ctx, "虚构秘密 %s", "密码")
	fc := func() (string, int64) { t.Fatal("不应生成 SQL"); return "", 0 }
	dbLog.Trace(ctx, time.Now(), fc, nil)
	dbLog.Trace(ctx, time.Now(), fc, gorm.ErrRecordNotFound)
	dbLog.LogMode(logger.Silent).Trace(ctx, time.Now(), fc, errors.New("虚构秘密"))
	if output.Len() != 0 {
		t.Fatalf("普通查询、记录不存在与任意自由文本不得进入诊断日志：%s", output.String())
	}
}
