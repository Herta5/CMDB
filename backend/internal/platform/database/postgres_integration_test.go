//go:build postgres

// 本文件在临时 PostgreSQL 17 中验证 CMDB 首次安装结构、数据库约束和应用账号权限的真实行为。
package database

import (
	"cmdb/internal/audit"
	"cmdb/internal/project"
	"cmdb/internal/resource"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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

// TestPostgreSQLServerDiskSizeSort 验证生产 JSONB 云盘数组可在数据库内汇总排序后分页。
func TestPostgreSQLServerDiskSizeSort(t *testing.T) {
	fixture := newPostgresTestDatabase(t)
	installCurrentSchema(t, fixture)
	projectID := insertCurrentProject(t, fixture.db, "服务器磁盘排序")
	sourceID := insertCurrentSource(t, fixture.db, projectID, "排序来源", "排序账号")
	requireExec(t, fixture.db, `INSERT INTO resources_servers
		(project_id, source_id, provider, resource_type, external_id, name, disks, first_seen_at, last_seen_at)
		VALUES (?, ?, 'aws', 'ec2', 'i-small', '小磁盘', '[{"id":"small","size_gib":40}]'::jsonb, NOW(), NOW()),
		       (?, ?, 'aws', 'ec2', 'i-large', '大磁盘', '[{"id":"root","size_gib":100},{"id":"data","size_gib":200}]'::jsonb, NOW(), NOW())`,
		"准备 JSONB 磁盘排序资源失败", projectID, sourceID, projectID, sourceID)
	service := resource.NewService(resource.NewRepository(fixture.db), nil, nil)
	values, total, err := service.ListResources(context.Background(), uint64(projectID), resource.ResourceListQuery{ResourceTypes: []string{"ec2"}, SortBy: "disk_size", SortOrder: "desc", Page: 1, PageSize: 1})
	if err != nil || total != 2 || len(values) != 1 || values[0].ExternalID != "i-large" {
		t.Fatalf("PostgreSQL 必须按 JSONB 磁盘总容量排序后分页：total=%d values=%+v err=%v", total, values, err)
	}
}

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

// TestPostgreSQLAccountIdentityIsRequiredAndUnique 验证来源只能保存完整身份，且同平台账号全局唯一。
func TestPostgreSQLAccountIdentityIsRequiredAndUnique(t *testing.T) {
	testDatabase := newPostgresTestDatabase(t)
	installCurrentSchema(t, testDatabase)
	assertAccountUniqueConstraint(t, testDatabase.db)
	if columnExists(t, testDatabase.db, "resource_sources", "identity_status") {
		t.Fatal("首次初始化结构不得包含历史身份状态字段")
	}
	projectID := insertCurrentProject(t, testDatabase.db, "unique-project")
	for _, invalid := range []struct {
		name      string
		accountID any
		verified  any
	}{
		{name: "缺少账号", accountID: nil, verified: time.Now()},
		{name: "空账号", accountID: "", verified: time.Now()},
		{name: "缺少验证时间", accountID: "missing-time", verified: nil},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			if err := testDatabase.db.Exec(`
				INSERT INTO resource_sources
				(project_id, provider, name, encrypted_credential, cloud_account_id, identity_verified_at)
				VALUES (?, 'aliyun', ?, 'integration-ciphertext', ?, ?)
			`, projectID, invalid.name, invalid.accountID, invalid.verified).Error; err == nil {
				t.Fatal("接入源必须同时具有非空云账号和身份验证时间")
			}
		})
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		index := index
		go func() {
			<-start
			results <- testDatabase.db.Exec(`
				INSERT INTO resource_sources
				(project_id, provider, name, encrypted_credential, cloud_account_id, identity_verified_at)
				VALUES (?, 'aliyun', ?, 'integration-ciphertext', 'shared-account', NOW())
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
		WHERE provider = 'aliyun' AND cloud_account_id = 'shared-account'
	`).Scan(&storedCount).Error; err != nil {
		t.Fatal("读取并发写入结果失败")
	}
	if storedCount != 1 {
		t.Fatalf("账号唯一约束必须只保留一条同平台账号记录，实际为 %d", storedCount)
	}
	requireExec(t, testDatabase.db, `
		INSERT INTO resource_sources
		(project_id, provider, name, encrypted_credential, cloud_account_id, identity_verified_at)
		VALUES (?, 'aws', 'aws-shared-account', 'integration-ciphertext', 'shared-account', NOW())
	`, "不同平台允许使用相同云账号标识", projectID)
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

// TestPostgreSQLApplicationRoleSupportsBusinessCRUDWithoutDDL 验证首次初始化只授予应用业务读写能力。
func TestPostgreSQLApplicationRoleSupportsBusinessCRUDWithoutDDL(t *testing.T) {
	testDatabase := newPostgresTestDatabase(t)
	installCurrentSchema(t, testDatabase)
	applicationDatabase := openPostgresConfiguration(t, applicationConfiguration(testDatabase.configuration), false)

	if relationExists(t, testDatabase.db, "schema_migrations") {
		t.Fatal("首次初始化不得创建数据库结构版本表")
	}
	projectID := insertCurrentProject(t, applicationDatabase, "应用账号业务读写")
	sourceID := insertCurrentSource(t, applicationDatabase, projectID, "应用账号来源", "应用账号虚构账号")
	if err := applicationDatabase.Exec("UPDATE resource_sources SET name = ? WHERE id = ?", "应用账号更新来源", sourceID).Error; err != nil {
		t.Fatal("cmdb 应用账号必须能够更新业务数据")
	}
	var sourceCount int64
	if err := applicationDatabase.Table("resource_sources").Where("id = ? AND name = ?", sourceID, "应用账号更新来源").Count(&sourceCount).Error; err != nil || sourceCount != 1 {
		t.Fatal("cmdb 应用账号必须能够读取业务数据")
	}
	if err := applicationDatabase.Exec("DELETE FROM resource_sources WHERE id = ?", sourceID).Error; err != nil {
		t.Fatal("cmdb 应用账号必须能够删除无依赖业务数据")
	}
	if err := applicationDatabase.Exec("ALTER TABLE projects ADD COLUMN forbidden_ddl TEXT").Error; err == nil {
		t.Fatal("cmdb 应用账号不得执行 ALTER TABLE")
	}
	if columnExists(t, testDatabase.db, "projects", "forbidden_ddl") {
		t.Fatal("被拒绝的应用账号 DDL 不得改变业务结构")
	}
}

// newPostgresTestDatabase 为每个测试场景创建并清理独立数据库，避免约束状态互相污染。
func newPostgresTestDatabase(t *testing.T) *postgresTestDatabase {
	t.Helper()
	baseDSN := os.Getenv(postgresTestDSNEnvironment)
	if baseDSN == "" {
		t.Fatal("未设置 CMDB_POSTGRES_TEST_DSN，请使用 scripts/test-backend.sh 执行 PostgreSQL 17 集成测试")
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

func insertCurrentProject(t *testing.T, database *gorm.DB, code string) int64 {
	t.Helper()
	var id int64
	if err := database.Raw("INSERT INTO projects (code, name) VALUES (?, ?) RETURNING id", code, code).Row().Scan(&id); err != nil {
		t.Fatal("写入业务项目失败")
	}
	return id
}

func insertCurrentSource(t *testing.T, database *gorm.DB, projectID int64, name string, accountID string) int64 {
	t.Helper()
	var id int64
	if err := database.Raw(`
		INSERT INTO resource_sources
		(project_id, provider, name, encrypted_credential, cloud_account_id, identity_verified_at)
		VALUES (?, 'aliyun', ?, 'integration-ciphertext', ?, NOW()) RETURNING id
	`, projectID, name, accountID).Row().Scan(&id); err != nil {
		t.Fatal("写入接入源失败")
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

func assertAccountUniqueConstraint(t *testing.T, database *gorm.DB) {
	t.Helper()
	var indexDefinition string
	if err := database.Raw(`
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = 'public' AND tablename = 'resource_sources'
		  AND indexname = 'uk_resource_sources_provider_account'
	`).Scan(&indexDefinition).Error; err != nil {
		t.Fatal("读取接入源账号唯一约束失败")
	}
	if indexDefinition == "" {
		t.Fatal("数据库结构必须存在命名稳定的云账号唯一约束")
	}
	if strings.Contains(indexDefinition, "WHERE") {
		t.Fatalf("云账号唯一约束不得依赖历史身份状态过滤，实际定义为 %s", indexDefinition)
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

// TestPostgreSQLIntegrationRequiresDSN 验证显式选择 PostgreSQL 测试时缺少环境配置必须失败，不能跳过后虚假通过。
func TestPostgreSQLIntegrationRequiresDSN(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestPostgreSQLAccountIdentityIsRequiredAndUnique$", "-test.count=1")
	command.Env = append(os.Environ(), "CMDB_POSTGRES_TEST_DSN=")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "未设置 CMDB_POSTGRES_TEST_DSN") {
		t.Fatalf("缺少 PostgreSQL 测试连接必须明确失败，实际错误：%v，输出：%s", err, output)
	}
}
