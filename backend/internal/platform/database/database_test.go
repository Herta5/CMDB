// Package database 验证 CMDB 数据库连接的 PostgreSQL 专用配置。
package database

import (
	"testing"

	"cmdb/internal/platform/config"
)

// TestDSNUsesPostgreSQLConnectionOptions 防止连接串沿用旧版格式或遗漏 PostgreSQL 部署约束。
func TestDSNUsesPostgreSQLConnectionOptions(t *testing.T) {
	databaseConfig := config.Database{
		Host:     "127.0.0.1",
		Port:     "5432",
		User:     "cmdb_user",
		Password: "test-password",
		Name:     "cmdb_test",
	}

	got := dsn(databaseConfig)
	want := "host=127.0.0.1 port=5432 user=cmdb_user password=test-password dbname=cmdb_test sslmode=disable TimeZone=UTC"
	if got != want {
		t.Fatalf("PostgreSQL DSN 不符合连接约束：got=%q want=%q", got, want)
	}
}
