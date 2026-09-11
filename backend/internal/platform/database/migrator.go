// Package database 提供由数据库管理员显式执行的结构迁移入口。
package database

import (
	"context"
	"embed"
	"errors"

	"gorm.io/gorm"
)

const migrationAdvisoryLockID int64 = 0x434D444200000002

var (
	errUnsupportedSchema = errors.New("数据库结构不受支持，未执行迁移")
	errMigrationFailed   = errors.New("数据库迁移失败，未完成任何结构变更")
)

//go:embed migrations/002_p0_safety.sql
var migrationFiles embed.FS

type migration struct {
	version   int
	statement string
}

type schemaColumn struct {
	TableName  string `gorm:"column:table_name"`
	ColumnName string `gorm:"column:column_name"`
}

// legacySchemaColumns 是唯一允许自动升级的无版本旧结构指纹。
// 只识别项目当前版本 1 的完整表列集合，避免猜测性修改未知数据库。
var legacySchemaColumns = map[string][]string{
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

// Migrate 在单个管理员事务中串行执行尚未应用的结构版本。
func Migrate(ctx context.Context, db *gorm.DB) error {
	statement, err := migrationFiles.ReadFile("migrations/002_p0_safety.sql")
	if err != nil {
		return errMigrationFailed
	}
	return migrate(ctx, db, []migration{{version: 2, statement: string(statement)}})
}

func migrate(ctx context.Context, db *gorm.DB, plan []migration) error {
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 事务级锁在提交或回滚时自动释放，使多个管理员命令只能串行推进结构。
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", migrationAdvisoryLockID).Error; err != nil {
			return err
		}
		version, hasVersionTable, err := migrationVersion(tx)
		if err != nil {
			return err
		}
		if !hasVersionTable {
			matches, err := matchesLegacySchema(tx)
			if err != nil || !matches {
				return errUnsupportedSchema
			}
			if err := createLegacyVersionRecord(tx); err != nil {
				return err
			}
			version = 1
		}
		if version < 1 || version > CurrentSchemaVersion {
			return errUnsupportedSchema
		}

		for _, step := range plan {
			if step.version <= version {
				continue
			}
			if step.version != version+1 {
				return errUnsupportedSchema
			}
			if err := tx.Exec(step.statement).Error; err != nil {
				return err
			}
			if err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", step.version).Error; err != nil {
				return err
			}
			version = step.version
		}
		if version != CurrentSchemaVersion {
			return errUnsupportedSchema
		}
		return nil
	})
	if errors.Is(err, errUnsupportedSchema) {
		return errUnsupportedSchema
	}
	if err != nil {
		return errMigrationFailed
	}
	return nil
}

func migrationVersion(db *gorm.DB) (int, bool, error) {
	if !db.Migrator().HasTable("schema_migrations") {
		return 0, false, nil
	}
	var version int
	if err := db.Raw("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version).Error; err != nil {
		return 0, true, err
	}
	return version, true, nil
}

func matchesLegacySchema(db *gorm.DB) (bool, error) {
	if db.Dialector.Name() != "postgres" {
		return false, nil
	}
	var rows []schemaColumn
	err := db.Raw(`
		SELECT table_name, column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		ORDER BY table_name, ordinal_position
	`).Scan(&rows).Error
	if err != nil {
		return false, err
	}
	return matchesLegacySchemaFingerprint(rows), nil
}

func matchesLegacySchemaFingerprint(rows []schemaColumn) bool {
	expectedCount := 0
	expected := make(map[string]map[string]struct{}, len(legacySchemaColumns))
	for table, columns := range legacySchemaColumns {
		expected[table] = make(map[string]struct{}, len(columns))
		for _, column := range columns {
			expected[table][column] = struct{}{}
			expectedCount++
		}
	}
	if len(rows) != expectedCount {
		return false
	}
	seen := make(map[string]map[string]struct{}, len(expected))
	for _, row := range rows {
		columns, ok := expected[row.TableName]
		if !ok {
			return false
		}
		if _, ok := columns[row.ColumnName]; !ok {
			return false
		}
		if seen[row.TableName] == nil {
			seen[row.TableName] = make(map[string]struct{})
		}
		if _, duplicate := seen[row.TableName][row.ColumnName]; duplicate {
			return false
		}
		seen[row.TableName][row.ColumnName] = struct{}{}
	}
	return true
}

func createLegacyVersionRecord(db *gorm.DB) error {
	if err := db.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
		)
	`).Error; err != nil {
		return err
	}
	return db.Exec("INSERT INTO schema_migrations (version) VALUES (1)").Error
}
