// 本文件集中定义 CMDB 用户名作为稳定对外身份标识时的格式约束。
package identity

// ValidUsername 判断用户名是否可作为稳定的对外身份标识。
func ValidUsername(username string) bool {
	if len(username) == 0 || len(username) > 64 {
		return false
	}
	for _, value := range []byte(username) {
		if (value < 'a' || value > 'z') && (value < 'A' || value > 'Z') &&
			(value < '0' || value > '9') && value != '_' {
			return false
		}
	}
	return true
}
