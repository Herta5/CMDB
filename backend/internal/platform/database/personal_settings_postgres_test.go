//go:build postgres

// 本文件在 PostgreSQL 17 的受限应用账号下验证个人设置及并发密码更新。
package database

import (
	"context"
	"errors"
	"testing"

	"cmdb/internal/audit"
	"cmdb/internal/identity"
)

func TestPostgreSQL个人设置与并发密码保护(t *testing.T) {
	fixture := newPostgresTestDatabase(t)
	installCurrentSchema(t, fixture)
	db := openPostgresConfiguration(t, applicationConfiguration(fixture.configuration), false)
	ctx := context.Background()
	repository := identity.NewUserRepository(db)
	password := "虚构初始密码十二字节"
	hash, err := identity.HashPassword(password)
	if err != nil {
		t.Fatal("准备测试密码失败")
	}
	user := &identity.User{Username: "personal_user", DisplayName: "原名称", PasswordHash: hash, GlobalRole: "user", Status: "active"}
	if err := repository.Create(ctx, user); err != nil {
		t.Fatal("准备个人账号失败")
	}
	service := identity.NewService(repository, "虚构测试签名密钥", audit.NewRepository(db))
	_, token, err := service.Login(ctx, user.Username, password)
	if err != nil {
		t.Fatal("准备登录失败")
	}
	claims := identity.UserClaims{Username: user.Username, InternalUserID: user.ID}
	updated, err := service.UpdateMyProfile(ctx, claims, " 新名称 ")
	if err != nil || updated.DisplayName != "新名称" {
		t.Fatal("应用账号必须支持个人名称更新")
	}
	if err := service.ChangeMyPassword(ctx, claims, password, "虚构轮换后的密码"); err != nil {
		t.Fatal("应用账号必须支持密码轮换")
	}
	if _, err := service.Authenticate(ctx, token); !errors.Is(err, identity.ErrInvalidSession) {
		t.Fatal("密码轮换必须使旧会话失效")
	}
	var count int64
	if err := db.Model(&audit.Log{}).Where("action = ?", audit.ActionUserUpdated).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("个人设置必须原子记录两次更新审计")
	}
	// 两个写入共享同一快照，数据库必须在等待行锁后重新判断密码条件。
	snapshot, err := repository.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatal("读取并发测试快照失败")
	}
	results := make(chan error, 2)
	for _, next := range []string{"虚构并发哈希甲", "虚构并发哈希乙"} {
		go func(next string) { results <- repository.UpdatePersonal(ctx, snapshot, nil, &next) }(next)
	}
	first, second := <-results, <-results
	if !((first == nil && errors.Is(second, identity.ErrInvalidSession)) || (second == nil && errors.Is(first, identity.ErrInvalidSession))) {
		t.Fatal("相同密码快照并发写入只能成功一次")
	}
}
