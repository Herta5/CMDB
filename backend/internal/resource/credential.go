// 本文件提供接入源凭证的认证加密，明文只在采集任务内短暂存在。
package resource

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

var errInvalidCredential = errors.New("凭证加密数据无效")

// CredentialCipher 使用部署密钥派生的 AES-256-GCM 密钥保护云凭证。
type CredentialCipher struct {
	key   [sha256.Size]byte
	valid bool
}

// NewCredentialCipher 由部署环境密钥创建加密器，不在对象中保留原始密钥字符串。
func NewCredentialCipher(deploymentKey string) *CredentialCipher {
	value := &CredentialCipher{}
	if deploymentKey != "" {
		value.key = sha256.Sum256([]byte(deploymentKey))
		value.valid = true
	}
	return value
}

// aead 创建带完整性认证的分组加密器，空部署密钥必须拒绝使用。
func (c *CredentialCipher) aead() (cipher.AEAD, error) {
	if c == nil || !c.valid {
		return nil, errInvalidCredential
	}
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return nil, errInvalidCredential
	}
	return cipher.NewGCM(block)
}

// Encrypt 使用随机 nonce 加密非空凭证，并编码为数据库可安全保存的文本。
func (c *CredentialCipher) Encrypt(plain []byte) (string, error) {
	if len(plain) == 0 {
		return "", errInvalidCredential
	}
	aead, err := c.aead()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", errInvalidCredential
	}
	sealed := aead.Seal(nonce, nonce, plain, nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Decrypt 校验并解密凭证，所有格式、密钥和完整性错误返回同一脱敏错误。
func (c *CredentialCipher) Decrypt(encoded string) ([]byte, error) {
	aead, err := c.aead()
	if err != nil {
		return nil, err
	}
	sealed, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(sealed) <= aead.NonceSize() {
		return nil, errInvalidCredential
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], nil)
	if err != nil {
		return nil, errInvalidCredential
	}
	return plain, nil
}
