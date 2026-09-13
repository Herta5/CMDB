// 本文件使用 Classic ELB 的模拟响应验证分页快照转换，不访问真实 AWS 账号。
package aws

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"cmdb/internal/resource"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// classicELBStub 只模拟 Classic ELB 的列表调用，用于隔离云端网络并保留请求分页参数。
type classicELBStub struct {
	pages  []*elasticloadbalancing.DescribeLoadBalancersOutput
	errors []error
	inputs []*elasticloadbalancing.DescribeLoadBalancersInput
}

func (s *classicELBStub) DescribeLoadBalancers(_ context.Context, input *elasticloadbalancing.DescribeLoadBalancersInput, _ ...func(*elasticloadbalancing.Options)) (*elasticloadbalancing.DescribeLoadBalancersOutput, error) {
	s.inputs = append(s.inputs, input)
	index := len(s.inputs) - 1
	if index < len(s.errors) && s.errors[index] != nil {
		return nil, s.errors[index]
	}
	if index < len(s.pages) {
		return s.pages[index], nil
	}
	return nil, errors.New("未配置的 Classic ELB 模拟响应")
}

// TestCollectCLBReadsAllPagesAndListeners 防止 Classic ELB 只读取首页或遗漏页面中的监听器。
func TestCollectCLBReadsAllPagesAndListeners(t *testing.T) {
	client := &classicELBStub{pages: []*elasticloadbalancing.DescribeLoadBalancersOutput{
		{LoadBalancerDescriptions: []elbtypes.LoadBalancerDescription{{
			LoadBalancerName: awssdk.String("classic-first"), DNSName: awssdk.String("first.example.invalid"),
			ListenerDescriptions: []elbtypes.ListenerDescription{{Listener: &elbtypes.Listener{LoadBalancerPort: 80, Protocol: awssdk.String("HTTP")}}},
		}}, NextMarker: awssdk.String("page-2")},
		{LoadBalancerDescriptions: []elbtypes.LoadBalancerDescription{{
			LoadBalancerName: awssdk.String("classic-second"), DNSName: awssdk.String("second.example.invalid"),
			ListenerDescriptions: []elbtypes.ListenerDescription{{Listener: &elbtypes.Listener{LoadBalancerPort: 443, Protocol: awssdk.String("HTTPS")}}},
		}}},
	}}

	snapshots, err := collectCLB(context.Background(), client, "cn-north-1")
	if err != nil {
		t.Fatalf("Classic ELB 全分页采集失败：%v", err)
	}
	if len(snapshots) != 2 || snapshots[0].ExternalID != "classic-first" || snapshots[1].ExternalID != "classic-second" {
		t.Fatalf("Classic ELB 必须合并所有页面的负载均衡：%+v", snapshots)
	}
	if len(snapshots[0].Endpoints) != 1 || snapshots[0].Endpoints[0].Port != 80 || snapshots[0].Endpoints[0].Protocol != "http" || len(snapshots[1].Endpoints) != 1 || snapshots[1].Endpoints[0].Port != 443 || snapshots[1].Endpoints[0].Protocol != "https" {
		t.Fatalf("Classic ELB 必须保留每页负载均衡的监听端口和协议：%+v", snapshots)
	}
	if len(client.inputs) != 2 || awssdk.ToInt32(client.inputs[0].PageSize) != 400 || awssdk.ToString(client.inputs[0].Marker) != "" || awssdk.ToInt32(client.inputs[1].PageSize) != 400 || awssdk.ToString(client.inputs[1].Marker) != "page-2" {
		t.Fatalf("Classic ELB 必须以 PageSize=400 沿 NextMarker 分页：%+v", client.inputs)
	}
}

// TestCollectCLBRejectsPaginationFailureWithoutPartialSnapshots 防止后续分页失败时用不完整事实覆盖旧快照。
func TestCollectCLBRejectsPaginationFailureWithoutPartialSnapshots(t *testing.T) {
	client := &classicELBStub{
		pages: []*elasticloadbalancing.DescribeLoadBalancersOutput{{
			LoadBalancerDescriptions: []elbtypes.LoadBalancerDescription{{LoadBalancerName: awssdk.String("classic-first")}},
			NextMarker:               awssdk.String("page-2"),
		}},
		errors: []error{nil, errors.New("第二页读取失败")},
	}

	snapshots, err := collectCLB(context.Background(), client, "cn-north-1")
	if err == nil {
		t.Fatal("Classic ELB 分页失败时必须返回错误")
	}
	if snapshots != nil {
		t.Fatalf("Classic ELB 分页失败时不得返回局部快照：%+v", snapshots)
	}
}

// TestCLBSnapshotsKeepOfficialTypeAndEndpoints 防止 Classic ELB 被伪装为 v2 类型或丢失监听器、网络边界。
func TestCLBSnapshotsKeepOfficialTypeAndEndpoints(t *testing.T) {
	client := &classicELBStub{pages: []*elasticloadbalancing.DescribeLoadBalancersOutput{{LoadBalancerDescriptions: []elbtypes.LoadBalancerDescription{
		{
			LoadBalancerName: awssdk.String("classic-internal"), DNSName: awssdk.String("internal.example.invalid"), Scheme: awssdk.String("internal"), AvailabilityZones: []string{"cn-north-1a", "cn-north-1b"},
			ListenerDescriptions: []elbtypes.ListenerDescription{
				{Listener: &elbtypes.Listener{LoadBalancerPort: 443, Protocol: awssdk.String("HTTPS")}},
				{Listener: &elbtypes.Listener{LoadBalancerPort: 443, Protocol: awssdk.String("HTTPS")}},
				{Listener: &elbtypes.Listener{LoadBalancerPort: 443, Protocol: awssdk.String("TCP")}},
			},
		},
		{LoadBalancerName: awssdk.String("classic-no-listener"), DNSName: awssdk.String("bare.example.invalid"), Scheme: awssdk.String("internet-facing")},
		{LoadBalancerName: awssdk.String("classic-no-address"), Scheme: awssdk.String("internal")},
	}}}}

	snapshots, err := collectCLB(context.Background(), client, "cn-north-1")
	if err != nil || len(snapshots) != 3 {
		t.Fatalf("Classic ELB 快照数量错误：snapshots=%+v err=%v", snapshots, err)
	}
	first := snapshots[0]
	wantEndpoints := []resource.EndpointSnapshot{
		{Kind: "private", Address: "internal.example.invalid", Port: 443, Protocol: "https"},
		{Kind: "private", Address: "internal.example.invalid", Port: 443, Protocol: "tcp"},
	}
	if first.ResourceType != "clb" || first.ExternalID != "classic-internal" || first.Name != "classic-internal" || first.Region != "cn-north-1" || first.Zone != "cn-north-1a" || first.NetworkType != "internal" || first.CloudStatus != "" || !reflect.DeepEqual(first.Endpoints, wantEndpoints) {
		t.Fatalf("Classic ELB 快照字段或端点错误：%+v", first)
	}
	if len(snapshots[1].Endpoints) != 1 || !reflect.DeepEqual(snapshots[1].Endpoints[0], resource.EndpointSnapshot{Kind: "public", Address: "bare.example.invalid"}) || len(snapshots[2].Endpoints) != 0 {
		t.Fatalf("Classic ELB 必须保留无监听器 DNS、且空 DNS 不生成端点：%+v", snapshots)
	}
	var raw elbtypes.LoadBalancerDescription
	if err := json.Unmarshal(first.RawAttributes, &raw); err != nil || awssdk.ToString(raw.LoadBalancerName) != "classic-internal" || awssdk.ToString(raw.DNSName) != "internal.example.invalid" {
		t.Fatalf("Classic ELB 原始属性必须只包含列表项：raw=%s err=%v", first.RawAttributes, err)
	}
	wantRaw, err := json.Marshal(client.pages[0].LoadBalancerDescriptions[0])
	if err != nil || string(first.RawAttributes) != string(wantRaw) {
		t.Fatal("Classic ELB 原始属性必须完整保留单个列表项，不能混入分页或快照字段")
	}
}

// TestCollectCLBRejectsNilResponse 防止空响应被当成成功空列表，或泄露此前页面的局部快照。
func TestCollectCLBRejectsNilResponse(t *testing.T) {
	for _, test := range []struct {
		name  string
		pages []*elasticloadbalancing.DescribeLoadBalancersOutput
	}{
		{name: "首页空响应", pages: []*elasticloadbalancing.DescribeLoadBalancersOutput{nil}},
		{name: "后续页空响应", pages: []*elasticloadbalancing.DescribeLoadBalancersOutput{
			{LoadBalancerDescriptions: []elbtypes.LoadBalancerDescription{{LoadBalancerName: awssdk.String("classic-first")}}, NextMarker: awssdk.String("page-2")}, nil,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshots, err := collectCLB(context.Background(), &classicELBStub{pages: test.pages}, "cn-north-1")
			if err == nil || err.Error() != "Classic ELB 分页响应为空" || snapshots != nil {
				t.Fatalf("空响应必须安全失败且丢弃全部局部快照：snapshots=%+v err=%v", snapshots, err)
			}
		})
	}
}

// TestCollectCLBRejectsNonAdjacentMarkerCycle 防止只比较相邻标记时漏过跨多个页面的循环。
func TestCollectCLBRejectsNonAdjacentMarkerCycle(t *testing.T) {
	client := &classicELBStub{pages: []*elasticloadbalancing.DescribeLoadBalancersOutput{
		{LoadBalancerDescriptions: []elbtypes.LoadBalancerDescription{{LoadBalancerName: awssdk.String("classic-first")}}, NextMarker: awssdk.String("page-2")},
		{NextMarker: awssdk.String("page-3")},
		{NextMarker: awssdk.String("page-2")},
	}}
	snapshots, err := collectCLB(context.Background(), client, "cn-north-1")
	if err == nil || err.Error() != "Classic ELB 分页标记重复" || snapshots != nil || len(client.inputs) != 3 {
		t.Fatalf("非相邻分页循环必须立即失败且丢弃全部局部快照：snapshots=%+v err=%v calls=%d", snapshots, err, len(client.inputs))
	}
}

// TestCollectCLBRejectsRepeatedMarker 防止异常响应的循环标记导致同步任务无限请求。
func TestCollectCLBRejectsRepeatedMarker(t *testing.T) {
	client := &classicELBStub{pages: []*elasticloadbalancing.DescribeLoadBalancersOutput{
		{NextMarker: awssdk.String("loop")},
		{NextMarker: awssdk.String("loop")},
	}}

	snapshots, err := collectCLB(context.Background(), client, "cn-north-1")
	if err == nil || err.Error() != "Classic ELB 分页标记重复" {
		t.Fatalf("重复分页标记必须返回固定中文内部错误：%v", err)
	}
	if snapshots != nil {
		t.Fatalf("重复分页标记时不得返回局部快照：%+v", snapshots)
	}
	if len(client.inputs) != 2 {
		t.Fatalf("Classic ELB 必须在发现重复标记后停止请求：calls=%d", len(client.inputs))
	}
}

// elbV2Stub 分别保存负载均衡器和各 ARN 的监听器分页响应，用于验证真实的分组快照行为。
type elbV2Stub struct {
	loadBalancerPages  []*elasticloadbalancingv2.DescribeLoadBalancersOutput
	loadBalancerErrors []error
	loadBalancerInputs []*elasticloadbalancingv2.DescribeLoadBalancersInput
	listenerPages      map[string][]*elasticloadbalancingv2.DescribeListenersOutput
	listenerErrors     map[string][]error
	listenerInputs     map[string][]*elasticloadbalancingv2.DescribeListenersInput
}

func (s *elbV2Stub) DescribeLoadBalancers(_ context.Context, input *elasticloadbalancingv2.DescribeLoadBalancersInput, _ ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error) {
	s.loadBalancerInputs = append(s.loadBalancerInputs, input)
	index := len(s.loadBalancerInputs) - 1
	if index < len(s.loadBalancerErrors) && s.loadBalancerErrors[index] != nil {
		return nil, s.loadBalancerErrors[index]
	}
	if index < len(s.loadBalancerPages) {
		return s.loadBalancerPages[index], nil
	}
	return nil, errors.New("未配置的 ELBv2 负载均衡器模拟响应")
}

func (s *elbV2Stub) DescribeListeners(_ context.Context, input *elasticloadbalancingv2.DescribeListenersInput, _ ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeListenersOutput, error) {
	arn := awssdk.ToString(input.LoadBalancerArn)
	if s.listenerInputs == nil {
		s.listenerInputs = make(map[string][]*elasticloadbalancingv2.DescribeListenersInput)
	}
	s.listenerInputs[arn] = append(s.listenerInputs[arn], input)
	index := len(s.listenerInputs[arn]) - 1
	if index < len(s.listenerErrors[arn]) && s.listenerErrors[arn][index] != nil {
		return nil, s.listenerErrors[arn][index]
	}
	if index < len(s.listenerPages[arn]) {
		return s.listenerPages[arn][index], nil
	}
	return nil, errors.New("未配置的 ELBv2 监听器模拟响应")
}

// TestListELBV2LoadBalancersRunsOnceAndReadsAllPages 防止调用方必须按类型重复列举，或遗漏后续页面。
func TestListELBV2LoadBalancersRunsOnceAndReadsAllPages(t *testing.T) {
	client := &elbV2Stub{loadBalancerPages: []*elasticloadbalancingv2.DescribeLoadBalancersOutput{
		{LoadBalancers: []elbv2types.LoadBalancer{
			{LoadBalancerArn: awssdk.String("arn:alb:first"), Type: elbv2types.LoadBalancerTypeEnumApplication},
			{LoadBalancerArn: awssdk.String("arn:nlb:first"), Type: elbv2types.LoadBalancerTypeEnumNetwork},
		}, NextMarker: awssdk.String("page-2")},
		{LoadBalancers: []elbv2types.LoadBalancer{{LoadBalancerArn: awssdk.String("arn:gwlb:first"), Type: elbv2types.LoadBalancerTypeEnumGateway}}},
	}}

	values, err := listELBV2LoadBalancers(context.Background(), client)
	if err != nil {
		t.Fatalf("ELBv2 全分页列表失败：%v", err)
	}
	if len(values) != 3 || awssdk.ToString(values[0].LoadBalancerArn) != "arn:alb:first" || awssdk.ToString(values[1].LoadBalancerArn) != "arn:nlb:first" || awssdk.ToString(values[2].LoadBalancerArn) != "arn:gwlb:first" {
		t.Fatalf("单次列表必须合并三种官方类型的所有页面：%+v", values)
	}
	if len(client.loadBalancerInputs) != 2 || awssdk.ToInt32(client.loadBalancerInputs[0].PageSize) != 400 || awssdk.ToString(client.loadBalancerInputs[0].Marker) != "" || awssdk.ToInt32(client.loadBalancerInputs[1].PageSize) != 400 || awssdk.ToString(client.loadBalancerInputs[1].Marker) != "page-2" {
		t.Fatalf("ELBv2 必须以 PageSize=400 沿 NextMarker 分页：%+v", client.loadBalancerInputs)
	}
}

// TestListELBV2LoadBalancersRejectsBrokenPagination 防止异常分页被当成成功或返回局部列表。
func TestListELBV2LoadBalancersRejectsBrokenPagination(t *testing.T) {
	pageError := errors.New("第二页读取失败")
	tests := []struct {
		name    string
		client  *elbV2Stub
		wantErr string
		isErr   error
	}{
		{name: "首页空响应", client: &elbV2Stub{loadBalancerPages: []*elasticloadbalancingv2.DescribeLoadBalancersOutput{nil}}, wantErr: "ELBv2 分页响应为空"},
		{name: "后续页空响应", client: &elbV2Stub{loadBalancerPages: []*elasticloadbalancingv2.DescribeLoadBalancersOutput{
			{LoadBalancers: []elbv2types.LoadBalancer{{LoadBalancerArn: awssdk.String("arn:alb:partial")}}, NextMarker: awssdk.String("page-2")}, nil,
		}}, wantErr: "ELBv2 分页响应为空"},
		{name: "后续页原始错误", client: &elbV2Stub{
			loadBalancerPages:  []*elasticloadbalancingv2.DescribeLoadBalancersOutput{{NextMarker: awssdk.String("page-2")}},
			loadBalancerErrors: []error{nil, pageError},
		}, isErr: pageError},
		{name: "非相邻标记循环", client: &elbV2Stub{loadBalancerPages: []*elasticloadbalancingv2.DescribeLoadBalancersOutput{
			{NextMarker: awssdk.String("page-2")}, {NextMarker: awssdk.String("page-3")}, {NextMarker: awssdk.String("page-2")},
		}}, wantErr: "ELBv2 分页标记重复"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values, err := listELBV2LoadBalancers(context.Background(), test.client)
			if err == nil || values != nil {
				t.Fatalf("异常分页必须失败且丢弃局部列表：values=%+v err=%v", values, err)
			}
			if test.wantErr != "" && err.Error() != test.wantErr {
				t.Fatalf("分页内部错误必须为固定中文：got=%v want=%s", err, test.wantErr)
			}
			if test.isErr != nil && !errors.Is(err, test.isErr) {
				t.Fatalf("云端分页错误必须原样返回：got=%v want=%v", err, test.isErr)
			}
		})
	}
}

// TestCollectELBV2GroupsOfficialTypes 防止三种官方类型混入同一资源身份，或快照遗漏云端字段与端点。
func TestCollectELBV2GroupsOfficialTypes(t *testing.T) {
	values := []elbv2types.LoadBalancer{
		{
			LoadBalancerArn:  awssdk.String("arn:nlb:one"),
			LoadBalancerName: awssdk.String("network-one"),
			DNSName:          awssdk.String("network.example.invalid"),
			Scheme:           elbv2types.LoadBalancerSchemeEnumInternal,
			Type:             elbv2types.LoadBalancerTypeEnumNetwork,
			State:            &elbv2types.LoadBalancerState{Code: elbv2types.LoadBalancerStateEnumProvisioning},
			AvailabilityZones: []elbv2types.AvailabilityZone{
				{ZoneName: awssdk.String("cn-north-1b")}, {ZoneName: awssdk.String("cn-north-1c")},
			},
		},
		{LoadBalancerArn: awssdk.String("arn:gwlb:one"), LoadBalancerName: awssdk.String("gateway-one"), DNSName: awssdk.String("gateway.example.invalid"), Scheme: elbv2types.LoadBalancerSchemeEnumInternal, Type: elbv2types.LoadBalancerTypeEnumGateway},
		{LoadBalancerArn: awssdk.String("arn:alb:one"), LoadBalancerName: awssdk.String("application-one"), DNSName: awssdk.String("application.example.invalid"), Scheme: elbv2types.LoadBalancerSchemeEnumInternetFacing, Type: elbv2types.LoadBalancerTypeEnumApplication, State: &elbv2types.LoadBalancerState{Code: elbv2types.LoadBalancerStateEnumActive}},
	}
	client := &elbV2Stub{listenerPages: map[string][]*elasticloadbalancingv2.DescribeListenersOutput{
		"arn:alb:one": {{Listeners: []elbv2types.Listener{{Port: awssdk.Int32(443), Protocol: elbv2types.ProtocolEnumHttps}}}},
		"arn:nlb:one": {{Listeners: []elbv2types.Listener{
			{Port: awssdk.Int32(443), Protocol: elbv2types.ProtocolEnumTls},
			{Port: awssdk.Int32(443), Protocol: elbv2types.ProtocolEnumTls},
			{Port: awssdk.Int32(443), Protocol: elbv2types.ProtocolEnumTcp},
		}}},
		"arn:gwlb:one": {{Listeners: []elbv2types.Listener{{Port: awssdk.Int32(6081), Protocol: elbv2types.ProtocolEnumGeneve}}}},
	}}

	results, err := collectELBV2ByType(context.Background(), client, values, "cn-north-1")
	if err != nil {
		t.Fatalf("ELBv2 分组采集不应返回整体错误：%v", err)
	}
	if len(results) != 3 || results[0].ResourceType != "alb" || results[1].ResourceType != "nlb" || results[2].ResourceType != "gwlb" {
		t.Fatalf("结果顺序必须固定为 alb,nlb,gwlb：%+v", results)
	}
	if results[0].Err != nil || results[1].Err != nil || results[2].Err != nil || len(results[0].Snapshots) != 1 || len(results[1].Snapshots) != 1 || len(results[2].Snapshots) != 1 {
		t.Fatalf("三种官方类型必须分别采集成功：%+v", results)
	}

	alb := results[0].Snapshots[0]
	if alb.ResourceType != "alb" || alb.ExternalID != "arn:alb:one" || alb.Name != "application-one" || alb.Region != "cn-north-1" || alb.CloudStatus != "active" || alb.NetworkType != "internet-facing" || !reflect.DeepEqual(alb.Endpoints, []resource.EndpointSnapshot{{Kind: "public", Address: "application.example.invalid", Port: 443, Protocol: "https"}}) {
		t.Fatalf("ALB 快照字段错误：%+v", alb)
	}
	nlb := results[1].Snapshots[0]
	if nlb.ResourceType != "nlb" || nlb.ExternalID != "arn:nlb:one" || nlb.Name != "network-one" || nlb.Zone != "cn-north-1b" || nlb.CloudStatus != "provisioning" || nlb.NetworkType != "internal" || !reflect.DeepEqual(nlb.Endpoints, []resource.EndpointSnapshot{
		{Kind: "private", Address: "network.example.invalid", Port: 443, Protocol: "tls"},
		{Kind: "private", Address: "network.example.invalid", Port: 443, Protocol: "tcp"},
	}) {
		t.Fatalf("NLB 快照字段或端点完整身份去重错误：%+v", nlb)
	}
	gwlb := results[2].Snapshots[0]
	if gwlb.ResourceType != "gwlb" || gwlb.ExternalID != "arn:gwlb:one" || !reflect.DeepEqual(gwlb.Endpoints, []resource.EndpointSnapshot{{Kind: "private", Address: "gateway.example.invalid", Port: 6081, Protocol: "geneve"}}) {
		t.Fatalf("GWLB 快照字段错误：%+v", gwlb)
	}
	var raw elbv2types.LoadBalancer
	if err := json.Unmarshal(nlb.RawAttributes, &raw); err != nil || awssdk.ToString(raw.LoadBalancerArn) != "arn:nlb:one" || raw.Type != elbv2types.LoadBalancerTypeEnumNetwork {
		t.Fatalf("ELBv2 原始属性必须只包含当前列表项：raw=%s err=%v", nlb.RawAttributes, err)
	}
	wantRaw, err := json.Marshal(values[0])
	if err != nil || string(nlb.RawAttributes) != string(wantRaw) {
		t.Fatal("ELBv2 原始属性必须完整保留单个列表项，不能混入监听器或分组字段")
	}
}

// TestCollectELBV2ListenerFailureOnlyFailsItsType 防止某类监听器失败污染其他类型，或保留当前类型的局部快照。
func TestCollectELBV2ListenerFailureOnlyFailsItsType(t *testing.T) {
	albError := errors.New("第二个 ALB 监听器读取失败")
	values := []elbv2types.LoadBalancer{
		{LoadBalancerArn: awssdk.String("arn:alb:first"), Type: elbv2types.LoadBalancerTypeEnumApplication},
		{LoadBalancerArn: awssdk.String("arn:alb:second"), Type: elbv2types.LoadBalancerTypeEnumApplication},
		{LoadBalancerArn: awssdk.String("arn:nlb:first"), Type: elbv2types.LoadBalancerTypeEnumNetwork},
		{LoadBalancerArn: awssdk.String("arn:gwlb:first"), Type: elbv2types.LoadBalancerTypeEnumGateway},
	}
	client := &elbV2Stub{
		listenerPages: map[string][]*elasticloadbalancingv2.DescribeListenersOutput{
			"arn:alb:first":  {{}},
			"arn:nlb:first":  {{}},
			"arn:gwlb:first": {{}},
		},
		listenerErrors: map[string][]error{"arn:alb:second": {albError}},
	}

	results, err := collectELBV2ByType(context.Background(), client, values, "cn-north-1")
	if err != nil || len(results) != 3 {
		t.Fatalf("监听器单类失败不应升级为整体失败：results=%+v err=%v", results, err)
	}
	if !errors.Is(results[0].Err, albError) || results[0].Snapshots != nil {
		t.Fatalf("ALB 失败必须返回原错误并丢弃该类型全部快照：%+v", results[0])
	}
	if results[1].Err != nil || len(results[1].Snapshots) != 1 || results[2].Err != nil || len(results[2].Snapshots) != 1 {
		t.Fatalf("NLB 与 GWLB 必须继续成功：%+v", results)
	}
}

// TestCollectELBV2RejectsUnknownTypeWithoutWritingSnapshots 防止未知官方值被错误归组，或在拒绝前读取监听器。
func TestCollectELBV2RejectsUnknownTypeWithoutWritingSnapshots(t *testing.T) {
	client := &elbV2Stub{listenerPages: map[string][]*elasticloadbalancingv2.DescribeListenersOutput{
		"arn:alb:first": {{}},
	}}
	values := []elbv2types.LoadBalancer{
		{LoadBalancerArn: awssdk.String("arn:alb:first"), Type: elbv2types.LoadBalancerTypeEnumApplication},
		{LoadBalancerArn: awssdk.String("arn:future:first"), Type: elbv2types.LoadBalancerTypeEnum("future")},
	}

	results, err := collectELBV2ByType(context.Background(), client, values, "cn-north-1")
	if err != nil || len(results) != 3 {
		t.Fatalf("未知类型应转换为三个类型级失败：results=%+v err=%v", results, err)
	}
	for index, resourceType := range []string{"alb", "nlb", "gwlb"} {
		if results[index].ResourceType != resourceType || results[index].Err == nil || results[index].Snapshots != nil {
			t.Fatalf("未知类型必须让 %s 无快照失败：%+v", resourceType, results[index])
		}
	}
	if len(client.listenerInputs) != 0 {
		t.Fatalf("全部列表类型验证通过前不得读取监听器：%+v", client.listenerInputs)
	}
}

// TestCollectELBV2ReadsAllListenerPages 防止监听器只读取首页，并验证跨页端点去重不合并不同协议。
func TestCollectELBV2ReadsAllListenerPages(t *testing.T) {
	client := &elbV2Stub{listenerPages: map[string][]*elasticloadbalancingv2.DescribeListenersOutput{
		"arn:alb:paged": {
			{Listeners: []elbv2types.Listener{{Port: awssdk.Int32(8443), Protocol: elbv2types.ProtocolEnumHttps}}, NextMarker: awssdk.String("listeners-2")},
			{Listeners: []elbv2types.Listener{
				{Port: awssdk.Int32(8443), Protocol: elbv2types.ProtocolEnumHttps},
				{Port: awssdk.Int32(8443), Protocol: elbv2types.ProtocolEnumHttp},
			}},
		},
	}}
	values := []elbv2types.LoadBalancer{{LoadBalancerArn: awssdk.String("arn:alb:paged"), DNSName: awssdk.String("paged.example.invalid"), Scheme: elbv2types.LoadBalancerSchemeEnumInternetFacing, Type: elbv2types.LoadBalancerTypeEnumApplication}}

	results, err := collectELBV2ByType(context.Background(), client, values, "cn-north-1")
	if err != nil || results[0].Err != nil || len(results[0].Snapshots) != 1 {
		t.Fatalf("监听器全分页采集失败：results=%+v err=%v", results, err)
	}
	wantEndpoints := []resource.EndpointSnapshot{
		{Kind: "public", Address: "paged.example.invalid", Port: 8443, Protocol: "https"},
		{Kind: "public", Address: "paged.example.invalid", Port: 8443, Protocol: "http"},
	}
	if !reflect.DeepEqual(results[0].Snapshots[0].Endpoints, wantEndpoints) {
		t.Fatalf("监听器所有页面必须转换为完整端点：%+v", results[0].Snapshots[0].Endpoints)
	}
	inputs := client.listenerInputs["arn:alb:paged"]
	if len(inputs) != 2 || awssdk.ToInt32(inputs[0].PageSize) != 400 || awssdk.ToString(inputs[0].Marker) != "" || awssdk.ToInt32(inputs[1].PageSize) != 400 || awssdk.ToString(inputs[1].Marker) != "listeners-2" {
		t.Fatalf("监听器必须以 PageSize=400 沿 NextMarker 分页：%+v", inputs)
	}
}

// TestCollectELBV2RejectsBrokenListenerPaginationPerType 防止异常监听器分页留下当前类型局部快照或阻断空分组。
func TestCollectELBV2RejectsBrokenListenerPaginationPerType(t *testing.T) {
	listenerError := errors.New("监听器第二页读取失败")
	tests := []struct {
		name    string
		pages   []*elasticloadbalancingv2.DescribeListenersOutput
		errors  []error
		wantErr string
		isErr   error
	}{
		{name: "首页空响应", pages: []*elasticloadbalancingv2.DescribeListenersOutput{nil}, wantErr: "ELBv2 监听器分页响应为空"},
		{name: "后续页原始错误", pages: []*elasticloadbalancingv2.DescribeListenersOutput{{NextMarker: awssdk.String("listeners-2")}}, errors: []error{nil, listenerError}, isErr: listenerError},
		{name: "非相邻标记循环", pages: []*elasticloadbalancingv2.DescribeListenersOutput{
			{NextMarker: awssdk.String("listeners-2")}, {NextMarker: awssdk.String("listeners-3")}, {NextMarker: awssdk.String("listeners-2")},
		}, wantErr: "ELBv2 监听器分页标记重复"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &elbV2Stub{
				listenerPages:  map[string][]*elasticloadbalancingv2.DescribeListenersOutput{"arn:alb:broken": test.pages},
				listenerErrors: map[string][]error{"arn:alb:broken": test.errors},
			}
			values := []elbv2types.LoadBalancer{{LoadBalancerArn: awssdk.String("arn:alb:broken"), Type: elbv2types.LoadBalancerTypeEnumApplication}}

			results, err := collectELBV2ByType(context.Background(), client, values, "cn-north-1")
			if err != nil || len(results) != 3 || results[0].Err == nil || results[0].Snapshots != nil {
				t.Fatalf("异常监听器分页只能使 ALB 失败且无局部快照：results=%+v err=%v", results, err)
			}
			if results[1].Err != nil || len(results[1].Snapshots) != 0 || results[2].Err != nil || len(results[2].Snapshots) != 0 {
				t.Fatalf("空 NLB/GWLB 分组仍必须成功：%+v", results)
			}
			if test.wantErr != "" && results[0].Err.Error() != test.wantErr {
				t.Fatalf("监听器分页内部错误必须为固定中文：got=%v want=%s", results[0].Err, test.wantErr)
			}
			if test.isErr != nil && !errors.Is(results[0].Err, test.isErr) {
				t.Fatalf("监听器云端错误必须原样返回：got=%v want=%v", results[0].Err, test.isErr)
			}
		})
	}
}

// TestCollectELBV2KeepsDNSWithoutListenersAndSkipsEmptyDNS 防止为无监听器资源虚构协议，或为空地址创建端点。
func TestCollectELBV2KeepsDNSWithoutListenersAndSkipsEmptyDNS(t *testing.T) {
	client := &elbV2Stub{listenerPages: map[string][]*elasticloadbalancingv2.DescribeListenersOutput{
		"arn:alb:no-listener": {{}},
		"arn:alb:no-dns":      {{}},
	}}
	values := []elbv2types.LoadBalancer{
		{LoadBalancerArn: awssdk.String("arn:alb:no-listener"), DNSName: awssdk.String("bare.example.invalid"), Scheme: elbv2types.LoadBalancerSchemeEnumInternetFacing, Type: elbv2types.LoadBalancerTypeEnumApplication},
		{LoadBalancerArn: awssdk.String("arn:alb:no-dns"), Scheme: elbv2types.LoadBalancerSchemeEnumInternal, Type: elbv2types.LoadBalancerTypeEnumApplication},
	}

	results, err := collectELBV2ByType(context.Background(), client, values, "cn-north-1")
	if err != nil || results[0].Err != nil || len(results[0].Snapshots) != 2 {
		t.Fatalf("无监听器与空 DNS 快照采集失败：results=%+v err=%v", results, err)
	}
	if !reflect.DeepEqual(results[0].Snapshots[0].Endpoints, []resource.EndpointSnapshot{{Kind: "public", Address: "bare.example.invalid"}}) {
		t.Fatalf("无监听器仍须保留 DNS，且端口为 0、协议为空：%+v", results[0].Snapshots[0].Endpoints)
	}
	if len(results[0].Snapshots[1].Endpoints) != 0 {
		t.Fatalf("空 DNS 不得生成端点：%+v", results[0].Snapshots[1].Endpoints)
	}
}
