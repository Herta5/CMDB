// 本文件使用阿里云官方 SDK 响应类型验证地址转换，不访问真实账号。
package aliyun

import (
	"errors"
	"testing"

	"cmdb/internal/resource"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/rds"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/slb"
)

// TestECSSnapshotsKeepPrivateAndPublicAddresses 验证 ECS 保存实例返回的全部内外网 IP。
func TestECSSnapshotsKeepPrivateAndPublicAddresses(t *testing.T) {
	instance := ecs.Instance{InstanceId: "i-1", InstanceName: "计算节点", RegionId: "cn-hangzhou", InnerIpAddress: ecs.InnerIpAddressInDescribeInstances{IpAddress: []string{"10.0.0.5"}}, PublicIpAddress: ecs.PublicIpAddressInDescribeInstances{IpAddress: []string{"203.0.113.5"}}, VpcAttributes: ecs.VpcAttributes{PrivateIpAddress: ecs.PrivateIpAddressInDescribeInstanceAttribute{IpAddress: []string{"10.0.0.6"}}}}
	values := ecsSnapshots([]ecs.Instance{instance})
	if countKind(values[0].Endpoints, "private") != 2 || countKind(values[0].Endpoints, "public") != 1 {
		t.Fatal("ECS 必须保留全部内外网 IP")
	}
}

// TestAliyunAccessErrorClassification 验证安全分类不会把权限不足误报为 AccessKey 无效。
func TestAliyunAccessErrorClassification(t *testing.T) {
	if !errors.Is(classifyAliyunAccessError(errors.New("Forbidden.RAM: User not authorized")), resource.ErrPermissionDenied) {
		t.Fatal("阿里云 Forbidden 响应必须归类为 RAM 权限不足")
	}
	if !errors.Is(classifyAliyunAccessError(errors.New("InvalidAccessKeyId.NotFound")), resource.ErrAuthenticationFailed) {
		t.Fatal("无效 AccessKey 必须归类为认证失败")
	}
}

// TestRDSAndSLBSnapshotsKeepEndpoints 验证 RDS 域名端口与负载均衡地址正确区分内外网。
func TestRDSAndSLBSnapshotsKeepEndpoints(t *testing.T) {
	database := rds.DBInstance{DBInstanceId: "rm-1", DBInstanceDescription: "订单库", RegionId: "cn-hangzhou"}
	networks := map[string][]rds.DBInstanceNetInfo{"rm-1": {{ConnectionString: "db.example.invalid", Port: "3306", IPType: "Public"}}}
	loadBalancer := slb.LoadBalancer{LoadBalancerId: "lb-1", LoadBalancerName: "入口", Address: "198.51.100.8", AddressType: "internet", RegionId: "cn-hangzhou"}
	if rdsSnapshots([]rds.DBInstance{database}, networks)[0].Endpoints[0].Port != 3306 {
		t.Fatal("RDS 必须保存域名端口")
	}
	if slbSnapshots([]slb.LoadBalancer{loadBalancer})[0].Endpoints[0].Kind != "public" {
		t.Fatal("公网负载均衡必须标记公网端点")
	}
}

func countKind(values []resource.EndpointSnapshot, kind string) int {
	count := 0
	for _, value := range values {
		if value.Kind == kind {
			count++
		}
	}
	return count
}
