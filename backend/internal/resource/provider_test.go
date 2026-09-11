// 本文件验证云平台适配器共用的严格凭证和非敏感配置边界。
package resource

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestDecodeStrictStringObjectRejectsInvalidCredential 防止未知字段、非字符串值或空白必填项进入平台适配器。
func TestDecodeStrictStringObjectRejectsInvalidCredential(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"access_key_id":"id","secret_access_key":"secret","unknown":"x"}`),
		json.RawMessage(`{"access_key_id":1,"secret_access_key":"secret"}`),
		json.RawMessage(`{"access_key_id":"   ","secret_access_key":"secret"}`),
	} {
		_, err := DecodeStrictStringObject(raw, []string{"access_key_id", "secret_access_key"}, nil)
		if !errors.Is(err, ErrInvalidProviderCredential) {
			t.Fatalf("无效凭证必须返回稳定领域错误：%v", err)
		}
	}
}

// TestDecodeStrictStringObjectAcceptsExactRequiredAndOptionalFields 验证平台可安全读取字段白名单内的完整字符串凭证。
func TestDecodeStrictStringObjectAcceptsExactRequiredAndOptionalFields(t *testing.T) {
	values, err := DecodeStrictStringObject(json.RawMessage(`{"access_key_id":"id","secret_access_key":"secret","session_token":"session"}`), []string{"access_key_id", "secret_access_key"}, []string{"session_token"})
	if err != nil {
		t.Fatalf("完整白名单凭证必须通过校验：%v", err)
	}
	if values["access_key_id"] != "id" || values["secret_access_key"] != "secret" || values["session_token"] != "session" {
		t.Fatal("严格凭证解析必须保留已校验的字段值")
	}
}

// TestValidateEmptyConfigRejectsSensitiveNestedValue 防止敏感配置以嵌套短横线字段形式绕过校验。
func TestValidateEmptyConfigRejectsSensitiveNestedValue(t *testing.T) {
	err := ValidateEmptyConfig(json.RawMessage(`{"options":{"access-key":"x"}}`))
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("敏感配置必须被拒绝：%v", err)
	}
}

// TestValidateEmptyConfigRejectsBusinessValue 防止首期未定义的平台扩展配置被提前持久化。
func TestValidateEmptyConfigRejectsBusinessValue(t *testing.T) {
	err := ValidateEmptyConfig(json.RawMessage(`{"options":{"endpoint":"https://example.invalid"}}`))
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("非空配置必须被拒绝：%v", err)
	}
}
