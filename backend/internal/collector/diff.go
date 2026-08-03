package collector

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ChangeEntry represents a single attribute-level change detected during a diff.
type ChangeEntry struct {
	Field    string      `json:"field"`
	OldValue interface{} `json:"old_value"`
	NewValue interface{} `json:"new_value"`
}

// DiffResult captures the outcome of comparing discovered data against an existing CI.
type DiffResult struct {
	HasChanges bool          `json:"has_changes"`
	Changes    []ChangeEntry `json:"changes"`
	Summary    string        `json:"summary"`
}

// DiffAttributes compares existing (stored) attributes with newly discovered data.
func DiffAttributes(existing map[string]interface{}, discovered map[string]interface{}) *DiffResult {
	result := &DiffResult{Changes: make([]ChangeEntry, 0)}

	for key, newVal := range discovered {
		oldVal, exists := existing[key]
		if !exists {
			result.Changes = append(result.Changes, ChangeEntry{
				Field: key, OldValue: nil, NewValue: newVal,
			})
			continue
		}
		if !deepEqual(oldVal, newVal) {
			result.Changes = append(result.Changes, ChangeEntry{
				Field: key, OldValue: oldVal, NewValue: newVal,
			})
		}
	}

	// detect removed keys
	for key, oldVal := range existing {
		if _, stillExists := discovered[key]; !stillExists {
			result.Changes = append(result.Changes, ChangeEntry{
				Field: key, OldValue: oldVal, NewValue: nil,
			})
		}
	}

	result.HasChanges = len(result.Changes) > 0
	if result.HasChanges {
		fields := make([]string, len(result.Changes))
		for i, c := range result.Changes {
			fields[i] = c.Field
		}
		result.Summary = fmt.Sprintf("changed fields: %s", strings.Join(fields, ", "))
	}
	return result
}

// deepEqual compares two values for equality, using JSON normalization.
func deepEqual(a, b interface{}) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}