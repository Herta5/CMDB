// 本文件以有限分类替代 GORM 默认 SQL 和原始错误输出，守住数据库日志敏感信息边界。
package diagnostics

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type databaseLogger struct {
	output *slog.Logger
	level  logger.LogLevel
}

// NewDatabaseLogger 创建不输出 SQL、参数、原始错误或自由文本的数据库诊断适配器。
func NewDatabaseLogger(output *slog.Logger) logger.Interface {
	if output == nil {
		output = slog.Default()
	}
	return &databaseLogger{output: output, level: logger.Warn}
}

// LogMode 按 GORM 会话复制等级设置，避免一个会话改变其他并发请求的日志行为。
func (l *databaseLogger) LogMode(level logger.LogLevel) logger.Interface {
	clone := *l
	clone.level = level
	return &clone
}

// Info 丢弃无法保证无凭证的 GORM 自由文本。
func (*databaseLogger) Info(context.Context, string, ...interface{}) {}

// Warn 丢弃无法保证无凭证的 GORM 自由文本。
func (*databaseLogger) Warn(context.Context, string, ...interface{}) {}

// Error 丢弃无法保证无凭证的 GORM 自由文本；查询失败由 Trace 统一分类。
func (*databaseLogger) Error(context.Context, string, ...interface{}) {}

// Trace 只记录查询失败和超过 200 毫秒的慢查询，正常未命中不属于基础设施故障。
func (l *databaseLogger) Trace(ctx context.Context, begin time.Time, _ func() (string, int64), err error) {
	if l.level == logger.Silent || errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}
	elapsed := time.Since(begin)
	category := ""
	level := slog.LevelWarn
	if err != nil && l.level >= logger.Error {
		level = slog.LevelError
		category = "database_error"
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			category = "timeout"
		case errors.Is(err, context.Canceled):
			category = "canceled"
		}
	} else if elapsed > 200*time.Millisecond && l.level >= logger.Warn {
		category = "slow_query"
	}
	if category == "" {
		return
	}
	// GORM 将行数与 SQL 共用一个延迟生成闭包；不执行闭包，行数统一标记为未知。
	attrs := []any{"event", "database_query", "category", category, "duration_ms", float64(elapsed) / float64(time.Millisecond), "rows", -1}
	if id := RequestID(ctx); id != "" {
		attrs = append(attrs, "request_id", id)
	}
	l.output.Log(ctx, level, "数据库执行诊断", attrs...)
}
