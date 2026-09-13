// 本文件负责将 AWS Classic Load Balancer 列表响应转换为统一资源快照，不接入顶层采集编排。
package aws

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"cmdb/internal/resource"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
)

// classicELBAPI 约束 Classic ELB 采集只依赖列表接口，便于使用模拟响应验证分页边界。
type classicELBAPI interface {
	DescribeLoadBalancers(context.Context, *elasticloadbalancing.DescribeLoadBalancersInput, ...func(*elasticloadbalancing.Options)) (*elasticloadbalancing.DescribeLoadBalancersOutput, error)
}

// collectCLB 沿 Classic ELB 的 Marker 手动读取全量列表；任一分页失败时不返回局部事实，避免覆盖有效快照。
func collectCLB(ctx context.Context, client classicELBAPI, region string) ([]resource.Snapshot, error) {
	var snapshots []resource.Snapshot
	seenMarkers := make(map[string]struct{})
	marker := ""

	for {
		input := &elasticloadbalancing.DescribeLoadBalancersInput{PageSize: awssdk.Int32(400)}
		if marker != "" {
			input.Marker = awssdk.String(marker)
		}
		output, err := client.DescribeLoadBalancers(ctx, input)
		if err != nil {
			return nil, err
		}
		if output == nil {
			return nil, errors.New("Classic ELB 分页响应为空")
		}
		snapshots = append(snapshots, clbSnapshots(output.LoadBalancerDescriptions, region)...)

		nextMarker := awssdk.ToString(output.NextMarker)
		if nextMarker == "" {
			return snapshots, nil
		}
		if _, exists := seenMarkers[nextMarker]; exists {
			return nil, errors.New("Classic ELB 分页标记重复")
		}
		seenMarkers[nextMarker] = struct{}{}
		marker = nextMarker
	}
}

// clbSnapshots 只保存 Classic ELB 列表项原始属性，并以 DNS 和监听器组合为访问端点。
func clbSnapshots(loadBalancers []elbtypes.LoadBalancerDescription, region string) []resource.Snapshot {
	snapshots := make([]resource.Snapshot, 0, len(loadBalancers))
	for _, loadBalancer := range loadBalancers {
		raw, _ := json.Marshal(loadBalancer)
		scheme := awssdk.ToString(loadBalancer.Scheme)
		kind := "private"
		if strings.EqualFold(scheme, "internet-facing") {
			kind = "public"
		}
		snapshot := resource.Snapshot{
			ResourceType:  "clb",
			ExternalID:    awssdk.ToString(loadBalancer.LoadBalancerName),
			Name:          awssdk.ToString(loadBalancer.LoadBalancerName),
			Region:        region,
			NetworkType:   scheme,
			RawAttributes: raw,
		}
		if len(loadBalancer.AvailabilityZones) > 0 {
			snapshot.Zone = loadBalancer.AvailabilityZones[0]
		}

		address := awssdk.ToString(loadBalancer.DNSName)
		if address != "" {
			for _, description := range loadBalancer.ListenerDescriptions {
				if description.Listener == nil {
					continue
				}
				appendCLBEndpoint(&snapshot, kind, address, int(description.Listener.LoadBalancerPort), strings.ToLower(awssdk.ToString(description.Listener.Protocol)))
			}
			// Classic 列表项没有监听器时仍保留 DNS，但不得臆造端口或协议。
			if len(snapshot.Endpoints) == 0 {
				appendCLBEndpoint(&snapshot, kind, address, 0, "")
			}
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots
}

// appendCLBEndpoint 以端点完整身份去重，确保同端口的不同协议不会被合并。
func appendCLBEndpoint(snapshot *resource.Snapshot, kind, address string, port int, protocol string) {
	for _, endpoint := range snapshot.Endpoints {
		if endpoint.Kind == kind && endpoint.Address == address && endpoint.Port == port && endpoint.Protocol == protocol {
			return
		}
	}
	snapshot.Endpoints = append(snapshot.Endpoints, resource.EndpointSnapshot{Kind: kind, Address: address, Port: port, Protocol: protocol})
}
