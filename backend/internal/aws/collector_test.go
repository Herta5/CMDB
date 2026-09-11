// 本文件使用官方 SDK 响应类型验证 AWS 资源与地址转换，不访问真实账号。
package aws

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cmdb/internal/resource"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/smithy-go"
)

type ec2ProbeStub struct{ input *ec2.DescribeInstancesInput }

func (s *ec2ProbeStub) DescribeInstances(_ context.Context, input *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	s.input = input
	return &ec2.DescribeInstancesOutput{}, nil
}

type rdsProbeStub struct{ input *rds.DescribeDBInstancesInput }

func (s *rdsProbeStub) DescribeDBInstances(_ context.Context, input *rds.DescribeDBInstancesInput, _ ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
	s.input = input
	return &rds.DescribeDBInstancesOutput{}, nil
}

type elbProbeStub struct {
	input *elasticloadbalancingv2.DescribeLoadBalancersInput
}

func (s *elbProbeStub) DescribeLoadBalancers(_ context.Context, input *elasticloadbalancingv2.DescribeLoadBalancersInput, _ ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error) {
	s.input = input
	return &elasticloadbalancingv2.DescribeLoadBalancersOutput{}, nil
}

// TestEC2SnapshotsKeepNetworkInterfaceAddresses 验证 EC2 保存网卡上的全部内外网 IP。
func TestEC2SnapshotsKeepNetworkInterfaceAddresses(t *testing.T) {
	output := &ec2.DescribeInstancesOutput{Reservations: []types.Reservation{{Instances: []types.Instance{{
		InstanceId: awssdk.String("i-1"), PrivateIpAddress: awssdk.String("10.0.0.5"), PublicIpAddress: awssdk.String("203.0.113.5"),
		NetworkInterfaces: []types.InstanceNetworkInterface{{PrivateIpAddresses: []types.InstancePrivateIpAddress{{PrivateIpAddress: awssdk.String("10.0.0.6"), Association: &types.InstanceNetworkInterfaceAssociation{PublicIp: awssdk.String("203.0.113.6")}}}}},
	}}}}}
	values := ec2Snapshots(output, "cn-north-1")
	if len(values) != 1 || countEndpointKind(values[0].Endpoints, "private") != 2 || countEndpointKind(values[0].Endpoints, "public") != 2 {
		t.Fatal("EC2 必须保留实例和网卡的全部内外网 IP")
	}
}

// TestRDSAndELBSnapshotsKeepHostnamesAndPorts 验证动态地址保留域名端口而不伪装成固定 IP。
func TestRDSAndELBSnapshotsKeepHostnamesAndPorts(t *testing.T) {
	rdsValues := rdsSnapshots(&rds.DescribeDBInstancesOutput{DBInstances: []rdstypes.DBInstance{{DBInstanceIdentifier: awssdk.String("db-1"), Endpoint: &rdstypes.Endpoint{Address: awssdk.String("db.example.invalid"), Port: awssdk.Int32(3306)}}}}, "cn-north-1")
	elbValues := elbSnapshots(&elasticloadbalancingv2.DescribeLoadBalancersOutput{LoadBalancers: []elbtypes.LoadBalancer{{LoadBalancerArn: awssdk.String("arn:lb:1"), LoadBalancerName: awssdk.String("web"), DNSName: awssdk.String("lb.example.invalid"), Scheme: elbtypes.LoadBalancerSchemeEnumInternetFacing}}}, map[string][]elbtypes.Listener{"arn:lb:1": {{Port: awssdk.Int32(443), Protocol: elbtypes.ProtocolEnumHttps}}}, "cn-north-1")
	if rdsValues[0].Endpoints[0].Address != "db.example.invalid" || rdsValues[0].Endpoints[0].Port != 3306 {
		t.Fatal("RDS 必须保存原始域名和端口")
	}
	if elbValues[0].Endpoints[0].Kind != "public" || elbValues[0].Endpoints[0].Address != "lb.example.invalid" || elbValues[0].Endpoints[0].Port != 443 || elbValues[0].Endpoints[0].Protocol != "https" {
		t.Fatal("公网 ELB 必须保存公网域名及监听端口")
	}
}

// TestRDSSnapshotMarksLatestRestorableTimeVolatile 验证持续推进的恢复时间不会被当成 RDS 配置变化，原始值仍完整保留。
func TestRDSSnapshotMarksLatestRestorableTimeVolatile(t *testing.T) {
	latest := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	values := rdsSnapshots(&rds.DescribeDBInstancesOutput{DBInstances: []rdstypes.DBInstance{{
		DBInstanceIdentifier: awssdk.String("db-volatile"),
		LatestRestorableTime: &latest,
	}}}, "cn-north-1")
	if len(values) != 1 {
		t.Fatalf("AWS RDS 快照数量错误：got=%d want=1", len(values))
	}
	if len(values[0].VolatileRawAttributeKeys) != 1 || values[0].VolatileRawAttributeKeys[0] != "LatestRestorableTime" {
		t.Fatalf("AWS RDS 必须只标记持续推进的恢复时间为易变属性：%v", values[0].VolatileRawAttributeKeys)
	}
	var raw map[string]any
	if err := json.Unmarshal(values[0].RawAttributes, &raw); err != nil || raw["LatestRestorableTime"] == nil {
		t.Fatalf("易变字段仍须保留在 RDS 原始属性中：preserved=%t err=%v", raw["LatestRestorableTime"] != nil, err)
	}
}

// TestAWSConnectionProbeRequestsOnePagePerType 验证连接测试只请求三类 API 的最小页面，不遍历实例和监听器。
func TestAWSConnectionProbeRequestsOnePagePerType(t *testing.T) {
	ec2Client, rdsClient, elbClient := &ec2ProbeStub{}, &rdsProbeStub{}, &elbProbeStub{}
	results, err := probeAWSAccess(context.Background(), ec2Client, rdsClient, elbClient)
	if err != nil || len(results) != 3 || results[0].ResourceType != "ec2" || results[1].ResourceType != "rds" || results[2].ResourceType != "elb" {
		t.Fatalf("AWS 连接探测必须返回三类资源结果：%v，错误：%v", results, err)
	}
	if ec2Client.input == nil || awssdk.ToInt32(ec2Client.input.MaxResults) != 5 || rdsClient.input == nil || awssdk.ToInt32(rdsClient.input.MaxRecords) != 20 || elbClient.input == nil || awssdk.ToInt32(elbClient.input.PageSize) != 1 {
		t.Fatal("AWS 连接探测必须使用各 API 允许的最小分页，且不得执行完整采集")
	}
}

// TestAWSAccessErrorClassification 防止权限不足被误报为 AccessKey 无效。
func TestAWSAccessErrorClassification(t *testing.T) {
	if !errors.Is(classifyAWSAccessError(&smithy.GenericAPIError{Code: "AccessDeniedException", Message: "denied"}), resource.ErrPermissionDenied) {
		t.Fatal("AWS AccessDenied 响应必须归类为 IAM 权限不足")
	}
	if !errors.Is(classifyAWSAccessError(&smithy.GenericAPIError{Code: "InvalidClientTokenId", Message: "invalid"}), resource.ErrAuthenticationFailed) {
		t.Fatal("AWS 无效凭证必须归类为认证失败")
	}
}

func countEndpointKind(values []resource.EndpointSnapshot, kind string) int {
	count := 0
	for _, value := range values {
		if value.Kind == kind {
			count++
		}
	}
	return count
}
