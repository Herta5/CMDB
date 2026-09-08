package router

import (
	"net/http"
	"testing"

	"github-cmdb/internal/handler"
	"github.com/gin-gonic/gin"
)

func TestStaticCIRoutesPrecedeInstanceIDRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, &Handlers{
		CIType:      &handler.CITypeHandler{},
		CIInstance:  &handler.CIInstanceHandler{},
		Relation:    &handler.RelationHandler{},
		Dashboard:   &handler.DashboardHandler{},
		Discovery:   &handler.DiscoveryHandler{},
		Snapshot:    &handler.SnapshotHandler{},
		Change:      &handler.ChangeHandler{},
		Batch:       &handler.BatchHandler{},
		Integration: &handler.IntegrationHandler{},
		Audit:       &handler.AuditHandler{},
		User:        &handler.UserHandler{},
	})

	routes := r.Routes()
	assertRoutePrecedes(t, routes, http.MethodPost, "/api/v1/ci-instances/import", http.MethodGet, "/api/v1/ci-instances/:id")
	assertRoutePrecedes(t, routes, http.MethodGet, "/api/v1/ci-instances/export", http.MethodGet, "/api/v1/ci-instances/:id")
	assertRoutePrecedes(t, routes, http.MethodGet, "/api/v1/snapshots/diff", http.MethodGet, "/api/v1/snapshots/:id")
}

func assertRoutePrecedes(t *testing.T, routes gin.RoutesInfo, staticMethod, staticPath, parameterMethod, parameterPath string) {
	t.Helper()
	staticIndex := routeIndex(routes, staticMethod, staticPath)
	parameterIndex := routeIndex(routes, parameterMethod, parameterPath)
	if staticIndex == -1 {
		t.Fatalf("static route %s %s is not registered", staticMethod, staticPath)
	}
	if parameterIndex == -1 {
		t.Fatalf("parameter route %s %s is not registered", parameterMethod, parameterPath)
	}
	if staticIndex > parameterIndex {
		t.Fatalf("static route %s %s is registered after %s %s", staticMethod, staticPath, parameterMethod, parameterPath)
	}
}

func routeIndex(routes gin.RoutesInfo, method, path string) int {
	for i, route := range routes {
		if route.Method == method && route.Path == path {
			return i
		}
	}
	return -1
}
