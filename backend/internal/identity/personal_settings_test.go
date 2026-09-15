// 本文件验证个人字段写入不会覆盖并发权限修改，并阻止旧密码状态覆盖新密码。
package identity

import (
	"cmdb/internal/audit"
	"context"
	"errors"
	"testing"
)

func Test个人资料写入保留并发权限且拒绝旧密码快照(t *testing.T) {
	db := identityAuditFailureDatabase(t)
	repository := NewUserRepository(db)
	original := User{Username: "personal_user", DisplayName: "原名称", PasswordHash: "虚构旧哈希", GlobalRole: "user", Status: "active"}
	if err := repository.Create(context.Background(), &original); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&User{}).Where("id = ?", original.ID).Update("global_role", "system_admin").Error; err != nil {
		t.Fatal(err)
	}
	name := "新名称"
	if err := repository.UpdatePersonal(context.Background(), &original, &name, nil); err != nil {
		t.Fatal(err)
	}
	after, err := repository.FindByID(context.Background(), original.ID)
	if err != nil || after.GlobalRole != "system_admin" || after.DisplayName != name {
		t.Fatal("名称更新覆盖了并发权限修改")
	}
	nextHash := "虚构新哈希"
	if err := repository.UpdatePersonal(context.Background(), after, nil, &nextHash); err != nil {
		t.Fatal(err)
	}
	staleHash := "虚构过时哈希"
	if err := repository.UpdatePersonal(context.Background(), &original, nil, &staleHash); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("旧快照不能覆盖已轮换密码")
	}
	stored, _ := repository.FindByID(context.Background(), original.ID)
	if stored.PasswordHash != nextHash {
		t.Fatal("并发密码被旧请求覆盖")
	}
	if err := repository.UpdateStatus(context.Background(), original.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpdatePersonal(context.Background(), stored, &name, nil); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("停用账号不得继续修改个人资料")
	}
}

// personalInterleaveRepository 在读出快照后插入另一个请求的写入，复现确定的交错顺序。
type personalInterleaveRepository struct {
	UserRepository
	afterRead   func()
	afterReadID func()
}

func (r *personalInterleaveRepository) FindByUsername(ctx context.Context, username string) (*User, error) {
	user, err := r.UserRepository.FindByUsername(ctx, username)
	if err == nil && r.afterRead != nil {
		change := r.afterRead
		r.afterRead = nil
		change()
	}
	return user, err
}

func (r *personalInterleaveRepository) FindByID(ctx context.Context, id uint64) (*User, error) {
	user, err := r.UserRepository.FindByID(ctx, id)
	if err == nil && r.afterReadID != nil {
		change := r.afterReadID
		r.afterReadID = nil
		change()
	}
	return user, err
}

func (r *personalInterleaveRepository) WithAuditTransaction(ctx context.Context, change func(UserRepository, audit.Recorder) error) error {
	return r.UserRepository.(auditTransactionUserRepository).WithAuditTransaction(ctx, change)
}

func Test管理员旧资料不得恢复本人已轮换密码和令牌(t *testing.T) {
	db := identityAuditFailureDatabase(t)
	repository := NewUserRepository(db)
	hash, _ := HashPassword("虚构初始密码")
	user := &User{Username: "personal_user", DisplayName: "旧名称", PasswordHash: hash, GlobalRole: "user", Status: "active"}
	if err := repository.Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, "虚构签名密钥")
	_, oldToken, err := service.Login(context.Background(), user.Username, "虚构初始密码")
	if err != nil {
		t.Fatal("准备会话失败")
	}
	interleave := &personalInterleaveRepository{UserRepository: repository, afterReadID: func() {
		if err := service.ChangeMyPassword(context.Background(), UserClaims{Username: user.Username, InternalUserID: user.ID}, "虚构初始密码", "虚构轮换密码"); err != nil {
			t.Fatal("本人改密失败")
		}
	}}
	adminService := NewService(interleave, "虚构签名密钥")
	if _, err := adminService.UpdateUser(context.Background(), "operator", user.Username, UpdateUserInput{DisplayName: "管理员新名称", GlobalRole: "user", Status: "active"}); err != nil {
		t.Fatal("管理员更新失败")
	}
	if _, _, err := service.Login(context.Background(), user.Username, "虚构轮换密码"); err != nil {
		t.Fatal("管理员未提交密码的更新不得覆盖本人新密码")
	}
	if _, err := service.Authenticate(context.Background(), oldToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("被撤销令牌不得因旧资料保存而恢复")
	}
}

func Test个人名称回改不能因旧快照漏记审计(t *testing.T) {
	db := identityAuditFailureDatabase(t)
	if err := db.AutoMigrate(&audit.Log{}); err != nil {
		t.Fatal(err)
	}
	repository := NewUserRepository(db)
	user := &User{Username: "personal_user", DisplayName: "甲", PasswordHash: "虚构哈希", GlobalRole: "user", Status: "active"}
	if err := repository.Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	interleave := &personalInterleaveRepository{UserRepository: repository, afterRead: func() {
		if err := db.Model(&User{}).Where("id = ?", user.ID).Update("display_name", "乙").Error; err != nil {
			t.Fatal(err)
		}
	}}
	service := NewService(interleave, "虚构签名密钥", audit.NewRepository(db))
	if _, err := service.UpdateMyProfile(context.Background(), UserClaims{Username: user.Username, InternalUserID: user.ID}, "甲"); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&audit.Log{}).Where("action = ?", audit.ActionUserUpdated).Count(&count)
	if count != 1 {
		t.Fatal("真实名称回改必须审计，不能用事务外旧快照判断无变更")
	}
}
