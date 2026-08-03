package model

import "time"

type ConfigSnapshot struct {
	ID            uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	CIID          uint64    `json:"ci_id" gorm:"index;not null"`
	SnapshotData  JSONMap   `json:"snapshot_data" gorm:"type:json;not null"`
	ChangeType    string    `json:"change_type" gorm:"type:enum('create','update','delete','discovery');default:update"`
	ChangeSummary *string   `json:"change_summary" gorm:"size:1024"`
	Source        *string   `json:"source" gorm:"size:256"`
	CreatedAt     time.Time `json:"created_at" gorm:"index"`
}

func (ConfigSnapshot) TableName() string { return "config_snapshot" }