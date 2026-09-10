// Package database 验证 CMDB 数据库连接的 PostgreSQL 专用配置。
package database

import (
	"testing"

	"cmdb/internal/platform/config"
	"github.com/jackc/pgx/v5/pgconn"
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
	want := "host='127.0.0.1' port='5432' user='cmdb_user' password='test-password' dbname='cmdb_test' sslmode=disable TimeZone=UTC"
	if got != want {
		t.Fatalf("PostgreSQL DSN 不符合连接约束：got=%q want=%q", got, want)
	}
}

// TestDSNEscapesSpecialCharacterPassword 防止密码中的空白、单引号或反斜杠截断值并注入连接参数。
func TestDSNEscapesSpecialCharacterPassword(t *testing.T) {
	password := `pa ss'word\sslmode=require`

	parsedConfig, err := pgconn.ParseConfig(dsn(config.Database{
		Host:     "127.0.0.1",
		Port:     "5432",
		User:     "cmdb_user",
		Password: password,
		Name:     "cmdb_test",
	}))
	if err != nil {
		t.Fatalf("特殊字符密码生成的 DSN 必须可被 PostgreSQL 驱动解析：%v", err)
	}
	if parsedConfig.Password != password {
		t.Fatalf("特殊字符密码必须完整保留：got=%q want=%q", parsedConfig.Password, password)
	}
	if parsedConfig.TLSConfig != nil {
		t.Fatal("密码内容不得覆盖固定的 sslmode=disable 连接约束")
	}
}
