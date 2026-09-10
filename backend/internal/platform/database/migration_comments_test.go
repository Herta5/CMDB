// 本文件验证生产迁移完整维护数据库中文元数据，避免管理工具再次显示乱码或空注释。
package database

import (
	"os"
	"strings"
	"testing"
)

// TestCommentRepairMigrationCoversSchema 验证全部业务表和字段都由修复迁移显式覆盖。
func TestCommentRepairMigrationCoversSchema(t *testing.T) {
	content, err := os.ReadFile("../../../migrations/006_repair_schema_comments.sql")
	if err != nil {
		t.Fatalf("读取注释修复迁移失败：%v", err)
	}
	sql := string(content)
	if !strings.Contains(sql, "SET NAMES utf8mb4") {
		t.Fatal("注释迁移必须显式声明 utf8mb4 连接字符集")
	}
	expected := map[string][]string{
		"users":              {"id", "username", "password_hash", "display_name", "email", "global_role", "status", "last_login_at", "created_at", "updated_at"},
		"projects":           {"id", "code", "name", "description", "status", "owner_user_id", "created_at", "updated_at"},
		"project_members":    {"id", "project_id", "user_id", "role", "created_at", "updated_at"},
		"audit_logs":         {"id", "actor_id", "project_id", "action", "resource_type", "resource_id", "detail", "request_ip", "created_at"},
		"resource_sources":   {"id", "project_id", "provider", "name", "region", "encrypted_credential", "credential_hint", "config", "enabled", "sync_interval_minutes", "last_sync_at", "next_sync_at", "created_at", "updated_at"},
		"resources":          {"id", "project_id", "source_id", "provider", "resource_type", "external_id", "name", "region", "zone", "cloud_status", "lifecycle_status", "raw_attributes", "first_seen_at", "last_seen_at", "missing_since", "created_at", "updated_at"},
		"resource_endpoints": {"id", "resource_id", "kind", "address", "port", "protocol", "resolved_ips", "resolved_at"},
		"sync_jobs":          {"id", "project_id", "source_id", "previous_job_id", "status", "trigger", "statistics", "error_summary", "started_at", "finished_at"},
	}
	quote := string(rune(96))
	for table, columns := range expected {
		if !strings.Contains(sql, "ALTER TABLE "+quote+table+quote) {
			t.Errorf("表 %s 缺少中文表注释", table)
		}
		for _, column := range columns {
			if !strings.Contains(sql, "MODIFY COLUMN "+quote+column+quote) {
				t.Errorf("字段 %s.%s 缺少注释修复", table, column)
			}
		}
	}
}

// TestCloudResourceSchemaDefinesThreeAssetKinds 验证初始化结构只创建三类带中文说明的资产表。
func TestCloudResourceSchemaDefinesThreeAssetKinds(t *testing.T) {
	content, err := os.ReadFile("../../../migrations/002_cloud_resources.sql")
	if err != nil {
		t.Fatalf("读取云资源初始化结构失败：%v", err)
	}
	sql := string(content)
	for _, fragment := range []string{"CREATE TABLE IF NOT EXISTS resources_servers", "CREATE TABLE IF NOT EXISTS resources_databases", "CREATE TABLE IF NOT EXISTS resources_load_balancers", "asset_status", "访问端点", "云服务器资产", "云数据库资产", "云负载均衡资产", "UNIQUE KEY uk_"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("云资源初始化结构缺少必要结构或中文注释：%s", fragment)
		}
	}
	if strings.Contains(sql, "CREATE TABLE IF NOT EXISTS resources (") || strings.Contains(sql, "CREATE TABLE IF NOT EXISTS resource_endpoints") {
		t.Fatal("初始化结构不得继续创建两张旧资产表")
	}
}
