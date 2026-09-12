// 本文件定义云平台适配器和共用 JSON 校验，保持平台模块与资源核心的职责边界。
package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"

	"cmdb/internal/security"
)

// ProviderAdapter 是平台模块接入资源核心的唯一契约，不包含项目、审计或生命周期职责。
type ProviderAdapter interface {
	Collector
	ValidateCredential(json.RawMessage) error
	ValidateConfig(json.RawMessage) error
	ResolveCloudAccountID(context.Context, Source, []byte) (string, error)
	ResourceTypes() []string
}

// DecodeStrictStringObject 只接受声明字段集合中的非空字符串凭证，防止未知字段静默进入加密存储。
func DecodeStrictStringObject(raw json.RawMessage, requiredKeys, optionalKeys []string) (map[string]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, ErrInvalidProviderCredential
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, ErrInvalidProviderCredential
	}
	allowed := make(map[string]struct{}, len(requiredKeys)+len(optionalKeys))
	for _, key := range requiredKeys {
		allowed[key] = struct{}{}
	}
	for _, key := range optionalKeys {
		allowed[key] = struct{}{}
	}
	values := make(map[string]string, len(allowed))
	for decoder.More() {
		field, fieldErr := decoder.Token()
		key, isString := field.(string)
		if fieldErr != nil || !isString {
			return nil, ErrInvalidProviderCredential
		}
		if _, exists := allowed[key]; !exists {
			return nil, ErrInvalidProviderCredential
		}
		if _, exists := values[key]; exists {
			return nil, ErrInvalidProviderCredential
		}
		var value string
		if err := decoder.Decode(&value); err != nil || strings.TrimSpace(value) == "" {
			return nil, ErrInvalidProviderCredential
		}
		values[key] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrInvalidProviderCredential
	}
	if err := ensureCredentialJSONEOF(decoder); err != nil {
		return nil, ErrInvalidProviderCredential
	}
	for _, key := range requiredKeys {
		if _, exists := values[key]; !exists {
			return nil, ErrInvalidProviderCredential
		}
	}
	return values, nil
}

// ValidateEmptyConfig 允许缺失或空对象配置，并拒绝任何业务字段和所有嵌套敏感键。
func ValidateEmptyConfig(raw json.RawMessage) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	if raw[0] != '{' {
		return ErrInvalidProviderConfig
	}
	hasSensitiveKey, err := security.ContainsSensitiveKey(raw)
	if err != nil || hasSensitiveKey {
		return ErrInvalidProviderConfig
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil || len(config) != 0 {
		return ErrInvalidProviderConfig
	}
	return nil
}

// ensureCredentialJSONEOF 拒绝凭证对象后的额外 JSON 值，避免不同消费者得到不同语义。
func ensureCredentialJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrInvalidProviderCredential
	}
	return nil
}
