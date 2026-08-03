package model

import "time"

type CIType struct {
	ID          uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name        string    `json:"name" gorm:"uniqueIndex;size:128;not null"`
	DisplayName string    `json:"display_name" gorm:"size:256;not null"`
	ParentID    *uint64   `json:"parent_id" gorm:"index"`
	Icon        string    `json:"icon" gorm:"size:64;default:server"`
	Description string    `json:"description" gorm:"type:text"`
	IsAbstract  bool      `json:"is_abstract" gorm:"default:false"`
	SortOrder   int       `json:"sort_order" gorm:"default:0"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	Children   []CIType      `json:"children,omitempty" gorm:"-"`
	Attributes []CIAttribute `json:"attributes,omitempty" gorm:"foreignKey:CITypeID"`
}

func (CIType) TableName() string { return "ci_type" }