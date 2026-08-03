package model

import "time"

type AuditLog struct {
	ID             uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID         uint64    `json:"user_id" gorm:"index"`
	Username       string    `json:"username" gorm:"size:128;index"`
	Method         string    `json:"method" gorm:"size:10"`
	Path           string    `json:"path" gorm:"size:512"`
	QueryString    string    `json:"query_string" gorm:"size:1024"`
	RequestBody    *string   `json:"request_body" gorm:"type:text"`
	ResponseStatus int       `json:"response_status"`
	ClientIP       string    `json:"client_ip" gorm:"size:64"`
	UserAgent      string    `json:"user_agent" gorm:"size:512"`
	DurationMs     int64     `json:"duration_ms"`
	CreatedAt      time.Time `json:"created_at" gorm:"index"`
}

func (AuditLog) TableName() string { return "audit_log" }

// WebhookRecord stores incoming webhook payloads for traceability
type WebhookRecord struct {
	ID          uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Source      string    `json:"source" gorm:"size:128;index"`
	EventType   string    `json:"event_type" gorm:"size:128"`
	Payload     string    `json:"payload" gorm:"type:mediumtext"`
	Headers     string    `json:"headers" gorm:"type:text"`
	Processed   bool      `json:"processed" gorm:"default:false"`
	ProcessedAt *time.Time `json:"processed_at"`
	CreatedAt   time.Time `json:"created_at" gorm:"index"`
}

func (WebhookRecord) TableName() string { return "webhook_record" }