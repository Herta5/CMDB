// Package database 提供 CMDB 数据库连接、结构版本检查和管理员迁移能力。
package database

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// CurrentSchemaVersion 是当前服务能够安全读写的唯一数据库结构版本。
const CurrentSchemaVersion = 2

var errSchemaVersionMismatch = errors.New("数据库结构版本不匹配，请先执行管理员迁移")

// CheckSchemaVersion 只读取版本记录；旧版、超前版和未知结构均阻止服务启动。
func CheckSchemaVersion(ctx context.Context, db *gorm.DB) error {
	var version int
	if err := db.WithContext(ctx).Raw("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version).Error; err != nil || version != CurrentSchemaVersion {
		return errSchemaVersionMismatch
	}
	return nil
}
