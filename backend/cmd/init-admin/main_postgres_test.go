//go:build postgres

// 本文件使用真实 PostgreSQL 17 验证管理员初始化与后续用户创建共享同一自增标识契约。
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cmdb/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	initAdminPostgresDSNEnvironment = "CMDB_POSTGRES_TEST_DSN"
	initAdminPostgresAppUser        = "cmdb"
	initAdminPostgresAppPassword    = "cmdb-app-integration-only"
)

var initAdminPostgresDatabaseSequence atomic.Uint64

// TestPostgreSQLInitializeAdminAllowsFirstManagedUser 防止首个管理员占用 ID 后未推进 identity sequence。
func TestPostgreSQLInitializeAdminAllowsFirstManagedUser(t *testing.T) {
	db := newInitAdminPostgresDatabase(t)
	if err := run([]string{"--username", "operator"}, strings.NewReader(adminPassword(t)), db); err != nil {
		t.Fatalf("初始化管理员失败：%v", err)
	}

	service := identity.NewService(identity.NewUserRepository(db), "integration-jwt-secret")
	created, err := service.CreateUser(context.Background(), identity.CreateUserInput{
		Username: "first_managed_user", Password: adminPassword(t), DisplayName: "首个普通用户", GlobalRole: identity.GlobalRoleUser, Status: "active",
	})
	if err != nil {
		t.Fatalf("管理员初始化后的首个普通用户必须一次创建成功：%v", err)
	}
	if created.ID != 2 || created.Username != "first_managed_user" {
		t.Fatalf("用户标识必须由 PostgreSQL identity sequence 连续分配：id=%d username=%q", created.ID, created.Username)
	}
}

// TestPostgreSQLConcurrentInitializeCreatesOnlyOneAdmin 验证空库判断与创建在并发初始化时保持原子。
func TestPostgreSQLConcurrentInitializeCreatesOnlyOneAdmin(t *testing.T) {
	db := newInitAdminPostgresDatabase(t)
	if err := db.Callback().Query().After("gorm:query").Register("test:delay_empty_user_count", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" && strings.Contains(strings.ToUpper(tx.Statement.SQL.String()), "COUNT(") {
			time.Sleep(200 * time.Millisecond)
		}
	}); err != nil {
		t.Fatal("注册并发初始化交错控制失败")
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	password := adminPassword(t)
	for _, username := range []string{"first_operator", "second_operator"} {
		go func(name string) {
			<-start
			results <- run([]string{"--username", name}, strings.NewReader(password), db)
		}(username)
	}
	close(start)
	successes := 0
	deadline := time.After(10 * time.Second)
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				successes++
			}
		case <-deadline:
			t.Fatal("并发初始化不得因表锁发生死锁")
		}
	}
	var count int64
	if err := db.Model(&identity.User{}).Count(&count).Error; err != nil {
		t.Fatal("读取并发初始化结果失败")
	}
	if successes != 1 || count != 1 {
		t.Fatalf("并发初始化必须只创建一个管理员：successes=%d users=%d", successes, count)
	}
}

// newInitAdminPostgresDatabase 创建应用账号连接的隔离数据库，并安装当前首次初始化结构。
func newInitAdminPostgresDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	baseDSN := os.Getenv(initAdminPostgresDSNEnvironment)
	if baseDSN == "" {
		t.Fatal("未设置 CMDB_POSTGRES_TEST_DSN，请使用 scripts/test-backend.sh 执行 PostgreSQL 17 集成测试")
	}
	adminConfiguration, err := pgx.ParseConfig(baseDSN)
	if err != nil {
		t.Fatal("解析 PostgreSQL 集成测试连接配置失败")
	}
	adminDatabase := openInitAdminPostgres(t, adminConfiguration, false, false)
	var serverVersion int
	if err := adminDatabase.Raw("SHOW server_version_num").Scan(&serverVersion).Error; err != nil || serverVersion/10000 != 17 {
		t.Fatal("管理员初始化集成测试必须使用 PostgreSQL 17")
	}

	name := fmt.Sprintf("cmdb_init_admin_it_%d_%d", os.Getpid(), initAdminPostgresDatabaseSequence.Add(1))
	quotedName := pgx.Identifier{name}.Sanitize()
	if err := adminDatabase.Exec("CREATE DATABASE " + quotedName).Error; err != nil {
		t.Fatal("创建管理员初始化隔离数据库失败")
	}
	var applicationDatabase *gorm.DB
	t.Cleanup(func() {
		if applicationDatabase != nil {
			if sqlDatabase, closeErr := applicationDatabase.DB(); closeErr == nil {
				_ = sqlDatabase.Close()
			}
		}
		if dropErr := adminDatabase.Exec("DROP DATABASE " + quotedName + " WITH (FORCE)").Error; dropErr != nil {
			t.Error("清理管理员初始化隔离数据库失败")
		}
		if sqlDatabase, closeErr := adminDatabase.DB(); closeErr == nil {
			_ = sqlDatabase.Close()
		}
	})
	if err := adminDatabase.Exec("GRANT CONNECT ON DATABASE " + quotedName + " TO " + pgx.Identifier{initAdminPostgresAppUser}.Sanitize()).Error; err != nil {
		t.Fatal("授权应用账号连接隔离数据库失败")
	}

	targetConfiguration := adminConfiguration.Copy()
	targetConfiguration.Database = name
	installInitAdminSchema(t, targetConfiguration)
	appConfiguration := targetConfiguration.Copy()
	appConfiguration.User = initAdminPostgresAppUser
	appConfiguration.Password = initAdminPostgresAppPassword
	applicationDatabase = openInitAdminPostgres(t, appConfiguration, false, true)
	return applicationDatabase
}

// installInitAdminSchema 通过简单协议一次执行当前空库结构脚本。
func installInitAdminSchema(t *testing.T, configuration *pgx.ConnConfig) {
	t.Helper()
	content, err := os.ReadFile("../../database/init/002_schema.sql")
	if err != nil {
		t.Fatal("读取 PostgreSQL 当前空库结构失败")
	}
	statement := strings.Replace(string(content), `\set ON_ERROR_STOP on`, "", 1)
	db := openInitAdminPostgres(t, configuration, true, false)
	if err := db.Exec(statement).Error; err != nil {
		t.Fatal("执行 PostgreSQL 当前空库结构失败")
	}
	if sqlDatabase, closeErr := db.DB(); closeErr == nil {
		_ = sqlDatabase.Close()
	}
}

// openInitAdminPostgres 使用指定身份连接测试数据库，必要时启用 GORM 生产错误转换。
func openInitAdminPostgres(t *testing.T, configuration *pgx.ConnConfig, simpleProtocol, translateError bool) *gorm.DB {
	t.Helper()
	connectionConfiguration := configuration.Copy()
	if simpleProtocol {
		connectionConfiguration.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	}
	sqlDatabase := stdlib.OpenDB(*connectionConfiguration)
	dialector := postgres.New(postgres.Config{Conn: sqlDatabase})
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), TranslateError: translateError})
	if err != nil {
		_ = sqlDatabase.Close()
		t.Fatal("连接 PostgreSQL 集成测试数据库失败")
	}
	if err := sqlDatabase.Ping(); err != nil {
		_ = sqlDatabase.Close()
		t.Fatal("探测 PostgreSQL 集成测试数据库失败")
	}
	return db
}

// TestPostgreSQLInitializeAdminRequiresDSN 验证显式选择 PostgreSQL 测试时缺少环境配置必须失败，不能跳过后虚假通过。
func TestPostgreSQLInitializeAdminRequiresDSN(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestPostgreSQLInitializeAdminAllowsFirstManagedUser$", "-test.count=1")
	command.Env = append(os.Environ(), "CMDB_POSTGRES_TEST_DSN=")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "未设置 CMDB_POSTGRES_TEST_DSN") {
		t.Fatalf("缺少 PostgreSQL 测试连接必须明确失败，实际错误：%v，输出：%s", err, output)
	}
}
