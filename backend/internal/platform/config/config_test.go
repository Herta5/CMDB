// Package config 验证 CMDB 服务启动时的环境配置约束。
package config

import (
	"strings"
	"testing"
)

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

// TestLoadExecutionDefaults 验证未设置运行参数时使用有界默认值。
func TestLoadExecutionDefaults(t *testing.T) {
	setRequiredEnvironment(t)
	for _, name := range []string{"CMDB_SYNC_MAX_CONCURRENT", "CMDB_SYNC_TIMEOUT_SECONDS", "DB_MAX_OPEN_CONNS", "DB_MAX_IDLE_CONNS", "DB_CONN_MAX_LIFETIME_SECONDS"} {
		t.Setenv(name, "")
	}
	configuration, err := Load()
	if err != nil {
		t.Fatal("加载默认运行参数失败")
	}
	if configuration.Sync.MaxConcurrent != 4 || configuration.Sync.Timeout.Seconds() != 900 || configuration.Database.MaxOpenConns != 20 || configuration.Database.MaxIdleConns != 5 || configuration.Database.ConnMaxLifetime.Seconds() != 1800 {
		t.Fatal("默认运行参数未限制同步和连接池")
	}
}

// TestLoadRejectsUnsafeExecutionLimits 验证错误参数不会静默回退或将输入值泄露到错误中。
func TestLoadRejectsUnsafeExecutionLimits(t *testing.T) {
	for _, item := range []struct{ name, value string }{
		{"CMDB_SYNC_MAX_CONCURRENT", "0"}, {"CMDB_SYNC_MAX_CONCURRENT", "65"},
		{"CMDB_SYNC_TIMEOUT_SECONDS", "0"}, {"CMDB_SYNC_TIMEOUT_SECONDS", "86401"},
		{"DB_MAX_OPEN_CONNS", "0"}, {"DB_MAX_IDLE_CONNS", "21"},
		{"DB_CONN_MAX_LIFETIME_SECONDS", "-1"}, {"CMDB_SYNC_MAX_CONCURRENT", "secret-test-input"},
	} {
		t.Run(item.name+"/"+item.value, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv(item.name, item.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), item.name) {
				t.Fatal("非法运行参数必须拒绝启动并指明参数名")
			}
			if strings.Contains(err.Error(), "secret-test-input") {
				t.Fatal("运行配置错误不得回显原始输入")
			}
		})
	}
}
