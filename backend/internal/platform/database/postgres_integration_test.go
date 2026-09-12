//go:build postgres

// 本文件在临时 PostgreSQL 17 中验证 CMDB 迁移、数据库约束和应用账号权限的真实行为。
package database

import (
	"cmdb/internal/audit"
	"cmdb/internal/project"
	"cmdb/internal/resource"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	postgresTestDSNEnvironment = "CMDB_POSTGRES_TEST_DSN"
	postgresTestAppUser        = "cmdb"
	postgresTestAppPassword    = "cmdb-app-integration-only"
	currentSchemaSQLPath       = "../../../database/init/002_schema.sql"
)

var postgresTestDatabaseSequence atomic.Uint64

// TestPostgreSQLDeletionDependencyRace 用两条真实事务验证先提交的依赖不能被父删除级联清理。
func TestPostgreSQLDeletionDependencyRace(t *testing.T) {
	for _, parent := range []string{"项目", "接入源"} {
		for _, dependency := range []string{"排队任务", "服务器", "数据库", "负载均衡"} {
			t.Run(parent+"/"+dependency, func(t *testing.T) {
				fixture := newPostgresTestDatabase(t)
				installCurrentSchema(t, fixture)
				projectID := insertCurrentProject(t, fixture.db, "删除竞争")
				sourceID := insertCurrentSource(t, fixture.db, projectID, "来源", "虚构账号")
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				writer := fixture.db.WithContext(ctx).Begin()
				if writer.Error != nil {
					t.Fatal("启动依赖事务失败")
				}
				defer writer.Rollback()
				var writerPID int
				if err := writer.Raw("SELECT pg_backend_pid()").Scan(&writerPID).Error; err != nil {
					t.Fatal("读取依赖事务失败")
				}
				// 写方按生产协议占用父锁；删除必须等待该事务后重新判断依赖。
				if err := writer.Exec("SELECT id FROM projects WHERE id = ? FOR UPDATE", projectID).Error; err != nil {
					t.Fatal("锁定项目失败")
				}
				if err := writer.Exec("SELECT id FROM resource_sources WHERE id = ? FOR UPDATE", sourceID).Error; err != nil {
					t.Fatal("锁定来源失败")
				}
				results := make(chan error, 1)
				go func() {
					if parent == "项目" {
						results <- project.NewService(project.NewRepository(fixture.db), audit.NewRepository(fixture.db)).Delete(ctx, uint64(projectID))
						return
					}
					results <- resource.NewService(resource.NewRepository(fixture.db), nil, nil, audit.NewRepository(fixture.db)).DeleteSource(ctx, uint64(projectID), uint64(sourceID))
				}()
				waitPostgresBlockedBy(t, ctx, fixture.db, writerPID)
				if dependency == "排队任务" {
					if err := resource.NewRepository(writer).CreateJob(ctx, &resource.SyncJob{ProjectID: uint64(projectID), SourceID: uint64(sourceID), Status: "queued", Trigger: "manual", StartedAt: time.Now()}); err != nil {
						t.Fatal("写入并发排队任务失败")
					}
				} else {
					table := map[string]string{"服务器": "resources_servers", "数据库": "resources_databases", "负载均衡": "resources_load_balancers"}[dependency]
					insertAsset(t, writer, table, projectID, sourceID, "竞争资产")
				}
				if err := writer.Commit().Error; err != nil {
					t.Fatal("提交并发依赖失败")
				}
				select {
				case err := <-results:
					if !errors.Is(err, resource.ErrDeleteDependencyConflict) {
						t.Fatalf("先提交依赖后删除必须返回稳定冲突，实际为 %v", err)
					}
				case <-ctx.Done():
					t.Fatal("删除与依赖写入不得死锁")
				}
				for table, where := range map[string]string{"projects": "id", "resource_sources": "project_id"} {
					var count int64
					if err := fixture.db.Table(table).Where(where+" = ?", projectID).Count(&count).Error; err != nil || count != 1 {
						t.Fatal("并发冲突必须保留父对象")
					}
				}
				dependencyTable := map[string]string{"排队任务": "sync_jobs", "服务器": "resources_servers", "数据库": "resources_databases", "负载均衡": "resources_load_balancers"}[dependency]
				var dependencies int64
				if err := fixture.db.Table(dependencyTable).Where("source_id = ?", sourceID).Count(&dependencies).Error; err != nil || dependencies != 1 {
					t.Fatal("已提交依赖不得在父删除竞争中丢失")
				}
				var deletions int64
				if err := fixture.db.Model(&audit.Log{}).Where("action IN ?", []string{audit.ActionProjectDeleted, audit.ActionSourceDeleted}).Count(&deletions).Error; err != nil || deletions != 0 {
					t.Fatal("并发冲突不得留下成功删除审计")
				}
			})
		}
	}
}

// TestPostgreSQLDeletionWinsDependencyRace 验证父删除先占用事务时，后续入队和三类资产插入均不能创建孤儿依赖。
func TestPostgreSQLDeletionWinsDependencyRace(t *testing.T) {
	for _, parent := range []string{"项目", "接入源"} {
		for _, dependency := range []string{"排队任务", "服务器", "数据库", "负载均衡"} {
			t.Run(parent+"/"+dependency, func(t *testing.T) {
				fixture := newPostgresTestDatabase(t)
				installCurrentSchema(t, fixture)
				projectID := insertCurrentProject(t, fixture.db, "先删除")
				sourceID := insertCurrentSource(t, fixture.db, projectID, "来源", "虚构账号")
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				deletion := fixture.db.WithContext(ctx).Begin()
				if deletion.Error != nil {
					t.Fatal("启动删除事务失败")
				}
				defer deletion.Rollback()
				var deletionPID int
				if err := deletion.Raw("SELECT pg_backend_pid()").Scan(&deletionPID).Error; err != nil {
					t.Fatal("读取删除事务失败")
				}
				var err error
				if parent == "项目" {
					err = project.NewService(project.NewRepository(deletion), audit.NewRepository(deletion)).Delete(ctx, uint64(projectID))
				} else {
					err = resource.NewService(resource.NewRepository(deletion), nil, nil, audit.NewRepository(deletion)).DeleteSource(ctx, uint64(projectID), uint64(sourceID))
				}
				if err != nil {
					t.Fatalf("空父对象应允许删除：%v", err)
				}
				results := make(chan error, 1)
				go func() {
					if dependency == "排队任务" {
						results <- resource.NewRepository(fixture.db).CreateJob(ctx, &resource.SyncJob{ProjectID: uint64(projectID), SourceID: uint64(sourceID), Status: "queued", Trigger: "manual", StartedAt: time.Now()})
						return
					}
					table := map[string]string{"服务器": "resources_servers", "数据库": "resources_databases", "负载均衡": "resources_load_balancers"}[dependency]
					results <- fixture.db.WithContext(ctx).Exec("INSERT INTO "+table+" (project_id, source_id, provider, resource_type, external_id, first_seen_at, last_seen_at) VALUES (?, ?, 'aliyun', 'integration', '竞争资产', NOW(), NOW())", projectID, sourceID).Error
				}()
				waitPostgresBlockedBy(t, ctx, fixture.db, deletionPID)
				if err := deletion.Commit().Error; err != nil {
					t.Fatal("提交父删除失败")
				}
				select {
				case err := <-results:
					if err == nil {
						t.Fatal("已删除父对象不得接受新依赖")
					}
				case <-ctx.Done():
					t.Fatal("依赖写入不得死锁")
				}
				for _, table := range []string{"resource_sources", "resources_servers", "resources_databases", "resources_load_balancers", "sync_jobs"} {
					var count int64
					if err := fixture.db.Table(table).Where("project_id = ?", projectID).Count(&count).Error; err != nil || count != 0 {
						t.Fatal("父删除成功后不得遗留新依赖")
					}
				}
				var count int64
				if err := fixture.db.Model(&audit.Log{}).Where("action IN ?", []string{audit.ActionProjectDeleted, audit.ActionSourceDeleted}).Count(&count).Error; err != nil || count != 1 {
					t.Fatal("成功删除应提交一条审计")
				}
			})
		}
	}
}

// TestPostgreSQLDeletionNormalizesForeignKeyConflict 验证数据库兜底拒绝无论是否启用 GORM 错误转换都返回同一业务错误，并回滚删除审计。
func TestPostgreSQLDeletionNormalizesForeignKeyConflict(t *testing.T) {
	for _, parent := range []string{"项目", "接入源"} {
		for _, translated := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/错误转换=%t", parent, translated), func(t *testing.T) {
				fixture := newPostgresTestDatabase(t)
				installCurrentSchema(t, fixture)
				projectID := insertCurrentProject(t, fixture.db, "外键兜底")
				sourceID := insertCurrentSource(t, fixture.db, projectID, "来源", "虚构账号")
				parentTable, parentID := "projects", projectID
				if parent == "接入源" {
					parentTable, parentID = "resource_sources", sourceID
				}
				// 隔离夹具增加真实外键，以模拟依赖预检查未覆盖的数据库最终拒绝。
				requireExec(t, fixture.db, "CREATE TABLE deletion_test_references (parent_id BIGINT REFERENCES "+parentTable+"(id) ON DELETE RESTRICT)", "创建外键兜底夹具失败")
				requireExec(t, fixture.db, "INSERT INTO deletion_test_references VALUES (?)", "写入外键引用失败", parentID)
				fixture.db.Config.TranslateError = translated
				var err error
				if parent == "项目" {
					err = project.NewService(project.NewRepository(fixture.db), audit.NewRepository(fixture.db)).Delete(context.Background(), uint64(projectID))
				} else {
					err = resource.NewService(resource.NewRepository(fixture.db), nil, nil, audit.NewRepository(fixture.db)).DeleteSource(context.Background(), uint64(projectID), uint64(sourceID))
				}
				if !errors.Is(err, resource.ErrDeleteDependencyConflict) {
					t.Fatalf("真实外键拒绝必须转换为稳定删除冲突，实际为 %v", err)
				}
				var parents, audits int64
				if err := fixture.db.Table(parentTable).Where("id = ?", parentID).Count(&parents).Error; err != nil || parents != 1 {
					t.Fatal("外键冲突必须保留父对象")
				}
				if err := fixture.db.Model(&audit.Log{}).Where("action IN ?", []string{audit.ActionProjectDeleted, audit.ActionSourceDeleted}).Count(&audits).Error; err != nil || audits != 0 {
					t.Fatal("外键冲突必须回滚成功删除审计")
				}
			})
		}
	}
}

// waitPostgresBlockedBy 通过 PostgreSQL 锁等待关系建立竞态屏障，避免依靠固定睡眠猜测事务已经开始。
func waitPostgresBlockedBy(t *testing.T, ctx context.Context, db *gorm.DB, blocker int) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := db.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid)))", blocker).Scan(&waiting).Error; err != nil {
			t.Fatal("读取事务等待关系失败")
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("删除与写入没有形成预期互斥")
		case <-ticker.C:
		}
	}
}

type postgresTestDatabase struct {
	name          string
	configuration *pgx.ConnConfig
	db            *gorm.DB
}

// TestPostgreSQLMigrationUpgradesLegacySchema 验证无版本旧库被精确识别、历史来源保持待验证，且重复迁移不改写版本。
func TestPostgreSQLMigrationUpgradesLegacySchema(t *testing.T) {
	testDatabase := newPostgresTestDatabase(t)
	installLegacySchema(t, testDatabase, true)
	projectID := insertLegacyProject(t, testDatabase.db, "legacy-project")
	insertLegacySource(t, testDatabase.db, projectID, "aliyun", "legacy-source-a")
	insertLegacySource(t, testDatabase.db, projectID, "aws", "legacy-source-b")

	if err := Migrate(context.Background(), testDatabase.db); err != nil {
		t.Fatal("版本 1 旧库迁移失败")
	}
	if err := CheckSchemaVersion(context.Background(), testDatabase.db); err != nil {
		t.Fatal("旧库迁移后版本门禁未放行")
	}

	var column struct {
		DataType               string `gorm:"column:data_type"`
		MaximumCharacterLength int    `gorm:"column:character_maximum_length"`
		IsNullable             string `gorm:"column:is_nullable"`
	}
	if err := testDatabase.db.Raw(`
		SELECT data_type, character_maximum_length, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'resource_sources' AND column_name = 'identity_status'
	`).Scan(&column).Error; err != nil {
		t.Fatal("读取迁移后身份字段定义失败")
	}
	if column.DataType != "character varying" || column.MaximumCharacterLength != 32 || column.IsNullable != "NO" {
		t.Fatalf("迁移后 identity_status 必须是 VARCHAR(32) NOT NULL，实际为 %+v", column)
	}

	var pendingCount int64
	if err := testDatabase.db.Raw(`
		SELECT COUNT(*) FROM resource_sources
		WHERE identity_status = 'pending' AND cloud_account_id IS NULL AND identity_verified_at IS NULL
	`).Scan(&pendingCount).Error; err != nil {
		t.Fatal("读取历史接入源身份状态失败")
	}
	if pendingCount != 2 {
		t.Fatalf("两个历史接入源都必须迁移为 pending，实际为 %d", pendingCount)
	}

	if err := testDatabase.db.Exec("UPDATE resource_sources SET identity_status = 'unknown' WHERE name = 'legacy-source-a'").Error; err == nil {
		t.Fatal("身份检查约束必须拒绝未知状态")
	}
	if err := testDatabase.db.Exec("UPDATE resource_sources SET identity_status = 'verified' WHERE name = 'legacy-source-a'").Error; err == nil {
		t.Fatal("身份检查约束必须拒绝缺少云账号标识和验证时间的已验证状态")
	}

	assertVerifiedAccountUniqueIndex(t, testDatabase.db)

	if err := Migrate(context.Background(), testDatabase.db); err != nil {
		t.Fatal("当前版本重复迁移必须成功")
	}
	var versionCount int64
	var maximumVersion int
	if err := testDatabase.db.Raw("SELECT COUNT(*), MAX(version) FROM schema_migrations").Row().Scan(&versionCount, &maximumVersion); err != nil {
		t.Fatal("读取重复迁移后的版本记录失败")
	}
	if versionCount != 2 || maximumVersion != CurrentSchemaVersion {
		t.Fatalf("重复迁移不得新增版本记录，记录数=%d，最高版本=%d", versionCount, maximumVersion)
	}
}

// TestPostgreSQLMigrationRejectsUnknownSchema 验证未知无版本结构原样保留，不被猜测性升级。
func TestPostgreSQLMigrationRejectsUnknownSchema(t *testing.T) {
	testDatabase := newPostgresTestDatabase(t)
	requireExec(t, testDatabase.db, "CREATE TABLE unknown_business_table (id BIGINT PRIMARY KEY)", "创建未知结构失败")

	err := Migrate(context.Background(), testDatabase.db)
	if err == nil || err.Error() != "数据库结构不受支持，未执行迁移" {
		t.Fatalf("未知结构必须返回稳定拒绝信息，实际为 %v", err)
	}
	if relationExists(t, testDatabase.db, "schema_migrations") {
		t.Fatal("未知结构不得遗留迁移版本表")
	}
	if !relationExists(t, testDatabase.db, "unknown_business_table") {
		t.Fatal("未知结构被拒绝后必须原样保留")
	}
}

// TestPostgreSQLMigrationRejectsInvalidAssetOwnershipAndRollsBack 验证孤儿和跨项目资产都使完整版本事务回滚。
func TestPostgreSQLMigrationRejectsInvalidAssetOwnershipAndRollsBack(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, *postgresTestDatabase)
	}{
		{
			name: "孤儿资产",
			prepare: func(t *testing.T, testDatabase *postgresTestDatabase) {
				installLegacySchema(t, testDatabase, false)
				requireExec(t, testDatabase.db, `
					INSERT INTO resources_servers
					(project_id, source_id, provider, resource_type, external_id, first_seen_at, last_seen_at)
					VALUES (90001, 90002, 'aliyun', 'ecs', 'orphan-server', NOW(), NOW())
				`, "写入孤儿资产夹具失败")
				installLegacyAssetForeignKeys(t, testDatabase, true)
			},
		},
		{
			name: "跨项目资产",
			prepare: func(t *testing.T, testDatabase *postgresTestDatabase) {
				installLegacySchema(t, testDatabase, true)
				projectA := insertLegacyProject(t, testDatabase.db, "cross-project-a")
				projectB := insertLegacyProject(t, testDatabase.db, "cross-project-b")
				sourceID := insertLegacySource(t, testDatabase.db, projectA, "aliyun", "cross-source")
				insertAsset(t, testDatabase.db, "resources_databases", projectB, sourceID, "cross-database")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testDatabase := newPostgresTestDatabase(t)
			test.prepare(t, testDatabase)

			err := Migrate(context.Background(), testDatabase.db)
			if err == nil || err.Error() != "数据库迁移失败，未完成任何结构变更" {
				t.Fatalf("异常归属必须返回稳定迁移失败信息，实际为 %v", err)
			}
			if relationExists(t, testDatabase.db, "schema_migrations") {
				t.Fatal("异常归属迁移失败不得遗留版本表")
			}
			if columnExists(t, testDatabase.db, "resource_sources", "identity_status") {
				t.Fatal("异常归属迁移失败不得遗留身份字段")
			}
		})
	}
}

// TestPostgreSQLVerifiedAccountUniqueIndexIsConcurrent 验证并发确认同一平台账号时只有一个接入源可以提交。
func TestPostgreSQLVerifiedAccountUniqueIndexIsConcurrent(t *testing.T) {
	testDatabase := newPostgresTestDatabase(t)
	installCurrentSchema(t, testDatabase)
	assertVerifiedAccountUniqueIndex(t, testDatabase.db)
	projectID := insertCurrentProject(t, testDatabase.db, "unique-project")

	start := make(chan struct{})
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		index := index
		go func() {
			<-start
			results <- testDatabase.db.Exec(`
				INSERT INTO resource_sources
				(project_id, provider, name, encrypted_credential, cloud_account_id, identity_status, identity_verified_at)
				VALUES (?, 'aliyun', ?, 'integration-ciphertext', 'shared-account', 'verified', NOW())
			`, projectID, fmt.Sprintf("concurrent-source-%d", index)).Error
		}()
	}
	close(start)

	successCount := 0
	for index := 0; index < 2; index++ {
		if err := <-results; err == nil {
			successCount++
		}
	}
	if successCount != 1 {
		t.Fatalf("同平台同云账号的两个并发写入必须只有一个成功，实际成功 %d 个", successCount)
	}

	var storedCount int64
	if err := testDatabase.db.Raw(`
		SELECT COUNT(*) FROM resource_sources
		WHERE provider = 'aliyun' AND cloud_account_id = 'shared-account' AND identity_status = 'verified'
	`).Scan(&storedCount).Error; err != nil {
		t.Fatal("读取并发写入结果失败")
	}
	if storedCount != 1 {
		t.Fatalf("部分唯一索引必须只保留一条已验证账号记录，实际为 %d", storedCount)
	}

	for index := 0; index < 2; index++ {
		requireExec(t, testDatabase.db, `
			INSERT INTO resource_sources (project_id, provider, name, encrypted_credential, identity_status)
			VALUES (?, 'aliyun', ?, 'integration-ciphertext', 'pending')
		`, "待验证接入源不应占用已验证账号唯一键", projectID, fmt.Sprintf("pending-source-%d", index))
	}
}

// TestPostgreSQLAssetForeignKeysRestrictParentDeletion 验证三类资产的项目和接入源父级删除都被数据库拒绝。
func TestPostgreSQLAssetForeignKeysRestrictParentDeletion(t *testing.T) {
	assetTables := []struct {
		table             string
		projectConstraint string
		sourceConstraint  string
	}{
		{"resources_servers", "fk_resources_servers_project", "fk_resources_servers_source"},
		{"resources_databases", "fk_resources_databases_project", "fk_resources_databases_source"},
		{"resources_load_balancers", "fk_resources_load_balancers_project", "fk_resources_load_balancers_source"},
	}

	for _, asset := range assetTables {
		t.Run(asset.table, func(t *testing.T) {
			testDatabase := newPostgresTestDatabase(t)
			installCurrentSchema(t, testDatabase)
			projectID := insertCurrentProject(t, testDatabase.db, "restrict-"+asset.table)
			sourceID := insertCurrentSource(t, testDatabase.db, projectID, "source-"+asset.table, "account-"+asset.table)
			insertAsset(t, testDatabase.db, asset.table, projectID, sourceID, "asset-"+asset.table)

			assertDeleteRule(t, testDatabase.db, asset.projectConstraint, "RESTRICT")
			assertDeleteRule(t, testDatabase.db, asset.sourceConstraint, "RESTRICT")
			if err := testDatabase.db.Exec("DELETE FROM resource_sources WHERE id = ?", sourceID).Error; err == nil {
				t.Fatal("仍有资产时不得删除接入源")
			}
			if err := testDatabase.db.Exec("DELETE FROM projects WHERE id = ?", projectID).Error; err == nil {
				t.Fatal("仍有资产时不得删除业务项目")
			}

			var assetCount int64
			if err := testDatabase.db.Table(asset.table).Count(&assetCount).Error; err != nil {
				t.Fatal("读取限制删除后的资产失败")
			}
			if assetCount != 1 {
				t.Fatal("限制删除失败后必须保留资产")
			}
		})
	}
}

// TestPostgreSQLMigrationUsesAdvisoryLockAndRollsBackDDL 验证迁移等待事务锁，并在版本记录失败时回滚已完成的 DDL。
func TestPostgreSQLMigrationUsesAdvisoryLockAndRollsBackDDL(t *testing.T) {
	t.Run("事务级迁移锁", func(t *testing.T) {
		testDatabase := newPostgresTestDatabase(t)
		installLegacySchema(t, testDatabase, true)

		sqlDatabase, err := testDatabase.db.DB()
		if err != nil {
			t.Fatal("读取 PostgreSQL 连接池失败")
		}
		blocker, err := sqlDatabase.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal("创建迁移锁事务失败")
		}
		if _, err := blocker.Exec("SELECT pg_advisory_xact_lock($1)", migrationAdvisoryLockID); err != nil {
			_ = blocker.Rollback()
			t.Fatal("取得迁移锁夹具失败")
		}

		result := make(chan error, 1)
		go func() { result <- Migrate(context.Background(), testDatabase.db) }()
		select {
		case <-result:
			_ = blocker.Rollback()
			t.Fatal("迁移必须等待已持有的事务级 advisory lock")
		case <-time.After(200 * time.Millisecond):
		}
		if err := blocker.Rollback(); err != nil {
			t.Fatal("释放迁移锁夹具失败")
		}
		select {
		case err := <-result:
			if err != nil {
				t.Fatal("迁移锁释放后升级失败")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("迁移锁释放后升级未在期限内完成")
		}
	})

	t.Run("版本记录失败回滚DDL", func(t *testing.T) {
		testDatabase := newPostgresTestDatabase(t)
		requireExec(t, testDatabase.db, `
			CREATE TABLE schema_migrations (
				version INTEGER PRIMARY KEY CHECK (version = 1),
				applied_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
			)
		`, "创建原子性测试版本表失败")
		requireExec(t, testDatabase.db, "INSERT INTO schema_migrations (version) VALUES (1)", "写入原子性测试版本失败")

		err := migrate(context.Background(), testDatabase.db, []migration{{
			version:   2,
			statement: "CREATE TABLE migration_atomicity_probe (id BIGINT PRIMARY KEY)",
		}})
		if err == nil || err.Error() != "数据库迁移失败，未完成任何结构变更" {
			t.Fatalf("版本记录失败必须返回稳定迁移错误，实际为 %v", err)
		}
		if relationExists(t, testDatabase.db, "migration_atomicity_probe") {
			t.Fatal("版本记录失败必须回滚同一事务内已经完成的 DDL")
		}
		var version int
		if err := testDatabase.db.Raw("SELECT MAX(version) FROM schema_migrations").Scan(&version).Error; err != nil {
			t.Fatal("读取回滚后的结构版本失败")
		}
		if version != 1 {
			t.Fatalf("原子性回滚后结构版本必须保持 1，实际为 %d", version)
		}
	})
}

// TestPostgreSQLApplicationRoleCanOnlyReadSchemaVersion 验证 cmdb 可做启动版本检查，但不能修改版本或执行 DDL。
func TestPostgreSQLApplicationRoleCanOnlyReadSchemaVersion(t *testing.T) {
	testDatabase := newPostgresTestDatabase(t)
	installCurrentSchema(t, testDatabase)
	applicationDatabase := openPostgresConfiguration(t, applicationConfiguration(testDatabase.configuration), false)

	if err := CheckSchemaVersion(context.Background(), applicationDatabase); err != nil {
		t.Fatal("cmdb 应用账号必须能够读取结构版本")
	}
	if err := applicationDatabase.Exec("INSERT INTO schema_migrations (version) VALUES (99)").Error; err == nil {
		t.Fatal("cmdb 应用账号不得写入结构版本")
	}
	if err := applicationDatabase.Exec("ALTER TABLE projects ADD COLUMN forbidden_ddl TEXT").Error; err == nil {
		t.Fatal("cmdb 应用账号不得执行 ALTER TABLE")
	}
	if columnExists(t, testDatabase.db, "projects", "forbidden_ddl") {
		t.Fatal("被拒绝的应用账号 DDL 不得改变业务结构")
	}
}

// newPostgresTestDatabase 为每个测试场景创建并清理独立数据库，避免版本和约束状态互相污染。
func newPostgresTestDatabase(t *testing.T) *postgresTestDatabase {
	t.Helper()
	baseDSN := os.Getenv(postgresTestDSNEnvironment)
	if baseDSN == "" {
		t.Skip("未设置 CMDB_POSTGRES_TEST_DSN，跳过 PostgreSQL 17 集成测试")
	}
	adminConfiguration, err := pgx.ParseConfig(baseDSN)
	if err != nil {
		t.Fatal("解析 PostgreSQL 集成测试连接配置失败")
	}
	adminDatabase := openPostgresConfiguration(t, adminConfiguration, false)
	var serverVersion int
	if err := adminDatabase.Raw("SHOW server_version_num").Scan(&serverVersion).Error; err != nil {
		t.Fatal("读取 PostgreSQL 测试服务版本失败")
	}
	if serverVersion/10000 != 17 {
		t.Fatalf("集成测试必须使用 PostgreSQL 17，实际主版本为 %d", serverVersion/10000)
	}

	name := fmt.Sprintf("cmdb_it_%d_%d", os.Getpid(), postgresTestDatabaseSequence.Add(1))
	requireExec(t, adminDatabase, "CREATE DATABASE "+quotePostgresIdentifier(name), "创建隔离 PostgreSQL 测试数据库失败")
	requireExec(t, adminDatabase, "GRANT CONNECT ON DATABASE "+quotePostgresIdentifier(name)+" TO "+quotePostgresIdentifier(postgresTestAppUser), "授权应用账号连接隔离数据库失败")

	targetConfiguration := adminConfiguration.Copy()
	targetConfiguration.Database = name
	targetDatabase := openPostgresConfiguration(t, targetConfiguration, false)

	t.Cleanup(func() {
		if sqlDatabase, err := targetDatabase.DB(); err == nil {
			_ = sqlDatabase.Close()
		}
		if err := adminDatabase.Exec("DROP DATABASE " + quotePostgresIdentifier(name) + " WITH (FORCE)").Error; err != nil {
			t.Error("清理隔离 PostgreSQL 测试数据库失败")
		}
		if sqlDatabase, err := adminDatabase.DB(); err == nil {
			_ = sqlDatabase.Close()
		}
	})
	return &postgresTestDatabase{name: name, configuration: targetConfiguration, db: targetDatabase}
}

func openPostgresConfiguration(t *testing.T, configuration *pgx.ConnConfig, simpleProtocol bool) *gorm.DB {
	t.Helper()
	connectionConfiguration := configuration.Copy()
	if simpleProtocol {
		connectionConfiguration.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	}
	sqlDatabase := stdlib.OpenDB(*connectionConfiguration)
	dialector := postgres.New(postgres.Config{Conn: sqlDatabase})
	database, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		_ = sqlDatabase.Close()
		t.Fatal("连接 PostgreSQL 集成测试数据库失败")
	}
	if err := sqlDatabase.Ping(); err != nil {
		_ = sqlDatabase.Close()
		t.Fatal("探测 PostgreSQL 集成测试数据库失败")
	}
	return database
}

func applicationConfiguration(adminConfiguration *pgx.ConnConfig) *pgx.ConnConfig {
	configuration := adminConfiguration.Copy()
	configuration.User = postgresTestAppUser
	configuration.Password = postgresTestAppPassword
	return configuration
}

func installCurrentSchema(t *testing.T, testDatabase *postgresTestDatabase) {
	t.Helper()
	content, err := os.ReadFile(currentSchemaSQLPath)
	if err != nil {
		t.Fatal("读取 PostgreSQL 当前空库结构失败")
	}
	statement := strings.Replace(string(content), `\set ON_ERROR_STOP on`, "", 1)
	execScript(t, testDatabase.configuration, statement, "执行 PostgreSQL 当前空库结构失败")
}

func installLegacySchema(t *testing.T, testDatabase *postgresTestDatabase, withAssetForeignKeys bool) {
	t.Helper()
	execScript(t, testDatabase.configuration, legacySchemaSQL, "创建 PostgreSQL 版本 1 结构失败")
	if withAssetForeignKeys {
		installLegacyAssetForeignKeys(t, testDatabase, false)
	}
}

func installLegacyAssetForeignKeys(t *testing.T, testDatabase *postgresTestDatabase, notValid bool) {
	t.Helper()
	validation := ""
	if notValid {
		validation = " NOT VALID"
	}
	for _, foreignKey := range []struct {
		table      string
		constraint string
		column     string
		parent     string
	}{
		{"resources_servers", "fk_resources_servers_project", "project_id", "projects"},
		{"resources_servers", "fk_resources_servers_source", "source_id", "resource_sources"},
		{"resources_databases", "fk_resources_databases_project", "project_id", "projects"},
		{"resources_databases", "fk_resources_databases_source", "source_id", "resource_sources"},
		{"resources_load_balancers", "fk_resources_load_balancers_project", "project_id", "projects"},
		{"resources_load_balancers", "fk_resources_load_balancers_source", "source_id", "resource_sources"},
	} {
		statement := fmt.Sprintf(
			"ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (id) ON DELETE CASCADE%s",
			quotePostgresIdentifier(foreignKey.table),
			quotePostgresIdentifier(foreignKey.constraint),
			quotePostgresIdentifier(foreignKey.column),
			quotePostgresIdentifier(foreignKey.parent),
			validation,
		)
		requireExec(t, testDatabase.db, statement, "创建版本 1 资产外键失败")
	}
}

func execScript(t *testing.T, configuration *pgx.ConnConfig, statement string, failureMessage string) {
	t.Helper()
	database := openPostgresConfiguration(t, configuration, true)
	if err := database.Exec(statement).Error; err != nil {
		t.Fatal(failureMessage)
	}
	if sqlDatabase, err := database.DB(); err == nil {
		_ = sqlDatabase.Close()
	}
}

func insertLegacyProject(t *testing.T, database *gorm.DB, code string) int64 {
	t.Helper()
	var id int64
	if err := database.Raw("INSERT INTO projects (code, name) VALUES (?, ?) RETURNING id", code, code).Row().Scan(&id); err != nil {
		t.Fatal("写入版本 1 业务项目失败")
	}
	return id
}

func insertCurrentProject(t *testing.T, database *gorm.DB, code string) int64 {
	t.Helper()
	return insertLegacyProject(t, database, code)
}

func insertLegacySource(t *testing.T, database *gorm.DB, projectID int64, provider string, name string) int64 {
	t.Helper()
	var id int64
	if err := database.Raw(`
		INSERT INTO resource_sources (project_id, provider, name, encrypted_credential)
		VALUES (?, ?, ?, 'integration-ciphertext') RETURNING id
	`, projectID, provider, name).Row().Scan(&id); err != nil {
		t.Fatal("写入版本 1 接入源失败")
	}
	return id
}

func insertCurrentSource(t *testing.T, database *gorm.DB, projectID int64, name string, accountID string) int64 {
	t.Helper()
	var id int64
	if err := database.Raw(`
		INSERT INTO resource_sources
		(project_id, provider, name, encrypted_credential, cloud_account_id, identity_status, identity_verified_at)
		VALUES (?, 'aliyun', ?, 'integration-ciphertext', ?, 'verified', NOW()) RETURNING id
	`, projectID, name, accountID).Row().Scan(&id); err != nil {
		t.Fatal("写入已验证接入源失败")
	}
	return id
}

func insertAsset(t *testing.T, database *gorm.DB, table string, projectID int64, sourceID int64, externalID string) {
	t.Helper()
	statement := fmt.Sprintf(`
		INSERT INTO %s
		(project_id, source_id, provider, resource_type, external_id, first_seen_at, last_seen_at)
		VALUES (?, ?, 'aliyun', 'integration', ?, NOW(), NOW())
	`, quotePostgresIdentifier(table))
	requireExec(t, database, statement, "写入资产测试数据失败", projectID, sourceID, externalID)
}

func assertDeleteRule(t *testing.T, database *gorm.DB, constraint string, expected string) {
	t.Helper()
	var deleteRule string
	if err := database.Raw(`
		SELECT delete_rule FROM information_schema.referential_constraints
		WHERE constraint_schema = 'public' AND constraint_name = ?
	`, constraint).Scan(&deleteRule).Error; err != nil {
		t.Fatal("读取资产外键删除规则失败")
	}
	if deleteRule != expected {
		t.Fatalf("外键 %s 的删除规则必须为 %s，实际为 %s", constraint, expected, deleteRule)
	}
}

func assertVerifiedAccountUniqueIndex(t *testing.T, database *gorm.DB) {
	t.Helper()
	var indexDefinition string
	if err := database.Raw(`
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = 'public' AND tablename = 'resource_sources'
		  AND indexname = 'uk_resource_sources_provider_account_verified'
	`).Scan(&indexDefinition).Error; err != nil {
		t.Fatal("读取接入源部分唯一索引失败")
	}
	if indexDefinition == "" {
		t.Fatal("数据库结构必须存在命名稳定的已验证云账号部分唯一索引")
	}
	if !strings.Contains(indexDefinition, "cloud_account_id IS NOT NULL") {
		t.Fatalf("已验证云账号部分唯一索引必须排除空账号，实际定义为 %s", indexDefinition)
	}
}

func relationExists(t *testing.T, database *gorm.DB, relation string) bool {
	t.Helper()
	var exists bool
	if err := database.Raw("SELECT to_regclass(?) IS NOT NULL", "public."+relation).Scan(&exists).Error; err != nil {
		t.Fatal("检查 PostgreSQL 关系是否存在失败")
	}
	return exists
}

func columnExists(t *testing.T, database *gorm.DB, table string, column string) bool {
	t.Helper()
	var exists bool
	if err := database.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = ? AND column_name = ?
		)
	`, table, column).Scan(&exists).Error; err != nil {
		t.Fatal("检查 PostgreSQL 字段是否存在失败")
	}
	return exists
}

func requireExec(t *testing.T, database *gorm.DB, statement string, failureMessage string, values ...any) {
	t.Helper()
	if err := database.Exec(statement, values...).Error; err != nil {
		t.Fatal(failureMessage)
	}
}

func quotePostgresIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

const legacySchemaSQL = `
CREATE TABLE users (
    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    username VARCHAR(64) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    display_name VARCHAR(128) NOT NULL,
    email VARCHAR(255),
    global_role VARCHAR(32) NOT NULL DEFAULT 'user',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    last_login_at TIMESTAMPTZ(3),
    created_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
);
CREATE TABLE projects (
    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    code VARCHAR(64) NOT NULL UNIQUE,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(500) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'enabled',
    owner_user_id BIGINT,
    created_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
);
CREATE TABLE project_members (
    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    project_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    role VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
);
CREATE TABLE audit_logs (
    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    actor_id BIGINT,
    project_id BIGINT,
    action VARCHAR(128) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id VARCHAR(255),
    detail JSONB,
    request_ip VARCHAR(45),
    created_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
);
CREATE TABLE resource_sources (
    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    provider VARCHAR(32) NOT NULL,
    name VARCHAR(128) NOT NULL,
    region VARCHAR(128) NOT NULL DEFAULT '',
    encrypted_credential TEXT NOT NULL,
    credential_hint VARCHAR(128) NOT NULL DEFAULT '',
    config JSONB,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    sync_interval_minutes INTEGER NOT NULL DEFAULT 60,
    last_sync_at TIMESTAMPTZ(3),
    next_sync_at TIMESTAMPTZ(3),
    created_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
);
CREATE TABLE resources_servers (
    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    project_id BIGINT NOT NULL,
    source_id BIGINT NOT NULL,
    provider VARCHAR(32) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    external_id VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL DEFAULT '',
    region VARCHAR(128) NOT NULL DEFAULT '',
    zone VARCHAR(128) NOT NULL DEFAULT '',
    cloud_status VARCHAR(64) NOT NULL DEFAULT '',
    asset_status VARCHAR(32) NOT NULL DEFAULT 'active',
    private_ips JSONB,
    public_ips JSONB,
    raw_attributes JSONB,
    first_seen_at TIMESTAMPTZ(3) NOT NULL,
    last_seen_at TIMESTAMPTZ(3) NOT NULL,
    missing_since TIMESTAMPTZ(3),
    created_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
);
CREATE TABLE resources_databases (
    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    project_id BIGINT NOT NULL,
    source_id BIGINT NOT NULL,
    provider VARCHAR(32) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    external_id VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL DEFAULT '',
    region VARCHAR(128) NOT NULL DEFAULT '',
    zone VARCHAR(128) NOT NULL DEFAULT '',
    cloud_status VARCHAR(64) NOT NULL DEFAULT '',
    asset_status VARCHAR(32) NOT NULL DEFAULT 'active',
    engine VARCHAR(64) NOT NULL DEFAULT '',
    engine_version VARCHAR(64) NOT NULL DEFAULT '',
    endpoints JSONB,
    raw_attributes JSONB,
    first_seen_at TIMESTAMPTZ(3) NOT NULL,
    last_seen_at TIMESTAMPTZ(3) NOT NULL,
    missing_since TIMESTAMPTZ(3),
    created_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
);
CREATE TABLE resources_load_balancers (
    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    project_id BIGINT NOT NULL,
    source_id BIGINT NOT NULL,
    provider VARCHAR(32) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    external_id VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL DEFAULT '',
    region VARCHAR(128) NOT NULL DEFAULT '',
    zone VARCHAR(128) NOT NULL DEFAULT '',
    cloud_status VARCHAR(64) NOT NULL DEFAULT '',
    asset_status VARCHAR(32) NOT NULL DEFAULT 'active',
    network_type VARCHAR(32) NOT NULL DEFAULT '',
    endpoints JSONB,
    raw_attributes JSONB,
    first_seen_at TIMESTAMPTZ(3) NOT NULL,
    last_seen_at TIMESTAMPTZ(3) NOT NULL,
    missing_since TIMESTAMPTZ(3),
    created_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
);
CREATE TABLE sync_jobs (
    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    project_id BIGINT NOT NULL,
    source_id BIGINT NOT NULL,
    previous_job_id BIGINT,
    status VARCHAR(32) NOT NULL,
    trigger VARCHAR(32) NOT NULL,
    statistics JSONB,
    error_summary VARCHAR(500) NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ(3) NOT NULL,
    finished_at TIMESTAMPTZ(3)
);
`
