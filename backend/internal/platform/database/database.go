// Package database 负责创建 CMDB 共享的数据库连接。
package database

import (
	"fmt"

	"cmdb/internal/platform/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Open 根据已校验的数据库配置创建 PostgreSQL 连接。
// 配置校验由 config.Load 负责，此处仅保留连接构造职责，避免启动入口混入基础设施细节。
func Open(databaseConfig config.Database) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn(databaseConfig)), &gorm.Config{TranslateError: true})
}

// dsn 按 PostgreSQL 驱动格式构造连接串，并固定首期单机部署的 TLS 与时区约束。
// 密码只传给驱动建立连接，调用方不得记录此返回值，以免泄露凭证。
func dsn(databaseConfig config.Database) string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		databaseConfig.Host,
		databaseConfig.Port,
		databaseConfig.User,
		databaseConfig.Password,
		databaseConfig.Name,
	)
}
