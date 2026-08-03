package collector

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// AgentCollector receives data pushed from lightweight agents installed on hosts.
// Agents POST their host info to the CMDB server; this collector is called
// when the discovery strategy is triggered manually (pull mode).
type AgentCollector struct {
	httpClient *http.Client
}

func init() {
	Register("agent", &AgentCollector{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	})
}

func (c *AgentCollector) Name() string { return "agent" }

func (c *AgentCollector) Collect(ctx context.Context, config map[string]interface{}) ([]CIDiscoveryData, error) {
	endpoint, _ := config["endpoint"].(string)
	if endpoint == "" {
		return nil, fmt.Errorf("agent: endpoint is required")
	}

	type agentPayload struct {
		Hostname   string                 `json:"hostname"`
		ExternalID string                 `json:"external_id"`
		Attributes map[string]interface{} `json:"attributes"`
	}

	// In production, this fetches from the agent gateway or receives push data.
	// For MVP, we return a stub.
	_ = endpoint

	return nil, fmt.Errorf("agent: pull mode not yet implemented. Agents should POST to /api/v1/discovery/agent/report")
}

// AgentReportHandler is a placeholder for the push endpoint.
// Agents call POST /api/v1/discovery/agent/report with their host data.
func AgentReportHandler() {
	// This would be wired as an HTTP handler in the router.
}