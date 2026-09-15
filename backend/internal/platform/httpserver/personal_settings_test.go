// 本文件验证个人设置的本人边界、密码轮换、严格输入与事务审计。
package httpserver_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"cmdb/internal/audit"
	"cmdb/internal/identity"
	"gorm.io/gorm"
)

func Test个人设置只修改本人且密码轮换使旧会话失效(t *testing.T) {
	for _, username := range []string{"operator", "member_a"} {
		t.Run(username, func(t *testing.T) {
			server, password, db := integrationServerWithDatabase(t)
			token := loginUser(t, server, username, password)
			oldSession := loginUser(t, server, username, password)
			var before identity.User
			if err := db.Where("username = ?", username).First(&before).Error; err != nil {
				t.Fatal(err)
			}
			response := integrationRequest(t, server, token, http.MethodPut, "/api/v1/me/profile", map[string]any{"display_name": "  新显示名称  "}, 200)
			var profile map[string]any
			decodeIntegration(t, response, &profile)
			assertNoPublicUserNumericIdentifiers(t, profile)
			if profile["username"] != username || profile["display_name"] != "新显示名称" {
				t.Fatal("个人资料未更新本人显示名称")
			}
			var after identity.User
			db.First(&after, before.ID)
			if after.PasswordHash != before.PasswordHash || after.GlobalRole != before.GlobalRole || after.Status != before.Status || after.Email != before.Email {
				t.Fatal("修改名称不得覆盖认证及权限字段")
			}
			integrationRequest(t, server, oldSession, http.MethodGet, "/api/v1/me", nil, 200)
			nextPassword := strings.Repeat("新", 24)
			integrationRequest(t, server, token, http.MethodPut, "/api/v1/me/password", map[string]any{"current_password": password, "new_password": nextPassword}, 204)
			integrationRequest(t, server, token, http.MethodGet, "/api/v1/me", nil, 401)
			integrationRequest(t, server, oldSession, http.MethodGet, "/api/v1/me", nil, 401)
			integrationRequest(t, server, "", http.MethodPost, "/api/v1/auth/login", map[string]any{"username": username, "password": password}, 401)
			fresh := loginUser(t, server, username, nextPassword)
			integrationRequest(t, server, fresh, http.MethodGet, "/api/v1/me", nil, 200)
			var logs []audit.Log
			if err := db.Where("action = ?", audit.ActionUserUpdated).Find(&logs).Error; err != nil || len(logs) != 2 {
				t.Fatal("资料及密码更新各需一条审计")
			}
			for _, log := range logs {
				detail := string(log.Detail)
				if strings.Contains(detail, password) || strings.Contains(detail, nextPassword) || strings.Contains(detail, before.PasswordHash) {
					t.Fatal("审计不得包含密码或哈希")
				}
			}
		})
	}
}

func Test个人设置拒绝越权字段与无效密码且不产生审计(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	token := loginUser(t, server, "member_a", password)
	for _, test := range []struct{ name, path, body string }{
		{"不接受其他用户名", "profile", `{"display_name":"越权","username":"operator"}`},
		{"不接受角色", "profile", `{"display_name":"越权","global_role":"system_admin"}`},
		{"不接受重复字段", "profile", `{"display_name":"甲","display_name":"乙"}`},
		{"不接受空名称", "profile", `{"display_name":"  "}`},
		{"不接受超长名称", "profile", `{"display_name":"` + strings.Repeat("名", 129) + `"}`},
		{"不接受尾随JSON", "profile", `{"display_name":"甲"}{}`},
		{"不接受空密码", "password", `{"current_password":"","new_password":""}`},
		{"不接受错误旧密码", "password", `{"current_password":"错误的旧密码","new_password":"全新测试密码十二字节"}`},
		{"不接受不足字节", "password", `{"current_password":"` + password + `","new_password":"abc"}`},
		{"不接受超长字节", "password", `{"current_password":"` + password + `","new_password":"` + strings.Repeat("新", 25) + `"}`},
		{"不接受重复密码", "password", `{"current_password":"甲","current_password":"乙","new_password":"全新测试密码十二字节"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			integrationRawRequest(t, server, token, http.MethodPut, "/api/v1/me/"+test.path, test.body, 400)
		})
	}
	for _, path := range []string{"profile", "password"} {
		integrationRawRequest(t, server, "", http.MethodPut, "/api/v1/me/"+path, `{}`, 401)
	}
	var count int64
	db.Model(&audit.Log{}).Where("action = ?", audit.ActionUserUpdated).Count(&count)
	if count != 0 {
		t.Fatal("被拒绝的个人设置不得产生成功审计")
	}
	loginUser(t, server, "member_a", password)
}

func Test个人设置审计失败回滚名称密码和会话(t *testing.T) {
	server, password, db := integrationServerWithDatabase(t)
	token := loginUser(t, server, "member_a", password)
	var before identity.User
	db.Where("username = ?", "member_a").First(&before)
	if err := db.Callback().Create().Before("gorm:create").Register("test:personal_audit_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(errors.New("虚构审计故障"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	integrationRequest(t, server, token, http.MethodPut, "/api/v1/me/profile", map[string]any{"display_name": "不能提交"}, 500)
	integrationRequest(t, server, token, http.MethodPut, "/api/v1/me/password", map[string]any{"current_password": password, "new_password": "不能提交的测试密码"}, 500)
	var after identity.User
	db.First(&after, before.ID)
	if after.DisplayName != before.DisplayName || after.PasswordHash != before.PasswordHash {
		t.Fatal("审计失败必须回滚个人设置")
	}
	integrationRequest(t, server, token, http.MethodGet, "/api/v1/me", nil, 200)
}
