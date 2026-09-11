// 本文件验证统一审计仓储的真实写入、脱敏、筛选和稳定分页行为。
package audit_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cmdb/internal/audit"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestRecordUsesActorContextAndFiltersSensitiveDetail 防止业务调用方误把敏感字段写入长期审计。
func TestRecordUsesActorContextAndFiltersSensitiveDetail(t *testing.T) {
	db := auditDatabase(t)
	repository := audit.NewRepository(db)
	projectID := uint64(9)
	ctx := audit.WithActor(context.Background(), 7, "203.0.113.8")
	err := repository.Record(ctx, audit.Entry{
		ProjectID:    &projectID,
		Action:       audit.ActionUserUpdated,
		ResourceType: "user",
		ResourceID:   "12",
		Detail: map[string]any{
			"target_username": "cloud-user",
			"password":        "never-store-me",
			"nested": map[string]any{
				"access_key_id":     "never-store-key",
				"access_key_secret": "never-store-aliyun-secret",
				"changed":           true,
			},
			"typed_items": []map[string]any{
				{
					"session_token": "never-store-session",
					"safe_name":     "第一层安全值",
					"children": []map[string]any{
						{
							"api_key":       "never-store-api-key",
							"client_secret": "never-store-client-secret",
							"authorization": "never-store-authorization",
							"safe_child":    "第二层安全值",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("写入审计失败：%v", err)
	}
	var persisted audit.Log
	if err := db.First(&persisted).Error; err != nil {
		t.Fatalf("读取审计失败：%v", err)
	}
	if persisted.ActorID == nil || *persisted.ActorID != 7 || persisted.RequestIP != "203.0.113.8" {
		t.Fatalf("审计必须保存请求上下文中的操作者和来源 IP：%+v", persisted)
	}
	encoded := string(persisted.Detail)
	if encoded == "" || containsAny(encoded, "never-store-me", "never-store-key", "never-store-aliyun-secret", "never-store-session", "never-store-api-key", "never-store-client-secret", "never-store-authorization", "password", "access_key_id", "access_key_secret", "session_token", "api_key", "client_secret", "authorization") {
		t.Fatalf("审计详情必须递归过滤敏感字段：%s", encoded)
	}
	if !containsAny(encoded, "cloud-user", "第一层安全值", "第二层安全值") {
		t.Fatalf("审计详情必须保留允许展示的目标快照：%s", encoded)
	}
}

// TestListFiltersProjectActionActorObjectAndTime 验证所有页面筛选都由数据库查询真实执行。
func TestListFiltersProjectActionActorObjectAndTime(t *testing.T) {
	db := auditDatabase(t)
	repository := audit.NewRepository(db)
	projectA, projectB := uint64(9), uint64(10)
	actor := uint64(7)
	now := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	logs := []audit.Log{
		{ID: 1, ActorID: &actor, ProjectID: &projectA, Action: audit.ActionProjectUpdated, ResourceType: "project", ResourceID: "9", Detail: json.RawMessage(`{"name":"平台项目"}`), CreatedAt: now.Add(-time.Hour)},
		{ID: 2, ActorID: &actor, ProjectID: &projectA, Action: audit.ActionUserUpdated, ResourceType: "user", ResourceID: "12", Detail: json.RawMessage(`{"target_username":"cloud-user"}`), CreatedAt: now},
		{ID: 3, ProjectID: &projectB, Action: audit.ActionResourceCreated, ResourceType: "ec2", ResourceID: "i-other", Detail: json.RawMessage(`{}`), CreatedAt: now.Add(time.Hour)},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("准备审计数据失败：%v", err)
	}
	start, end := now.Add(-time.Minute), now.Add(time.Minute)
	items, total, _, err := repository.List(context.Background(), audit.Filter{ProjectID: &projectA, Action: audit.ActionUserUpdated, ActorID: &actor, ResourceType: "user", ResourceID: "12", StartAt: &start, EndAt: &end, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("筛选审计失败：%v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != 2 {
		t.Fatalf("筛选必须只返回完全匹配的记录：total=%d items=%+v", total, items)
	}
}

// TestListUsesStableNewestFirstPagination 防止同一时间产生的审计在翻页时重复或遗漏。
func TestListUsesStableNewestFirstPagination(t *testing.T) {
	db := auditDatabase(t)
	repository := audit.NewRepository(db)
	createdAt := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	for id := uint64(1); id <= 3; id++ {
		if err := db.Create(&audit.Log{ID: id, Action: audit.ActionResourceUpdated, ResourceType: "ec2", ResourceID: "i-page", Detail: json.RawMessage(`{}`), CreatedAt: createdAt}).Error; err != nil {
			t.Fatalf("准备分页数据失败：%v", err)
		}
	}
	items, total, _, err := repository.List(context.Background(), audit.Filter{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("分页查询失败：%v", err)
	}
	if total != 3 || len(items) != 1 || items[0].ID != 1 {
		t.Fatalf("相同时间必须按 ID 倒序稳定分页：total=%d items=%+v", total, items)
	}
}

// auditDatabase 创建只包含审计表的真实 SQLite 仓储测试数据库。
func auditDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("创建审计测试数据库失败：%v", err)
	}
	if err := db.AutoMigrate(&audit.Log{}); err != nil {
		t.Fatalf("迁移审计测试表失败：%v", err)
	}
	// 查询仓储会关联公开显示名称，测试建立最小身份与项目表但不依赖其他领域实现。
	if err := db.Exec("CREATE TABLE users (id integer primary key, username text, display_name text)").Error; err != nil {
		t.Fatalf("创建审计身份关联表失败：%v", err)
	}
	if err := db.Exec("CREATE TABLE projects (id integer primary key, name text)").Error; err != nil {
		t.Fatalf("创建审计项目关联表失败：%v", err)
	}
	return db
}

// containsAny 使用直接字符串匹配验证序列化结果，不复用生产脱敏逻辑形成同源断言。
func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if candidate != "" && len(value) >= len(candidate) {
			for index := 0; index+len(candidate) <= len(value); index++ {
				if value[index:index+len(candidate)] == candidate {
					return true
				}
			}
		}
	}
	return false
}
