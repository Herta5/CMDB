package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSystemHealthRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerSystemRoutes(r)
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"status":"ok"}` {
		t.Fatalf("health = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestCIInstanceStaticRoutesDispatchToTheirHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("roles", []string{"cmdb_admin"})
	})
	registerCIInstanceRoutes(r.Group("/ci-instances"), ciInstanceRouteHandlers{
		List:         routeMarker("list"),
		Create:       routeMarker("create"),
		Import:       routeMarker("import"),
		Export:       routeMarker("export"),
		Get:          routeMarker("id"),
		Update:       routeMarker("update"),
		Delete:       routeMarker("delete"),
		UpdateStatus: routeMarker("status"),
	})

	assertRouteMarker(t, r, http.MethodPost, "/ci-instances/import", "import")
	assertRouteMarker(t, r, http.MethodGet, "/ci-instances/export", "export")
}

func TestSnapshotDiffRouteDispatchesToItsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerSnapshotRoutes(r.Group("/snapshots"), snapshotRouteHandlers{
		List: routeMarker("list"),
		Diff: routeMarker("diff"),
		Get:  routeMarker("id"),
	})

	assertRouteMarker(t, r, http.MethodGet, "/snapshots/diff", "diff")
}

func routeMarker(marker string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.String(http.StatusOK, marker)
	}
}

func assertRouteMarker(t *testing.T, r *gin.Engine, method, path, want string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != want {
		t.Fatalf("%s %s resolved with status %d and body %q, want status %d and marker %q", method, path, recorder.Code, recorder.Body.String(), http.StatusOK, want)
	}
}
