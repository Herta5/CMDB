// 本文件通过隔离数据库验证用户仓储的实际持久化和错误传播行为。
package identity

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newUserRepositoryTestDB 创建只供单个测试使用的内存数据库，避免依赖外部 MySQL 服务。
func newUserRepositoryTestDB(t *testing.T) (*gorm.DB, *sql.DB) {
	t.Helper()

	sqlDB, err := sql.Open("sqlite3", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("创建用户仓储测试数据库失败：%v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	db, err := gorm.Open(sqlite.Dialector{Conn: sqlDB}, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("打开用户仓储测试数据库失败：%v", err)
	}
	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatalf("创建新版用户表失败：%v", err)
	}
	return db, sqlDB
}

// TestUserRepositoryCreatesAndFindsUserByUsername 验证仓储把用户写入新版 users 表并可按用户名读取。
func TestUserRepositoryCreatesAndFindsUserByUsername(t *testing.T) {
	db, _ := newUserRepositoryTestDB(t)
	repository := NewUserRepository(db)
	user := &User{
		Username:     "repository-user",
		PasswordHash: "bcrypt-hash-for-test-only",
		DisplayName:  "仓储测试用户",
		GlobalRole:   GlobalRoleUser,
		Status:       "active",
	}

	if err := repository.Create(context.Background(), user); err != nil {
		t.Fatalf("创建用户失败：%v", err)
	}
	if user.ID == 0 {
		t.Fatal("创建用户后必须获得数据库分配的用户标识")
	}

	var storedCount int64
	if err := db.Table("users").Where("username = ?", user.Username).Count(&storedCount).Error; err != nil {
		t.Fatalf("查询新版 users 表失败：%v", err)
	}
	if storedCount != 1 {
		t.Fatalf("新版 users 表中的用户数量错误：got=%d want=1", storedCount)
	}

	found, err := repository.FindByUsername(context.Background(), user.Username)
	if err != nil {
		t.Fatalf("按用户名查询用户失败：%v", err)
	}
	if found.ID != user.ID || found.Username != user.Username || found.DisplayName != user.DisplayName {
		t.Fatalf("按用户名查询到的用户与写入记录不一致：got=(id=%d username=%q display_name=%q)", found.ID, found.Username, found.DisplayName)
	}
}

// TestUserRepositoryPropagatesDatabaseError 验证连接不可用时仓储将数据库错误返回给调用方。
func TestUserRepositoryPropagatesDatabaseError(t *testing.T) {
	db, sqlDB := newUserRepositoryTestDB(t)
	repository := NewUserRepository(db)
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("关闭测试数据库失败：%v", err)
	}

	_, err := repository.FindByUsername(context.Background(), "unavailable-user")
	if err == nil {
		t.Fatal("数据库连接关闭后查询必须返回错误")
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("数据库连接错误不能被错误转换为未找到用户：%v", err)
	}
}
