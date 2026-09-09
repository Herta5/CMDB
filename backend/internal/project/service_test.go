// 本文件通过真实的内存数据库验证项目领域规则，不依赖外部 MySQL 服务。
package project

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github-cmdb/internal/identity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestCreateProjectAllowsEmptyOwnerAndRejectsDuplicateCode 防止未指定负责人时创建失败，或重复项目编码绕过全局唯一边界。
func TestCreateProjectAllowsEmptyOwnerAndRejectsDuplicateCode(t *testing.T) {
	service := newProjectService(t)
	input := CreateInput{Name: "云平台", Code: "cloud", OwnerUserID: nil}

	created, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("未指定负责人时创建项目失败：%v", err)
	}
	if created.OwnerUserID != nil {
		t.Fatalf("未指定负责人时必须保留空负责人，实际为 %d", *created.OwnerUserID)
	}
	if created.Status != ProjectStatusEnabled {
		t.Fatalf("新项目必须默认启用，实际状态为 %q", created.Status)
	}

	if _, err := service.Create(context.Background(), input); !errors.Is(err, ErrDuplicateCode) {
		t.Fatalf("期望重复编码错误，实际为 %v", err)
	}
}

// TestUpdateProjectPreservesCodeAndValidatesStatus 防止更新项目资料时重写稳定编码，或接受未定义生命周期状态。
func TestUpdateProjectPreservesCodeAndValidatesStatus(t *testing.T) {
	service := newProjectService(t)
	created, err := service.Create(context.Background(), CreateInput{Name: "云平台", Code: "cloud"})
	if err != nil {
		t.Fatalf("准备项目失败：%v", err)
	}

	updated, err := service.Update(context.Background(), created.ID, UpdateInput{
		Name:        "云资源平台",
		Description: "托管公有云和 Kubernetes 资源",
		Status:      ProjectStatusDisabled,
		OwnerUserID: nil,
	})
	if err != nil {
		t.Fatalf("更新项目失败：%v", err)
	}
	if updated.Code != "cloud" || updated.Name != "云资源平台" || updated.Status != ProjectStatusDisabled || updated.OwnerUserID != nil {
		t.Fatalf("更新只能修改可变项目资料：got=%+v", updated)
	}

	if _, err := service.Update(context.Background(), created.ID, UpdateInput{Name: "云资源平台", Status: "archived"}); !errors.Is(err, ErrInvalidProjectStatus) {
		t.Fatalf("期望非法状态错误，实际为 %v", err)
	}
}

// TestListForUserRestrictsRegularUserToMembership 防止普通用户通过项目列表枚举没有成员关系的业务项目。
func TestListForUserRestrictsRegularUserToMembership(t *testing.T) {
	service, db := newProjectServiceWithDatabase(t)
	first, err := service.Create(context.Background(), CreateInput{Name: "云平台", Code: "cloud"})
	if err != nil {
		t.Fatalf("创建第一个项目失败：%v", err)
	}
	if _, err := service.Create(context.Background(), CreateInput{Name: "数据平台", Code: "data"}); err != nil {
		t.Fatalf("创建第二个项目失败：%v", err)
	}
	if err := db.Create(&identity.User{ID: 7, Username: "project-viewer", PasswordHash: "test-hash", DisplayName: "项目查看者", GlobalRole: identity.GlobalRoleUser, Status: "active"}).Error; err != nil {
		t.Fatalf("准备项目成员用户失败：%v", err)
	}
	if err := db.Create(&MemberRole{ProjectID: first.ID, UserID: 7, Role: MemberRoleViewer}).Error; err != nil {
		t.Fatalf("准备项目成员关系失败：%v", err)
	}

	regularProjects, err := service.ListForUser(context.Background(), 7, identity.GlobalRoleUser)
	if err != nil {
		t.Fatalf("查询普通用户可见项目失败：%v", err)
	}
	if len(regularProjects) != 1 || regularProjects[0].Code != "cloud" {
		t.Fatalf("普通用户只能看到自己的成员项目：got=%+v", regularProjects)
	}

	administratorProjects, err := service.ListForUser(context.Background(), 7, identity.GlobalRoleSystemAdmin)
	if err != nil {
		t.Fatalf("查询系统管理员项目失败：%v", err)
	}
	if len(administratorProjects) != 2 {
		t.Fatalf("系统管理员必须看到全部项目，实际数量为 %d", len(administratorProjects))
	}
}

// newProjectService 创建每个测试独立的真实仓储，确保唯一性由持久化层与领域错误转换共同保障。
func newProjectService(t *testing.T) *Service {
	t.Helper()
	service, _ := newProjectServiceWithDatabase(t)
	return service
}

// newProjectServiceWithDatabase 创建项目服务及其测试专用数据库，供成员关系边界测试准备真实数据。
func newProjectServiceWithDatabase(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()

	sqlDB, err := sql.Open("sqlite3", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("创建项目测试数据库失败：%v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(sqlite.Dialector{Conn: sqlDB}, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("打开项目测试数据库失败：%v", err)
	}
	if err := db.AutoMigrate(&identity.User{}, &Project{}, &MemberRole{}); err != nil {
		t.Fatalf("创建项目测试表失败：%v", err)
	}
	return NewService(NewRepository(db)), db
}
