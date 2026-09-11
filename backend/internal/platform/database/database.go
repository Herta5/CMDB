// Package database 负责创建 CMDB 共享的数据库连接。
package database

import (
	"fmt"
	"strings"

	"cmdb/internal/platform/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open 根据已校验的数据库配置创建 PostgreSQL 连接。
// 配置校验由 config.Load 负责，此处仅保留连接构造职责，避免启动入口混入基础设施细节。
func Open(databaseConfig config.Database) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn(databaseConfig)), &gorm.Config{TranslateError: true})
}

// OpenMigration 在驱动创建连接前关闭 GORM 输出，防止连接探测失败泄露管理员账号和数据库地址。
// 管理员命令只向终端返回自身定义的中文安全摘要，底层连接和 SQL 错误不得旁路输出。
func OpenMigration(databaseConfig config.Database) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn(databaseConfig)), &gorm.Config{
		TranslateError: true,
		Logger:         logger.Default.LogMode(logger.Silent),
	})
}

// dsn 按 PostgreSQL 驱动格式构造连接串，并固定首期单机部署的 TLS 与时区约束。
// 密码只传给驱动建立连接，调用方不得记录此返回值，以免泄露凭证。
func dsn(databaseConfig config.Database) string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		dsnValue(databaseConfig.Host),
		dsnValue(databaseConfig.Port),
		dsnValue(databaseConfig.User),
		dsnValue(databaseConfig.Password),
		dsnValue(databaseConfig.Name),
	)
}

// dsnValue 使用 PostgreSQL 键值 DSN 的单引号字面量，并转义会改变字面量边界的字符。
// 所有动态配置都必须经过该函数，避免环境变量中的空白或参数片段改变实际连接配置。
func dsnValue(value string) string {
	escapedValue := strings.ReplaceAll(value, `\`, `\\`)
	escapedValue = strings.ReplaceAll(escapedValue, `'`, `\'`)
	return "'" + escapedValue + "'"
}
