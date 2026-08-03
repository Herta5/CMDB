package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
)

// DiscoveryExecutor runs collection strategies against the data store.
type DiscoveryExecutor struct {
	ciRepo       *repository.CIInstanceRepo
	typeRepo     *repository.CITypeRepo
	snapRepo     *repository.ConfigSnapshotRepo
	relRepo      *repository.RelationRepo
	strategyRepo *repository.DiscoveryRepo
}

// NewDiscoveryExecutor creates a discovery executor wired to all needed repos.
func NewDiscoveryExecutor(
	ciRepo *repository.CIInstanceRepo,
	typeRepo *repository.CITypeRepo,
	snapRepo *repository.ConfigSnapshotRepo,
	relRepo *repository.RelationRepo,
	strategyRepo *repository.DiscoveryRepo,
) *DiscoveryExecutor {
	return &DiscoveryExecutor{
		ciRepo:       ciRepo,
		typeRepo:     typeRepo,
		snapRepo:     snapRepo,
		relRepo:      relRepo,
		strategyRepo: strategyRepo,
	}
}

// RunStrategy executes a single discovery strategy and records the history.
func (e *DiscoveryExecutor) RunStrategy(ctx context.Context, strategy *model.DiscoveryStrategy) *model.DiscoveryHistory {
	startTime := time.Now()
	history := &model.DiscoveryHistory{
		StrategyID: strategy.ID,
		StartedAt:  startTime,
		Status:     "running",
	}

	c, err := Get(strategy.SourceType)
	if err != nil {
		e.finishHistory(history, "failed", fmt.Sprintf("collector not found: %v", err), startTime)
		return history
	}

	var config map[string]interface{}
	if err := json.Unmarshal(strategy.TargetConfig, &config); err != nil {
		e.finishHistory(history, "failed", fmt.Sprintf("invalid config: %v", err), startTime)
		return history
	}

	data, err := c.Collect(ctx, config)
	if err != nil {
		e.finishHistory(history, "failed", fmt.Sprintf("collection error: %v", err), startTime)
		return history
	}

	created, updated, unchanged, errors := e.processDiscoveryData(data)
	history.CreatedCount = created
	history.UpdatedCount = updated
	history.UnchangedCount = unchanged

	if errors > 0 {
		history.Status = "partial"
		history.ErrorMessage = fmt.Sprintf("%d items had errors", errors)
	} else {
		history.Status = "success"
	}

	history.FinishedAt = timePtr(time.Now())
	history.DurationMs = int(time.Since(startTime).Milliseconds())

	if err := e.strategyRepo.CreateHistory(history); err != nil {
		log.Printf("failed to save discovery history: %v", err)
	}

	// update strategy last-run
	strategy.LastRunAt = timePtr(time.Now())
	strategy.LastRunStatus = &history.Status
	e.strategyRepo.UpdateRunStatus(strategy)

	return history
}

func (e *DiscoveryExecutor) processDiscoveryData(data []CIDiscoveryData) (created, updated, unchanged, errors int) {
	for _, item := range data {
		ciType, err := e.typeRepo.GetByName(item.CITypeName)
		if err != nil {
			log.Printf("discovery: unknown CI type %q for %s: %v", item.CITypeName, item.Name, err)
			errors++
			continue
		}

		// Try to find existing CI by external ID first, then by IP+type
		existing, err := e.findExisting(ciType.ID, item)
		if err != nil {
			// new CI — create
			if createErr := e.createDiscoveredCI(ciType, item); createErr != nil {
				log.Printf("discovery: failed to create CI %s: %v", item.Name, createErr)
				errors++
			} else {
				created++
			}
			continue
		}

		// existing CI — diff and update
		diff := DiffAttributes(existing.Attributes, item.Attributes)
		if diff.HasChanges {
			existing.Attributes = item.Attributes
			existing.Name = item.Name
			existing.SyncRedundantFields()
			existing.LastSeenAt = timePtr(time.Now())

			if err := e.ciRepo.Update(existing); err != nil {
				log.Printf("discovery: failed to update CI %d: %v", existing.ID, err)
				errors++
				continue
			}

			summary := diff.Summary
			snap := &model.ConfigSnapshot{
				CIID:          existing.ID,
				SnapshotData:  existing.Attributes,
				ChangeType:    "discovery",
				ChangeSummary: &summary,
				Source:        strPtr("auto_discovery"),
			}
			_ = e.snapRepo.Create(snap)
			updated++
		} else {
			_ = e.ciRepo.UpdateLastSeen(existing.ID)
			unchanged++
		}
	}
	return
}

// findExisting looks for an existing CI that matches the discovered item.
func (e *DiscoveryExecutor) findExisting(ciTypeID uint64, item CIDiscoveryData) (*model.CIInstance, error) {
	// Strategy 1: match by external ID stored in source_detail
	if item.ExternalID != "" {
		ci, err := e.ciRepo.GetByExternalID(item.ExternalID)
		if err == nil {
			return ci, nil
		}
	}
	// Strategy 2: match by IP address + CI type
	if ip, ok := item.Attributes["ip_address"].(string); ok && ip != "" {
		ci, err := e.ciRepo.GetByTypeAndIP(ciTypeID, ip)
		if err == nil {
			return ci, nil
		}
	}
	return nil, fmt.Errorf("no existing CI found")
}

func (e *DiscoveryExecutor) createDiscoveredCI(ciType *model.CIType, item CIDiscoveryData) error {
	now := time.Now()
	extID := item.ExternalID
	ci := &model.CIInstance{
		CITypeID:     ciType.ID,
		Name:         item.Name,
		Status:       "active",
		Attributes:   item.Attributes,
		Source:       "auto_discovery",
		SourceDetail: &extID,
		DiscoveredAt: &now,
		LastSeenAt:   &now,
	}
	ci.SyncRedundantFields()

	code, err := e.ciRepo.GenerateCICode(ciType.Name)
	if err != nil {
		return err
	}
	ci.CICode = code

	if err := e.ciRepo.Create(ci); err != nil {
		return err
	}

	// write creation snapshot
	snap := &model.ConfigSnapshot{
		CIID:         ci.ID,
		SnapshotData: ci.Attributes,
		ChangeType:   "create",
		Source:       strPtr("auto_discovery"),
	}
	_ = e.snapRepo.Create(snap)

	return nil
}

func (e *DiscoveryExecutor) finishHistory(h *model.DiscoveryHistory, status, errMsg string, startTime time.Time) {
	h.Status = status
	h.ErrorMessage = errMsg
	h.FinishedAt = timePtr(time.Now())
	h.DurationMs = int(time.Since(startTime).Milliseconds())
	if err := e.strategyRepo.CreateHistory(h); err != nil {
		log.Printf("failed to save discovery history: %v", err)
	}
}

func timePtr(t time.Time) *time.Time { return &t }
func strPtr(s string) *string        { return &s }