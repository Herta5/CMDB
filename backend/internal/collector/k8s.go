package collector

import (
	"context"
	"fmt"
)

// K8sCollector discovers Kubernetes resources via the K8s API.
// This is a stub — wire in client-go for production use.
type K8sCollector struct{}

func init() {
	Register("k8s_api", &K8sCollector{})
}

func (c *K8sCollector) Name() string { return "k8s_api" }

func (c *K8sCollector) Collect(ctx context.Context, config map[string]interface{}) ([]CIDiscoveryData, error) {
	// In production, this would:
	//   1. Use client-go to connect to the K8s API server
	//   2. List Nodes, Pods, Services, Deployments, ConfigMaps
	//   3. Map them to CIDiscoveryData with appropriate ExternalIDs and relations
	//
	// For now, return a stub result showing the collector is registered.
	kubeconfig, _ := config["kubeconfig"].(string)
	namespace, _ := config["namespace"].(string)
	if namespace == "" {
		namespace = "default"
	}

	return nil, fmt.Errorf("k8s_api: not yet implemented (kubeconfig=%s, namespace=%s). Install client-go and wire in the K8s API client", kubeconfig, namespace)
}