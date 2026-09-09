// 本文件验证用户密码仅以不可逆哈希形式保存，避免认证信息泄露。
package identity

import "testing"

// TestPasswordHashNeverStoresPlaintext 防止密码被原样写入用户认证记录。
func TestPasswordHashNeverStoresPlaintext(t *testing.T) {
	hash, err := HashPassword("Cmdb-Test-123")
	if err != nil || hash == "Cmdb-Test-123" || !VerifyPassword(hash, "Cmdb-Test-123") {
		t.Fatalf("密码必须安全哈希并可验证，生成哈希是否为空=%t，err=%v", hash == "", err)
	}
}
