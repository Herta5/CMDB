package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMountServesSPAAndAssetsWithoutCapturingAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	requireWrite(t, filepath.Join(dir, "index.html"), "<html>cmdb</html>")
	requireWrite(t, filepath.Join(dir, "assets", "app-abc123.js"), "app")

	r := gin.New()
	r.GET("/api/v1/example", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"source": "api"}) })
	if err := Mount(r, dir); err != nil {
		t.Fatal(err)
	}

	assertResponse(t, r, "/dashboard", http.StatusOK, "<html>cmdb</html>", "no-cache")
	assertResponse(t, r, "/assets/app-abc123.js", http.StatusOK, "app", "public, max-age=31536000, immutable")
	assertResponse(t, r, "/missing.js", http.StatusNotFound, "", "")
	assertResponse(t, r, "/api/v1/example", http.StatusOK, `{"source":"api"}`, "")
	assertResponse(t, r, "/api/v1/missing", http.StatusNotFound, "", "")
}

// TestMountServesAssetPageRoutesThroughSPA 防止资产页面与构建产物共用 /assets 前缀时刷新返回 404。
func TestMountServesAssetPageRoutesThroughSPA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	requireWrite(t, filepath.Join(dir, "index.html"), "<html>cmdb</html>")

	r := gin.New()
	if err := Mount(r, dir); err != nil {
		t.Fatal(err)
	}

	for _, requestPath := range []string{
		"/assets/servers",
		"/assets/databases",
		"/assets/load-balancers",
	} {
		assertResponse(t, r, requestPath, http.StatusOK, "<html>cmdb</html>", "no-cache")
	}
}

func TestMountRejectsAssetTraversalAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	requireWrite(t, filepath.Join(dir, "index.html"), "<html>cmdb</html>")

	r := gin.New()
	if err := Mount(r, dir); err != nil {
		t.Fatal(err)
	}

	assertResponse(t, r, "/assets/%2e%2e/index.html", http.StatusNotFound, "", "")
}

func TestMountMarksDirectIndexRequestNoCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	requireWrite(t, filepath.Join(dir, "index.html"), "<html>cmdb</html>")

	r := gin.New()
	if err := Mount(r, dir); err != nil {
		t.Fatal(err)
	}

	assertResponse(t, r, "/index.html", http.StatusMovedPermanently, "", "no-cache")
}

func TestMountWithoutStaticDirectoryLeavesFallbackUninstalled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := Mount(r, ""); err != nil {
		t.Fatal(err)
	}

	assertResponse(t, r, "/dashboard", http.StatusNotFound, "", "")
}

func TestMountRequiresIndexFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := Mount(gin.New(), t.TempDir()); err == nil {
		t.Fatal("Mount returned nil error without index.html")
	}
}

func requireWrite(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertResponse(t *testing.T, r http.Handler, requestPath string, wantStatus int, wantBody, wantCache string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestPath, nil))
	if recorder.Code != wantStatus {
		t.Fatalf("%s status = %d, want %d", requestPath, recorder.Code, wantStatus)
	}
	if wantBody != "" && strings.TrimSpace(recorder.Body.String()) != wantBody {
		t.Fatalf("%s body = %q, want %q", requestPath, recorder.Body.String(), wantBody)
	}
	if got := recorder.Header().Get("Cache-Control"); got != wantCache {
		t.Fatalf("%s cache = %q, want %q", requestPath, got, wantCache)
	}
}
