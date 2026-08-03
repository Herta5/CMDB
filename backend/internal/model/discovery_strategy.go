package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// JSONRaw is a wrapper for raw JSON stored in MySQL JSON columns.
type JSONRaw json.RawMessage

func (j JSONRaw) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return []byte(j), nil
}

func (j *JSONRaw) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to scan JSONRaw: source is not []byte")
	}
	*j = make(JSONRaw, len(bytes))
	copy(*j, bytes)
	return nil
}

// DiscoveryStrategy defines how and when to discover resources from an external source.
type DiscoveryStrategy struct {
	ID            uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	Name          string     `json:"name" gorm:"size:256;not null"`
	SourceType    string     `json:"source_type" gorm:"type:enum('agent','ssh','k8s_api','cloud_api','snmp');not null"`
	TargetConfig  JSONRaw    `json:"target_config" gorm:"type:json;not null"`
	ScheduleExpr  *string    `json:"schedule_expr" gorm:"size:128"`
	Enabled       bool       `json:"enabled" gorm:"default:true"`
	TimeoutSec    int        `json:"timeout_sec" gorm:"default:300"`
	RetryCount    int        `json:"retry_count" gorm:"default:3"`
	LastRunAt     *time.Time `json:"last_run_at"`
	LastRunStatus *string    `json:"last_run_status" gorm:"size:32"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (DiscoveryStrategy) TableName() string { return "discovery_strategy" }