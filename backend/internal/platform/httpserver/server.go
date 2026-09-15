// Package httpserver 负责创建 CMDB HTTP 服务基础实例。
package httpserver

import (
	"log/slog"
	"net/http"
	"time"

	"cmdb/internal/api"
	"cmdb/internal/audit"
	"cmdb/internal/identity"
	"cmdb/internal/platform/diagnostics"
	"cmdb/internal/project"
	cloudresource "cmdb/internal/resource"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Dependencies 声明 HTTP 服务依赖，后续领域路由通过该边界接入共享基础设施。
type Dependencies struct {
	Logger          *slog.Logger
	Database        *gorm.DB
	UserRepository  identity.UserRepository
	JWTSecret       string
	EncryptionKey   string
	Adapters        map[string]cloudresource.ProviderAdapter
	ResourceService *cloudresource.Service
}

// New 创建启用恢复中间件的 Gin 引擎并装配公共身份路由。
// 用户仓储允许测试替换，生产环境则统一由平台层数据库创建，避免领域模块自行管理连接。
func New(dependencies Dependencies) *gin.Engine {
	engine := gin.New()
	// Gin 自动重定向在中间件之前执行，debug 模式还会输出完整 URL；统一交给安全未匹配路径处理。
	engine.RedirectTrailingSlash = false
	engine.RedirectFixedPath = false
	// 当前单体直接对外提供 HTTP，不信任客户端自报的转发头，避免伪造审计来源 IP。
	_ = engine.SetTrustedProxies(nil)
	output := dependencies.Logger
	if output == nil {
		output = slog.Default()
	}
	engine.Use(requestDiagnostics(output))

	repository := dependencies.UserRepository
	var projectRepository project.Repository
	if repository == nil && dependencies.Database != nil {
		repository = identity.NewUserRepository(dependencies.Database)
	}
	if dependencies.Database != nil {
		// 项目父删除显式接入资源核心守卫，系统管理员入口也不能绕过资产生命周期。
		projectRepository = project.NewRepository(dependencies.Database, cloudresource.NewDeletionGuard(dependencies.Database))
	}
	// 全部领域共享同一个审计仓储，平台模块不得复制审计表写入逻辑。
	auditRepository := audit.NewRepository(dependencies.Database)
	identityService := identity.NewService(repository, dependencies.JWTSecret, auditRepository)
	authenticator := NewAuthenticator(identityService)
	resourceService := dependencies.ResourceService
	if resourceService == nil {
		resourceService = cloudresource.NewService(cloudresource.NewRepository(dependencies.Database), cloudresource.NewCredentialCipher(dependencies.EncryptionKey), dependencies.Adapters, auditRepository)
	}
	api.Register(engine, api.New(identityService, project.NewService(projectRepository, auditRepository), resourceService, audit.NewService(auditRepository)), authenticator.RequireUser(), projectRepository)

	return engine
}

// requestDiagnostics 在请求边界生成可信标识，并以固定字段记录结果和恢复异常。
func requestDiagnostics(output *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		ctx := diagnostics.WithRequestID(c.Request.Context())
		c.Request = c.Request.WithContext(ctx)
		id := diagnostics.RequestID(ctx)
		c.Header("X-Request-ID", id)
		defer func() {
			// 不读取 panic 值、请求正文或堆栈，避免 Gin 默认恢复日志泄露密码和令牌。
			if recover() != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "服务暂时不可用"})
				output.ErrorContext(ctx, "HTTP 请求异常", "event", "http_panic", "request_id", id, "category", "internal_error")
			}
			route := c.FullPath()
			status := c.Writer.Status()
			if route == "/health" && status == http.StatusOK {
				return
			}
			if route == "" {
				route = "unmatched"
			}
			method := c.Request.Method
			switch method {
			case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
			default:
				method = "OTHER"
			}
			level := slog.LevelInfo
			if status >= 500 {
				level = slog.LevelError
			} else if status >= 400 {
				level = slog.LevelWarn
			}
			output.Log(ctx, level, "HTTP 请求完成", "event", "http_request", "request_id", id, "method", method, "route", route, "status", status, "duration_ms", float64(time.Since(started))/float64(time.Millisecond))
		}()
		c.Next()
	}
}
