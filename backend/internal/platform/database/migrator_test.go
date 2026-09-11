// 本文件通过隔离数据库验证版本迁移框架，不代替 PostgreSQL 17 对具体约束的集成验收。
package database

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"

	"github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const migrationTestDriver = "cmdb_migration_test_sqlite"

var registerMigrationTestDriver sync.Once

// TestMigrateAdvancesOneVersion 验证版本 1 的结构和版本记录在同一迁移调用中推进到版本 2。
func TestMigrateAdvancesOneVersion(t *testing.T) {
	db := migrationDatabaseAtVersion(t, 1)
	plan := []migration{{version: 2, statement: "CREATE TABLE migration_probe (id INTEGER PRIMARY KEY)"}}

	if err := migrate(context.Background(), db, plan); err != nil {
		t.Fatalf("迁移版本 1 失败：%v", err)
	}
	if version := readTestSchemaVersion(t, db); version != 2 {
		t.Fatalf("迁移后结构版本应为 2，实际为 %d", version)
	}
	if !db.Migrator().HasTable("migration_probe") {
		t.Fatal("迁移必须执行当前版本对应的结构变更")
	}
}

// TestMigrateRollsBackFailedVersion 验证单个版本中任一步骤失败都不会留下结构或版本记录。
func TestMigrateRollsBackFailedVersion(t *testing.T) {
	db := migrationDatabaseAtVersion(t, 1)
	plan := []migration{{version: 2, statement: "CREATE TABLE migration_probe (id INTEGER PRIMARY KEY); INVALID MIGRATION"}}

	err := migrate(context.Background(), db, plan)
	if err == nil || err.Error() != "数据库迁移失败，未完成任何结构变更" {
		t.Fatalf("无效迁移必须返回稳定失败信息，实际为 %v", err)
	}
	if version := readTestSchemaVersion(t, db); version != 1 {
		t.Fatalf("失败迁移不得推进结构版本，实际为 %d", version)
	}
	if db.Migrator().HasTable("migration_probe") {
		t.Fatal("失败迁移不得遗留部分结构变更")
	}
}

// TestMigrateKeepsCurrentVersion 验证已是当前版本时重复执行管理员命令不会改变结构。
func TestMigrateKeepsCurrentVersion(t *testing.T) {
	db := migrationDatabaseAtVersion(t, CurrentSchemaVersion)

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("当前结构重复迁移应幂等成功：%v", err)
	}
	if version := readTestSchemaVersion(t, db); version != CurrentSchemaVersion {
		t.Fatalf("幂等迁移不得改变当前版本，实际为 %d", version)
	}
}

// TestMigrateRejectsUnknownSchemaWithoutVersionTable 防止空库或来源不明的结构被猜测性修改。
func TestMigrateRejectsUnknownSchemaWithoutVersionTable(t *testing.T) {
	db := migrationTestDatabase(t)

	err := Migrate(context.Background(), db)
	if err == nil || err.Error() != "数据库结构不受支持，未执行迁移" {
		t.Fatalf("未知结构必须原样保留并返回稳定提示，实际为 %v", err)
	}
	if db.Migrator().HasTable("schema_migrations") {
		t.Fatal("未知结构不得创建版本表")
	}
}

// TestMatchesLegacySchemaRequiresExactTablesAndColumns 防止额外或缺失字段被误认成可迁移旧结构。
func TestMatchesLegacySchemaRequiresExactTablesAndColumns(t *testing.T) {
	valid := legacySchemaFingerprintRows()
	if !matchesLegacySchemaFingerprint(valid) {
		t.Fatal("精确匹配的旧版表列指纹应被识别")
	}
	missing := append([]schemaColumn(nil), valid[1:]...)
	if matchesLegacySchemaFingerprint(missing) {
		t.Fatal("缺少字段的结构不得被识别为旧版")
	}
	extraColumn := append(append([]schemaColumn(nil), valid...), schemaColumn{TableName: "users", ColumnName: "unknown_column"})
	if matchesLegacySchemaFingerprint(extraColumn) {
		t.Fatal("包含额外字段的结构不得被识别为旧版")
	}
	extraTable := append(append([]schemaColumn(nil), valid...), schemaColumn{TableName: "unknown_table", ColumnName: "id"})
	if matchesLegacySchemaFingerprint(extraTable) {
		t.Fatal("包含额外表的结构不得被识别为旧版")
	}
}

// migrationTestDatabase 注册 PostgreSQL 锁函数的测试等价实现，以真实执行事务和版本写入。
func migrationTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	registerMigrationTestDriver.Do(func() {
		sql.Register(migrationTestDriver, &sqlite3.SQLiteDriver{ConnectHook: func(connection *sqlite3.SQLiteConn) error {
			return connection.RegisterFunc("pg_advisory_xact_lock", func(_ int64) int64 { return 1 }, true)
		}})
	})
	dialector := sqlite.New(sqlite.Config{
		DriverName: migrationTestDriver,
		DSN:        "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared",
	})
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("打开迁移测试数据库失败")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("读取迁移测试连接失败")
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// migrationDatabaseAtVersion 创建带指定版本记录的结构，隔离每个迁移场景。
func migrationDatabaseAtVersion(t *testing.T, version int) *gorm.DB {
	t.Helper()
	db := migrationTestDatabase(t)
	if err := db.Exec("CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)").Error; err != nil {
		t.Fatal("创建迁移版本表失败")
	}
	if err := db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version).Error; err != nil {
		t.Fatal("写入迁移版本失败")
	}
	return db
}

// readTestSchemaVersion 返回隔离数据库中的最高版本。
func readTestSchemaVersion(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var version int
	if err := db.Raw("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version).Error; err != nil {
		t.Fatal("读取迁移版本失败")
	}
	return version
}

// legacySchemaFingerprintRows 使用手工核对的版本 1 字面量，避免测试复用生产指纹形成同源断言。
func legacySchemaFingerprintRows() []schemaColumn {
	legacy := map[string][]string{
		"users":                    {"id", "username", "password_hash", "display_name", "email", "global_role", "status", "last_login_at", "created_at", "updated_at"},
		"projects":                 {"id", "code", "name", "description", "status", "owner_user_id", "created_at", "updated_at"},
		"project_members":          {"id", "project_id", "user_id", "role", "created_at", "updated_at"},
		"audit_logs":               {"id", "actor_id", "project_id", "action", "resource_type", "resource_id", "detail", "request_ip", "created_at"},
		"resource_sources":         {"id", "project_id", "provider", "name", "region", "encrypted_credential", "credential_hint", "config", "enabled", "sync_interval_minutes", "last_sync_at", "next_sync_at", "created_at", "updated_at"},
		"resources_servers":        {"id", "project_id", "source_id", "provider", "resource_type", "external_id", "name", "region", "zone", "cloud_status", "asset_status", "private_ips", "public_ips", "raw_attributes", "first_seen_at", "last_seen_at", "missing_since", "created_at", "updated_at"},
		"resources_databases":      {"id", "project_id", "source_id", "provider", "resource_type", "external_id", "name", "region", "zone", "cloud_status", "asset_status", "engine", "engine_version", "endpoints", "raw_attributes", "first_seen_at", "last_seen_at", "missing_since", "created_at", "updated_at"},
		"resources_load_balancers": {"id", "project_id", "source_id", "provider", "resource_type", "external_id", "name", "region", "zone", "cloud_status", "asset_status", "network_type", "endpoints", "raw_attributes", "first_seen_at", "last_seen_at", "missing_since", "created_at", "updated_at"},
		"sync_jobs":                {"id", "project_id", "source_id", "previous_job_id", "status", "trigger", "statistics", "error_summary", "started_at", "finished_at"},
	}
	rows := make([]schemaColumn, 0)
	for table, columns := range legacy {
		for _, column := range columns {
			rows = append(rows, schemaColumn{TableName: table, ColumnName: column})
		}
	}
	return rows
}
