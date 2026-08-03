package collector

import "context"

// Collector interface — every data source implements this.
type Collector interface {
	Name() string
	Collect(ctx context.Context, config map[string]interface{}) ([]CIDiscoveryData, error)
}

// CIDiscoveryData represents a discovered configuration item from any source.
type CIDiscoveryData struct {
	ExternalID string                 `json:"external_id"`
	CITypeName string                 `json:"ci_type_name"`
	Name       string                 `json:"name"`
	Attributes map[string]interface{} `json:"attributes"`
	Relations  []RelationHint         `json:"relations"`
}

// RelationHint is a lightweight reference to another CI discovered in the same run.
type RelationHint struct {
	RuleName         string `json:"rule_name"`
	TargetExternalID string `json:"target_external_id"`
}