// 本文件提供凭证、原始错误正文和内部用户引用的统一敏感键分类。
package security

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode"
)

var errInvalidSensitiveJSON = errors.New("敏感字段 JSON 格式无效")

// IsSensitiveKey 判断键名是否属于禁止进入审计详情或非敏感配置的字段。
func IsSensitiveKey(key string) bool {
	normalized := normalizeKey(key)
	_, sensitive := sensitiveKeys[normalized]
	return sensitive
}

// ContainsSensitiveKey 在对象或数组 JSON 中递归检查是否存在统一分类的敏感键。
func ContainsSensitiveKey(raw json.RawMessage) (bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return false, errInvalidSensitiveJSON
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return false, errInvalidSensitiveJSON
	}
	switch typed := value.(type) {
	case map[string]any, []any:
		return containsSensitiveValue(typed), nil
	default:
		return false, errors.New("敏感字段 JSON 必须为对象或数组")
	}
}

// normalizeKey 统一大小写、蛇形和短横线，避免同一语义使用不同拼写绕过过滤。
func normalizeKey(key string) string {
	return string([]rune(stringMap(key)))
}

// stringMap 只保留字母和数字，使不同分隔符的键归入同一比较空间。
func stringMap(key string) []rune {
	value := make([]rune, 0, len(key))
	for _, char := range key {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			value = append(value, unicode.ToLower(char))
		}
	}
	return value
}

// containsSensitiveValue 递归检查已解码集合，不读取或返回任何字段值。
func containsSensitiveValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if IsSensitiveKey(key) || containsSensitiveValue(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsSensitiveValue(child) {
				return true
			}
		}
	}
	return false
}

// ensureJSONEOF 拒绝一个 JSON 值之后附带的任意内容，避免校验与后续解码含义不同。
func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errInvalidSensitiveJSON
	}
	return nil
}

var sensitiveKeys = map[string]struct{}{
	"actorid": {}, "userid": {}, "targetuserid": {}, "owneruserid": {}, "previousowneruserid": {},
	"password": {}, "passwordhash": {},
	"token": {}, "sessiontoken": {}, "accesstoken": {}, "refreshtoken": {},
	"apikey": {}, "accesskey": {}, "accesskeyid": {}, "accesskeysecret": {},
	"secret": {}, "secretkey": {}, "secretaccesskey": {}, "clientsecret": {},
	"credential": {}, "encryptedcredential": {}, "authorization": {},
	"ciphertext": {}, "cmdbencryptionkey": {},
	"rawerror": {}, "rawerrorbody": {}, "rawerrorresponse": {}, "errorbody": {}, "errorresponse": {},
	"requestbody": {}, "responsebody": {}, "原始错误正文": {},
}
