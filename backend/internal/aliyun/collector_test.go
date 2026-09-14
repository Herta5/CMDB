// 本文件使用阿里云官方 SDK 响应类型验证地址转换，不访问真实账号。
package aliyun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"cmdb/internal/resource"
	aliyunresponses "github.com/aliyun/alibaba-cloud-sdk-go/sdk/responses"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/rds"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/slb"
)

// TestNewCollectorImplementsProviderAdapter 防止阿里云构造器返回的采集器失去资源核心要求的平台适配器能力。
func TestNewCollectorImplementsProviderAdapter(t *testing.T) {
	var adapter resource.ProviderAdapter = NewCollector()
	if adapter == nil {
		t.Fatal("阿里云构造器必须返回可接入资源核心的平台适配器")
	}
}

type ecsProbeStub struct {
	request     *ecs.DescribeInstancesRequest
	diskRequest *ecs.DescribeDisksRequest
}

func (s *ecsProbeStub) DescribeInstances(request *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error) {
	s.request = request
	return ecs.CreateDescribeInstancesResponse(), nil
}

func (s *ecsProbeStub) DescribeDisks(request *ecs.DescribeDisksRequest) (*ecs.DescribeDisksResponse, error) {
	s.diskRequest = request
	return ecs.CreateDescribeDisksResponse(), nil
}

type ecsCollectStub struct {
	instanceRequest *ecs.DescribeInstancesRequest
	diskRequest     *ecs.DescribeDisksRequest
	omitHardware    bool
	disks           []ecs.Disk
}

func (s *ecsCollectStub) DescribeInstances(request *ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error) {
	s.instanceRequest = request
	response := ecs.CreateDescribeInstancesResponse()
	response.PageSize, response.TotalCount = 100, 1
	instance := ecs.Instance{InstanceId: "i-1", InstanceType: "ecs.c6.large", Cpu: 2, Memory: 4096}
	if s.omitHardware {
		instance.InstanceType, instance.Cpu, instance.Memory = "", 0, 0
	}
	response.Instances.Instance = []ecs.Instance{instance}
	return response, nil
}

func (s *ecsCollectStub) DescribeDisks(request *ecs.DescribeDisksRequest) (*ecs.DescribeDisksResponse, error) {
	s.diskRequest = request
	response := ecs.CreateDescribeDisksResponse()
	disks := s.disks
	if disks == nil {
		disks = []ecs.Disk{{DiskId: "d-1", InstanceId: "i-1", Type: "system", Category: "cloud_essd", Size: 40, Device: "/dev/xvda", Status: "In_use"}}
	}
	response.PageSize, response.TotalCount = 100, len(disks)
	response.Disks.Disk = disks
	return response, nil
}

type rdsProbeStub struct {
	request          *rds.DescribeDBInstancesRequest
	attributeRequest *rds.DescribeDBInstanceAttributeRequest
}

func (s *rdsProbeStub) DescribeDBInstances(request *rds.DescribeDBInstancesRequest) (*rds.DescribeDBInstancesResponse, error) {
	s.request = request
	response := rds.CreateDescribeDBInstancesResponse()
	response.Items.DBInstance = []rds.DBInstance{{DBInstanceId: "rm-probe"}}
	return response, nil
}

func (s *rdsProbeStub) DescribeDBInstanceAttribute(request *rds.DescribeDBInstanceAttributeRequest) (*rds.DescribeDBInstanceAttributeResponse, error) {
	s.attributeRequest = request
	return rds.CreateDescribeDBInstanceAttributeResponse(), nil
}

type rdsCollectStub struct {
	instanceRequest  *rds.DescribeDBInstancesRequest
	networkRequest   *rds.DescribeDBInstanceNetInfoRequest
	attributeRequest *rds.DescribeDBInstanceAttributeRequest
	omitAttribute    bool
}

func (s *rdsCollectStub) DescribeDBInstances(request *rds.DescribeDBInstancesRequest) (*rds.DescribeDBInstancesResponse, error) {
	s.instanceRequest = request
	response := rds.CreateDescribeDBInstancesResponse()
	response.PageRecordCount, response.TotalRecordCount = 1, 1
	response.Items.DBInstance = []rds.DBInstance{{DBInstanceId: "rm-detail"}}
	return response, nil
}

func (s *rdsCollectStub) DescribeDBInstanceNetInfo(request *rds.DescribeDBInstanceNetInfoRequest) (*rds.DescribeDBInstanceNetInfoResponse, error) {
	s.networkRequest = request
	return rds.CreateDescribeDBInstanceNetInfoResponse(), nil
}

func (s *rdsCollectStub) DescribeDBInstanceAttribute(request *rds.DescribeDBInstanceAttributeRequest) (*rds.DescribeDBInstanceAttributeResponse, error) {
	s.attributeRequest = request
	response := rds.CreateDescribeDBInstanceAttributeResponse()
	if !s.omitAttribute {
		response.Items.DBInstanceAttribute = []rds.DBInstanceAttribute{{DBInstanceId: "rm-detail", DBInstanceClass: "rds.mysql.s2.large", DBInstanceStorage: 200}}
	}
	return response, nil
}

type slbProbeStub struct {
	request *slb.DescribeLoadBalancersRequest
}

func (s *slbProbeStub) DescribeLoadBalancers(request *slb.DescribeLoadBalancersRequest) (*slb.DescribeLoadBalancersResponse, error) {
	s.request = request
	return slb.CreateDescribeLoadBalancersResponse(), nil
}

// TestECSSnapshotsKeepPrivateAndPublicAddresses 验证 ECS 保存实例返回的全部内外网 IP。
func TestECSSnapshotsKeepPrivateAndPublicAddresses(t *testing.T) {
	instance := ecs.Instance{InstanceId: "i-1", InstanceName: "计算节点", RegionId: "cn-hangzhou", InnerIpAddress: ecs.InnerIpAddressInDescribeInstances{IpAddress: []string{"10.0.0.5"}}, PublicIpAddress: ecs.PublicIpAddressInDescribeInstances{IpAddress: []string{"203.0.113.5"}}, VpcAttributes: ecs.VpcAttributes{PrivateIpAddress: ecs.PrivateIpAddressInDescribeInstanceAttribute{IpAddress: []string{"10.0.0.6"}}}}
	values := ecsSnapshots([]ecs.Instance{instance}, nil)
	if countKind(values[0].Endpoints, "private") != 2 || countKind(values[0].Endpoints, "public") != 1 {
		t.Fatal("ECS 必须保留全部内外网 IP")
	}
}

// TestECSSnapshotsKeepHardwareAndSortedCloudDisks 防止 ECS 规格丢失或云盘返回顺序制造虚假更新。
func TestECSSnapshotsKeepHardwareAndSortedCloudDisks(t *testing.T) {
	instance := ecs.Instance{InstanceId: "i-hardware", InstanceType: "ecs.c6.large", Cpu: 2, Memory: 4096}
	disks := map[string][]ecs.Disk{"i-hardware": {
		{DiskId: "d-root", InstanceId: "i-hardware", Type: "system", Category: "cloud_essd", Size: 40, Device: "/dev/xvda", Encrypted: true},
		{DiskId: "d-data", InstanceId: "i-hardware", Type: "data", Category: "cloud_essd", Size: 100, Device: "/dev/xvdb"},
		{DiskId: "d-data", InstanceId: "i-hardware", Type: "data", Category: "cloud_essd", Size: 100, Device: "/dev/xvdb"},
	}}

	values := ecsSnapshots([]ecs.Instance{instance}, disks)
	wantDisks := []resource.ServerDisk{
		{ID: "d-data", Kind: "data", Type: "cloud_essd", SizeGiB: 100, Device: "/dev/xvdb"},
		{ID: "d-root", Kind: "system", Type: "cloud_essd", SizeGiB: 40, Device: "/dev/xvda", Encrypted: true},
	}
	if len(values) != 1 || values[0].InstanceType != "ecs.c6.large" || values[0].VCPU != 2 || values[0].Memory != 4096 || !reflect.DeepEqual(values[0].Disks, wantDisks) {
		t.Fatalf("ECS 规格和云盘明细必须完整且顺序稳定：%+v", values)
	}
}

// TestCollectECSLoadsAttachedCloudDisks 防止完整同步只读实例列表而漏采已挂载云盘。
func TestCollectECSLoadsAttachedCloudDisks(t *testing.T) {
	client := &ecsCollectStub{}
	instances, disks, err := collectECS(client, "cn-hangzhou")
	if err != nil || len(instances) != 1 || len(disks["i-1"]) != 1 {
		t.Fatalf("ECS 完整采集必须同时返回实例和已挂载云盘：instances=%v disks=%v err=%v", instances, disks, err)
	}
	if client.instanceRequest == nil || client.diskRequest == nil || client.diskRequest.RegionId != "cn-hangzhou" || client.diskRequest.DiskType != "all" || client.diskRequest.Status != "In_use" {
		t.Fatalf("ECS 云盘采集必须限定当前地域和已挂载状态，并使用合法的用途筛选值：%+v", client.diskRequest)
	}
}

// TestCollectECSRejectsMissingHardwareDetails 防止实例类型、CPU 或内存缺失时仍写入 ECS 快照。
func TestCollectECSRejectsMissingHardwareDetails(t *testing.T) {
	client := &ecsCollectStub{omitHardware: true}
	if _, _, err := collectECS(client, "cn-hangzhou"); err == nil {
		t.Fatal("ECS 实例规格不完整时必须使该资源类型采集失败")
	}
}

// TestCollectECSKeepsOnlyAttachedCloudDisks 防止接口异常返回未挂载盘或本地盘时污染服务器磁盘明细。
func TestCollectECSKeepsOnlyAttachedCloudDisks(t *testing.T) {
	client := &ecsCollectStub{disks: []ecs.Disk{
		{DiskId: "d-cloud", InstanceId: "i-1", Type: "system", Category: "cloud_essd", Size: 40, Device: "/dev/xvda", Status: "In_use"},
		{DiskId: "d-detached", InstanceId: "i-1", Type: "data", Category: "cloud_essd", Size: 100, Device: "/dev/xvdb", Status: "Available"},
		{DiskId: "d-local", InstanceId: "i-1", Type: "data", Category: "local_ssd_pro", Size: 100, Device: "/dev/xvdc", Status: "In_use"},
	}}
	_, disks, err := collectECS(client, "cn-hangzhou")
	if err != nil || len(disks["i-1"]) != 1 || disks["i-1"][0].DiskId != "d-cloud" {
		t.Fatalf("ECS 只能保存已挂载云盘：disks=%+v err=%v", disks, err)
	}
}

// TestCollectECSRejectsIncompleteAttachedCloudDisk 防止不完整云盘被静默丢弃并以空明细覆盖旧快照。
func TestCollectECSRejectsIncompleteAttachedCloudDisk(t *testing.T) {
	valid := ecs.Disk{DiskId: "d-1", InstanceId: "i-1", Type: "data", Category: "cloud_essd", Size: 100, Device: "/dev/xvdb", Status: "In_use"}
	tests := []struct {
		name string
		disk ecs.Disk
	}{
		{name: "缺少磁盘 ID", disk: func() ecs.Disk { value := valid; value.DiskId = ""; return value }()},
		{name: "缺少实例 ID", disk: func() ecs.Disk { value := valid; value.InstanceId = ""; return value }()},
		{name: "磁盘用途非法", disk: func() ecs.Disk { value := valid; value.Type = ""; return value }()},
		{name: "缺少磁盘类型", disk: func() ecs.Disk { value := valid; value.Category = "cloud_"; return value }()},
		{name: "容量无效", disk: func() ecs.Disk { value := valid; value.Size = 0; return value }()},
		{name: "缺少设备名", disk: func() ecs.Disk { value := valid; value.Device = ""; return value }()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &ecsCollectStub{disks: []ecs.Disk{test.disk}}
			if _, _, err := collectECS(client, "cn-hangzhou"); err == nil {
				t.Fatal("已挂载云盘的必要字段不完整时必须使 ECS 类型失败")
			}
		})
	}
}

// TestValidateAliyunDiskEncryptionPresence 区分明确未加密与云响应缺少加密状态。
func TestValidateAliyunDiskEncryptionPresence(t *testing.T) {
	parse := func(body string) *ecs.DescribeDisksResponse {
		response := ecs.CreateDescribeDisksResponse()
		err := aliyunresponses.Unmarshal(response, &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, "JSON")
		if err != nil {
			t.Fatalf("准备阿里云响应失败：%v", err)
		}
		return response
	}
	withFalse := parse(`{"Disks":{"Disk":[{"DiskId":"d-1","Category":"cloud_essd","Status":"In_use","Encrypted":false}]}}`)
	if err := validateAliyunDiskEncryptionPresence(withFalse); err != nil {
		t.Fatalf("明确返回 false 必须视为有效未加密状态：%v", err)
	}
	withoutField := parse(`{"Disks":{"Disk":[{"DiskId":"d-1","Category":"cloud_essd","Status":"In_use"}]}}`)
	if err := validateAliyunDiskEncryptionPresence(withoutField); err == nil {
		t.Fatal("阿里云云盘响应缺少加密状态时必须使 ECS 类型失败")
	}
	excludedLocal := parse(`{"Disks":{"Disk":[{"DiskId":"local-1","Category":"local_ssd_pro","Status":"In_use"}]}}`)
	if err := validateAliyunDiskEncryptionPresence(excludedLocal); err != nil {
		t.Fatalf("不纳入明细的临时本地盘不得因缺少加密字段拖垮 ECS 类型：%v", err)
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

// TestAliyunConnectionProbeRequestsOnlyOneItemPerType 验证连接测试只探测六类 API，不遍历资源详情或域名。
func TestAliyunConnectionProbeRequestsOnlyOneItemPerType(t *testing.T) {
	ecsClient, rdsClient, slbClient := &ecsProbeStub{}, &rdsProbeStub{}, &slbProbeStub{}
	results, err := probeAliyunAccess(context.Background(), ecsClient, rdsClient, slbClient, &albCollectStub{}, &nlbCollectStub{}, &gwlbCollectStub{}, "cn-hangzhou")
	if err != nil || len(results) != 6 || results[0].ResourceType != "ecs" || results[1].ResourceType != "rds" || results[2].ResourceType != "slb" || results[3].ResourceType != "alb" || results[4].ResourceType != "nlb" || results[5].ResourceType != "gwlb" {
		t.Fatalf("阿里云连接探测必须返回六类资源结果：%v，错误：%v", results, err)
	}
	if ecsClient.request == nil || string(ecsClient.request.PageSize) != "1" || rdsClient.request == nil || string(rdsClient.request.PageSize) != "1" || slbClient.request == nil || string(slbClient.request.PageSize) != "1" {
		t.Fatal("阿里云连接探测每类资源只能请求一条数据")
	}
}

// TestAliyunConnectionProbeChecksRDSAttributePermission 防止连接测试放过缺少 RDS 规格详情读取权限的接入源。
func TestAliyunConnectionProbeChecksRDSAttributePermission(t *testing.T) {
	ecsClient, rdsClient, slbClient := &ecsProbeStub{}, &rdsProbeStub{}, &slbProbeStub{}
	if _, err := probeAliyunAccess(context.Background(), ecsClient, rdsClient, slbClient, &albCollectStub{}, &nlbCollectStub{}, &gwlbCollectStub{}, "cn-hangzhou"); err != nil {
		t.Fatalf("阿里云连接探测失败：%v", err)
	}
	if rdsClient.attributeRequest == nil || rdsClient.attributeRequest.DBInstanceId != "rm-probe" {
		t.Fatalf("连接测试必须以首个 RDS 实例验证详情权限：%+v", rdsClient.attributeRequest)
	}
}

// TestAliyunConnectionProbeChecksCloudDiskPermission 防止连接测试放过缺少 DescribeDisks 权限的接入源。
func TestAliyunConnectionProbeChecksCloudDiskPermission(t *testing.T) {
	ecsClient, rdsClient, slbClient := &ecsProbeStub{}, &rdsProbeStub{}, &slbProbeStub{}
	if _, err := probeAliyunAccess(context.Background(), ecsClient, rdsClient, slbClient, &albCollectStub{}, &nlbCollectStub{}, &gwlbCollectStub{}, "cn-hangzhou"); err != nil {
		t.Fatalf("阿里云连接探测失败：%v", err)
	}
	if ecsClient.diskRequest == nil || string(ecsClient.diskRequest.PageSize) != "1" || ecsClient.diskRequest.DiskType != "all" || ecsClient.diskRequest.Status != "In_use" {
		t.Fatalf("连接测试必须以最小请求验证云盘读取权限：%+v", ecsClient.diskRequest)
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

// TestRDSSnapshotsKeepSpecification 验证阿里云 RDS 详情中的计算和存储规格进入统一快照。
func TestRDSSnapshotsKeepSpecification(t *testing.T) {
	instance := rds.DBInstance{DBInstanceId: "rm-specification"}
	attributes := map[string]rds.DBInstanceAttribute{
		"rm-specification": {
			DBInstanceClass:       "rds.mysql.s2.large",
			DBInstanceCPU:         "4",
			DBInstanceMemory:      8192,
			DBInstanceStorageType: "cloud_essd",
			DBInstanceStorage:     200,
		},
	}
	values := rdsSnapshots([]rds.DBInstance{instance}, nil, attributes)
	if len(values) != 1 || values[0].InstanceType != "rds.mysql.s2.large" || values[0].VCPU != 4 || values[0].Memory != 8192 || values[0].StorageType != "cloud_essd" || values[0].StorageSizeGiB != 200 {
		t.Fatalf("阿里云 RDS 规格转换错误：%+v", values)
	}
}

// TestCollectRDSLoadsInstanceAttributes 防止完整同步只读列表和网络信息而漏掉存储容量等详情字段。
func TestCollectRDSLoadsInstanceAttributes(t *testing.T) {
	client := &rdsCollectStub{}
	instances, _, attributes, err := collectRDS(client)
	if err != nil || len(instances) != 1 || attributes["rm-detail"].DBInstanceStorage != 200 {
		t.Fatalf("阿里云 RDS 完整采集必须返回实例详情：instances=%v attributes=%v err=%v", instances, attributes, err)
	}
	if client.attributeRequest == nil || client.attributeRequest.DBInstanceId != "rm-detail" {
		t.Fatalf("RDS 详情请求必须使用当前实例标识：%+v", client.attributeRequest)
	}
}

// TestCollectRDSRejectsMissingInstanceAttributes 防止详情缺失时用空规格覆盖最近一次有效 RDS 快照。
func TestCollectRDSRejectsMissingInstanceAttributes(t *testing.T) {
	if _, _, _, err := collectRDS(&rdsCollectStub{omitAttribute: true}); err == nil {
		t.Fatal("阿里云 RDS 详情缺失时必须使该资源类型采集失败")
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
