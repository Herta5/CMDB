// 本文件验证显式管理员初始化只作用于空用户库，并限制凭证输入及存储方式。
package main

import (
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"cmdb/internal/identity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestInitializeAdminFromStandardInput 验证命令只持久化哈希，并拒绝第二次初始化。
func TestInitializeAdminFromStandardInput(t *testing.T) {
	db := adminDatabase(t)
	password := adminPassword(t)
	if err := run([]string{"--username", "operator"}, strings.NewReader(password+"\n"), db); err != nil {
		t.Fatalf("初始化失败：%v", err)
	}
	var user identity.User
	if err := db.First(&user).Error; err != nil {
		t.Fatal("初始化后应存在管理员")
	}
	if user.Username != "operator" || user.GlobalRole != "system_admin" || user.Status != "active" || !identity.VerifyPassword(user.PasswordHash, password) {
		t.Fatal("初始化必须创建可登录的系统管理员且只保存密码哈希")
	}
	if err := run([]string{"--username", "another"}, strings.NewReader(adminPassword(t)), db); err == nil {
		t.Fatal("已有用户时必须拒绝再次初始化")
	}
	var count int64
	if err := db.Model(&identity.User{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("重复初始化不得创建或覆盖用户")
	}
}

// TestInitializeAdminRejectsUnsafeInput 防止凭证通过参数泄露或无效输入创建身份。
func TestInitializeAdminRejectsUnsafeInput(t *testing.T) {
	for _, test := range []struct {
		name  string
		args  []string
		input string
	}{
		{"拒绝密码参数", []string{"--username", "operator", "--password", "invalid"}, ""},
		{"拒绝缺失用户名", nil, adminPassword(t)},
		{"拒绝空白用户名", []string{"--username", " "}, adminPassword(t)},
		{"拒绝短横线用户名", []string{"--username", "user-name"}, adminPassword(t)},
		{"拒绝含空格用户名", []string{"--username", "user name"}, adminPassword(t)},
		{"拒绝非 ASCII 用户名", []string{"--username", "用户"}, adminPassword(t)},
		{"拒绝超长用户名", []string{"--username", strings.Repeat("a", 65)}, adminPassword(t)},
		{"拒绝过短密码", []string{"--username", "operator"}, "short"},
		{"拒绝超出 bcrypt 长度的密码", []string{"--username", "operator"}, strings.Repeat("x", 73)},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := adminDatabase(t)
			if err := run(test.args, strings.NewReader(test.input), db); err == nil {
				t.Fatal("不安全输入必须失败")
			}
			var count int64
			if err := db.Model(&identity.User{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("无效输入不得改变空用户库")
			}
		})
	}
}

// TestInitializeAdminRejectsAnyExistingUser 防止在没有管理员但已有普通用户时意外提升系统权限。
func TestInitializeAdminRejectsAnyExistingUser(t *testing.T) {
	db := adminDatabase(t)
	if err := db.Create(&identity.User{ID: 9, Username: "existing", GlobalRole: "user", Status: "active"}).Error; err != nil {
		t.Fatal("准备已有用户失败")
	}
	if err := run([]string{"--username", "operator"}, strings.NewReader(adminPassword(t)), db); err == nil {
		t.Fatal("存在任何用户都必须拒绝初始化")
	}
}

// TestConcurrentInitializeAdminCreatesOnlyOneUser 验证两个并发命令最多建立一个可登录管理员，不覆盖胜出者。
func TestConcurrentInitializeAdminCreatesOnlyOneUser(t *testing.T) {
	// 文件数据库与独立连接允许两个事务真实竞争；WAL 避免读事务阻塞胜出者提交。
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "bootstrap.db")+"?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("创建并发初始化数据库失败")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("读取并发初始化连接失败")
	}
	sqlDB.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&identity.User{}); err != nil {
		t.Fatal("创建并发初始化身份表失败")
	}
	passwords := []string{adminPassword(t), adminPassword(t)}
	names := []string{"first_operator", "second_operator"}
	start := make(chan struct{})
	results := make(chan int, 2)
	for index := range 2 {
		go func() {
			<-start
			if err := run([]string{"--username", names[index]}, strings.NewReader(passwords[index]), db); err != nil {
				results <- -1
				return
			}
			results <- index
		}()
	}
	close(start)
	successes, winner := 0, -1
	for range 2 {
		if result := <-results; result >= 0 {
			successes++
			winner = result
		}
	}
	var users []identity.User
	if err := db.Find(&users).Error; err != nil {
		t.Fatal("读取并发初始化结果失败")
	}
	if successes != 1 || len(users) != 1 {
		t.Fatal("并发初始化必须恰好一次成功且只保留一个用户")
	}
	user := users[0]
	if user.ID != 1 || user.Username != names[winner] || user.GlobalRole != "system_admin" || !identity.VerifyPassword(user.PasswordHash, passwords[winner]) {
		t.Fatal("并发初始化不得覆盖胜出管理员的身份或凭证")
	}
}

// adminDatabase 创建独立数据库，避免管理员初始化验收影响现有数据。
func adminDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("打开测试数据库失败")
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&identity.User{}); err != nil {
		t.Fatal("迁移测试身份表失败")
	}
	return db
}

// adminPassword 运行时产生测试密码，避免固定凭证和哈希进入版本库。
func adminPassword(t *testing.T) string {
	t.Helper()
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		t.Fatal("生成测试密码失败")
	}
	return hex.EncodeToString(value)
}
