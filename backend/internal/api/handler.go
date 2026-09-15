// Package api 将唯一 OpenAPI 契约连接到领域服务，不将生成类型传播到仓储和服务。
package api

import (
	"cmdb/internal/api/generated"
	"cmdb/internal/audit"
	"cmdb/internal/identity"
	"cmdb/internal/platform/diagnostics"
	"cmdb/internal/project"
	"cmdb/internal/resource"
	"context"
	"github.com/gin-gonic/gin"
)

// Handler 仅持有领域能力，所有公开结构由 generated 提供。
type Handler struct {
	identity *identity.Service
	project  *project.Service
	resource *resource.Service
	audit    *audit.Service
}

// New 创建严格服务端实现，路由与授权统一由 Register 装配。
func New(users *identity.Service, projects *project.Service, resources *resource.Service, audits *audit.Service) *Handler {
	return &Handler{users, projects, resources, audits}
}

var _ generated.StrictServerInterface = (*Handler)(nil)

func requestContext(ctx context.Context) context.Context {
	if c, ok := ctx.(*gin.Context); ok {
		return c.Request.Context()
	}
	return ctx
}
func requestID(ctx context.Context) string { return diagnostics.RequestID(requestContext(ctx)) }
func currentClaims(ctx context.Context) identity.UserClaims {
	if c, ok := ctx.(*gin.Context); ok {
		v, _ := c.Get(identity.UserClaimsContextKey)
		claims, _ := v.(identity.UserClaims)
		return claims
	}
	return identity.UserClaims{}
}
func value[T any](p *T) T {
	if p != nil {
		return *p
	}
	var zero T
	return zero
}
func defaultValue[T any](p *T, fallback T) T {
	if p != nil {
		return *p
	}
	return fallback
}
func nonzero[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// Health 仅公开进程存活状态。
func (h *Handler) Health(ctx context.Context, _ generated.HealthRequestObject) (generated.HealthResponseObject, error) {
	return generated.Health200JSONResponse{Body: generated.Health{Status: generated.Ok}, Headers: generated.Health200ResponseHeaders{XRequestID: requestID(ctx)}}, nil
}
