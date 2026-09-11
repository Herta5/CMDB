// 本文件验证管理员迁移命令不会向终端传播配置、连接或数据库底层敏感错误。
package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"

	"cmdb/internal/platform/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestMigrationCommandConnectionFailureOnlyPrintsSafeSummary 捕获真实命令输出，防止 GORM 在错误收敛前泄露数据库定位信息。
func TestMigrationCommandConnectionFailureOnlyPrintsSafeSummary(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestMigrationCommandHelperProcess")
	command.Env = migrationCommandEnvironment(map[string]string{
		"CMDB_MIGRATION_HELPER_PROCESS": "1",
		"DB_HOST":                       "127.0.0.1",
		"DB_PORT":                       "1",
		"DB_NAME":                       "cmdb_review_fixture",
		"DB_MIGRATION_USER":             "review_fixture_admin",
		"DB_MIGRATION_PASSWORD":         "仅用于失败输出测试的虚构密码",
	})
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("不可连接的数据库必须使迁移命令失败")
	}
	if got, want := strings.TrimSpace(string(output)), "数据库迁移失败：无法连接数据库"; got != want {
		t.Fatalf("连接失败时终端只能输出安全摘要，实际为：%s", got)
	}
	for _, forbidden := range []string{
		"review_fixture_admin",
		"cmdb_review_fixture",
		"127.0.0.1",
		"connection refused",
		"dial tcp",
		"socket",
	} {
		if strings.Contains(strings.ToLower(string(output)), strings.ToLower(forbidden)) {
			t.Fatalf("迁移命令输出泄露数据库详情：%s", forbidden)
		}
	}
}

// TestMigrationCommandHelperProcess 在独立进程中调用生产入口，使标准输出、标准错误和 os.Exit 均可观测。
func TestMigrationCommandHelperProcess(t *testing.T) {
	if os.Getenv("CMDB_MIGRATION_HELPER_PROCESS") != "1" {
		return
	}
	main()
}

// TestRunMigrationSilencesSQLFailureLogs 防止迁移查询失败在安全摘要返回前写出 SQL 或数据库底层错误。
func TestRunMigrationSilencesSQLFailureLogs(t *testing.T) {
	var logOutput bytes.Buffer
	testLogger := logger.New(log.New(&logOutput, "", 0), logger.Config{LogLevel: logger.Error})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: testLogger})
	if err != nil {
		t.Fatal("打开迁移日志测试数据库失败")
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })

	err = runMigration(
		context.Background(),
		func() (config.Database, error) { return config.Database{}, nil },
		func(config.Database) (*gorm.DB, error) { return db, nil },
		func(_ context.Context, migrationDB *gorm.DB) error {
			return migrationDB.Exec("SELECT * FROM review_fixture_missing_table").Error
		},
	)
	if err == nil || err.Error() != "数据库迁移失败：结构未更新" {
		t.Fatalf("SQL 失败必须返回固定安全摘要，实际为 %v", err)
	}
	if logOutput.Len() != 0 {
		t.Fatalf("SQL 失败不得写入 GORM 日志，实际为：%s", logOutput.String())
	}
}

// TestRunMigrationSanitizesFailures 防止管理员密码随任一迁移失败路径输出。
func TestRunMigrationSanitizesFailures(t *testing.T) {
	secret := "仅用于测试且不得出现在错误中的迁移密码"
	migrationDB := migrationCommandTestDatabase(t)
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
			open:    func(config.Database) (*gorm.DB, error) { return migrationDB, nil },
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
	migrationDB := migrationCommandTestDatabase(t)
	err := runMigration(
		context.Background(),
		func() (config.Database, error) { return config.Database{}, nil },
		func(config.Database) (*gorm.DB, error) { return migrationDB, nil },
		func(context.Context, *gorm.DB) error { return nil },
	)
	if err != nil {
		t.Fatalf("完整迁移流程应成功：%v", err)
	}
}

// migrationCommandTestDatabase 使用完整 GORM 连接替代不具备 Session 行为的裸结构体替身。
func migrationCommandTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("打开迁移命令测试数据库失败")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("读取迁移命令测试连接失败")
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// migrationCommandEnvironment 隔离真实环境中的数据库变量，确保测试只使用明确虚构的不可连接目标。
func migrationCommandEnvironment(values map[string]string) []string {
	environment := make([]string, 0, len(os.Environ())+len(values))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "CMDB_MIGRATION_HELPER_PROCESS", "DB_HOST", "DB_PORT", "DB_NAME", "DB_MIGRATION_USER", "DB_MIGRATION_PASSWORD":
			continue
		}
		environment = append(environment, entry)
	}
	for name, value := range values {
		environment = append(environment, name+"="+value)
	}
	return environment
}
