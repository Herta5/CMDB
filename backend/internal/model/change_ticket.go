package model

import "time"

type ChangeTicket struct {
	ID                uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	TicketNo          string     `json:"ticket_no" gorm:"uniqueIndex;size:64;not null"`
	Title             string     `json:"title" gorm:"size:256;not null"`
	Description       *string    `json:"description" gorm:"type:text"`
	CITargetID        uint64     `json:"ci_target_id" gorm:"index;not null"`
	CITargetName      string     `json:"ci_target_name" gorm:"size:256"`
	ChangeType        string     `json:"change_type" gorm:"type:enum('modify','deploy','restart','scale','migrate','decommission','config_change','other');default:'modify'"`
	Priority          string     `json:"priority" gorm:"type:enum('low','medium','high','critical');default:'medium'"`
	Status            string     `json:"status" gorm:"type:enum('draft','pending_approval','approved','rejected','executing','completed','rolled_back','failed');default:'draft';index"`
	RiskLevel         string     `json:"risk_level" gorm:"type:enum('low','medium','high');default:'medium'"`
	BeforeSnapshotID  *uint64    `json:"before_snapshot_id"`
	AfterSnapshotID   *uint64    `json:"after_snapshot_id"`
	RollbackPlan      *string    `json:"rollback_plan" gorm:"type:text"`
	ExecutionLog      *string    `json:"execution_log" gorm:"type:text"`
	ProposedBy        string     `json:"proposed_by" gorm:"size:128"`
	ApprovedBy        *string    `json:"approved_by" gorm:"size:128"`
	ExecutedBy        *string    `json:"executed_by" gorm:"size:128"`
	ApprovedAt        *time.Time `json:"approved_at"`
	ExecutedAt        *time.Time `json:"executed_at"`
	ScheduledAt       *time.Time `json:"scheduled_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`

	CITarget *CIInstance `json:"ci_target,omitempty" gorm:"foreignKey:CITargetID"`
}

func (ChangeTicket) TableName() string { return "change_ticket" }