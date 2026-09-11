// 本文件验证审计服务的参数边界，避免无界查询拖垮管理页面。
package audit_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cmdb/internal/audit"
)

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
