// 本文件验证跨模块共享的敏感键分类，避免秘密进入审计详情或非敏感配置。
package security

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestIsSensitiveKeyNormalizesSupportedSpellings 防止大小写、蛇形或短横线写法绕过统一分类。
func TestIsSensitiveKeyNormalizesSupportedSpellings(t *testing.T) {
	for _, key := range []string{
		"Password", "session_token", "access-key", "client_secret", "Authorization", "cipher-text",
		"raw-error-body", "原始错误正文", "ActorID", "previous_owner_user_id",
	} {
		if !IsSensitiveKey(key) {
			t.Fatalf("敏感键 %q 必须被统一分类", key)
		}
	}
	for _, key := range []string{"name", "project_id", "resource_type", "safe_option"} {
		if IsSensitiveKey(key) {
			t.Fatalf("安全业务键 %q 不得被误判为敏感", key)
		}
	}
}

// TestContainsSensitiveKeyFindsNestedArrayValue 防止嵌套数组中的敏感字段绕过非敏感配置检查。
func TestContainsSensitiveKeyFindsNestedArrayValue(t *testing.T) {
	found, err := ContainsSensitiveKey(json.RawMessage(`{"options":[{"safe":"保留"},{"raw_error_body":"虚构错误正文"}]}`))
	if err != nil {
		t.Fatalf("解析嵌套配置失败：%v", err)
	}
	if !found {
		t.Fatal("嵌套数组中的原始错误正文必须被识别")
	}
}

// TestContainsSensitiveKeyRejectsNonContainerJSON 防止标量 JSON 被误当作可安全保存的配置对象。
func TestContainsSensitiveKeyRejectsNonContainerJSON(t *testing.T) {
	const raw = `"虚构令牌"`
	_, err := ContainsSensitiveKey(json.RawMessage(raw))
	if err == nil {
		t.Fatal("非对象或数组 JSON 必须被拒绝")
	}
	if strings.Contains(err.Error(), "虚构令牌") {
		t.Fatal("配置解析错误不得回显原始值")
	}
}
