// 本文件使用 AWS 官方 SDK 采集 EC2、RDS 和 ELB，并转换为共享资源快照。
package aws

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strings"

	"cmdb/internal/resource"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/smithy-go"
)

// Collector 是 AWS 独立平台采集器。
type Collector struct {
	newSTSClient func(awssdk.Config) awsIdentityAPI
}

// NewCollector 创建 AWS 采集器。
func NewCollector() *Collector { return &Collector{newSTSClient: newAWSSTSClient} }

type credential struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
}

// ec2ProbeAPI 约束连接测试只调用实例、规格与 EBS 列表的最小页面。
type ec2ProbeAPI interface {
	ec2.DescribeInstancesAPIClient
	ec2.DescribeInstanceTypesAPIClient
	ec2.DescribeVolumesAPIClient
}

// ec2CollectAPI 约束 EC2 完整采集读取实例、规格详情和 EBS，便于使用模拟响应验收。
type ec2CollectAPI interface {
	ec2.DescribeInstancesAPIClient
	ec2.DescribeInstanceTypesAPIClient
	ec2.DescribeVolumesAPIClient
}

// rdsProbeAPI 约束连接测试只调用 RDS 列表首页。
type rdsProbeAPI interface {
	DescribeDBInstances(context.Context, *rds.DescribeDBInstancesInput, ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error)
}

// elbProbeAPI 约束连接测试只调用 ELB 列表首页。
type elbProbeAPI interface {
	DescribeLoadBalancers(context.Context, *elasticloadbalancingv2.DescribeLoadBalancersInput, ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error)
}

// Collect 使用接入源区域和静态凭证采集三类 AWS 资源。
func (c *Collector) Collect(ctx context.Context, source resource.Source, plain []byte) ([]resource.CollectionResult, error) {
	var auth credential
	if json.Unmarshal(plain, &auth) != nil || auth.AccessKeyID == "" || auth.SecretAccessKey == "" || source.Region == "" {
		return nil, resource.ErrAuthenticationFailed
	}
	configuration, err := config.LoadDefaultConfig(ctx, config.WithRegion(source.Region), config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(auth.AccessKeyID, auth.SecretAccessKey, auth.SessionToken)))
	if err != nil {
		return nil, resource.ErrAuthenticationFailed
	}
	results, err := resource.CollectByType(ctx, []resource.TypeCollection{
		{ResourceType: "ec2", Collect: func(ctx context.Context) ([]resource.Snapshot, error) {
			output, instanceTypes, volumes, collectErr := collectEC2(ctx, ec2.NewFromConfig(configuration))
			if collectErr != nil {
				return nil, collectErr
			}
			return ec2Snapshots(output, source.Region, instanceTypes, volumes), nil
		}},
		{ResourceType: "rds", Collect: func(ctx context.Context) ([]resource.Snapshot, error) {
			output, collectErr := collectRDS(ctx, rds.NewFromConfig(configuration))
			if collectErr != nil {
				return nil, collectErr
			}
			return rdsSnapshots(output, source.Region), nil
		}},
		{ResourceType: "elb", Collect: func(ctx context.Context) ([]resource.Snapshot, error) {
			output, listeners, collectErr := collectELB(ctx, elasticloadbalancingv2.NewFromConfig(configuration))
			if collectErr != nil {
				return nil, collectErr
			}
			return elbSnapshots(output, listeners, source.Region), nil
		}},
	}, classifyAWSAccessError)
	if err != nil {
		return nil, err
	}
	for resultIndex := range results {
		for snapshotIndex := range results[resultIndex].Snapshots {
			resolveEndpoints(ctx, &results[resultIndex].Snapshots[snapshotIndex])
		}
	}
	return results, nil
}

// Probe 通过三类资源的最小分页请求验证认证、权限和网络，不读取监听器或解析动态地址。
func (c *Collector) Probe(ctx context.Context, source resource.Source, plain []byte) ([]resource.CollectionResult, error) {
	var auth credential
	if json.Unmarshal(plain, &auth) != nil || auth.AccessKeyID == "" || auth.SecretAccessKey == "" || source.Region == "" {
		return nil, resource.ErrAuthenticationFailed
	}
	configuration, err := config.LoadDefaultConfig(ctx, config.WithRegion(source.Region), config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(auth.AccessKeyID, auth.SecretAccessKey, auth.SessionToken)))
	if err != nil {
		return nil, resource.ErrAuthenticationFailed
	}
	return probeAWSAccess(ctx, ec2.NewFromConfig(configuration), rds.NewFromConfig(configuration), elasticloadbalancingv2.NewFromConfig(configuration))
}

// probeAWSAccess 使用各 API 允许的最小分页，仅返回资源类型可达性。
func probeAWSAccess(ctx context.Context, ec2Client ec2ProbeAPI, rdsClient rdsProbeAPI, elbClient elbProbeAPI) ([]resource.CollectionResult, error) {
	_, ec2Err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{MaxResults: awssdk.Int32(5)})
	if ec2Err == nil {
		_, ec2Err = ec2Client.DescribeInstanceTypes(ctx, &ec2.DescribeInstanceTypesInput{MaxResults: awssdk.Int32(5)})
	}
	if ec2Err == nil {
		_, ec2Err = ec2Client.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{MaxResults: awssdk.Int32(5)})
	}
	_, rdsErr := rdsClient.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{MaxRecords: awssdk.Int32(20)})
	_, elbErr := elbClient.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{PageSize: awssdk.Int32(1)})
	results := []resource.CollectionResult{{ResourceType: "ec2", Err: ec2Err}, {ResourceType: "rds", Err: rdsErr}, {ResourceType: "elb", Err: elbErr}}
	for _, result := range results {
		if accessErr := classifyAWSAccessError(result.Err); accessErr != nil {
			return nil, accessErr
		}
	}
	return results, nil
}

func collectEC2(ctx context.Context, client ec2CollectAPI) (*ec2.DescribeInstancesOutput, map[ec2types.InstanceType]ec2types.InstanceTypeInfo, map[string][]ec2types.Volume, error) {
	output := &ec2.DescribeInstancesOutput{}
	instanceTypes := make(map[ec2types.InstanceType]ec2types.InstanceTypeInfo)
	volumesByInstance := make(map[string][]ec2types.Volume)
	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return output, instanceTypes, volumesByInstance, err
		}
		output.Reservations = append(output.Reservations, page.Reservations...)
	}

	uniqueTypes := make(map[ec2types.InstanceType]struct{})
	instanceIDs := make(map[string]struct{})
	rootDevices := make(map[string]string)
	for _, reservation := range output.Reservations {
		for _, instance := range reservation.Instances {
			if instance.InstanceType == "" {
				return output, instanceTypes, volumesByInstance, errors.New("EC2 实例规格信息不完整")
			}
			uniqueTypes[instance.InstanceType] = struct{}{}
			if instanceID := awssdk.ToString(instance.InstanceId); instanceID != "" {
				instanceIDs[instanceID] = struct{}{}
				rootDevices[instanceID] = awssdk.ToString(instance.RootDeviceName)
			}
		}
	}
	typeNames := make([]ec2types.InstanceType, 0, len(uniqueTypes))
	for instanceType := range uniqueTypes {
		typeNames = append(typeNames, instanceType)
	}
	sort.Slice(typeNames, func(left, right int) bool { return typeNames[left] < typeNames[right] })
	// AWS 单次最多接受 100 个实例类型，按稳定顺序分批查询以覆盖大型账号。
	for start := 0; start < len(typeNames); start += 100 {
		end := start + 100
		if end > len(typeNames) {
			end = len(typeNames)
		}
		typePaginator := ec2.NewDescribeInstanceTypesPaginator(client, &ec2.DescribeInstanceTypesInput{InstanceTypes: typeNames[start:end]})
		for typePaginator.HasMorePages() {
			page, err := typePaginator.NextPage(ctx)
			if err != nil {
				return output, instanceTypes, volumesByInstance, err
			}
			for _, info := range page.InstanceTypes {
				instanceTypes[info.InstanceType] = info
			}
		}
	}
	for _, instanceType := range typeNames {
		info, exists := instanceTypes[instanceType]
		if !exists || info.VCpuInfo == nil || awssdk.ToInt32(info.VCpuInfo.DefaultVCpus) <= 0 || info.MemoryInfo == nil || awssdk.ToInt64(info.MemoryInfo.SizeInMiB) <= 0 {
			return output, instanceTypes, volumesByInstance, errors.New("EC2 实例规格信息不完整")
		}
	}

	// EBS 需要独立 API 获取；任何失败都使 EC2 类型失败，避免以空磁盘覆盖完整快照。
	volumePaginator := ec2.NewDescribeVolumesPaginator(client, &ec2.DescribeVolumesInput{})
	for volumePaginator.HasMorePages() {
		page, err := volumePaginator.NextPage(ctx)
		if err != nil {
			return output, instanceTypes, volumesByInstance, err
		}
		for _, volume := range page.Volumes {
			for _, attachment := range volume.Attachments {
				if attachment.State != ec2types.VolumeAttachmentStateAttached {
					continue
				}
				instanceID := awssdk.ToString(attachment.InstanceId)
				if awssdk.ToString(volume.VolumeId) == "" || volume.VolumeType == "" || volume.Size == nil || awssdk.ToInt32(volume.Size) <= 0 || volume.Encrypted == nil || instanceID == "" || awssdk.ToString(attachment.Device) == "" {
					return output, instanceTypes, volumesByInstance, errors.New("EC2 EBS 信息不完整")
				}
				if _, collected := instanceIDs[instanceID]; collected {
					if rootDevices[instanceID] == "" {
						return output, instanceTypes, volumesByInstance, errors.New("EC2 根设备信息不完整")
					}
					volumesByInstance[instanceID] = append(volumesByInstance[instanceID], volume)
				}
			}
		}
	}
	return output, instanceTypes, volumesByInstance, nil
}
func collectRDS(ctx context.Context, client *rds.Client) (*rds.DescribeDBInstancesOutput, error) {
	output := &rds.DescribeDBInstancesOutput{}
	paginator := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return output, err
		}
		output.DBInstances = append(output.DBInstances, page.DBInstances...)
	}
	return output, nil
}
func collectELB(ctx context.Context, client *elasticloadbalancingv2.Client) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, map[string][]elbtypes.Listener, error) {
	output := &elasticloadbalancingv2.DescribeLoadBalancersOutput{}
	listeners := map[string][]elbtypes.Listener{}
	paginator := elasticloadbalancingv2.NewDescribeLoadBalancersPaginator(client, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return output, listeners, err
		}
		output.LoadBalancers = append(output.LoadBalancers, page.LoadBalancers...)
	}
	// 监听端口需要独立 API 获取，任何失败都使 ELB 类型失败，避免写入不完整端点。
	for _, loadBalancer := range output.LoadBalancers {
		arn := awssdk.ToString(loadBalancer.LoadBalancerArn)
		listenerPaginator := elasticloadbalancingv2.NewDescribeListenersPaginator(client, &elasticloadbalancingv2.DescribeListenersInput{LoadBalancerArn: loadBalancer.LoadBalancerArn})
		for listenerPaginator.HasMorePages() {
			page, err := listenerPaginator.NextPage(ctx)
			if err != nil {
				return output, listeners, err
			}
			listeners[arn] = append(listeners[arn], page.Listeners...)
		}
	}
	return output, listeners, nil
}

func ec2Snapshots(output *ec2.DescribeInstancesOutput, region string, instanceTypes map[ec2types.InstanceType]ec2types.InstanceTypeInfo, volumesByInstance map[string][]ec2types.Volume) []resource.Snapshot {
	values := []resource.Snapshot{}
	if output == nil {
		return values
	}
	for _, reservation := range output.Reservations {
		for _, instance := range reservation.Instances {
			raw, _ := json.Marshal(instance)
			status := ""
			if instance.State != nil {
				status = string(instance.State.Name)
			}
			instanceID := awssdk.ToString(instance.InstanceId)
			value := resource.Snapshot{ResourceType: "ec2", ExternalID: instanceID, Name: tagName(instance.Tags), Region: region, CloudStatus: status, InstanceType: string(instance.InstanceType), RawAttributes: raw}
			if info, exists := instanceTypes[instance.InstanceType]; exists {
				if info.VCpuInfo != nil {
					value.VCPU = int(awssdk.ToInt32(info.VCpuInfo.DefaultVCpus))
				}
				if info.MemoryInfo != nil {
					value.Memory = awssdk.ToInt64(info.MemoryInfo.SizeInMiB)
				}
			}
			if instance.CpuOptions != nil {
				cores := awssdk.ToInt32(instance.CpuOptions.CoreCount)
				threadsPerCore := awssdk.ToInt32(instance.CpuOptions.ThreadsPerCore)
				if cores > 0 && threadsPerCore > 0 {
					value.VCPU = int(cores * threadsPerCore)
				}
			}
			if instance.Placement != nil {
				value.Zone = awssdk.ToString(instance.Placement.AvailabilityZone)
			}
			addAddress := func(kind, address string) {
				if address != "" && !hasEndpoint(value.Endpoints, kind, address) {
					value.Endpoints = append(value.Endpoints, resource.EndpointSnapshot{Kind: kind, Address: address})
				}
			}
			addAddress("private", awssdk.ToString(instance.PrivateIpAddress))
			addAddress("public", awssdk.ToString(instance.PublicIpAddress))
			for _, networkInterface := range instance.NetworkInterfaces {
				for _, address := range networkInterface.PrivateIpAddresses {
					addAddress("private", awssdk.ToString(address.PrivateIpAddress))
					if address.Association != nil {
						addAddress("public", awssdk.ToString(address.Association.PublicIp))
					}
				}
			}
			seenDisks := make(map[string]struct{})
			for _, volume := range volumesByInstance[instanceID] {
				volumeID := awssdk.ToString(volume.VolumeId)
				if volumeID == "" {
					continue
				}
				if _, exists := seenDisks[volumeID]; exists {
					continue
				}
				device := ""
				for _, attachment := range volume.Attachments {
					if attachment.State == ec2types.VolumeAttachmentStateAttached && awssdk.ToString(attachment.InstanceId) == instanceID {
						device = awssdk.ToString(attachment.Device)
						break
					}
				}
				if device == "" {
					continue
				}
				kind := "data"
				if device == awssdk.ToString(instance.RootDeviceName) {
					kind = "system"
				}
				seenDisks[volumeID] = struct{}{}
				value.Disks = append(value.Disks, resource.ServerDisk{ID: volumeID, Kind: kind, Type: string(volume.VolumeType), SizeGiB: int64(awssdk.ToInt32(volume.Size)), Device: device, Encrypted: awssdk.ToBool(volume.Encrypted)})
			}
			sort.Slice(value.Disks, func(left, right int) bool { return value.Disks[left].ID < value.Disks[right].ID })
			values = append(values, value)
		}
	}
	return values
}
func rdsSnapshots(output *rds.DescribeDBInstancesOutput, region string) []resource.Snapshot {
	values := []resource.Snapshot{}
	if output == nil {
		return values
	}
	for _, instance := range output.DBInstances {
		raw, _ := json.Marshal(instance)
		value := resource.Snapshot{
			ResourceType:  "rds",
			ExternalID:    awssdk.ToString(instance.DBInstanceIdentifier),
			Name:          awssdk.ToString(instance.DBInstanceIdentifier),
			Region:        region,
			Zone:          awssdk.ToString(instance.AvailabilityZone),
			CloudStatus:   awssdk.ToString(instance.DBInstanceStatus),
			Engine:        awssdk.ToString(instance.Engine),
			EngineVersion: awssdk.ToString(instance.EngineVersion),
			RawAttributes: raw,
			// AWS 会持续推进可时间点恢复的最新时间，它属于观测信息而非实例配置变化。
			VolatileRawAttributeKeys: []string{"LatestRestorableTime"},
		}
		if instance.Endpoint != nil {
			kind := "private"
			if awssdk.ToBool(instance.PubliclyAccessible) {
				kind = "public"
			}
			value.Endpoints = append(value.Endpoints, resource.EndpointSnapshot{Kind: kind, Address: awssdk.ToString(instance.Endpoint.Address), Port: int(awssdk.ToInt32(instance.Endpoint.Port)), Protocol: "tcp"})
		}
		values = append(values, value)
	}
	return values
}
func elbSnapshots(output *elasticloadbalancingv2.DescribeLoadBalancersOutput, listeners map[string][]elbtypes.Listener, region string) []resource.Snapshot {
	values := []resource.Snapshot{}
	if output == nil {
		return values
	}
	for _, loadBalancer := range output.LoadBalancers {
		raw, _ := json.Marshal(loadBalancer)
		kind := "private"
		if loadBalancer.Scheme == elbtypes.LoadBalancerSchemeEnumInternetFacing {
			kind = "public"
		}
		status := ""
		if loadBalancer.State != nil {
			status = string(loadBalancer.State.Code)
		}
		endpoints := []resource.EndpointSnapshot{}
		for _, listener := range listeners[awssdk.ToString(loadBalancer.LoadBalancerArn)] {
			endpoints = append(endpoints, resource.EndpointSnapshot{Kind: kind, Address: awssdk.ToString(loadBalancer.DNSName), Port: int(awssdk.ToInt32(listener.Port)), Protocol: strings.ToLower(string(listener.Protocol))})
		}
		if len(endpoints) == 0 {
			endpoints = append(endpoints, resource.EndpointSnapshot{Kind: kind, Address: awssdk.ToString(loadBalancer.DNSName), Protocol: "tcp"})
		}
		values = append(values, resource.Snapshot{ResourceType: "elb", ExternalID: awssdk.ToString(loadBalancer.LoadBalancerArn), Name: awssdk.ToString(loadBalancer.LoadBalancerName), Region: region, Zone: firstELBZone(loadBalancer.AvailabilityZones), CloudStatus: status, NetworkType: string(loadBalancer.Scheme), RawAttributes: raw, Endpoints: endpoints})
	}
	return values
}

func tagName(tags []ec2types.Tag) string {
	for _, tag := range tags {
		if awssdk.ToString(tag.Key) == "Name" {
			return awssdk.ToString(tag.Value)
		}
	}
	return ""
}
func firstELBZone(zones []elbtypes.AvailabilityZone) string {
	if len(zones) == 0 {
		return ""
	}
	return awssdk.ToString(zones[0].ZoneName)
}
func hasEndpoint(values []resource.EndpointSnapshot, kind, address string) bool {
	for _, value := range values {
		if value.Kind == kind && value.Address == address {
			return true
		}
	}
	return false
}
func resolveEndpoints(ctx context.Context, snapshot *resource.Snapshot) {
	for index := range snapshot.Endpoints {
		endpoint := &snapshot.Endpoints[index]
		if endpoint.Address == "" || net.ParseIP(endpoint.Address) != nil {
			continue
		}
		if values, err := net.DefaultResolver.LookupHost(ctx, endpoint.Address); err == nil {
			endpoint.ResolvedIPs = values
		}
	}
}

// classifyAWSAccessError 将 AWS API 错误映射为稳定的认证或权限领域错误。
func classifyAWSAccessError(err error) error {
	if err == nil {
		return nil
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		code := strings.ToLower(apiError.ErrorCode())
		if strings.Contains(code, "accessdenied") || strings.Contains(code, "unauthorizedoperation") || strings.Contains(code, "notauthorized") {
			return resource.ErrPermissionDenied
		}
		if strings.Contains(code, "auth") || strings.Contains(code, "invalidclienttoken") || strings.Contains(code, "signature") || strings.Contains(code, "expiredtoken") || strings.Contains(code, "unrecognizedclient") {
			return resource.ErrAuthenticationFailed
		}
	}
	return nil
}
