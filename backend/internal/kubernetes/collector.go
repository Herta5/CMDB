// 本文件实现 Kubernetes API 只读采集，并转换为共享资源快照。
package kubernetes

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cmdb/internal/resource"
)

// Collector 是 Kubernetes 独立平台采集器。
type Collector struct{ client *http.Client }

// NewCollector 创建带有限超时的 Kubernetes 采集器。
func NewCollector() *Collector { return &Collector{} }

type credential struct {
	Server   string `json:"server"`
	Token    string `json:"token"`
	Insecure bool   `json:"insecure"`
}
type listResponse struct {
	Items    []map[string]any `json:"items"`
	Continue string           `json:"continue"`
}

var paths = []struct{ kind, path string }{
	{"namespace", "/api/v1/namespaces"}, {"node", "/api/v1/nodes"}, {"pod", "/api/v1/pods"}, {"service", "/api/v1/services"},
	{"deployment", "/apis/apps/v1/deployments"}, {"statefulset", "/apis/apps/v1/statefulsets"}, {"daemonset", "/apis/apps/v1/daemonsets"},
	{"job", "/apis/batch/v1/jobs"}, {"cronjob", "/apis/batch/v1/cronjobs"}, {"ingress", "/apis/networking.k8s.io/v1/ingresses"},
}

// Collect 获取集群与所需 Kubernetes 对象；单个 API 失败作为该资源类型失败返回。
func (c *Collector) Collect(ctx context.Context, source resource.Source, plain []byte) ([]resource.CollectionResult, error) {
	var auth credential
	if json.Unmarshal(plain, &auth) != nil || auth.Server == "" || auth.Token == "" {
		return nil, resource.ErrAuthenticationFailed
	}
	client := c.client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: auth.Insecure}}}
	} //nolint:gosec -- 是否跳过校验由接入源显式配置。
	results := []resource.CollectionResult{{ResourceType: "cluster", Snapshots: []resource.Snapshot{{ResourceType: "cluster", ExternalID: strconv.FormatUint(source.ID, 10), Name: source.Name, CloudStatus: "reachable", Endpoints: []resource.EndpointSnapshot{{Kind: "hostname", Address: auth.Server, Protocol: "https"}}}}}}
	for _, target := range paths {
		items, err := collectList(ctx, client, auth, target.path)
		if errors.Is(err, resource.ErrAuthenticationFailed) {
			return nil, err
		}
		result := resource.CollectionResult{ResourceType: target.kind, Err: err}
		for _, item := range items {
			result.Snapshots = append(result.Snapshots, snapshot(target.kind, item))
		}
		results = append(results, result)
	}
	return results, nil
}

// collectList 请求单个 Kubernetes 列表接口并支持 continue 分页。
func collectList(ctx context.Context, client *http.Client, auth credential, path string) ([]map[string]any, error) {
	var items []map[string]any
	next := ""
	for {
		endpoint := strings.TrimRight(auth.Server, "/") + path
		if next != "" {
			endpoint += "?continue=" + url.QueryEscape(next)
		}
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		request.Header.Set("Authorization", "Bearer "+auth.Token)
		request.Header.Set("Accept", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			response.Body.Close()
			return nil, resource.ErrAuthenticationFailed
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return nil, fmt.Errorf("Kubernetes API 返回状态 %d", response.StatusCode)
		}
		var page listResponse
		err = json.NewDecoder(response.Body).Decode(&page)
		response.Body.Close()
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.Continue == "" {
			return items, nil
		}
		next = page.Continue
	}
}

// snapshot 将非结构化 Kubernetes 对象转换为稳定身份和访问端点。
func snapshot(kind string, item map[string]any) resource.Snapshot {
	metadata := object(item, "metadata")
	uid := text(metadata, "uid")
	name := text(metadata, "name")
	namespace := text(metadata, "namespace")
	if uid == "" {
		uid = namespace + "/" + name
	}
	raw, _ := json.Marshal(item)
	value := resource.Snapshot{ResourceType: kind, ExternalID: uid, Name: name, CloudStatus: "present", RawAttributes: raw}
	status, spec := object(item, "status"), object(item, "spec")
	if kind == "node" {
		for _, entry := range objects(status, "addresses") {
			address := text(entry, "address")
			switch text(entry, "type") {
			case "InternalIP":
				value.Endpoints = append(value.Endpoints, resource.EndpointSnapshot{Kind: "private", Address: address})
			case "ExternalIP":
				value.Endpoints = append(value.Endpoints, resource.EndpointSnapshot{Kind: "public", Address: address})
			}
		}
	}
	if kind == "pod" {
		if address := text(status, "podIP"); address != "" {
			value.Endpoints = append(value.Endpoints, resource.EndpointSnapshot{Kind: "private", Address: address})
		}
	}
	if kind == "service" {
		port := firstPort(spec)
		if address := text(spec, "clusterIP"); address != "" && address != "None" {
			value.Endpoints = append(value.Endpoints, resource.EndpointSnapshot{Kind: "private", Address: address, Port: port})
		}
		value.Endpoints = append(value.Endpoints, loadBalancerEndpoints(status, port)...)
	}
	if kind == "ingress" {
		value.Endpoints = append(value.Endpoints, loadBalancerEndpoints(status, 0)...)
	}
	return value
}

func object(value map[string]any, key string) map[string]any {
	result, _ := value[key].(map[string]any)
	return result
}
func text(value map[string]any, key string) string { result, _ := value[key].(string); return result }
func objects(value map[string]any, key string) []map[string]any {
	raw, _ := value[key].([]any)
	result := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		if item, ok := entry.(map[string]any); ok {
			result = append(result, item)
		}
	}
	return result
}
func firstPort(spec map[string]any) int {
	values := objects(spec, "ports")
	if len(values) == 0 {
		return 0
	}
	number, _ := values[0]["port"].(float64)
	return int(number)
}
func loadBalancerEndpoints(status map[string]any, port int) []resource.EndpointSnapshot {
	ingress := objects(object(status, "loadBalancer"), "ingress")
	result := []resource.EndpointSnapshot{}
	for _, item := range ingress {
		if address := text(item, "ip"); address != "" {
			result = append(result, resource.EndpointSnapshot{Kind: "public", Address: address, Port: port})
		}
		if address := text(item, "hostname"); address != "" {
			result = append(result, resource.EndpointSnapshot{Kind: "hostname", Address: address, Port: port})
		}
	}
	return result
}
