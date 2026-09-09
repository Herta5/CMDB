// 本文件提供用户密码的 bcrypt 哈希和验证能力，不暴露或记录明文密码。
package identity

import "golang.org/x/crypto/bcrypt"

// HashPassword 使用 bcrypt 生成不可逆密码哈希，调用方只能持久化返回值。
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword 判断明文密码是否与已存储的 bcrypt 哈希匹配，格式异常时返回 false。
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
