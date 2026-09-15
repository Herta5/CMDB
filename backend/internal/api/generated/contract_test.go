// 本文件验证公开契约完整覆盖现有操作及不可公开的字段边界。
package generated_test

import (
	"cmdb/internal/api/generated"
	"context"
	"github.com/getkin/kin-openapi/openapi3"
	"testing"
)

func Test公开契约完整且可解析(t *testing.T) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	spec, err := loader.LoadFromFile("../../../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("加载公开契约失败：%v", err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		t.Fatalf("契约无效：%v", err)
	}
	want := map[string]bool{}
	for _, id := range []string{"health", "login", "getMe", "listUsers", "createUser", "updateUser", "deleteUser", "updateUserStatus", "listProjects", "createProject", "getProject", "updateProject", "deleteProject", "listProjectMembers", "addProjectMember", "updateProjectMemberRole", "removeProjectMember", "listProjectMemberCandidates", "listGlobalAuditLogs", "listProjectAuditLogs", "listAllResources", "listProjectResources", "listSources", "createSource", "updateSource", "deleteSource", "syncSource", "testSourceConnection", "listSyncJobs", "retrySyncJob"} {
		want[id] = true
	}
	for path, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			if !want[op.OperationID] {
				t.Errorf("重复或未盘点操作：%s %s", path, op.OperationID)
			}
			delete(want, op.OperationID)
		}
	}
	if len(want) != 0 {
		t.Errorf("契约遗漏操作：%v", want)
	}
	for _, name := range []string{"PublicUser", "Project", "ProjectMember", "MemberCandidate", "Source", "Resource", "SyncJob", "AuditLog"} {
		schema := spec.Components.Schemas[name].Value
		for _, field := range []string{"user_id", "owner_user_id", "actor_id", "cloud_account_id", "identity_verified_at", "encrypted_credential", "raw_attributes", "password_hash"} {
			if name == "Source" && field == "cloud_account_id" {
				continue // 仅授权接入源响应允许展示云账号，其他 DTO 仍禁止扩散。
			}
			if _, exists := schema.Properties[field]; exists {
				t.Errorf("%s 公开敏感字段 %s", name, field)
			}
		}
		if schema.AdditionalProperties.Has == nil || *schema.AdditionalProperties.Has {
			t.Errorf("%s 必须拒绝未声明响应字段", name)
		}
	}
}

func Test凭证结构及更新保留语义(t *testing.T) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	spec, err := loader.LoadFromFile("../../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, schema string
		value        any
		valid        bool
	}{
		{"阿里云完整凭证", "SourceCredential", map[string]any{"access_key_id": "虚构标识", "access_key_secret": "虚构密钥"}, true},
		{"AWS完整凭证", "SourceCredential", map[string]any{"access_key_id": "虚构标识", "secret_access_key": "虚构密钥"}, true},
		{"创建缺少密钥", "SourceCredential", map[string]any{"access_key_id": "虚构标识"}, false},
		{"创建凭证不得为空", "SourceCredential", nil, false},
		{"凭证不接受未知字段", "SourceCredential", map[string]any{"access_key_id": "虚构标识", "secret_access_key": "虚构密钥", "unknown": "值"}, false},
		{"空白凭证不合法", "SourceCredential", map[string]any{"access_key_id": " ", "secret_access_key": "虚构密钥"}, false},
		{"更新null保留凭证", "UpdateSourceCredential", nil, true},
		{"更新空字符串保留凭证", "UpdateSourceCredential", "", true},
		{"更新空对象不能替换凭证", "UpdateSourceCredential", map[string]any{}, false},
		{"配置只能为空对象", "EmptyConfig", map[string]any{}, true},
		{"配置不能为null", "EmptyConfig", nil, false},
		{"配置不得夹带字段", "EmptyConfig", map[string]any{"region": "虚构区域"}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := spec.Components.Schemas[test.schema].Value.VisitJSON(test.value, openapi3.VisitAsRequest())
			if (err == nil) != test.valid {
				t.Fatalf("结构校验结果不符合约定：%v", err)
			}
		})
	}
}

// 内嵌规范的操作标识同时用于授权，不能被生成器改成另一套大小写。
func Test内嵌契约保留原始操作标识(t *testing.T) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	source, err := loader.LoadFromFile("../../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := generated.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	for path, item := range source.Paths.Map() {
		for method, op := range item.Operations() {
			actual := embedded.Paths.Find(path).GetOperation(method)
			if actual == nil || actual.OperationID != op.OperationID {
				t.Errorf("内嵌操作标识发生变化：%s %s", method, path)
			}
		}
	}
}

// Test云账号字段只读契约 防止生成类型误将身份标识作为客户端可写字段。
func Test云账号字段只读契约(t *testing.T) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	spec, err := loader.LoadFromFile("../../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	field := spec.Components.Schemas["Source"].Value.Properties["cloud_account_id"]
	if field == nil || !field.Value.Type.Is("string") || !field.Value.ReadOnly {
		t.Fatal("来源账号必须声明为只读字符串")
	}
	for _, name := range []string{"CreateSourceRequest", "UpdateSourceRequest"} {
		if _, exists := spec.Components.Schemas[name].Value.Properties["cloud_account_id"]; exists {
			t.Fatal("来源输入不得声明云账号 ID")
		}
	}
}
