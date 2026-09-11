// 本文件验证管理员迁移命令不会向终端传播配置、连接或数据库底层敏感错误。
package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cmdb/internal/platform/config"
	"gorm.io/gorm"
)

// TestRunMigrationSanitizesFailures 防止管理员密码随任一迁移失败路径输出。
func TestRunMigrationSanitizesFailures(t *testing.T) {
	secret := "仅用于测试且不得出现在错误中的迁移密码"
	tests := []struct {
		name    string
		load    migrationConfigLoader
		open    migrationDatabaseOpener
		migrate schemaMigrator
		want    string
	}{
		{
			name:    "配置失败",
			load:    func() (config.Database, error) { return config.Database{}, errors.New(secret) },
			open:    func(config.Database) (*gorm.DB, error) { return nil, nil },
			migrate: func(context.Context, *gorm.DB) error { return nil },
			want:    "数据库迁移失败：请检查管理员迁移配置",
		},
		{
			name:    "连接失败",
			load:    func() (config.Database, error) { return config.Database{Password: secret}, nil },
			open:    func(config.Database) (*gorm.DB, error) { return nil, errors.New(secret) },
			migrate: func(context.Context, *gorm.DB) error { return nil },
			want:    "数据库迁移失败：无法连接数据库",
		},
		{
			name:    "执行失败",
			load:    func() (config.Database, error) { return config.Database{Password: secret}, nil },
			open:    func(config.Database) (*gorm.DB, error) { return &gorm.DB{}, nil },
			migrate: func(context.Context, *gorm.DB) error { return errors.New(secret) },
			want:    "数据库迁移失败：结构未更新",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := runMigration(context.Background(), test.load, test.open, test.migrate)
			if err == nil || err.Error() != test.want {
				t.Fatalf("迁移命令错误不符合安全契约，实际为 %v", err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatal("迁移命令错误不得泄露管理员密码")
			}
		})
	}
}

// TestRunMigrationReportsSuccess 验证三个阶段均成功时命令返回成功。
func TestRunMigrationReportsSuccess(t *testing.T) {
	err := runMigration(
		context.Background(),
		func() (config.Database, error) { return config.Database{}, nil },
		func(config.Database) (*gorm.DB, error) { return &gorm.DB{}, nil },
		func(context.Context, *gorm.DB) error { return nil },
	)
	if err != nil {
		t.Fatalf("完整迁移流程应成功：%v", err)
	}
}
