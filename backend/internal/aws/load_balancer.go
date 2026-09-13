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
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
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

// elbV2API 约束 ELBv2 采集只依赖全量列表和监听器列表，便于模拟分页与单类型失败。
type elbV2API interface {
	DescribeLoadBalancers(context.Context, *elasticloadbalancingv2.DescribeLoadBalancersInput, ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error)
	DescribeListeners(context.Context, *elasticloadbalancingv2.DescribeListenersInput, ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeListenersOutput, error)
}

// listELBV2LoadBalancers 单次入口沿 Marker 读取三种官方 ELBv2 类型的全量列表，失败时丢弃局部事实。
func listELBV2LoadBalancers(ctx context.Context, client elbV2API) ([]elbv2types.LoadBalancer, error) {
	var values []elbv2types.LoadBalancer
	seenMarkers := make(map[string]struct{})
	marker := ""

	for {
		input := &elasticloadbalancingv2.DescribeLoadBalancersInput{PageSize: awssdk.Int32(400)}
		if marker != "" {
			input.Marker = awssdk.String(marker)
		}
		output, err := client.DescribeLoadBalancers(ctx, input)
		if err != nil {
			return nil, err
		}
		if output == nil {
			return nil, errors.New("ELBv2 分页响应为空")
		}
		values = append(values, output.LoadBalancers...)

		nextMarker := awssdk.ToString(output.NextMarker)
		if nextMarker == "" {
			return values, nil
		}
		if _, exists := seenMarkers[nextMarker]; exists {
			return nil, errors.New("ELBv2 分页标记重复")
		}
		seenMarkers[nextMarker] = struct{}{}
		marker = nextMarker
	}
}

// collectELBV2ByType 先验证所有官方类型，再按 alb、nlb、gwlb 顺序独立读取监听器并生成结果。
func collectELBV2ByType(ctx context.Context, client elbV2API, values []elbv2types.LoadBalancer, region string) ([]resource.CollectionResult, error) {
	resourceTypes := []string{"alb", "nlb", "gwlb"}
	groups := map[string][]elbv2types.LoadBalancer{
		"alb":  nil,
		"nlb":  nil,
		"gwlb": nil,
	}
	for _, value := range values {
		resourceType, ok := elbV2ResourceType(value.Type)
		if !ok {
			typeErr := errors.New("ELBv2 负载均衡类型无效")
			results := make([]resource.CollectionResult, 0, len(resourceTypes))
			for _, failedType := range resourceTypes {
				results = append(results, resource.CollectionResult{ResourceType: failedType, Err: typeErr})
			}
			return results, nil
		}
		groups[resourceType] = append(groups[resourceType], value)
	}

	results := make([]resource.CollectionResult, 0, len(resourceTypes))
	for _, resourceType := range resourceTypes {
		snapshots, err := collectELBV2Group(ctx, client, groups[resourceType], region, resourceType)
		if err != nil {
			if accessErr := classifyAWSAccessError(err); accessErr != nil {
				err = accessErr
			}
			// 凭证失效影响整个接入源，必须立即停止后续类型请求并丢弃此前成功结果。
			if errors.Is(err, resource.ErrCloudAuthentication) {
				return nil, err
			}
		}
		results = append(results, resource.CollectionResult{ResourceType: resourceType, Snapshots: snapshots, Err: err})
	}
	return results, nil
}

// elbV2ResourceType 只接受当前 AWS 官方三种类型，未知值必须由调用方整体拒绝。
func elbV2ResourceType(value elbv2types.LoadBalancerTypeEnum) (string, bool) {
	switch value {
	case elbv2types.LoadBalancerTypeEnumApplication:
		return "alb", true
	case elbv2types.LoadBalancerTypeEnumNetwork:
		return "nlb", true
	case elbv2types.LoadBalancerTypeEnumGateway:
		return "gwlb", true
	default:
		return "", false
	}
}

// collectELBV2Group 将监听器失败限制在当前资源类型内，并在失败时丢弃该类型已经构造的快照。
func collectELBV2Group(ctx context.Context, client elbV2API, values []elbv2types.LoadBalancer, region, resourceType string) ([]resource.Snapshot, error) {
	snapshots := make([]resource.Snapshot, 0, len(values))
	for _, value := range values {
		listeners, err := listELBV2Listeners(ctx, client, value.LoadBalancerArn)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, elbV2Snapshot(value, listeners, region, resourceType))
	}
	return snapshots, nil
}

// listELBV2Listeners 沿单个负载均衡器的 Marker 读取全量监听器，异常分页不得留下局部监听器。
func listELBV2Listeners(ctx context.Context, client elbV2API, loadBalancerARN *string) ([]elbv2types.Listener, error) {
	var listeners []elbv2types.Listener
	seenMarkers := make(map[string]struct{})
	marker := ""

	for {
		input := &elasticloadbalancingv2.DescribeListenersInput{
			LoadBalancerArn: loadBalancerARN,
			PageSize:        awssdk.Int32(400),
		}
		if marker != "" {
			input.Marker = awssdk.String(marker)
		}
		output, err := client.DescribeListeners(ctx, input)
		if err != nil {
			return nil, err
		}
		if output == nil {
			return nil, errors.New("ELBv2 监听器分页响应为空")
		}
		listeners = append(listeners, output.Listeners...)

		nextMarker := awssdk.ToString(output.NextMarker)
		if nextMarker == "" {
			return listeners, nil
		}
		if _, exists := seenMarkers[nextMarker]; exists {
			return nil, errors.New("ELBv2 监听器分页标记重复")
		}
		seenMarkers[nextMarker] = struct{}{}
		marker = nextMarker
	}
}

// elbV2Snapshot 只序列化负载均衡器列表项，监听器仅用于构造去重后的访问端点。
func elbV2Snapshot(loadBalancer elbv2types.LoadBalancer, listeners []elbv2types.Listener, region, resourceType string) resource.Snapshot {
	raw, _ := json.Marshal(loadBalancer)
	scheme := string(loadBalancer.Scheme)
	kind := "private"
	if loadBalancer.Scheme == elbv2types.LoadBalancerSchemeEnumInternetFacing {
		kind = "public"
	}
	snapshot := resource.Snapshot{
		ResourceType:  resourceType,
		ExternalID:    awssdk.ToString(loadBalancer.LoadBalancerArn),
		Name:          awssdk.ToString(loadBalancer.LoadBalancerName),
		Region:        region,
		NetworkType:   scheme,
		RawAttributes: raw,
	}
	if len(loadBalancer.AvailabilityZones) > 0 {
		snapshot.Zone = awssdk.ToString(loadBalancer.AvailabilityZones[0].ZoneName)
	}
	if loadBalancer.State != nil {
		snapshot.CloudStatus = string(loadBalancer.State.Code)
	}

	address := awssdk.ToString(loadBalancer.DNSName)
	if address == "" {
		return snapshot
	}
	for _, listener := range listeners {
		appendELBV2Endpoint(&snapshot, kind, address, int(awssdk.ToInt32(listener.Port)), strings.ToLower(string(listener.Protocol)))
	}
	if len(snapshot.Endpoints) == 0 {
		appendELBV2Endpoint(&snapshot, kind, address, 0, "")
	}
	return snapshot
}

// appendELBV2Endpoint 以端点完整身份去重，避免错误合并同端口的不同协议。
func appendELBV2Endpoint(snapshot *resource.Snapshot, kind, address string, port int, protocol string) {
	for _, endpoint := range snapshot.Endpoints {
		if endpoint.Kind == kind && endpoint.Address == address && endpoint.Port == port && endpoint.Protocol == protocol {
			return
		}
	}
	snapshot.Endpoints = append(snapshot.Endpoints, resource.EndpointSnapshot{Kind: kind, Address: address, Port: port, Protocol: protocol})
}
