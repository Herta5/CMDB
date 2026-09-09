// 本文件通过真实的内存数据库验证项目领域规则，不依赖外部 MySQL 服务。
package project

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"cmdb/internal/identity"
	"github.com/go-sql-driver/mysql"
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

// TestUpdateMemberRoleReturnsPersistedMember 防止角色更新响应丢失成员主键和创建时间，误导调用方把已有关系当成新记录。
func TestUpdateMemberRoleReturnsPersistedMember(t *testing.T) {
	service, db := newProjectServiceWithDatabase(t)
	createdProject, err := service.Create(context.Background(), CreateInput{Name: "云平台", Code: "cloud"})
	if err != nil {
		t.Fatalf("准备项目失败：%v", err)
	}
	if err := db.Create(&identity.User{ID: 7, Username: "member-update", PasswordHash: "test-hash", DisplayName: "待更新成员", GlobalRole: identity.GlobalRoleUser, Status: "active"}).Error; err != nil {
		t.Fatalf("准备成员用户失败：%v", err)
	}
	createdMember, err := service.AddMember(context.Background(), createdProject.ID, 7, MemberRoleViewer)
	if err != nil {
		t.Fatalf("准备成员关系失败：%v", err)
	}

	updatedMember, err := service.UpdateMemberRole(context.Background(), createdProject.ID, 7, MemberRoleMember)
	if err != nil {
		t.Fatalf("更新成员角色失败：%v", err)
	}
	if updatedMember.ID != createdMember.ID || !updatedMember.CreatedAt.Equal(createdMember.CreatedAt) || updatedMember.Role != MemberRoleMember {
		t.Fatalf("角色更新必须返回真实持久化成员：created=%+v updated=%+v", createdMember, updatedMember)
	}
	var persisted MemberRole
	if err := db.Where("project_id = ? AND user_id = ?", createdProject.ID, 7).First(&persisted).Error; err != nil {
		t.Fatalf("读取更新后的成员关系失败：%v", err)
	}
	if persisted.ID != updatedMember.ID || !persisted.CreatedAt.Equal(updatedMember.CreatedAt) || persisted.Role != updatedMember.Role {
		t.Fatalf("响应必须准确反映已持久化成员：persisted=%+v updated=%+v", persisted, updatedMember)
	}
}

// TestUpdateDoesNotResurrectProjectDeletedAfterRead 防止并发删除发生在读取和更新之间时，Save 把已删除项目重新插入。
func TestUpdateDoesNotResurrectProjectDeletedAfterRead(t *testing.T) {
	service, db := newProjectServiceWithDatabase(t)
	created, err := service.Create(context.Background(), CreateInput{Name: "云平台", Code: "cloud"})
	if err != nil {
		t.Fatalf("准备项目失败：%v", err)
	}
	service = NewService(&deleteProjectAfterReadRepository{Repository: NewRepository(db), db: db})

	_, err = service.Update(context.Background(), created.ID, UpdateInput{Name: "云资源平台", Status: ProjectStatusDisabled})
	if !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("并发删除后的更新必须返回项目不存在，实际为 %v", err)
	}
	var remaining int64
	if err := db.Model(&Project{}).Where("id = ?", created.ID).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("并发删除后的更新不得复活项目：count=%d err=%v", remaining, err)
	}
}

// TestCreateConvertsMySQLDuplicateKeyWhenPrecheckMisses 防止并发创建绕过预查询后将 MySQL 1062 误报为内部错误。
func TestCreateConvertsMySQLDuplicateKeyWhenPrecheckMisses(t *testing.T) {
	service := NewService(&duplicateOnCreateRepository{})

	_, err := service.Create(context.Background(), CreateInput{Name: "云平台", Code: "cloud"})
	if !errors.Is(err, ErrDuplicateCode) {
		t.Fatalf("MySQL 重复键必须转换为领域错误，实际为 %v", err)
	}
}

// TestUpdatePropagatesRepositoryFailure 防止数据库不可用时被错误转换为项目不存在。
func TestUpdatePropagatesRepositoryFailure(t *testing.T) {
	repositoryFailure := errors.New("项目数据库不可用")
	service := NewService(&findProjectFailureRepository{err: repositoryFailure})

	_, err := service.Update(context.Background(), 1, UpdateInput{Name: "云平台", Status: ProjectStatusEnabled})
	if !errors.Is(err, repositoryFailure) || errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("更新必须保留仓储错误而非报告项目不存在，实际为 %v", err)
	}
}

// TestDeletePropagatesRepositoryFailure 防止数据库不可用时被错误转换为项目不存在。
func TestDeletePropagatesRepositoryFailure(t *testing.T) {
	repositoryFailure := errors.New("项目数据库不可用")
	service := NewService(&findProjectFailureRepository{err: repositoryFailure})

	err := service.Delete(context.Background(), 1)
	if !errors.Is(err, repositoryFailure) || errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("删除必须保留仓储错误而非报告项目不存在，实际为 %v", err)
	}
}

// deleteProjectAfterReadRepository 在测试中精确模拟读取完成后、更新开始前被并发删除的时序。
type deleteProjectAfterReadRepository struct {
	Repository
	db *gorm.DB
}

// FindByID 返回已读取的项目后立即删除它，使服务更新路径面对真实的零行更新条件。
func (r *deleteProjectAfterReadRepository) FindByID(ctx context.Context, id uint64) (*Project, error) {
	project, err := r.Repository.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Delete(&Project{}, id).Error; err != nil {
		return nil, err
	}
	return project, nil
}

// duplicateOnCreateRepository 模拟预查询未命中后 MySQL 在写入阶段返回 1062 的并发冲突。
type duplicateOnCreateRepository struct {
	Repository
}

// FindByCode 固定模拟创建前不存在同编码项目。
func (r *duplicateOnCreateRepository) FindByCode(context.Context, string) (*Project, error) {
	return nil, gorm.ErrRecordNotFound
}

// Create 返回未由 GORM TranslateError 转换的原始 MySQL 唯一键错误。
func (r *duplicateOnCreateRepository) Create(context.Context, *Project) error {
	return &mysql.MySQLError{Number: 1062, Message: "Duplicate entry"}
}

// findProjectFailureRepository 模拟项目读取阶段的基础设施错误，其他方法不应在这些测试中被调用。
type findProjectFailureRepository struct {
	Repository
	err error
}

// FindByID 返回基础设施错误，验证服务不会先用 nil 结果误判项目不存在。
func (r *findProjectFailureRepository) FindByID(context.Context, uint64) (*Project, error) {
	return nil, r.err
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
