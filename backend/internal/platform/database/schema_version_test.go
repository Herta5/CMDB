// 本文件验证 CMDB 服务只允许与当前代码精确匹配的数据库结构启动。
package database

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestCheckSchemaVersionRejectsMissingVersionTable 防止未初始化或未知结构绕过启动门禁。
func TestCheckSchemaVersionRejectsMissingVersionTable(t *testing.T) {
	db := schemaVersionDatabase(t)

	err := CheckSchemaVersion(context.Background(), db)
	if err == nil || err.Error() != "数据库结构版本不匹配，请先执行管理员迁移" {
		t.Fatalf("版本表不存在时必须返回稳定迁移提示，实际为 %v", err)
	}
}

// TestCheckSchemaVersionRequiresExactVersion 防止旧版或超前结构被当前服务误用。
func TestCheckSchemaVersionRequiresExactVersion(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("结构版本%d", version), func(t *testing.T) {
			db := schemaVersionDatabase(t)
			if err := db.Exec("CREATE TABLE schema_migrations (version INTEGER NOT NULL)").Error; err != nil {
				t.Fatal("创建测试版本表失败")
			}
			if err := db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version).Error; err != nil {
				t.Fatal("写入测试结构版本失败")
			}

			err := CheckSchemaVersion(context.Background(), db)
			if version == CurrentSchemaVersion && err != nil {
				t.Fatalf("当前结构版本应通过：%v", err)
			}
			if version != CurrentSchemaVersion && (err == nil || !strings.Contains(err.Error(), "请先执行管理员迁移")) {
				t.Fatalf("结构版本 %d 不应允许应用启动，实际为 %v", version, err)
			}
		})
	}
}

// schemaVersionDatabase 创建不含业务结构的隔离数据库，测试不会访问真实 PostgreSQL。
func schemaVersionDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("打开测试数据库失败")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("读取测试数据库连接失败")
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
