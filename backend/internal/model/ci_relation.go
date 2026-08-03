package model

import "time"

type CIRelationRule struct {
	ID               uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name             string    `json:"name" gorm:"uniqueIndex;size:128;not null"`
	DisplayName      string    `json:"display_name" gorm:"size:256;not null"`
	ReverseName      string    `json:"reverse_name" gorm:"size:128;not null"`
	SourceTypeID     uint64    `json:"source_type_id" gorm:"index;not null"`
	TargetTypeID     uint64    `json:"target_type_id" gorm:"index;not null"`
	Cardinality      string    `json:"cardinality" gorm:"type:enum('1:1','1:N','N:1','N:M');default:'N:1'"`
	IsHardDependency bool      `json:"is_hard_dependency" gorm:"default:false"`
	Description      *string   `json:"description" gorm:"type:text"`
	CreatedAt        time.Time `json:"created_at"`

	SourceType *CIType `json:"source_type,omitempty" gorm:"foreignKey:SourceTypeID"`
	TargetType *CIType `json:"target_type,omitempty" gorm:"foreignKey:TargetTypeID"`
}

func (CIRelationRule) TableName() string { return "ci_relation_rule" }

type CIRelationInstance struct {
	ID         uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	RuleID     uint64    `json:"rule_id" gorm:"uniqueIndex:uk_relation;not null"`
	SourceCIID uint64    `json:"source_ci_id" gorm:"uniqueIndex:uk_relation;index;not null"`
	TargetCIID uint64    `json:"target_ci_id" gorm:"uniqueIndex:uk_relation;index;not null"`
	Properties *JSONMap  `json:"properties" gorm:"type:json"`
	CreatedAt  time.Time `json:"created_at"`

	Rule     *CIRelationRule `json:"rule,omitempty" gorm:"foreignKey:RuleID"`
	SourceCI *CIInstance     `json:"source_ci,omitempty" gorm:"foreignKey:SourceCIID"`
	TargetCI *CIInstance     `json:"target_ci,omitempty" gorm:"foreignKey:TargetCIID"`
}

func (CIRelationInstance) TableName() string { return "ci_relation_instance" }