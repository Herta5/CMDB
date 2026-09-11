// 本文件验证用户名作为稳定对外身份标识时必须满足的字符与长度边界。
package identity

import (
	"strings"
	"testing"
)

// TestValidUsernameOnlyAcceptsASCIIIdentityCharacters 防止用户名包含不稳定的空白、非 ASCII 或分隔符。
func TestValidUsernameOnlyAcceptsASCIIIdentityCharacters(t *testing.T) {
	for _, value := range []string{"admin", "Admin_01", strings.Repeat("a", 64)} {
		if !ValidUsername(value) {
			t.Fatalf("合法用户名被拒绝：%q", value)
		}
	}
	for _, value := range []string{"", "user-name", "user name", "用户", strings.Repeat("a", 65)} {
		if ValidUsername(value) {
			t.Fatalf("非法用户名被接受：%q", value)
		}
	}
}
