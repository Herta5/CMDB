// 本文件验证身份服务在仓储异常时保持用户状态与审计记录一致。
package identity

import (
	"context"
	"errors"
	"testing"

	"cmdb/internal/audit"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestUpdateUserStatusStopsWhenPreviousUserCannotBeRead 防止旧状态读取失败后仍修改用户并产生不完整审计。
func TestUpdateUserStatusStopsWhenPreviousUserCannotBeRead(t *testing.T) {
	lookupFailure := errors.New("用户读取失败")
	repository := &statusLookupFailureRepository{
		UserRepository: nil,
		user:           User{ID: 12, Username: "status-user", Status: "active"},
		err:            lookupFailure,
	}
	auditDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("创建审计测试数据库失败：%v", err)
	}
	if err := auditDB.AutoMigrate(&audit.Log{}); err != nil {
		t.Fatalf("创建审计测试表失败：%v", err)
	}
	service := NewService(repository, "identity-test-signing-key", audit.NewRepository(auditDB))

	if _, err := service.UpdateUserStatus(context.Background(), 7, repository.user.ID, "disabled"); !errors.Is(err, lookupFailure) {
		t.Fatalf("旧用户读取失败必须原样返回：%v", err)
	}
	if repository.user.Status != "active" {
		t.Fatalf("旧用户读取失败时不得修改状态：%s", repository.user.Status)
	}
	var count int64
	if err := auditDB.Model(&audit.Log{}).Count(&count).Error; err != nil {
		t.Fatalf("查询审计记录失败：%v", err)
	}
	if count != 0 {
		t.Fatalf("旧用户读取失败时不得写入缺少快照的审计：%d", count)
	}
}

// TestCreateUserRollsBackWhenAuditWriteFails 防止用户已经创建但对应审计缺失。
func TestCreateUserRollsBackWhenAuditWriteFails(t *testing.T) {
	db := identityAuditFailureDatabase(t)
	service := NewService(NewUserRepository(db), "identity-test-signing-key", audit.NewRepository(db))

	_, err := service.CreateUser(context.Background(), CreateUserInput{Username: "atomic-create", Password: "long-enough-password", DisplayName: "原子创建用户", GlobalRole: GlobalRoleUser, Status: "active"})
	if err == nil {
		t.Fatal("审计写入失败时创建用户必须返回错误")
	}
	var count int64
	if err := db.Model(&User{}).Where("username = ?", "atomic-create").Count(&count).Error; err != nil {
		t.Fatalf("查询创建回滚结果失败：%v", err)
	}
	if count != 0 {
		t.Fatalf("审计写入失败时必须回滚新用户：%d", count)
	}
}

// TestUpdateUserRollsBackProfileAndPermissionsWhenAuditWriteFails 防止用户资料与项目授权先于审计单独提交。
func TestUpdateUserRollsBackProfileAndPermissionsWhenAuditWriteFails(t *testing.T) {
	db := identityAuditFailureDatabase(t)
	if err := db.Exec("INSERT INTO projects (id, name) VALUES (?, ?)", 1, "原项目").Error; err != nil {
		t.Fatalf("准备项目失败：%v", err)
	}
	repository := NewUserRepository(db)
	user := &User{Username: "atomic-update", PasswordHash: "test-hash", DisplayName: "原名称", GlobalRole: GlobalRoleUser, Status: "active"}
	if err := repository.CreateWithPermissions(context.Background(), user, []ProjectPermission{{ProjectID: 1, Role: "member"}}); err != nil {
		t.Fatalf("准备用户失败：%v", err)
	}
	service := NewService(repository, "identity-test-signing-key", audit.NewRepository(db))

	_, err := service.UpdateUser(context.Background(), 99, user.ID, UpdateUserInput{DisplayName: "新名称", GlobalRole: GlobalRoleUser, Status: "active", ProjectPermissions: []ProjectPermission{{ProjectID: 1, Role: "project_admin"}}})
	if err == nil {
		t.Fatal("审计写入失败时编辑用户必须返回错误")
	}
	persisted, findErr := repository.FindByID(context.Background(), user.ID)
	if findErr != nil {
		t.Fatalf("读取编辑回滚结果失败：%v", findErr)
	}
	if persisted.DisplayName != "原名称" || len(persisted.ProjectPermissions) != 1 || persisted.ProjectPermissions[0].Role != "member" {
		t.Fatalf("审计写入失败时必须同时回滚资料和项目授权：%+v", persisted)
	}
}

// TestUpdateUserStatusRollsBackWhenAuditWriteFails 防止用户状态改变后审计缺失。
func TestUpdateUserStatusRollsBackWhenAuditWriteFails(t *testing.T) {
	db := identityAuditFailureDatabase(t)
	repository := NewUserRepository(db)
	user := &User{Username: "atomic-status", PasswordHash: "test-hash", DisplayName: "状态用户", GlobalRole: GlobalRoleUser, Status: "active"}
	if err := repository.Create(context.Background(), user); err != nil {
		t.Fatalf("准备用户失败：%v", err)
	}
	service := NewService(repository, "identity-test-signing-key", audit.NewRepository(db))

	if _, err := service.UpdateUserStatus(context.Background(), 99, user.ID, "disabled"); err == nil {
		t.Fatal("审计写入失败时停用用户必须返回错误")
	}
	persisted, err := repository.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("读取状态回滚结果失败：%v", err)
	}
	if persisted.Status != "active" {
		t.Fatalf("审计写入失败时必须回滚用户状态：%s", persisted.Status)
	}
}

// TestDeleteUserRollsBackWhenAuditWriteFails 防止用户已删除但删除审计缺失。
func TestDeleteUserRollsBackWhenAuditWriteFails(t *testing.T) {
	db := identityAuditFailureDatabase(t)
	repository := NewUserRepository(db)
	user := &User{Username: "atomic-delete", PasswordHash: "test-hash", DisplayName: "待删除用户", GlobalRole: GlobalRoleUser, Status: "active"}
	if err := repository.Create(context.Background(), user); err != nil {
		t.Fatalf("准备待删除用户失败：%v", err)
	}
	service := NewService(repository, "identity-test-signing-key", audit.NewRepository(db))

	if err := service.DeleteUser(context.Background(), 99, user.ID); err == nil {
		t.Fatal("审计写入失败时删除用户必须返回错误")
	}
	if _, err := repository.FindByID(context.Background(), user.ID); err != nil {
		t.Fatalf("审计写入失败时必须保留原用户：%v", err)
	}
}

// TestDeleteUserAuditsUserAndProjectMemberships 验证用户删除和级联移除的项目关系都可追溯。
func TestDeleteUserAuditsUserAndProjectMemberships(t *testing.T) {
	db := identityAuditFailureDatabase(t)
	if err := db.AutoMigrate(&audit.Log{}); err != nil {
		t.Fatalf("创建审计测试表失败：%v", err)
	}
	if err := db.Exec("INSERT INTO projects (id, name) VALUES (?, ?)", 1, "删除审计项目").Error; err != nil {
		t.Fatalf("准备项目失败：%v", err)
	}
	repository := NewUserRepository(db)
	user := &User{Username: "audited-delete", PasswordHash: "test-hash", DisplayName: "删除审计用户", GlobalRole: GlobalRoleUser, Status: "active"}
	permissions := []ProjectPermission{{ProjectID: 1, Role: "member"}}
	if err := repository.CreateWithPermissions(context.Background(), user, permissions); err != nil {
		t.Fatalf("准备带项目权限的用户失败：%v", err)
	}
	service := NewService(repository, "identity-test-signing-key", audit.NewRepository(db))
	ctx := audit.WithActorProfile(context.Background(), 99, "admin", "系统管理员", "192.0.2.10")

	if err := service.DeleteUser(ctx, 99, user.ID); err != nil {
		t.Fatalf("删除用户失败：%v", err)
	}
	for _, action := range []string{audit.ActionUserDeleted, audit.ActionProjectMemberRemoved} {
		var count int64
		if err := db.Model(&audit.Log{}).Where("action = ? AND resource_id = ?", action, user.ID).Count(&count).Error; err != nil {
			t.Fatalf("查询删除审计失败：%v", err)
		}
		if count != 1 {
			t.Fatalf("用户删除必须生成动作 %s：%d", action, count)
		}
	}
}

// identityAuditFailureDatabase 创建缺少 audit_logs 的真实数据库，稳定触发事务末端审计失败。
func identityAuditFailureDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("创建身份事务测试数据库失败：%v", err)
	}
	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatalf("创建用户测试表失败：%v", err)
	}
	if err := db.Exec("CREATE TABLE projects (id integer primary key, name text)").Error; err != nil {
		t.Fatalf("创建项目测试表失败：%v", err)
	}
	if err := db.Exec("CREATE TABLE project_members (id integer primary key autoincrement, project_id integer not null, user_id integer not null, role text not null, created_at datetime not null, updated_at datetime not null)").Error; err != nil {
		t.Fatalf("创建项目成员测试表失败：%v", err)
	}
	return db
}

// statusLookupFailureRepository 用可观察状态模拟读取失败，其他身份仓储能力不参与本测试。
type statusLookupFailureRepository struct {
	UserRepository
	user User
	err  error
}

// WithAuditTransaction 执行测试回调；读取在任何写入前失败，因此无需建立真实事务。
func (r *statusLookupFailureRepository) WithAuditTransaction(ctx context.Context, operation func(UserRepository, audit.Recorder) error) error {
	return operation(r, nil)
}

// FindByID 稳定返回注入错误，复现底层数据库读取失败。
func (r *statusLookupFailureRepository) FindByID(context.Context, uint64) (*User, error) {
	return nil, r.err
}

// UpdateStatus 修改内存用户，使测试能观察服务是否错误越过读取失败继续写入。
func (r *statusLookupFailureRepository) UpdateStatus(_ context.Context, id uint64, status string) error {
	if r.user.ID != id {
		return gorm.ErrRecordNotFound
	}
	r.user.Status = status
	return nil
}
