// 本文件验证统一审计仓储的真实写入、脱敏、筛选和稳定分页行为。
package audit_test

import (
	"context"
	"encoding/json"
	"strings"
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

// TestRecordRecursivelyRemovesInternalUserReferences 验证用户内部引用在持久化前被清理，包括数组内的数组。
func TestRecordRecursivelyRemovesInternalUserReferences(t *testing.T) {
	db := auditDatabase(t)
	if err := audit.NewRepository(db).Record(context.Background(), audit.Entry{
		Action: audit.ActionUserUpdated, ResourceType: "user", ResourceID: "admin_1",
		Detail: map[string]any{
			"actor_id": 7, "user_id": 8, "target_user_id": 9,
			"nested": map[string]any{"owner_user_id": 10, "previous_owner_user_id": 11, "target_username": "member_1"},
			"arrays": []any{[]any{map[string]any{"ActorID": 12, "User-ID": 13, "target_user_id": 14, "password": "虚构密码", "safe": "保留"}}},
		},
	}); err != nil {
		t.Fatalf("写入用户审计失败：%v", err)
	}
	var stored audit.Log
	if err := db.First(&stored).Error; err != nil {
		t.Fatalf("读取已持久化审计失败：%v", err)
	}
	want := `{"arrays":[[{"safe":"保留"}]],"nested":{"target_username":"member_1"}}`
	if string(stored.Detail) != want {
		t.Fatalf("持久化详情必须递归删除用户内部引用且保留安全字段：%s", stored.Detail)
	}
}

// TestListSanitizesLegacyUserReferences 验证历史记录无法通过详情或用户对象标识泄露数字 ID。
func TestListSanitizesLegacyUserReferences(t *testing.T) {
	for _, tt := range []struct {
		name, resourceType, resourceID, detail, wantID string
	}{
		{"用户快照", "user", "12", `{"target_username":"member_1"}`, "member_1"},
		{"成员快照", "project_member", "12", `{"target_username":"member_1"}`, "member_1"},
		{"无快照", "user", "12", `{}`, ""},
		{"成员无详情", "project_member", "12", `null`, ""},
		{"空详情", "user", "12", "", ""},
		{"巨大旧标识", "user", "99999999999999999999999999999999", `{}`, ""},
		{"数字用户名快照", "user", "12", `{"target_username":"1234"}`, "1234"},
		{"已有用户名", "project_member", "member_1", `{}`, "member_1"},
		{"其他对象数字标识", "project", "12", `{}`, "12"},
		{"保留其他业务数字精度", "project", "12", `{"project_id":9007199254740993,"user_id":7}`, "12"},
		{"递归历史详情", "user", "12", `{"actor_id":7,"user_id":8,"target_user_id":9,"nested":{"owner_user_id":10,"previous_owner_user_id":11},"arrays":[[{"user_id":12,"password":"虚构密码","safe":"保留"}]]}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := auditDatabase(t)
			actorID := uint64(7)
			entry := audit.Log{ActorID: &actorID, Action: audit.ActionUserUpdated, ResourceType: tt.resourceType, ResourceID: tt.resourceID, Detail: json.RawMessage(tt.detail)}
			if err := db.Create(&entry).Error; err != nil {
				t.Fatalf("准备历史审计失败：%v", err)
			}
			items, _, _, err := audit.NewRepository(db).List(context.Background(), audit.Filter{Page: 1, PageSize: 20})
			if err != nil || len(items) != 1 {
				t.Fatalf("查询历史审计失败：%v，记录数=%d", err, len(items))
			}
			if items[0].ResourceID != tt.wantID {
				t.Errorf("用户对象必须使用用户名或空标识：得到 %q，期望 %q", items[0].ResourceID, tt.wantID)
			}
			encoded, err := json.Marshal(items[0])
			if err != nil {
				t.Fatalf("序列化审计失败：%v", err)
			}
			if containsAny(string(encoded), `"actor_id"`, `"user_id"`, `"target_user_id"`, `"owner_user_id"`, `"previous_owner_user_id"`, `"password"`) {
				t.Errorf("公开审计不得包含用户内部引用或凭证：%s", encoded)
			}
			if tt.name == "递归历史详情" && !strings.Contains(string(encoded), `"safe":"保留"`) {
				t.Error("历史详情清理必须保留安全字段")
			}
			if tt.name == "保留其他业务数字精度" && !strings.Contains(string(encoded), `"project_id":9007199254740993`) {
				t.Errorf("重新编码历史详情不得丢失其他业务数字的精度：%s", encoded)
			}
		})
	}
}

// TestListFiltersProjectActionActorObjectAndTime 验证所有页面筛选都由数据库查询真实执行。
func TestListFiltersProjectActionActorObjectAndTime(t *testing.T) {
	db := auditDatabase(t)
	repository := audit.NewRepository(db)
	projectA, projectB := uint64(9), uint64(10)
	actor := uint64(7)
	if err := db.Exec("INSERT INTO users (id, username, display_name) VALUES (7, 'admin_1', '管理员')").Error; err != nil {
		t.Fatalf("准备操作者失败：%v", err)
	}
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
	items, total, _, err := repository.List(context.Background(), audit.Filter{ProjectID: &projectA, Action: audit.ActionUserUpdated, ActorUsername: "admin_1", ResourceType: "user", ResourceID: "cloud-user", StartAt: &start, EndAt: &end, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("筛选审计失败：%v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != 2 {
		t.Fatalf("筛选必须只返回完全匹配的记录：total=%d items=%+v", total, items)
	}
}

// TestListFiltersEffectiveUserResourceID 防止通过历史内部数字 ID 查询用户审计，并保持用户名碰撞时的精确筛选。
func TestListFiltersEffectiveUserResourceID(t *testing.T) {
	for _, resourceType := range []string{"user", "project_member"} {
		t.Run(resourceType, func(t *testing.T) {
			db := auditDatabase(t)
			createdAt := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
			logs := []audit.Log{
				{ID: 1, ResourceType: resourceType, ResourceID: "12", Detail: json.RawMessage(`{"target_username":"member_1"}`)},
				{ID: 2, ResourceType: resourceType, ResourceID: "12", Detail: json.RawMessage(`{}`)},
				{ID: 3, ResourceType: resourceType, ResourceID: "12", Detail: json.RawMessage(`{"target_username":"12"}`)},
				{ID: 4, ResourceType: resourceType, ResourceID: "99", Detail: json.RawMessage(`{"target_username":"12"}`)},
				{ID: 5, ResourceType: resourceType, ResourceID: "new_member", Detail: json.RawMessage(`{"target_username":"ignored_snapshot"}`)},
				{ID: 6, ResourceType: "project", ResourceID: "12", Detail: json.RawMessage(`{"target_username":"ignored_snapshot"}`)},
				{ID: 7, ResourceType: resourceType, ResourceID: "12", Detail: json.RawMessage(`{"target_username":12}`)},
				{ID: 8, ResourceType: resourceType, ResourceID: "45", Detail: json.RawMessage(`null`)},
			}
			for index := range logs {
				logs[index].Action = audit.ActionUserUpdated
				logs[index].CreatedAt = createdAt
			}
			if err := db.Create(&logs).Error; err != nil {
				t.Fatalf("准备用户对象审计失败：%v", err)
			}
			for _, tt := range []struct {
				name, resourceType, resourceID string
				wantIDs                        []uint64
			}{
				{"历史对象按用户名快照查询", resourceType, "member_1", []uint64{1}},
				{"数字用户名不会匹配相同旧内部标识", resourceType, "12", []uint64{4, 3}},
				{"旧内部标识不能作为查询入口", resourceType, "99", nil},
				{"无快照的数字标识不能查询", resourceType, "45", nil},
				{"新用户名按自身查询", resourceType, "new_member", []uint64{5}},
				{"新用户名不使用其他快照", resourceType, "ignored_snapshot", nil},
				{"用户名不按子串匹配", resourceType, "member", nil},
				{"其他对象保留原始标识", "project", "12", []uint64{6}},
				{"全局对象筛选也遵守用户名边界", "", "12", []uint64{6, 4, 3}},
			} {
				t.Run(tt.name, func(t *testing.T) {
					items, total, snapshot, err := audit.NewRepository(db).List(context.Background(), audit.Filter{ResourceType: tt.resourceType, ResourceID: tt.resourceID, Page: 1, PageSize: 20})
					if err != nil {
						t.Fatalf("按公开对象标识查询失败：%v", err)
					}
					if total != int64(len(tt.wantIDs)) || len(items) != len(tt.wantIDs) {
						t.Fatalf("必须仅返回有效公开对象标识相等的记录：总数=%d，记录=%+v，期望日志 ID=%v", total, items, tt.wantIDs)
					}
					var wantSnapshot uint64
					if len(tt.wantIDs) > 0 {
						wantSnapshot = tt.wantIDs[0]
					}
					if snapshot != wantSnapshot {
						t.Errorf("快照边界必须使用同一对象筛选：得到 %d，期望 %d", snapshot, wantSnapshot)
					}
					for index, item := range items {
						if item.ID != tt.wantIDs[index] || item.ResourceID != tt.resourceID {
							t.Errorf("筛选和公开输出必须采用同一对象标识：日志=%+v，查询标识=%q", item, tt.resourceID)
						}
					}
				})
			}
		})
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
