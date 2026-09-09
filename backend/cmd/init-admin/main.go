// Command init-admin 供部署操作者显式初始化空用户库，不参与服务自动启动或公开 HTTP 接口。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"cmdb/internal/identity"
	"cmdb/internal/platform/config"
	"cmdb/internal/platform/database"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// main 只输出固定结果；数据库和密码处理失败均不打印底层内容，避免泄露认证材料。
func main() {
	configuration, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败：请检查部署环境配置")
		os.Exit(1)
	}
	db, err := database.Open(configuration.Database)
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败：无法连接数据库")
		os.Exit(1)
	}
	if err := run(os.Args[1:], os.Stdin, db); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "系统管理员初始化成功")
}

// run 限制用户名参数和标准输入密码，只能向空用户库创建第一个系统管理员。
func run(args []string, input io.Reader, db *gorm.DB) error {
	flags := flag.NewFlagSet("init-admin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	username := flags.String("username", "", "首次系统管理员用户名")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("初始化参数无效：只接受 --username，密码必须从标准输入提供")
	}
	name := strings.TrimSpace(*username)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return errors.New("初始化用户名必须为 1 至 64 个字符")
	}
	// 限制读取长度，拒绝超出 bcrypt 72 字节边界的密码；允许管道末尾单个换行。
	value, err := io.ReadAll(io.LimitReader(input, 75))
	if err != nil {
		return errors.New("读取初始化密码失败")
	}
	password := strings.TrimSuffix(strings.TrimSuffix(string(value), "\n"), "\r")
	if len(password) < 12 || len(password) > 72 || strings.ContainsAny(password, "\r\n") {
		return errors.New("初始化密码必须为 12 至 72 字节且不能包含换行")
	}
	hash, err := identity.HashPassword(password)
	if err != nil {
		return errors.New("生成初始化密码哈希失败")
	}
	// 关闭此敏感写入的 SQL 日志，失败时也不得输出密码哈希；固定首个 ID 使并发初始化最多成功一次。
	return db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&identity.User{}).Count(&count).Error; err != nil {
			return errors.New("初始化失败：请先完成新版数据库迁移")
		}
		if count != 0 {
			return errors.New("初始化被拒绝：用户库非空")
		}
		user := identity.User{ID: 1, Username: name, PasswordHash: hash, DisplayName: name, GlobalRole: identity.GlobalRoleSystemAdmin, Status: "active"}
		if err := tx.Create(&user).Error; err != nil {
			return errors.New("管理员初始化失败，未覆盖已有身份")
		}
		return nil
	})
}
