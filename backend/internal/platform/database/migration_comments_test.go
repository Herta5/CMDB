// 本文件验证 PostgreSQL 初始化脚本的结构、权限和中文元数据，防止重新引入 MySQL 方言或越权账号。
package database

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const (
	// applicationRoleInitializationPath 是应用账号初始化脚本相对于当前测试包的固定位置。
	applicationRoleInitializationPath = "../../../database/init/001_create_app_role.sh"
	// schemaInitializationPath 是由 PostgreSQL 管理员执行的唯一业务结构初始化文件。
	schemaInitializationPath = "../../../database/init/002_schema.sql"
)

// TestPostgreSQLInitializationCreatesRestrictedApplicationRole 防止应用账号取得管理员或建库权限。
func TestPostgreSQLInitializationCreatesRestrictedApplicationRole(t *testing.T) {
	content := readInitializationFile(t, applicationRoleInitializationPath)
	roleScript := string(content)

	for _, fragment := range []string{
		"POSTGRES_USER",
		"POSTGRES_DB",
		"POSTGRES_PASSWORD",
		"DB_PASSWORD",
		"psql",
		"--set=app_password=\"$DB_PASSWORD\"",
		"CREATE ROLE cmdb LOGIN PASSWORD :'app_password'",
		"NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION",
		"REVOKE ALL ON DATABASE :\"db_name\" FROM PUBLIC",
		"GRANT CONNECT ON DATABASE :\"db_name\" TO cmdb",
		"FROM pg_database",
		"WHERE datallowconn AND datname <> :'db_name'",
		"\\gexec",
	} {
		if !strings.Contains(roleScript, fragment) {
			t.Errorf("应用账号初始化脚本缺少受限账号或数据库边界：%s", fragment)
		}
	}

	if strings.Contains(roleScript, "echo \"$DB_PASSWORD\"") || strings.Contains(roleScript, "echo $DB_PASSWORD") {
		t.Fatal("应用账号初始化脚本不得回显应用密码")
	}
}

// TestPostgreSQLInitializationRejectsUnsafeAdministratorEnvironment 验证脚本会在调用 psql 前拒绝不安全管理员环境。
func TestPostgreSQLInitializationRejectsUnsafeAdministratorEnvironment(t *testing.T) {
	testCases := []struct {
		name        string
		environment map[string]string
		expected    string
		secret      string
	}{
		{
			name: "管理员不是 postgres",
			environment: map[string]string{
				"POSTGRES_USER":     "operator",
				"POSTGRES_DB":       "cmdb",
				"POSTGRES_PASSWORD": "admin-password",
				"DB_PASSWORD":       "app-password",
			},
			expected: "POSTGRES_USER 必须为 postgres",
			secret:   "admin-password",
		},
		{
			name: "管理员与应用密码相同",
			environment: map[string]string{
				"POSTGRES_USER":     "postgres",
				"POSTGRES_DB":       "cmdb",
				"POSTGRES_PASSWORD": "shared-password",
				"DB_PASSWORD":       "shared-password",
			},
			expected: "POSTGRES_PASSWORD 与 DB_PASSWORD 不得相同",
			secret:   "shared-password",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := runApplicationRoleScript(testCase.environment)
			if err == nil {
				t.Fatal("不安全的管理员环境不应继续执行应用账号初始化")
			}
			if !strings.Contains(output, testCase.expected) {
				t.Fatalf("初始化脚本未拒绝不安全环境，输出：%s", output)
			}
			if strings.Contains(output, testCase.secret) {
				t.Fatalf("初始化脚本错误输出泄露了密码：%s", output)
			}
		})
	}
}

// TestPostgreSQLSchemaDefinesBusinessStructure 验证业务结构使用 PostgreSQL 方言且完整保留领域约束。
func TestPostgreSQLSchemaDefinesBusinessStructure(t *testing.T) {
	schema := string(readInitializationFile(t, schemaInitializationPath))
	expectedColumns := map[string][]string{
		"users": {
			"id", "username", "password_hash", "display_name", "email", "global_role", "status", "last_login_at", "created_at", "updated_at",
		},
		"projects": {
			"id", "code", "name", "description", "status", "owner_user_id", "created_at", "updated_at",
		},
		"project_members": {
			"id", "project_id", "user_id", "role", "created_at", "updated_at",
		},
		"audit_logs": {
			"id", "actor_id", "project_id", "action", "resource_type", "resource_id", "detail", "request_ip", "created_at",
		},
		"resource_sources": {
			"id", "project_id", "provider", "name", "region", "encrypted_credential", "credential_hint", "config", "enabled", "sync_interval_minutes", "last_sync_at", "next_sync_at", "created_at", "updated_at",
		},
		"resources_servers": {
			"id", "project_id", "source_id", "provider", "resource_type", "external_id", "name", "region", "zone", "cloud_status", "asset_status", "private_ips", "public_ips", "raw_attributes", "first_seen_at", "last_seen_at", "missing_since", "created_at", "updated_at",
		},
		"resources_databases": {
			"id", "project_id", "source_id", "provider", "resource_type", "external_id", "name", "region", "zone", "cloud_status", "asset_status", "engine", "engine_version", "endpoints", "raw_attributes", "first_seen_at", "last_seen_at", "missing_since", "created_at", "updated_at",
		},
		"resources_load_balancers": {
			"id", "project_id", "source_id", "provider", "resource_type", "external_id", "name", "region", "zone", "cloud_status", "asset_status", "network_type", "endpoints", "raw_attributes", "first_seen_at", "last_seen_at", "missing_since", "created_at", "updated_at",
		},
		"sync_jobs": {
			"id", "project_id", "source_id", "previous_job_id", "status", "trigger", "statistics", "error_summary", "started_at", "finished_at",
		},
	}

	for table, columns := range expectedColumns {
		if !strings.Contains(schema, "CREATE TABLE "+table) {
			t.Errorf("初始化结构缺少业务表：%s", table)
		}
		if !strings.Contains(schema, "COMMENT ON TABLE public."+table+" IS") {
			t.Errorf("业务表 %s 缺少中文表注释", table)
		}
		for _, column := range columns {
			if !strings.Contains(schema, "COMMENT ON COLUMN public."+table+"."+column+" IS") {
				t.Errorf("业务字段 %s.%s 缺少中文字段注释", table, column)
			}
		}
	}

	for _, fragment := range []string{
		"BIGINT GENERATED BY DEFAULT AS IDENTITY",
		"JSONB",
		"TIMESTAMPTZ(3)",
		"CHECK (global_role IN",
		"CHECK (asset_status IN",
		"CHECK (status IN",
		"FOREIGN KEY",
		"CREATE INDEX",
		"GRANT USAGE ON SCHEMA public TO cmdb",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO cmdb",
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO cmdb",
		"IF current_user <> 'postgres' THEN",
		"CMDB 初始化结构必须由 postgres 管理员执行",
	} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("PostgreSQL 初始化结构缺少必要定义：%s", fragment)
		}
	}

	allInitialization := string(readInitializationFile(t, applicationRoleInitializationPath)) + schema
	for _, forbidden := range []string{"AUTO_INCREMENT", "ENUM(", string(rune(96)), "ON UPDATE"} {
		if strings.Contains(allInitialization, forbidden) {
			t.Errorf("PostgreSQL 初始化文件不得包含 MySQL 方言：%s", forbidden)
		}
	}
}

// TestPostgreSQLInitializationRemovesMySQLMigrationChain 防止容器继续加载旧 MySQL 增量迁移。
func TestPostgreSQLInitializationRemovesMySQLMigrationChain(t *testing.T) {
	for _, path := range []string{
		"../../../migrations/001_schema.sql",
		"../../../migrations/002_cloud_resources.sql",
		"../../../migrations/003_async_sync_jobs.sql",
		"../../../migrations/004_remove_kubernetes.sql",
		"../../../migrations/005_remove_viewer_role.sql",
		"../../../migrations/006_repair_schema_comments.sql",
	} {
		_, err := os.Stat(path)
		if err == nil {
			t.Errorf("旧 MySQL 初始化文件仍然存在：%s", path)
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("检查旧 MySQL 初始化文件 %s 时失败：%v", path, err)
		}
	}
}

// readInitializationFile 统一读取初始化文件，并在路径错误时提供中文测试上下文。
func readInitializationFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取初始化文件 %s 失败：%v", path, err)
	}
	return content
}

// runApplicationRoleScript 在隔离环境中执行真实脚本，只覆盖必须在连接数据库前阻断的安全分支。
func runApplicationRoleScript(environment map[string]string) (string, error) {
	command := exec.Command("sh", applicationRoleInitializationPath)
	command.Env = applicationRoleScriptEnvironment(environment)
	output, err := command.CombinedOutput()
	return string(output), err
}

// applicationRoleScriptEnvironment 排除继承环境中的数据库变量，避免测试误用真实部署凭证。
func applicationRoleScriptEnvironment(values map[string]string) []string {
	environment := make([]string, 0, len(os.Environ())+len(values))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "POSTGRES_USER", "POSTGRES_DB", "POSTGRES_PASSWORD", "DB_PASSWORD":
			continue
		}
		environment = append(environment, entry)
	}
	for name, value := range values {
		environment = append(environment, name+"="+value)
	}
	return environment
}
