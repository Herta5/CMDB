// 本文件验证资源列表公开查询参数的 HTTP 边界，不启动真实服务或连接云平台。
package resource

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestResourceListQueryReadsDatabaseEngine 防止前端已提交的引擎筛选在 HTTP 边界被静默丢弃。
func TestResourceListQueryReadsDatabaseEngine(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, "/resources?engine=PostgreSQL", nil)

	query, ok := resourceListQuery(context)
	if !ok || query.Engine != "PostgreSQL" {
		t.Fatalf("数据库引擎查询参数未进入资源筛选：ok=%t query=%+v", ok, query)
	}
}
