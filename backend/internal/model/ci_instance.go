package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

type JSONMap map[string]interface{}

func (j JSONMap) Value() (driver.Value, error) {
	if j == nil { return nil, nil }
	return json.Marshal(j)
}

func (j *JSONMap) Scan(value interface{}) error {
	if value == nil { *j = nil; return nil }
	bytes, ok := value.([]byte)
	if !ok { return errors.New("failed to scan JSONMap: source is not []byte") }
	return json.Unmarshal(bytes, j)
}

type CIInstance struct {
	ID           uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	CITypeID     uint64     `json:"ci_type_id" gorm:"index;not null"`
	CICode       string     `json:"ci_code" gorm:"uniqueIndex;size:128;not null"`
	Name         string     `json:"name" gorm:"size:512;not null"`
	Status       string     `json:"status" gorm:"type:enum('active','inactive','maintenance','retired');default:active;index"`
	Attributes   JSONMap    `json:"attributes" gorm:"type:json;not null"`
	IPAddress    *string    `json:"ip_address" gorm:"size:45;index"`
	MACAddress   *string    `json:"mac_address" gorm:"size:17"`
	SN           *string    `json:"sn" gorm:"size:128;index"`
	AssetTag     *string    `json:"asset_tag" gorm:"size:128;index"`
	DepartmentID *uint64    `json:"department_id" gorm:"index"`
	Owner        *string    `json:"owner" gorm:"size:128"`
	DiscoveredAt *time.Time `json:"discovered_at"`
	LastSeenAt   *time.Time `json:"last_seen_at" gorm:"index"`
	RetiredAt    *time.Time `json:"retired_at"`
	Source       string     `json:"source" gorm:"type:enum('manual','auto_discovery','api','import');default:manual"`
	SourceDetail *string    `json:"source_detail" gorm:"size:512"`
	CreatedBy    *string    `json:"created_by" gorm:"size:128"`
	UpdatedBy    *string    `json:"updated_by" gorm:"size:128"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	CIType       *CIType    `json:"ci_type,omitempty" gorm:"foreignKey:CITypeID"`
}

func (CIInstance) TableName() string { return "ci_instance" }

func (ci *CIInstance) SyncRedundantFields() {
	if ci.Attributes == nil { return }
	if v, ok := ci.Attributes["ip_address"].(string); ok && v != "" { ci.IPAddress = &v }
	if v, ok := ci.Attributes["mac_address"].(string); ok && v != "" { ci.MACAddress = &v }
	if v, ok := ci.Attributes["sn"].(string); ok && v != "" { ci.SN = &v }
	if v, ok := ci.Attributes["asset_tag"].(string); ok && v != "" { ci.AssetTag = &v }
}