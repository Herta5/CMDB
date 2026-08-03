package repository

import (
	"time"

	"github-cmdb/internal/model"
	"gorm.io/gorm"
)

type UserRepo struct {
	db *gorm.DB
}

func NewUserRepo(db *gorm.DB) *UserRepo {
	return &UserRepo{db: db}
}

type UserFilter struct {
	Username string `form:"username"`
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

func (r *UserRepo) List(filter UserFilter) ([]model.User, int64, error) {
	var users []model.User
	var total int64
	query := r.db.Model(&model.User{})

	if filter.Username != "" { query = query.Where("username LIKE ?", "%"+filter.Username+"%") }
	if filter.Status != ""   { query = query.Where("status = ?", filter.Status) }

	if err := query.Count(&total).Error; err != nil { return nil, 0, err }

	page := filter.Page; if page < 1 { page = 1 }
	size := filter.PageSize; if size < 1 || size > 100 { size = 20 }

	err := query.Order("id ASC").Offset((page - 1) * size).Limit(size).Find(&users).Error
	return users, total, err
}

func (r *UserRepo) GetByID(id uint64) (*model.User, error) {
	var u model.User
	err := r.db.First(&u, id).Error
	return &u, err
}

func (r *UserRepo) GetByUsername(username string) (*model.User, error) {
	var u model.User
	err := r.db.Where("username = ?", username).First(&u).Error
	return &u, err
}

func (r *UserRepo) Create(u *model.User) error {
	return r.db.Create(u).Error
}

func (r *UserRepo) Update(u *model.User) error {
	return r.db.Save(u).Error
}

func (r *UserRepo) Delete(id uint64) error {
	return r.db.Delete(&model.User{}, id).Error
}

func (r *UserRepo) UpdateLastLogin(id uint64) {
	r.db.Model(&model.User{}).Where("id = ?", id).Update("last_login_at", time.Now())
}