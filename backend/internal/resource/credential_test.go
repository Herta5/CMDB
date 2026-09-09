// 本文件验证云凭证加密边界，测试数据均为虚构值且不得进入失败输出。
package resource

import (
	"encoding/base64"
	"testing"
)

// TestCredentialCipherRoundTrip 验证合法密钥可还原原始凭证。
func TestCredentialCipherRoundTrip(t *testing.T) {
	cipher := NewCredentialCipher("deployment-encryption-key-with-enough-entropy")
	plain := []byte(`{"access_key_id":"example","access_key_secret":"example-secret"}`)
	encrypted, err := cipher.Encrypt(plain)
	if err != nil {
		t.Fatal("加密凭证失败")
	}
	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil || string(decrypted) != string(plain) {
		t.Fatal("解密后凭证与原始值不一致")
	}
}

// TestCredentialCipherUsesRandomNonce 防止相同凭证产生可关联的固定密文。
func TestCredentialCipherUsesRandomNonce(t *testing.T) {
	cipher := NewCredentialCipher("deployment-encryption-key-with-enough-entropy")
	first, _ := cipher.Encrypt([]byte("same-credential"))
	second, _ := cipher.Encrypt([]byte("same-credential"))
	if first == second {
		t.Fatal("相同凭证必须使用随机 nonce 生成不同密文")
	}
}

// TestCredentialCipherRejectsWrongKeyAndTampering 验证错误密钥和被篡改密文都不能解密。
func TestCredentialCipherRejectsWrongKeyAndTampering(t *testing.T) {
	encrypted, _ := NewCredentialCipher("first-deployment-key").Encrypt([]byte("credential"))
	if _, err := NewCredentialCipher("different-deployment-key").Decrypt(encrypted); err == nil {
		t.Fatal("错误密钥不得解密凭证")
	}
	tampered, _ := base64.RawStdEncoding.DecodeString(encrypted)
	tampered[len(tampered)-1] ^= 1
	if _, err := NewCredentialCipher("first-deployment-key").Decrypt(base64.RawStdEncoding.EncodeToString(tampered)); err == nil {
		t.Fatal("篡改密文必须被认证失败拒绝")
	}
}

// TestCredentialCipherRejectsEmptyValues 验证空密钥、空明文和非法密文不会形成有效凭证。
func TestCredentialCipherRejectsEmptyValues(t *testing.T) {
	if _, err := NewCredentialCipher("").Encrypt([]byte("credential")); err == nil {
		t.Fatal("空部署密钥必须被拒绝")
	}
	if _, err := NewCredentialCipher("valid-key").Encrypt(nil); err == nil {
		t.Fatal("空凭证必须被拒绝")
	}
	if _, err := NewCredentialCipher("valid-key").Decrypt("invalid"); err == nil {
		t.Fatal("非法密文必须被拒绝")
	}
}
