// Package database 负责创建 CMDB 共享的数据库连接。
package database

import (
	"fmt"

	"github-cmdb/internal/platform/config"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Open 根据已校验的数据库配置创建 MySQL 连接。
// 配置校验由 config.Load 负责，此处仅保留连接构造职责，避免启动入口混入基础设施细节。
func Open(databaseConfig config.Database) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		databaseConfig.User,
		databaseConfig.Password,
		databaseConfig.Host,
		databaseConfig.Port,
		databaseConfig.Name,
	)

	return gorm.Open(mysql.Open(dsn), &gorm.Config{})
}
