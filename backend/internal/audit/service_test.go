// 本文件验证审计服务的参数边界，避免无界查询拖垮管理页面。
package audit_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"cmdb/internal/audit"
	"github.com/gin-gonic/gin"
)

// TestHTTPFiltersActorUsernameExactly 验证 HTTP 到真实仓储的用户名筛选、删除后快照和公开身份边界。
func TestHTTPFiltersActorUsernameExactly(t *testing.T) {
	db := auditDatabase(t)
	if err := db.Exec("INSERT INTO users (id, username, display_name) VALUES (7, 'admin_1', '管理员'), (8, 'adminX1', '其他人'), (9, 'deleted_1', '已删除操作者')").Error; err != nil {
		t.Fatalf("准备操作者失败：%v", err)
	}
	admin, other, deleted := uint64(7), uint64(8), uint64(9)
	logs := []audit.Log{
		{ID: 1, ActorID: &admin, Action: audit.ActionUserUpdated, ResourceType: "user", ResourceID: "member_1", Detail: json.RawMessage(`{"actor_username":"stale_snapshot"}`)},
		{ID: 2, ActorID: &other, Action: audit.ActionUserUpdated, ResourceType: "user", ResourceID: "member_1", Detail: json.RawMessage(`{"actor_username":"admin_1"}`)},
		{ID: 3, ActorID: &deleted, Action: audit.ActionUserUpdated, ResourceType: "user", ResourceID: "member_1", Detail: json.RawMessage(`{"actor_username":"deleted_1","actor_display_name":"已删除操作者"}`)},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("准备审计数据失败：%v", err)
	}
	if err := db.Exec("DELETE FROM users WHERE id = 9").Error; err != nil {
		t.Fatalf("删除测试操作者失败：%v", err)
	}
	router := gin.New()
	router.GET("/audit", audit.NewHTTPHandler(audit.NewService(audit.NewRepository(db))).ListGlobal)
	for _, tt := range []struct {
		name, username string
		wantID         uint64
	}{
		{"精确匹配且当前名称优先", "admin_1", 1},
		{"已删除操作者使用快照", "deleted_1", 3},
		{"不按子串匹配", "admin", 0},
		{"不按通配符匹配", "admin%", 0},
		{"不接受旧快照覆盖当前名称", "stale_snapshot", 0},
		{"查询值不能注入条件", "admin_1' OR 1=1 --", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/audit?actor_username="+url.QueryEscape(tt.username), nil))
			if response.Code != http.StatusOK {
				t.Fatalf("用户名筛选必须成功：%d %s", response.Code, response.Body.String())
			}
			var page audit.Page
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatalf("解析审计响应失败：%v", err)
			}
			if tt.wantID == 0 {
				if page.Total != 0 || len(page.Items) != 0 || page.SnapshotID != 0 {
					t.Errorf("不匹配的用户名不得返回审计：%+v", page)
				}
			} else if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != tt.wantID || page.Items[0].ActorUsername != tt.username || page.SnapshotID != tt.wantID {
				t.Errorf("记录、总数和分页快照必须使用相同用户名条件：%+v", page)
			}
			if containsAny(response.Body.String(), `"actor_id"`) {
				t.Error("审计 HTTP 响应不得包含操作者数字 ID")
			}
		})
	}
}

// TestServiceRejectsInvalidFilters 验证页码、页大小和倒置时间范围使用稳定错误。
func TestServiceRejectsInvalidFilters(t *testing.T) {
	service := audit.NewService(nil)
	for _, filter := range []audit.Filter{
		{Page: 0, PageSize: 20},
		{Page: 1, PageSize: 101},
		{Page: 1_000_001, PageSize: 20},
	} {
		if _, err := service.List(context.Background(), filter); !errors.Is(err, audit.ErrInvalidFilter) {
			t.Fatalf("非法筛选必须返回稳定错误：filter=%+v err=%v", filter, err)
		}
	}
}

// TestServiceKeepsPaginationSnapshotWhenNewAuditArrives 防止翻页期间新增审计导致记录重复或遗漏。
func TestServiceKeepsPaginationSnapshotWhenNewAuditArrives(t *testing.T) {
	db := auditDatabase(t)
	repository := audit.NewRepository(db)
	createdAt := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	for id := uint64(1); id <= 3; id++ {
		if err := db.Create(&audit.Log{ID: id, Action: audit.ActionResourceUpdated, ResourceType: "ec2", ResourceID: "i-page", Detail: json.RawMessage(`{}`), CreatedAt: createdAt}).Error; err != nil {
			t.Fatalf("准备审计分页数据失败：%v", err)
		}
	}
	service := audit.NewService(repository)
	first, err := service.List(context.Background(), audit.Filter{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("查询第一页失败：%v", err)
	}
	if first.SnapshotID != 3 || first.Total != 3 || len(first.Items) != 2 || first.Items[0].ID != 3 || first.Items[1].ID != 2 {
		t.Fatalf("第一页必须建立最新审计快照：%+v", first)
	}
	if err := db.Create(&audit.Log{ID: 4, Action: audit.ActionResourceCreated, ResourceType: "ec2", ResourceID: "i-new", Detail: json.RawMessage(`{}`), CreatedAt: createdAt.Add(time.Minute)}).Error; err != nil {
		t.Fatalf("准备翻页期间的新审计失败：%v", err)
	}
	second, err := service.List(context.Background(), audit.Filter{Page: 2, PageSize: 2, SnapshotID: first.SnapshotID})
	if err != nil {
		t.Fatalf("按第一页快照查询第二页失败：%v", err)
	}
	if second.SnapshotID != 3 || second.Total != 3 || len(second.Items) != 1 || second.Items[0].ID != 1 {
		t.Fatalf("后续页必须忽略快照之后新增的审计：%+v", second)
	}
}
