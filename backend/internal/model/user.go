package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// JSONArray is a []string that serializes to/from JSON for GORM.
type JSONArray []string

func (j JSONArray) Value() (driver.Value, error) {
	if j == nil { return "[]", nil }
	b, err := json.Marshal(j)
	return string(b), err
}

func (j *JSONArray) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok { return errors.New("failed to scan JSONArray: source is not []byte") }
	return json.Unmarshal(bytes, j)
}

type User struct {
	ID           uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	Username     string     `json:"username" gorm:"size:64;uniqueIndex;not null"`
	PasswordHash string     `json:"-" gorm:"size:256;not null"`
	DisplayName  string     `json:"display_name" gorm:"size:128"`
	Email        string     `json:"email" gorm:"size:256"`
	Phone        string     `json:"phone" gorm:"size:32"`
	Roles        JSONArray  `json:"roles" gorm:"type:json;not null"`
	Departments  JSONArray  `json:"departments" gorm:"type:json"`
	Status       string     `json:"status" gorm:"size:32;default:active"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (User) TableName() string { return "cmdb_user" }

func (u *User) SetPassword(plain string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil { return err }
	u.PasswordHash = string(hash)
	return nil
}

func (u *User) CheckPassword(plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(plain)) == nil
}

// SeedDefaultAdmin creates the default admin user if no users exist.
func SeedDefaultAdmin(db *gorm.DB) {
	var count int64
	db.Model(&User{}).Count(&count)
	if count > 0 { return }

	admin := &User{
		Username:    "admin",
		DisplayName: "超级管理员",
		Email:       "admin@cmdb.local",
		Roles:       JSONArray{"super_admin"},
		Status:      "active",
	}
	_ = admin.SetPassword("admin123")
	db.Create(admin)
}

func (u *User) HasRole(role string) bool {
	for _, r := range u.Roles {
		if r == role { return true }
	}
	return false
}