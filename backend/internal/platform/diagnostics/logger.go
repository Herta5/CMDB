// Package diagnostics 提供共享 JSON 日志与服务端请求关联，调用方仅可传入安全摘要和元数据。
package diagnostics

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
)

// New 创建遵循配置等级的 JSON 日志；自由文本须由调用边界收敛，禁止直接传原始错误。
func New(writer io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level}))
}

type requestIDKey struct{}

// WithRequestID 为每次请求生成独立随机标识，禁止从请求头或其他客户端输入派生。
func WithRequestID(ctx context.Context) context.Context {
	var value [16]byte
	_, _ = rand.Read(value[:])
	return context.WithValue(ctx, requestIDKey{}, hex.EncodeToString(value[:]))
}

// RequestID 读取服务端已生成的关联标识；没有 HTTP 来源的操作返回空字符串。
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// CopyRequestID 只传递可信关联标识，不保留原请求的取消信号、认证对象或其他上下文值。
func CopyRequestID(target, source context.Context) context.Context {
	if id := RequestID(source); id != "" {
		return context.WithValue(target, requestIDKey{}, id)
	}
	return target
}
