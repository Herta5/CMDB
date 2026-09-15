// 本文件在授权完成后验证请求结构，并在通用解码前保留凭证重复键证据。
package api

import (
	"bytes"
	"cmdb/internal/resource"
	"encoding/json"
	"errors"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/gin-gonic/gin"
	"io"
	"strings"
)

func requestValidation(swagger *openapi3.T, lookup func(*gin.Context) *openapi3.Operation) gin.HandlerFunc {
	router, err := legacy.NewRouter(swagger)
	if err != nil {
		panic("接口校验器不可用")
	}
	return func(c *gin.Context) {
		op := lookup(c)
		if op == nil {
			c.Next()
			return
		}
		name := operationName(op.OperationID)
		reject := func(e *errorResponse) { c.Abort(); _ = e.write(c.Writer) }
		if op.RequestBody != nil {
			raw, err := io.ReadAll(c.Request.Body)
			if err != nil {
				reject(invalidRequest(name, false))
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(raw))
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
				reject(invalidRequest(name, false))
				return
			}
			if (name == "updateMyProfile" || name == "changeMyPassword") && !uniqueObjectKeys(raw) {
				reject(invalidRequest(name, false))
				return
			}
			if name == "createSource" || name == "updateSource" {
				// RawMessage 保留凭证对象字节；先扫描重复键，之后的 Schema 解码不能抹掉证据。
				if !uniqueObjectKeys(raw) {
					reject(invalidRequest(name, false))
					return
				}
				if err := resource.ValidateEmptyConfig(fields["config"]); err != nil {
					reject(sourceMutationError(err))
					return
				}
				credential := fields["credential"]
				trimmed := bytes.TrimSpace(credential)
				if len(trimmed) > 0 && trimmed[0] == '{' {
					if !uniqueObjectKeys(trimmed) {
						reject(sourceMutationError(resource.ErrInvalidProviderCredential))
						return
					}
				}
			}
			if c.GetHeader("Content-Type") == "" {
				c.Request.Header.Set("Content-Type", "application/json")
			}
		}
		route, params, err := router.FindRoute(c.Request)
		if err != nil {
			reject(invalidRequest(name, false))
			return
		}
		options := &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, SkipSettingDefaults: true}
		options.WithCustomSchemaErrorFunc(func(*openapi3.SchemaError) string { return "请求结构无效" })
		err = openapi3filter.ValidateRequest(c.Request.Context(), &openapi3filter.RequestValidationInput{Request: c.Request, PathParams: params, Route: route, Options: options})
		if err != nil {
			var schemaError *openapi3.SchemaError
			semantic := errors.As(err, &schemaError) && schemaError.SchemaField != "type" && schemaError.SchemaField != "additionalProperties" && schemaError.SchemaField != "properties"
			var parameterError *openapi3filter.RequestError
			if errors.As(err, &parameterError) && parameterError.Parameter != nil {
				if parameterError.Parameter.In == "path" {
					if name == "listProjectResources" {
						reject(failure(400, "RESOURCE_INVALID_REQUEST", "请求格式错误"))
						return
					}
					if parameterError.Parameter.Name == "username" && strings.Contains(name, "Member") {
						reject(failure(400, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效"))
						return
					}
				}
				semantic = false
			}
			reject(invalidRequest(name, semantic))
			return
		}
		c.Next()
		// strict 绑定器仅设置状态而不写错误正文时，在尚未发送响应前补齐安全契约。
		// 只检查错误存在性，不读取、保存或记录绑定器的原始输入异常。
		if len(c.Errors) > 0 && !c.Writer.Written() {
			response := failure(500, "INTERNAL_ERROR", "服务暂时不可用")
			if c.Writer.Status() == 400 {
				response = invalidRequest(name, false)
			}
			reject(response)
		}
	}
}

// uniqueObjectKeys 只读取键与原始值，不构造凭证明文字符串映射。
func uniqueObjectKeys(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	keys := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		s, ok := key.(string)
		if err != nil || !ok || keys[s] {
			return false
		}
		keys[s] = true
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return false
		}
	}
	_, err = d.Token()
	return err == nil
}
func invalidRequest(op string, semantic bool) *errorResponse {
	switch {
	case op == "login":
		return failure(400, "AUTH_INVALID_REQUEST", "请求格式错误")
	case strings.Contains(op, "AuditLogs"):
		return failure(400, "AUDIT_INVALID_REQUEST", "审计查询参数无效")
	case op == "listProjectResources" || op == "listAllResources":
		return failure(400, "RESOURCE_INVALID_QUERY", "资源查询参数无效")
	case op == "listSyncJobs" || op == "retrySyncJob":
		return failure(400, "SYNC_JOB_INVALID_REQUEST", "请求格式错误")
	case strings.Contains(op, "Source") || op == "listSources":
		if semantic {
			return sourceMutationError(resource.ErrInvalidSourceInput)
		}
		return failure(400, "SOURCE_INVALID_REQUEST", "请求格式错误")
	case strings.Contains(op, "User") || op == "updateMyProfile" || op == "changeMyPassword":
		if semantic {
			return failure(400, "USER_INVALID_INPUT", "用户参数无效")
		}
		return failure(400, "USER_INVALID_REQUEST", "请求格式错误")
	default:
		if semantic {
			if strings.Contains(op, "Member") {
				return failure(400, "PROJECT_MEMBER_INVALID_INPUT", "项目成员参数无效")
			}
			return failure(400, "PROJECT_INVALID_INPUT", "项目参数无效")
		}
		return failure(400, "PROJECT_INVALID_REQUEST", "请求格式错误")
	}
}
