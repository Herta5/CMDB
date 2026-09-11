// 本文件验证生产与测试数据库的审计用户名 JSON 表达式兼容边界。
package audit

import "testing"

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
