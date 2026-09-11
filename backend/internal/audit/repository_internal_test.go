// 本文件验证生产与测试数据库的审计用户名 JSON 表达式兼容边界。
package audit

import "testing"

// TestSanitizeMapUsesSharedSensitiveClassification 防止审计保留一份会落后的敏感键名单。
func TestSanitizeMapUsesSharedSensitiveClassification(t *testing.T) {
	value := sanitizeMap(map[string]any{
		"raw-error-body": "虚构平台错误正文",
		"safe":           "保留",
	})
	if _, exists := value["raw-error-body"]; exists {
		t.Fatal("审计清理必须复用共享分类删除原始错误正文")
	}
	if value["safe"] != "保留" {
		t.Fatal("审计清理必须保留安全字段")
	}
}

// TestActorUsernameExpressionByDialect 防止 PostgreSQL 查询误用 SQLite 的 JSON 函数。
func TestActorUsernameExpressionByDialect(t *testing.T) {
	for _, tt := range []struct{ dialect, want string }{
		{"postgres", "COALESCE(users.username, audit_logs.detail ->> 'actor_username')"},
		{"sqlite", "COALESCE(users.username, json_extract(audit_logs.detail, '$.actor_username'))"},
	} {
		t.Run(tt.dialect, func(t *testing.T) {
			if got := actorUsernameExpression(tt.dialect); got != tt.want {
				t.Fatalf("操作者筛选必须使用对应数据库的 JSON 提取语法：得到 %q，期望 %q", got, tt.want)
			}
		})
	}
}

// TestEffectiveResourceIDExpressionByDialect 验证用户对象查询在两种数据库中使用同一快照与纯数字边界。
func TestEffectiveResourceIDExpressionByDialect(t *testing.T) {
	for _, tt := range []struct{ dialect, want string }{
		{"postgres", "CASE WHEN audit_logs.resource_type IN ('user', 'project_member') AND audit_logs.resource_id ~ '^[0-9]+$' THEN CASE WHEN jsonb_typeof(audit_logs.detail::jsonb -> 'target_username') = 'string' THEN audit_logs.detail ->> 'target_username' ELSE '' END ELSE audit_logs.resource_id END"},
		{"sqlite", "CASE WHEN audit_logs.resource_type IN ('user', 'project_member') AND audit_logs.resource_id <> '' AND audit_logs.resource_id NOT GLOB '*[^0-9]*' THEN CASE WHEN json_type(audit_logs.detail, '$.target_username') = 'text' THEN json_extract(audit_logs.detail, '$.target_username') ELSE '' END ELSE audit_logs.resource_id END"},
	} {
		t.Run(tt.dialect, func(t *testing.T) {
			if got := effectiveResourceIDExpression(tt.dialect); got != tt.want {
				t.Fatalf("公开对象标识筛选必须使用对应数据库的数字与字符串快照判定：得到 %q，期望 %q", got, tt.want)
			}
		})
	}
}
