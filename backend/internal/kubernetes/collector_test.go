// 本文件使用本地 TLS 服务验证 Kubernetes 资源转换，不连接真实集群。
package kubernetes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cmdb/internal/resource"
)

// TestCollectorCollectsRequiredKindsAndAddresses 验证集群、核心对象、工作负载和访问地址均转换为统一快照。
func TestCollectorCollectsRequiredKindsAndAddresses(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		payload := map[string]any{"items": []any{}}
		switch request.URL.Path {
		case "/api/v1/nodes":
			payload["items"] = []any{map[string]any{
				"metadata": map[string]any{"uid": "node-1", "name": "node-a"},
				"status":   map[string]any{"addresses": []any{map[string]any{"type": "InternalIP", "address": "10.0.0.2"}, map[string]any{"type": "ExternalIP", "address": "203.0.113.2"}}},
			}}
		case "/api/v1/services":
			payload["items"] = []any{map[string]any{
				"metadata": map[string]any{"uid": "svc-1", "name": "web", "namespace": "default"},
				"spec":     map[string]any{"clusterIP": "10.96.0.1", "ports": []any{map[string]any{"port": 443}}},
				"status":   map[string]any{"loadBalancer": map[string]any{"ingress": []any{map[string]any{"hostname": "lb.example.invalid"}}}},
			}}
		}
		_ = json.NewEncoder(response).Encode(payload)
	}))
	defer server.Close()
	credential, _ := json.Marshal(map[string]any{"server": server.URL, "token": "test-token", "insecure": true})
	results, err := NewCollector().Collect(context.Background(), resource.Source{ID: 4, Name: "测试集群"}, credential)
	if err != nil {
		t.Fatal("采集 Kubernetes 失败")
	}
	found := map[string]resource.Snapshot{}
	for _, result := range results {
		for _, snapshot := range result.Snapshots {
			found[result.ResourceType+":"+snapshot.ExternalID] = snapshot
		}
	}
	if _, ok := found["cluster:4"]; !ok {
		t.Fatal("缺少集群资源")
	}
	node := found["node:node-1"]
	if len(node.Endpoints) != 2 {
		t.Fatal("Node 必须保留内外网地址")
	}
	service := found["service:svc-1"]
	if len(service.Endpoints) < 2 {
		t.Fatal("Service 必须保留 ClusterIP 和负载均衡域名")
	}
	for _, kind := range []string{"namespace", "pod", "deployment", "statefulset", "daemonset", "job", "cronjob", "ingress"} {
		if _, ok := findResult(results, kind); !ok {
			t.Fatalf("缺少资源类型 %s 的采集结果", kind)
		}
	}
}

// findResult 查询是否为指定资源类型返回了独立结果，即使当前列表为空也必须存在。
func findResult(results []resource.CollectionResult, kind string) (resource.CollectionResult, bool) {
	for _, result := range results {
		if result.ResourceType == kind {
			return result, true
		}
	}
	return resource.CollectionResult{}, false
}

// TestCollectorReportsAuthenticationFailure 验证未授权响应升级为接入源认证失败。
func TestCollectorReportsAuthenticationFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	credential, _ := json.Marshal(map[string]any{"server": server.URL, "token": "wrong", "insecure": true})
	if _, err := NewCollector().Collect(context.Background(), resource.Source{}, credential); err != resource.ErrAuthenticationFailed {
		t.Fatal("401 必须转换为统一认证失败")
	}
}
