// 本文件使用官方 SDK 响应类型验证 AWS 资源与地址转换，不访问真实账号。
package aws

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
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

// TestNewCollectorImplementsProviderAdapter 防止 AWS 构造器返回的采集器失去资源核心要求的平台适配器能力。
func TestNewCollectorImplementsProviderAdapter(t *testing.T) {
	var adapter resource.ProviderAdapter = NewCollector()
	if adapter == nil {
		t.Fatal("AWS 采集器必须实现平台适配器")
	}
}

type ec2ProbeStub struct {
	input             *ec2.DescribeInstancesInput
	instanceTypeInput *ec2.DescribeInstanceTypesInput
	volumeInput       *ec2.DescribeVolumesInput
	instanceErr       error
	instanceTypeErr   error
}

func (s *ec2ProbeStub) DescribeInstances(_ context.Context, input *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	s.input = input
	return &ec2.DescribeInstancesOutput{}, s.instanceErr
}

func (s *ec2ProbeStub) DescribeInstanceTypes(_ context.Context, input *ec2.DescribeInstanceTypesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error) {
	s.instanceTypeInput = input
	return &ec2.DescribeInstanceTypesOutput{}, s.instanceTypeErr
}

func (s *ec2ProbeStub) DescribeVolumes(_ context.Context, input *ec2.DescribeVolumesInput, _ ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
	s.volumeInput = input
	return &ec2.DescribeVolumesOutput{}, nil
}

type ec2CollectStub struct {
	instanceInput     *ec2.DescribeInstancesInput
	instanceTypeInput *ec2.DescribeInstanceTypesInput
	volumeInput       *ec2.DescribeVolumesInput
	omitInstanceType  bool
	emptyInstanceType bool
	emptyRootDevice   bool
	volumes           []types.Volume
}

func (s *ec2CollectStub) DescribeInstances(_ context.Context, input *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	s.instanceInput = input
	instanceType := types.InstanceType("c6a.xlarge")
	if s.emptyInstanceType {
		instanceType = ""
	}
	rootDeviceName := awssdk.String("/dev/sda1")
	if s.emptyRootDevice {
		rootDeviceName = nil
	}
	return &ec2.DescribeInstancesOutput{Reservations: []types.Reservation{{Instances: []types.Instance{{InstanceId: awssdk.String("i-1"), InstanceType: instanceType, RootDeviceName: rootDeviceName}}}}}, nil
}

func (s *ec2CollectStub) DescribeInstanceTypes(_ context.Context, input *ec2.DescribeInstanceTypesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error) {
	s.instanceTypeInput = input
	if s.omitInstanceType {
		return &ec2.DescribeInstanceTypesOutput{}, nil
	}
	return &ec2.DescribeInstanceTypesOutput{InstanceTypes: []types.InstanceTypeInfo{{
		InstanceType: types.InstanceType("c6a.xlarge"),
		VCpuInfo:     &types.VCpuInfo{DefaultVCpus: awssdk.Int32(4)},
		MemoryInfo:   &types.MemoryInfo{SizeInMiB: awssdk.Int64(8192)},
	}}}, nil
}

func (s *ec2CollectStub) DescribeVolumes(_ context.Context, input *ec2.DescribeVolumesInput, _ ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
	s.volumeInput = input
	volumes := s.volumes
	if volumes == nil {
		volumes = []types.Volume{{VolumeId: awssdk.String("vol-1"), VolumeType: types.VolumeTypeGp3, Size: awssdk.Int32(40), Encrypted: awssdk.Bool(false), Attachments: []types.VolumeAttachment{{InstanceId: awssdk.String("i-1"), Device: awssdk.String("/dev/sda1"), State: types.VolumeAttachmentStateAttached}}}}
	}
	return &ec2.DescribeVolumesOutput{Volumes: volumes}, nil
}

type rdsProbeStub struct{ input *rds.DescribeDBInstancesInput }

func (s *rdsProbeStub) DescribeDBInstances(_ context.Context, input *rds.DescribeDBInstancesInput, _ ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
	s.input = input
	return &rds.DescribeDBInstancesOutput{}, nil
}

type rdsCollectStub struct {
	input *rds.DescribeDBInstancesInput
}

func (s *rdsCollectStub) DescribeDBInstances(_ context.Context, input *rds.DescribeDBInstancesInput, _ ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
	s.input = input
	return &rds.DescribeDBInstancesOutput{DBInstances: []rdstypes.DBInstance{{DBInstanceIdentifier: awssdk.String("db-detail"), DBInstanceClass: awssdk.String("db.r6g.large")}}}, nil
}

type rdsInstanceTypeStub struct {
	input      *ec2.DescribeInstanceTypesInput
	omitVCPU   bool
	omitMemory bool
}

func (s *rdsInstanceTypeStub) DescribeInstanceTypes(_ context.Context, input *ec2.DescribeInstanceTypesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error) {
	s.input = input
	info := types.InstanceTypeInfo{
		InstanceType: types.InstanceType("r6g.large"),
		VCpuInfo:     &types.VCpuInfo{DefaultVCpus: awssdk.Int32(2)},
		MemoryInfo:   &types.MemoryInfo{SizeInMiB: awssdk.Int64(16384)},
	}
	if s.omitVCPU {
		info.VCpuInfo = nil
	}
	if s.omitMemory {
		info.MemoryInfo = nil
	}
	return &ec2.DescribeInstanceTypesOutput{InstanceTypes: []types.InstanceTypeInfo{info}}, nil
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
	values := ec2Snapshots(output, "cn-north-1", nil, nil)
	if len(values) != 1 || countEndpointKind(values[0].Endpoints, "private") != 2 || countEndpointKind(values[0].Endpoints, "public") != 2 {
		t.Fatal("EC2 必须保留实例和网卡的全部内外网 IP")
	}
}

// TestEC2SnapshotsKeepHardwareAndSortedEBSVolumes 防止 EC2 规格丢失、根卷误判或 EBS 顺序制造虚假更新。
func TestEC2SnapshotsKeepHardwareAndSortedEBSVolumes(t *testing.T) {
	instanceType := types.InstanceType("c6a.xlarge")
	output := &ec2.DescribeInstancesOutput{Reservations: []types.Reservation{{Instances: []types.Instance{{
		InstanceId: awssdk.String("i-hardware"), InstanceType: instanceType, RootDeviceName: awssdk.String("/dev/sda1"),
	}}}}}
	instanceTypes := map[types.InstanceType]types.InstanceTypeInfo{
		instanceType: {InstanceType: instanceType, VCpuInfo: &types.VCpuInfo{DefaultVCpus: awssdk.Int32(4)}, MemoryInfo: &types.MemoryInfo{SizeInMiB: awssdk.Int64(8192)}},
	}
	volumes := map[string][]types.Volume{"i-hardware": {
		{VolumeId: awssdk.String("vol-root"), VolumeType: types.VolumeTypeGp3, Size: awssdk.Int32(100), Encrypted: awssdk.Bool(true), Attachments: []types.VolumeAttachment{{InstanceId: awssdk.String("i-hardware"), Device: awssdk.String("/dev/sda1"), State: types.VolumeAttachmentStateAttached}}},
		{VolumeId: awssdk.String("vol-data"), VolumeType: types.VolumeTypeGp3, Size: awssdk.Int32(400), Encrypted: awssdk.Bool(false), Attachments: []types.VolumeAttachment{{InstanceId: awssdk.String("i-hardware"), Device: awssdk.String("/dev/sdf"), State: types.VolumeAttachmentStateAttached}}},
		{VolumeId: awssdk.String("vol-data"), VolumeType: types.VolumeTypeGp3, Size: awssdk.Int32(400), Encrypted: awssdk.Bool(false), Attachments: []types.VolumeAttachment{{InstanceId: awssdk.String("i-hardware"), Device: awssdk.String("/dev/sdf"), State: types.VolumeAttachmentStateAttached}}},
	}}

	values := ec2Snapshots(output, "cn-north-1", instanceTypes, volumes)
	wantDisks := []resource.ServerDisk{
		{ID: "vol-data", Kind: "data", Type: "gp3", SizeGiB: 400, Device: "/dev/sdf"},
		{ID: "vol-root", Kind: "system", Type: "gp3", SizeGiB: 100, Device: "/dev/sda1", Encrypted: true},
	}
	if len(values) != 1 || values[0].InstanceType != "c6a.xlarge" || values[0].VCPU != 4 || values[0].Memory != 8192 || !reflect.DeepEqual(values[0].Disks, wantDisks) {
		t.Fatalf("EC2 规格和 EBS 明细必须完整且顺序稳定：%+v", values)
	}
}

// TestEC2SnapshotsUsesOnlyAttachedDevice 防止同一卷的过渡态 attachment 被误用为设备名或系统盘判定依据。
func TestEC2SnapshotsUsesOnlyAttachedDevice(t *testing.T) {
	instanceType := types.InstanceType("c6a.xlarge")
	output := &ec2.DescribeInstancesOutput{Reservations: []types.Reservation{{Instances: []types.Instance{{InstanceId: awssdk.String("i-1"), InstanceType: instanceType, RootDeviceName: awssdk.String("/dev/sda1")}}}}}
	instanceTypes := map[types.InstanceType]types.InstanceTypeInfo{instanceType: {InstanceType: instanceType, VCpuInfo: &types.VCpuInfo{DefaultVCpus: awssdk.Int32(4)}, MemoryInfo: &types.MemoryInfo{SizeInMiB: awssdk.Int64(8192)}}}
	volumes := map[string][]types.Volume{"i-1": {{
		VolumeId: awssdk.String("vol-root"), VolumeType: types.VolumeTypeGp3, Size: awssdk.Int32(100), Encrypted: awssdk.Bool(false),
		Attachments: []types.VolumeAttachment{
			{InstanceId: awssdk.String("i-1"), Device: awssdk.String("/dev/wrong"), State: types.VolumeAttachmentStateDetaching},
			{InstanceId: awssdk.String("i-1"), Device: awssdk.String("/dev/sda1"), State: types.VolumeAttachmentStateAttached},
		},
	}}}
	values := ec2Snapshots(output, "cn-north-1", instanceTypes, volumes)
	if len(values) != 1 || len(values[0].Disks) != 1 || values[0].Disks[0].Device != "/dev/sda1" || values[0].Disks[0].Kind != "system" {
		t.Fatalf("EC2 磁盘映射只能使用 attached 设备：%+v", values)
	}
}

// TestEC2SnapshotsPreferConfiguredVCPU 防止自定义 CPU Options 的实例被错误展示为机型默认 vCPU。
func TestEC2SnapshotsPreferConfiguredVCPU(t *testing.T) {
	instanceType := types.InstanceType("c6a.xlarge")
	output := &ec2.DescribeInstancesOutput{Reservations: []types.Reservation{{Instances: []types.Instance{{
		InstanceId: awssdk.String("i-custom-cpu"), InstanceType: instanceType,
		CpuOptions: &types.CpuOptions{CoreCount: awssdk.Int32(1), ThreadsPerCore: awssdk.Int32(2)},
	}}}}}
	instanceTypes := map[types.InstanceType]types.InstanceTypeInfo{
		instanceType: {InstanceType: instanceType, VCpuInfo: &types.VCpuInfo{DefaultVCpus: awssdk.Int32(4)}, MemoryInfo: &types.MemoryInfo{SizeInMiB: awssdk.Int64(8192)}},
	}

	values := ec2Snapshots(output, "cn-north-1", instanceTypes, nil)
	if len(values) != 1 || values[0].VCPU != 2 {
		t.Fatalf("EC2 必须优先保存实例实际配置的 vCPU：%+v", values)
	}
}

// TestCollectEC2LoadsInstanceTypesAndAttachedVolumes 防止完整同步漏掉规格详情或 EBS 云盘。
func TestCollectEC2LoadsInstanceTypesAndAttachedVolumes(t *testing.T) {
	client := &ec2CollectStub{}
	instances, instanceTypes, volumes, err := collectEC2(context.Background(), client)
	if err != nil || len(instances.Reservations) != 1 || len(instanceTypes) != 1 || len(volumes["i-1"]) != 1 {
		t.Fatalf("EC2 完整采集必须返回实例、规格详情和已挂载 EBS：types=%v volumes=%v err=%v", instanceTypes, volumes, err)
	}
	if client.instanceTypeInput == nil || !reflect.DeepEqual(client.instanceTypeInput.InstanceTypes, []types.InstanceType{types.InstanceType("c6a.xlarge")}) || client.volumeInput == nil {
		t.Fatalf("EC2 补充采集必须按实际实例类型查询规格并分页读取 EBS：typeInput=%+v volumeInput=%+v", client.instanceTypeInput, client.volumeInput)
	}
}

// TestCollectEC2RejectsMissingInstanceTypeDetails 防止 CPU 和内存缺失时仍写入看似成功的 EC2 快照。
func TestCollectEC2RejectsMissingInstanceTypeDetails(t *testing.T) {
	client := &ec2CollectStub{omitInstanceType: true}
	if _, _, _, err := collectEC2(context.Background(), client); err == nil {
		t.Fatal("EC2 实例类型详情不完整时必须使该资源类型采集失败")
	}
}

// TestCollectEC2RejectsEmptyInstanceType 防止空实例类型绕过规格查询并写入 0 CPU、0 内存快照。
func TestCollectEC2RejectsEmptyInstanceType(t *testing.T) {
	client := &ec2CollectStub{emptyInstanceType: true}
	if _, _, _, err := collectEC2(context.Background(), client); err == nil {
		t.Fatal("EC2 实例类型为空时必须使该资源类型采集失败")
	}
}

// TestCollectEC2RejectsMissingRootDeviceForAttachedEBS 防止根设备缺失时把系统盘静默标成数据盘。
func TestCollectEC2RejectsMissingRootDeviceForAttachedEBS(t *testing.T) {
	client := &ec2CollectStub{emptyRootDevice: true}
	if _, _, _, err := collectEC2(context.Background(), client); err == nil {
		t.Fatal("实例存在已挂载 EBS 但缺少根设备名时必须使 EC2 类型失败")
	}
}

// TestCollectEC2KeepsOnlyAttachedEBS 防止挂载中、卸载中或已卸载的 EBS 被保存为服务器磁盘。
func TestCollectEC2KeepsOnlyAttachedEBS(t *testing.T) {
	client := &ec2CollectStub{volumes: []types.Volume{
		{VolumeId: awssdk.String("vol-attached"), VolumeType: types.VolumeTypeGp3, Size: awssdk.Int32(40), Encrypted: awssdk.Bool(true), Attachments: []types.VolumeAttachment{{InstanceId: awssdk.String("i-1"), Device: awssdk.String("/dev/sda1"), State: types.VolumeAttachmentStateAttached}}},
		{VolumeId: awssdk.String("vol-detaching"), VolumeType: types.VolumeTypeGp3, Size: awssdk.Int32(100), Encrypted: awssdk.Bool(false), Attachments: []types.VolumeAttachment{{InstanceId: awssdk.String("i-1"), Device: awssdk.String("/dev/sdf"), State: types.VolumeAttachmentStateDetaching}}},
	}}
	_, _, volumes, err := collectEC2(context.Background(), client)
	if err != nil || len(volumes["i-1"]) != 1 || awssdk.ToString(volumes["i-1"][0].VolumeId) != "vol-attached" {
		t.Fatalf("EC2 只能保存 attachment 状态为 attached 的 EBS：volumes=%+v err=%v", volumes, err)
	}
}

// TestCollectEC2RejectsIncompleteAttachedEBS 防止不完整 EBS 被静默丢弃并以空明细覆盖旧快照。
func TestCollectEC2RejectsIncompleteAttachedEBS(t *testing.T) {
	valid := types.Volume{VolumeId: awssdk.String("vol-1"), VolumeType: types.VolumeTypeGp3, Size: awssdk.Int32(100), Encrypted: awssdk.Bool(false), Attachments: []types.VolumeAttachment{{InstanceId: awssdk.String("i-1"), Device: awssdk.String("/dev/sdf"), State: types.VolumeAttachmentStateAttached}}}
	tests := []struct {
		name   string
		mutate func(*types.Volume)
	}{
		{name: "缺少磁盘 ID", mutate: func(value *types.Volume) { value.VolumeId = nil }},
		{name: "缺少磁盘类型", mutate: func(value *types.Volume) { value.VolumeType = "" }},
		{name: "缺少容量", mutate: func(value *types.Volume) { value.Size = nil }},
		{name: "容量无效", mutate: func(value *types.Volume) { value.Size = awssdk.Int32(0) }},
		{name: "缺少加密状态", mutate: func(value *types.Volume) { value.Encrypted = nil }},
		{name: "缺少实例 ID", mutate: func(value *types.Volume) { value.Attachments[0].InstanceId = nil }},
		{name: "缺少设备名", mutate: func(value *types.Volume) { value.Attachments[0].Device = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			value.Attachments = append([]types.VolumeAttachment(nil), valid.Attachments...)
			test.mutate(&value)
			client := &ec2CollectStub{volumes: []types.Volume{value}}
			if _, _, _, err := collectEC2(context.Background(), client); err == nil {
				t.Fatal("已挂载 EBS 的必要字段不完整时必须使 EC2 类型失败")
			}
		})
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

// TestRDSSnapshotsKeepSpecification 验证 AWS RDS 原生存储字段和实例类型详情进入统一快照。
func TestRDSSnapshotsKeepSpecification(t *testing.T) {
	instanceType := types.InstanceType("r6g.large")
	output := &rds.DescribeDBInstancesOutput{DBInstances: []rdstypes.DBInstance{{
		DBInstanceIdentifier: awssdk.String("db-specification"),
		DBInstanceClass:      awssdk.String("db.r6g.large"),
		AllocatedStorage:     awssdk.Int32(200),
		StorageType:          awssdk.String("gp3"),
	}}}
	instanceTypes := map[types.InstanceType]types.InstanceTypeInfo{
		instanceType: {InstanceType: instanceType, VCpuInfo: &types.VCpuInfo{DefaultVCpus: awssdk.Int32(2)}, MemoryInfo: &types.MemoryInfo{SizeInMiB: awssdk.Int64(16384)}},
	}
	values := rdsSnapshots(output, "cn-north-1", instanceTypes)
	if len(values) != 1 || values[0].InstanceType != "db.r6g.large" || values[0].VCPU != 2 || values[0].Memory != 16384 || values[0].StorageType != "gp3" || values[0].StorageSizeGiB != 200 {
		t.Fatalf("AWS RDS 规格转换错误：%+v", values)
	}
}

// TestRDSSnapshotsPreferConfiguredVCPU 防止可调整处理器配置的 RDS 被错误展示为实例类型默认 vCPU。
func TestRDSSnapshotsPreferConfiguredVCPU(t *testing.T) {
	instanceType := types.InstanceType("r6i.large")
	output := &rds.DescribeDBInstancesOutput{DBInstances: []rdstypes.DBInstance{{
		DBInstanceIdentifier: awssdk.String("db-custom-cpu"),
		DBInstanceClass:      awssdk.String("db.r6i.large"),
		ProcessorFeatures: []rdstypes.ProcessorFeature{
			{Name: awssdk.String("coreCount"), Value: awssdk.String("1")},
			{Name: awssdk.String("threadsPerCore"), Value: awssdk.String("2")},
		},
	}}}
	instanceTypes := map[types.InstanceType]types.InstanceTypeInfo{
		instanceType: {InstanceType: instanceType, VCpuInfo: &types.VCpuInfo{DefaultVCpus: awssdk.Int32(4)}, MemoryInfo: &types.MemoryInfo{SizeInMiB: awssdk.Int64(16384)}},
	}
	values := rdsSnapshots(output, "cn-north-1", instanceTypes)
	if len(values) != 1 || values[0].VCPU != 2 {
		t.Fatalf("AWS RDS 必须优先保存实际处理器配置：%+v", values)
	}
}

// TestCollectRDSLoadsFixedInstanceTypeDetails 防止完整同步只保存 db.* 名称而无法展示固定规格 CPU 和内存。
func TestCollectRDSLoadsFixedInstanceTypeDetails(t *testing.T) {
	rdsClient, typeClient := &rdsCollectStub{}, &rdsInstanceTypeStub{}
	output, instanceTypes, err := collectRDS(context.Background(), rdsClient, typeClient)
	if err != nil || len(output.DBInstances) != 1 || len(instanceTypes) != 1 {
		t.Fatalf("AWS RDS 完整采集必须返回固定实例类型详情：instances=%v types=%v err=%v", output.DBInstances, instanceTypes, err)
	}
	if typeClient.input == nil || !reflect.DeepEqual(typeClient.input.InstanceTypes, []types.InstanceType{types.InstanceType("r6g.large")}) {
		t.Fatalf("AWS RDS 必须移除 db. 前缀后查询实例类型详情：%+v", typeClient.input)
	}
}

// TestCollectRDSRejectsIncompleteFixedInstanceType 防止不完整的固定规格详情清空最近一次有效 CPU 或内存。
func TestCollectRDSRejectsIncompleteFixedInstanceType(t *testing.T) {
	for _, test := range []struct {
		name   string
		client *rdsInstanceTypeStub
	}{
		{name: "缺少 vCPU", client: &rdsInstanceTypeStub{omitVCPU: true}},
		{name: "缺少内存", client: &rdsInstanceTypeStub{omitMemory: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := collectRDS(context.Background(), &rdsCollectStub{}, test.client); err == nil {
				t.Fatal("AWS RDS 固定实例类型详情不完整时必须使该资源类型采集失败")
			}
		})
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

// TestAWSConnectionProbeChecksHardwareDetailPermissions 防止连接测试放过缺少规格或 EBS 读取权限的接入源。
func TestAWSConnectionProbeChecksHardwareDetailPermissions(t *testing.T) {
	ec2Client, rdsClient, elbClient := &ec2ProbeStub{}, &rdsProbeStub{}, &elbProbeStub{}
	if _, err := probeAWSAccess(context.Background(), ec2Client, rdsClient, elbClient); err != nil {
		t.Fatalf("AWS 连接探测失败：%v", err)
	}
	if ec2Client.instanceTypeInput == nil || awssdk.ToInt32(ec2Client.instanceTypeInput.MaxResults) != 5 || ec2Client.volumeInput == nil || awssdk.ToInt32(ec2Client.volumeInput.MaxResults) != 5 {
		t.Fatalf("连接测试必须以最小请求验证规格和 EBS 读取权限：type=%+v volume=%+v", ec2Client.instanceTypeInput, ec2Client.volumeInput)
	}
}

// TestAWSConnectionProbeSharesInstanceTypeFailureWithRDS 防止公共规格接口故障时仍误报 RDS 可达。
func TestAWSConnectionProbeSharesInstanceTypeFailureWithRDS(t *testing.T) {
	instanceTypeErr := errors.New("实例类型目录暂不可用")
	ec2Client := &ec2ProbeStub{instanceErr: errors.New("实例列表暂不可用"), instanceTypeErr: instanceTypeErr}
	results, err := probeAWSAccess(context.Background(), ec2Client, &rdsProbeStub{}, &elbProbeStub{})
	if err != nil {
		t.Fatalf("普通云端故障应保留为类型级结果：%v", err)
	}
	if ec2Client.instanceTypeInput == nil {
		t.Fatal("实例列表失败后仍须独立探测 EC2 与 RDS 共用的实例类型目录")
	}
	if len(results) != 3 || !errors.Is(results[1].Err, instanceTypeErr) {
		t.Fatalf("公共实例类型目录故障必须同时使 RDS 探测失败：%+v", results)
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
