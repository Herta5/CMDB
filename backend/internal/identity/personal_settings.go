// 本文件承载已认证用户的个人设置，不接受客户端提供的目标身份或权限。
package identity

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"cmdb/internal/audit"
)

// ErrCurrentPasswordInvalid 表示本人密码校验失败，不应注销仍有效的当前会话。
var ErrCurrentPasswordInvalid = errors.New("当前密码不正确")

// UpdateMyProfile 只允许本人修改辅助显示名称，稳定用户名和权限保持原有归属。
func (s *Service) UpdateMyProfile(ctx context.Context, claims UserClaims, displayName string) (*User, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || !utf8.ValidString(displayName) || utf8.RuneCountInString(displayName) > 128 {
		return nil, ErrInvalidUserInput
	}
	user, err := s.CurrentUser(ctx, claims)
	if err != nil {
		return nil, err
	}
	err = s.withAuditTransaction(ctx, func(repository UserRepository, recorder audit.Recorder) error {
		if err := repository.UpdatePersonal(ctx, user, &displayName, nil); err != nil {
			return err
		}
		// 每次成功保存均审计，不能用事务外旧名称判断并发写入后的真实变化。
		return recordPersonalUpdate(ctx, recorder, user, "display_name")
	})
	if err != nil {
		return nil, err
	}
	// 再读当前权限，响应不携带资料提交前的旧角色快照。
	return s.CurrentUser(ctx, claims)
}

// ChangeMyPassword 校验当前密码后原子写入新哈希和审计，密码轮换使所有旧令牌失效。
func (s *Service) ChangeMyPassword(ctx context.Context, claims UserClaims, currentPassword, newPassword string) error {
	if len(newPassword) < 12 || len(newPassword) > 72 || len(currentPassword) == 0 || len(currentPassword) > 72 {
		return ErrInvalidUserInput
	}
	user, err := s.CurrentUser(ctx, claims)
	if err != nil {
		return err
	}
	if !VerifyPassword(user.PasswordHash, currentPassword) {
		return ErrCurrentPasswordInvalid
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.withAuditTransaction(ctx, func(repository UserRepository, recorder audit.Recorder) error {
		if err := repository.UpdatePersonal(ctx, user, nil, &hash); err != nil {
			return err
		}
		return recordPersonalUpdate(ctx, recorder, user, "password")
	})
}

// recordPersonalUpdate 仅记录变更字段名，密码、哈希和完整请求不进入审计详情。
func recordPersonalUpdate(ctx context.Context, recorder audit.Recorder, user *User, field string) error {
	return recordAuditWith(ctx, recorder, audit.Entry{Action: audit.ActionUserUpdated, ResourceType: "user", ResourceID: user.Username, Detail: map[string]any{
		"target_username": user.Username, "changed_fields": []string{field},
	}})
}
