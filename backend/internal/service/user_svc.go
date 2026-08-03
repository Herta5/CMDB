package service

import (
	"errors"
	"time"

	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
)

type UserSvc struct {
	repo *repository.UserRepo
}

func NewUserSvc(repo *repository.UserRepo) *UserSvc {
	return &UserSvc{repo: repo}
}

func (s *UserSvc) Login(username, password string) (*model.User, string, error) {
	u, err := s.repo.GetByUsername(username)
	if err != nil { return nil, "", errors.New("invalid credentials") }
	if u.Status != "active" { return nil, "", errors.New("account disabled") }
	if !u.CheckPassword(password) { return nil, "", errors.New("invalid credentials") }

	s.repo.UpdateLastLogin(u.ID)
	now := time.Now()
	u.LastLoginAt = &now
	return u, "", nil
}

func (s *UserSvc) List(filter repository.UserFilter) ([]model.User, int64, error) {
	return s.repo.List(filter)
}

func (s *UserSvc) Get(id uint64) (*model.User, error) {
	return s.repo.GetByID(id)
}

func (s *UserSvc) Create(u *model.User) error {
	existing, _ := s.repo.GetByUsername(u.Username)
	if existing != nil { return errors.New("username already exists") }
	if u.Roles == nil { u.Roles = model.JSONArray{"viewer"} }
	if u.Status == "" { u.Status = "active" }
	return s.repo.Create(u)
}

func (s *UserSvc) Update(u *model.User) error {
	existing, err := s.repo.GetByID(u.ID)
	if err != nil { return err }
	u.PasswordHash = existing.PasswordHash // preserve password
	return s.repo.Update(u)
}

func (s *UserSvc) Delete(id uint64) error {
	u, err := s.repo.GetByID(id)
	if err != nil { return err }
	if u.HasRole("super_admin") {
		users, _, err := s.repo.List(repository.UserFilter{Page: 1, PageSize: 100})
		if err != nil { return err }
		superCount := 0
		for _, usr := range users {
			if usr.HasRole("super_admin") { superCount++ }
		}
		if superCount <= 1 { return errors.New("cannot delete the last super admin") }
	}
	return s.repo.Delete(id)
}

func (s *UserSvc) ChangePassword(id uint64, oldPassword, newPassword string) error {
	u, err := s.repo.GetByID(id)
	if err != nil { return err }
	if !u.CheckPassword(oldPassword) { return errors.New("old password incorrect") }
	if err := u.SetPassword(newPassword); err != nil { return err }
	return s.repo.Update(u)
}

func (s *UserSvc) ResetPassword(id uint64, newPassword string) error {
	u, err := s.repo.GetByID(id)
	if err != nil { return err }
	if err := u.SetPassword(newPassword); err != nil { return err }
	return s.repo.Update(u)
}