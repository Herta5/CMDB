// 本文件使用官方 SDK 响应类型验证 AWS 资源与地址转换，不访问真实账号。
package aws

import (
	"testing"

	"cmdb/internal/resource"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

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

func countEndpointKind(values []resource.EndpointSnapshot, kind string) int {
	count := 0
	for _, value := range values {
		if value.Kind == kind {
			count++
		}
	}
	return count
}
