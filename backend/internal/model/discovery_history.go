package model

import "time"

// DiscoveryHistory records each execution of a discovery strategy.
type DiscoveryHistory struct {
	ID             uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	StrategyID     uint64     `json:"strategy_id" gorm:"index;not null"`
	Status         string     `json:"status" gorm:"type:enum('running','success','failed','partial');default:running"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	DurationMs     int        `json:"duration_ms"`
	CreatedCount   int        `json:"created_count" gorm:"default:0"`
	UpdatedCount   int        `json:"updated_count" gorm:"default:0"`
	UnchangedCount int        `json:"unchanged_count" gorm:"default:0"`
	ErrorMessage   string     `json:"error_message" gorm:"type:text"`
	CreatedAt      time.Time  `json:"created_at"`

	Strategy *DiscoveryStrategy `json:"strategy,omitempty" gorm:"foreignKey:StrategyID"`
}

func (DiscoveryHistory) TableName() string { return "discovery_history" }