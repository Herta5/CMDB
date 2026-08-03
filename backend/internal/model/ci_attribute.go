package model

import "time"

type CIAttribute struct {
	ID             uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	CITypeID       uint64    `json:"ci_type_id" gorm:"uniqueIndex:uk_type_attr;not null"`
	Name           string    `json:"name" gorm:"uniqueIndex:uk_type_attr;size:128;not null"`
	DisplayName    string    `json:"display_name" gorm:"size:256;not null"`
	ValueType      string    `json:"value_type" gorm:"type:enum('string','int','float','bool','date','datetime','json','enum');not null"`
	IsRequired     bool      `json:"is_required" gorm:"default:false"`
	IsUnique       bool      `json:"is_unique" gorm:"default:false"`
	IsIndexed      bool      `json:"is_indexed" gorm:"default:false"`
	DefaultValue   *string   `json:"default_value" gorm:"type:text"`
	EnumValues     *string   `json:"enum_values" gorm:"type:json"`
	ValidationRule *string   `json:"validation_rule" gorm:"size:512"`
	SortOrder      int       `json:"sort_order" gorm:"default:0"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	CIType *CIType `json:"ci_type,omitempty" gorm:"foreignKey:CITypeID"`
}

func (CIAttribute) TableName() string { return "ci_attribute" }