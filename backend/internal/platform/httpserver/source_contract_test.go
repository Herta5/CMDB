// 本文件覆盖生成请求解码前必须保留的接入源 JSON 安全边界。
package httpserver_test

import (
	"bytes"
	"cmdb/internal/audit"
	awscollector "cmdb/internal/aws"
	"cmdb/internal/platform/httpserver"
	"cmdb/internal/project"
	cloudresource "cmdb/internal/resource"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// TestSourceStrictJSONBoundary 验证拒绝模糊正文不会进入持久化或成功审计。
func TestSourceStrictJSONBoundary(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"未知外层字段", `{"provider":"aws","name":"虚构来源","region":"ap-east-1","credential":{"access_key_id":"identity-ok","secret_access_key":"private-contract-value"},"unexpected":"private-contract-value"}`},
		{"尾随对象", `{"provider":"aws","name":"虚构来源","region":"ap-east-1","credential":{"access_key_id":"identity-ok","secret_access_key":"private-contract-value"}} {}`},
		{"重复凭证字段", `{"provider":"aws","name":"虚构来源","region":"ap-east-1","credential":{"access_key_id":"identity-ok","secret_access_key":"private-contract-value","secret_access_key":"other"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, password, db := integrationServerWithDatabase(t)
			var logs bytes.Buffer
			server := httpserver.New(httpserver.Dependencies{Database: db, JWTSecret: "strict-test-signing-key", EncryptionKey: "integration-encryption-key", Adapters: map[string]cloudresource.ProviderAdapter{"aws": integrationCollector{Collector: awscollector.NewCollector()}}, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
			token := loginUser(t, server, "operator", password)
			parent := project.Project{Code: "strict", Name: "严格解析项目", Status: "enabled"}
			if err := db.Create(&parent).Error; err != nil {
				t.Fatal("准备项目失败")
			}
			response := integrationRawRequest(t, server, token, http.MethodPost, fmt.Sprintf("/api/v1/projects/%d/sources", parent.ID), tc.body, 400)
			if strings.Contains(response.Body.String()+logs.String(), "private-contract-value") {
				t.Fatal("错误响应泄露输入")
			}
			for _, model := range []any{&cloudresource.Source{}, &audit.Log{}} {
				var n int64
				if err := db.Model(model).Count(&n).Error; err != nil || n != 0 {
					t.Fatal("拒绝请求不应写入来源或审计")
				}
			}
		})
	}
}

// TestSourceAuthorizationBeforeSensitiveDecode 验证无权项目在解析敏感正文前保持统一不存在响应。
func TestSourceAuthorizationBeforeSensitiveDecode(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	token := loginUser(t, server, "member_a", password)
	parent := project.Project{Code: "denied", Name: "不可见项目", Status: "enabled"}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal("准备项目失败")
	}
	for _, body := range []string{`{`, `{"credential":{"secret_access_key":"secret","secret_access_key":"other"}}`} {
		denied := integrationRawRequest(t, server, token, http.MethodPost, fmt.Sprintf("/api/v1/projects/%d/sources", parent.ID), body, 404)
		missing := integrationRawRequest(t, server, token, http.MethodPost, "/api/v1/projects/99999/sources", body, 404)
		if denied.Body.String() != missing.Body.String() {
			t.Fatal("敏感正文不得改变无权项目的响应")
		}
	}
}

// TestSourceCredentialRetentionContract 验证生成器不能混淆保留凭证与不完整替换。
func TestSourceCredentialRetentionContract(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		status      int
	}{
		{"缺失凭证保留", "", 200}, {"空值凭证保留", `,"credential":null`, 200}, {"空字符串凭证保留", `,"credential":""`, 200},
		{"空对象不是保留", `,"credential":{}`, 400}, {"部分凭证不是保留", `,"credential":{"access_key_id":"identity-ok"}`, 400},
		{"重复凭证拒绝", `,"credential":{"access_key_id":"identity-ok","secret_access_key":"private-contract-value","secret_access_key":"other"}`, 400},
		{"未知配置字段拒绝", `,"config":{"unexpected":"private-contract-value"}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, password, db := integrationServerWithDatabase(t)
			token := loginUser(t, server, "operator", password)
			parent := project.Project{Code: "retain", Name: "凭证保留项目", Status: "enabled"}
			if err := db.Create(&parent).Error; err != nil {
				t.Fatal("准备项目失败")
			}
			original := integrationIdentitySource(t, db, parent.ID, "111111111111", "identity-ok")
			body := `{"name":"编辑名称","region":"ap-east-1","enabled":true,"sync_interval_minutes":60` + tc.field + `}`
			response := integrationRawRequest(t, server, token, http.MethodPut, fmt.Sprintf("/api/v1/projects/%d/sources/%d", parent.ID, original.ID), body, tc.status)
			if tc.status == 400 {
				assertIdentityVerificationError(t, response.Body.String(), "SOURCE_INVALID_INPUT")
			}
			var current cloudresource.Source
			if err := db.First(&current, original.ID).Error; err != nil {
				t.Fatal("读取保存来源失败")
			}
			if current.EncryptedCredential != original.EncryptedCredential || current.CloudAccountID != original.CloudAccountID || !current.IdentityVerifiedAt.Equal(*original.IdentityVerifiedAt) {
				t.Fatal("保留或拒绝凭证不应改变密文和身份")
			}
			var count int64
			if err := db.Model(&audit.Log{}).Where("action = ?", audit.ActionSourceUpdated).Count(&count).Error; err != nil {
				t.Fatal("读取审计失败")
			}
			if tc.status == 400 && (count != 0 || current.Name != original.Name) {
				t.Fatal("拒绝凭证不得修改来源或生成成功审计")
			}
			if tc.status == 200 && (count != 1 || current.Name != "编辑名称") {
				t.Fatal("保留凭证仍应完成其余合法编辑")
			}
		})
	}
}

// TestSyncJobPaginationContract 验证缺失默认值、有效数字与非法数字的明确边界。
func TestSyncJobPaginationContract(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	token := loginUser(t, server, "operator", password)
	parent := project.Project{Code: "page", Name: "任务分页项目", Status: "enabled"}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal("准备项目失败")
	}
	for _, tc := range []struct {
		name, query        string
		status, page, size int
	}{{"默认分页", "", 200, 1, 20}, {"显式分页", "?page=2&page_size=5", 200, 2, 5}, {"非法页码", "?page=invalid", 400, 0, 0}, {"非法接入源标识", "?source_id=invalid", 400, 0, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			response := integrationRequest(t, server, token, http.MethodGet, fmt.Sprintf("/api/v1/projects/%d/sync-jobs%s", parent.ID, tc.query), nil, tc.status)
			if tc.status == 200 {
				var page struct {
					Page     int `json:"page"`
					PageSize int `json:"page_size"`
				}
				decodeIntegration(t, response, &page)
				if page.Page != tc.page || page.PageSize != tc.size {
					t.Fatal("任务分页响应未保留规范默认或提交数字")
				}
			}
		})
	}
}
