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
