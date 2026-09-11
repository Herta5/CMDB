// Package config 验证 CMDB 服务启动时的环境配置约束。
package config

import (
	"strings"
	"testing"
)

// TestLoadMigrationDatabaseRequiresAdministratorCredentials 防止管理员迁移误用应用账号或空凭证。
func TestLoadMigrationDatabaseRequiresAdministratorCredentials(t *testing.T) {
	for _, key := range []string{"DB_MIGRATION_USER", "DB_MIGRATION_PASSWORD"} {
		t.Run(key, func(t *testing.T) {
			setMigrationEnvironment(t)
			t.Setenv(key, "")

			_, err := LoadMigrationDatabase()
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("缺少 %s 时必须拒绝管理员迁移，实际为 %v", key, err)
			}
			if strings.Contains(err.Error(), "仅用于测试的迁移密码") {
				t.Fatal("迁移配置错误不得包含管理员密码")
			}
		})
	}
}

// TestLoadMigrationDatabaseOnlyReadsMigrationConnectionSettings 验证迁移命令不依赖应用密码或服务密钥。
func TestLoadMigrationDatabaseOnlyReadsMigrationConnectionSettings(t *testing.T) {
	setMigrationEnvironment(t)
	for _, key := range []string{"DB_USER", "DB_PASSWORD", "JWT_SECRET", "CMDB_ENCRYPTION_KEY", "SERVER_PORT", "GIN_MODE"} {
		t.Setenv(key, "")
	}

	database, err := LoadMigrationDatabase()
	if err != nil {
		t.Fatalf("完整迁移配置应加载成功：%v", err)
	}
	if database.Host != "127.0.0.1" || database.Port != "5432" || database.Name != "cmdb_test" || database.User != "postgres" || database.Password != "仅用于测试的迁移密码" {
		t.Fatal("迁移配置必须使用数据库定位信息和专用管理员凭证")
	}
}

// TestLoadRejectsMissingJWTSecret 防止服务在缺少签名密钥时使用不安全默认值启动。
func TestLoadRejectsMissingJWTSecret(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("JWT_SECRET", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("期望缺少 JWT_SECRET 时返回明确错误，实际为 %v", err)
	}
}

// TestLoadRejectsMissingRequiredEnvironment 防止数据库连接与凭证加密依赖以空值启动。
func TestLoadRejectsMissingRequiredEnvironment(t *testing.T) {
	for _, key := range []string{
		"DB_HOST",
		"DB_PORT",
		"DB_USER",
		"DB_PASSWORD",
		"DB_NAME",
		"CMDB_ENCRYPTION_KEY",
	} {
		t.Run(key, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv(key, "")

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("期望缺少 %s 时返回明确错误，实际为 %v", key, err)
			}
		})
	}
}

// TestLoadReturnsRequiredConfiguration 验证配置加载会保留启动所需的有效环境值。
func TestLoadReturnsRequiredConfiguration(t *testing.T) {
	setRequiredEnvironment(t)

	config, err := Load()
	if err != nil {
		t.Fatalf("期望有效配置加载成功，实际为 %v", err)
	}
	if config.Database.Host != "127.0.0.1" || config.Database.Port != "5432" || config.Database.Name != "cmdb_test" {
		// 失败诊断仅输出非敏感数据库定位信息，避免测试日志泄露密码。
		t.Fatalf("数据库配置未按预期加载：host=%q, port=%q, name=%q", config.Database.Host, config.Database.Port, config.Database.Name)
	}
	if config.JWTSecret != "test-jwt-secret" || config.EncryptionKey != "test-encryption-key" {
		t.Fatal("安全配置未按预期加载")
	}
}

// setRequiredEnvironment 为测试提供除当前场景外均有效的配置，确保断言聚焦单个缺失项。
func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "cmdb_test")
	t.Setenv("DB_PASSWORD", "test-password")
	t.Setenv("DB_NAME", "cmdb_test")
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	t.Setenv("CMDB_ENCRYPTION_KEY", "test-encryption-key")
}

// setMigrationEnvironment 提供独立于应用运行配置的管理员迁移环境。
func setMigrationEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_NAME", "cmdb_test")
	t.Setenv("DB_MIGRATION_USER", "postgres")
	t.Setenv("DB_MIGRATION_PASSWORD", "仅用于测试的迁移密码")
}
