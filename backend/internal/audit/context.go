// 本文件只在请求上下文中传递已认证操作者元数据，避免修改所有领域方法签名。
package audit

import "context"

// actorMetadata 保存一次人工请求的用户标识与来源 IP，禁止加入令牌或用户资料。
type actorMetadata struct {
	ID          uint64
	RequestIP   string
	Username    string
	DisplayName string
}

// actorContextKey 使用私有类型防止其他包意外覆盖审计身份。
type actorContextKey struct{}

// WithActor 将已经过认证的操作者放入请求上下文；后台任务不调用本方法即表示系统任务。
func WithActor(ctx context.Context, actorID uint64, requestIP string) context.Context {
	return WithActorProfile(ctx, actorID, requestIP, "", "")
}

// WithActorProfile 同时保存操作者最小公开名称，使批量资源审计无需逐条查询用户表。
func WithActorProfile(ctx context.Context, actorID uint64, requestIP, username, displayName string) context.Context {
	if actorID == 0 {
		return ctx
	}
	return context.WithValue(ctx, actorContextKey{}, actorMetadata{ID: actorID, RequestIP: requestIP, Username: username, DisplayName: displayName})
}

// actorFromContext 读取审计身份；调用方显式字段优先于上下文默认值。
func actorFromContext(ctx context.Context) (actorMetadata, bool) {
	metadata, ok := ctx.Value(actorContextKey{}).(actorMetadata)
	return metadata, ok && metadata.ID > 0
}

// DetachedContext 将人工请求的最小审计身份复制到独立后台上下文，避免保留已取消的 HTTP 请求。
func DetachedContext(ctx context.Context) context.Context {
	metadata, ok := actorFromContext(ctx)
	if !ok {
		return context.Background()
	}
	return WithActorProfile(context.Background(), metadata.ID, metadata.RequestIP, metadata.Username, metadata.DisplayName)
}
