// 本文件使用 AWS 官方 SDK 编排六类型资源采集与轻量探测，并转换 EC2、RDS 共享资源快照。
package aws

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"

	"cmdb/internal/resource"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
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

// rdsCollectAPI 的完整列表与探测共用官方 RDS API，分页策略由采集入口决定。
type rdsCollectAPI interface {
	rdsProbeAPI
}

// instanceTypeAPI 为 RDS 固定规格复用 EC2 的只读实例类型目录，不参与 EC2 资产采集。
type instanceTypeAPI interface {
	ec2.DescribeInstanceTypesAPIClient
}

// elbProbeAPI 约束连接测试只调用 ELBv2 列表首页。
type elbProbeAPI interface {
	DescribeLoadBalancers(context.Context, *elasticloadbalancingv2.DescribeLoadBalancersInput, ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeLoadBalancersOutput, error)
}

// Collect 使用接入源区域和静态凭证创建官方 SDK v2 客户端，统一交给六类型编排。
func (c *Collector) Collect(ctx context.Context, source resource.Source, plain []byte) ([]resource.CollectionResult, error) {
	var auth credential
	if json.Unmarshal(plain, &auth) != nil || auth.AccessKeyID == "" || auth.SecretAccessKey == "" || source.Region == "" {
		return nil, resource.ErrAuthenticationFailed
	}
	configuration, err := config.LoadDefaultConfig(ctx, config.WithRegion(source.Region), config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(auth.AccessKeyID, auth.SecretAccessKey, auth.SessionToken)))
	if err != nil {
		return nil, resource.ErrAuthenticationFailed
	}
	ec2Client := ec2.NewFromConfig(configuration)
	rdsClient := rds.NewFromConfig(configuration)
	classicClient := elasticloadbalancing.NewFromConfig(configuration)
	v2Client := elasticloadbalancingv2.NewFromConfig(configuration)
	return collectAWSResources(ctx, ec2Client, rdsClient, classicClient, v2Client, source.Region)
}

// collectAWSResources 保留类型级失败边界，ELBv2 共享一次完整列表后按三种正式类型独立采集。
func collectAWSResources(ctx context.Context, ec2Client ec2CollectAPI, rdsClient rdsCollectAPI, classicClient classicELBAPI, v2Client elbV2API, region string) ([]resource.CollectionResult, error) {
	results, err := resource.CollectByType(ctx, []resource.TypeCollection{
		{ResourceType: "ec2", Collect: func(ctx context.Context) ([]resource.Snapshot, error) {
			output, instanceTypes, volumes, collectErr := collectEC2(ctx, ec2Client)
			if collectErr != nil {
				return nil, collectErr
			}
			return ec2Snapshots(output, region, instanceTypes, volumes), nil
		}},
		{ResourceType: "rds", Collect: func(ctx context.Context) ([]resource.Snapshot, error) {
			output, instanceTypes, collectErr := collectRDS(ctx, rdsClient, ec2Client)
			if collectErr != nil {
				return nil, collectErr
			}
			return rdsSnapshots(output, region, instanceTypes), nil
		}},
		{ResourceType: "clb", Collect: func(ctx context.Context) ([]resource.Snapshot, error) {
			return collectCLB(ctx, classicClient, region)
		}},
	}, classifyAWSAccessError)
	if err != nil {
		return nil, err
	}
	values, listErr := listELBV2LoadBalancers(ctx, v2Client)
	var v2Results []resource.CollectionResult
	if listErr != nil {
		// 不完整列表无法判断任何 v2 类型是否缺失，三类必须共同失败，保留此前完整类型。
		for _, resourceType := range []string{"alb", "nlb", "gwlb"} {
			v2Results = append(v2Results, resource.CollectionResult{ResourceType: resourceType, Err: listErr})
		}
	} else {
		v2Results, err = collectELBV2ByType(ctx, v2Client, values, region)
		if err != nil {
			return nil, err
		}
	}
	for index := range v2Results {
		result := &v2Results[index]
		if result.Err == nil {
			continue
		}
		// 每类监听器错误独立收敛；未分类错误继续交给共享核心生成安全摘要。
		if accessErr := classifyAWSAccessError(result.Err); accessErr != nil {
			result.Err = accessErr
		}
		if errors.Is(result.Err, resource.ErrAuthenticationFailed) {
			return nil, result.Err
		}
		result.Snapshots = nil
	}
	results = append(results, v2Results...)
	for resultIndex := range results {
		if results[resultIndex].Err != nil {
			continue
		}
		for snapshotIndex := range results[resultIndex].Snapshots {
			resolveEndpoints(ctx, &results[resultIndex].Snapshots[snapshotIndex])
		}
	}
	return results, nil
}

// Probe 通过六类型资源依赖的最小分页请求验证认证、权限和网络，不读取监听器或解析动态地址。
func (c *Collector) Probe(ctx context.Context, source resource.Source, plain []byte) ([]resource.CollectionResult, error) {
	var auth credential
	if json.Unmarshal(plain, &auth) != nil || auth.AccessKeyID == "" || auth.SecretAccessKey == "" || source.Region == "" {
		return nil, resource.ErrAuthenticationFailed
	}
	configuration, err := config.LoadDefaultConfig(ctx, config.WithRegion(source.Region), config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(auth.AccessKeyID, auth.SecretAccessKey, auth.SessionToken)))
	if err != nil {
		return nil, resource.ErrAuthenticationFailed
	}
	return probeAWSAccess(ctx, ec2.NewFromConfig(configuration), rds.NewFromConfig(configuration), elasticloadbalancing.NewFromConfig(configuration), elasticloadbalancingv2.NewFromConfig(configuration))
}

// probeAWSAccess 使用各 API 允许的最小分页，仅返回资源类型可达性。
func probeAWSAccess(ctx context.Context, ec2Client ec2ProbeAPI, rdsClient rdsProbeAPI, classicClient classicELBAPI, elbClient elbProbeAPI) ([]resource.CollectionResult, error) {
	_, ec2Err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{MaxResults: awssdk.Int32(5)})
	// 实例类型目录同时服务 EC2 和固定规格 RDS，即使实例列表失败也要独立验证该共同依赖。
	_, instanceTypeErr := ec2Client.DescribeInstanceTypes(ctx, &ec2.DescribeInstanceTypesInput{MaxResults: awssdk.Int32(5)})
	if ec2Err == nil {
		ec2Err = instanceTypeErr
	}
	if ec2Err == nil {
		_, ec2Err = ec2Client.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{MaxResults: awssdk.Int32(5)})
	}
	_, rdsErr := rdsClient.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{MaxRecords: awssdk.Int32(20)})
	if rdsErr == nil {
		rdsErr = instanceTypeErr
	}
	_, classicErr := classicClient.DescribeLoadBalancers(ctx, &elasticloadbalancing.DescribeLoadBalancersInput{PageSize: awssdk.Int32(1)})
	_, elbErr := elbClient.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{PageSize: awssdk.Int32(1)})
	// 三种 ELBv2 类型共享同一列表权限，只请求一次即可确定三类可达性。
	results := []resource.CollectionResult{
		{ResourceType: "ec2", Err: ec2Err}, {ResourceType: "rds", Err: rdsErr}, {ResourceType: "clb", Err: classicErr},
		{ResourceType: "alb", Err: elbErr}, {ResourceType: "nlb", Err: elbErr}, {ResourceType: "gwlb", Err: elbErr},
	}
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
func collectRDS(ctx context.Context, client rdsProbeAPI, typeClient instanceTypeAPI) (*rds.DescribeDBInstancesOutput, map[ec2types.InstanceType]ec2types.InstanceTypeInfo, error) {
	output := &rds.DescribeDBInstancesOutput{}
	instanceTypes := make(map[ec2types.InstanceType]ec2types.InstanceTypeInfo)
	paginator := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return output, instanceTypes, err
		}
		output.DBInstances = append(output.DBInstances, page.DBInstances...)
	}
	uniqueTypes := make(map[ec2types.InstanceType]struct{})
	for _, instance := range output.DBInstances {
		class := strings.TrimPrefix(awssdk.ToString(instance.DBInstanceClass), "db.")
		if class != "" && class != "serverless" {
			uniqueTypes[ec2types.InstanceType(class)] = struct{}{}
		}
	}
	typeNames := make([]ec2types.InstanceType, 0, len(uniqueTypes))
	for instanceType := range uniqueTypes {
		typeNames = append(typeNames, instanceType)
	}
	sort.Slice(typeNames, func(left, right int) bool { return typeNames[left] < typeNames[right] })
	for start := 0; start < len(typeNames); start += 100 {
		end := start + 100
		if end > len(typeNames) {
			end = len(typeNames)
		}
		paginator := ec2.NewDescribeInstanceTypesPaginator(typeClient, &ec2.DescribeInstanceTypesInput{InstanceTypes: typeNames[start:end]})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return output, instanceTypes, err
			}
			for _, info := range page.InstanceTypes {
				if info.VCpuInfo == nil || awssdk.ToInt32(info.VCpuInfo.DefaultVCpus) <= 0 || info.MemoryInfo == nil || awssdk.ToInt64(info.MemoryInfo.SizeInMiB) <= 0 {
					return output, instanceTypes, errors.New("AWS RDS 固定实例类型规格信息不完整")
				}
				instanceTypes[info.InstanceType] = info
			}
		}
	}
	return output, instanceTypes, nil
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
func rdsSnapshots(output *rds.DescribeDBInstancesOutput, region string, instanceTypeSets ...map[ec2types.InstanceType]ec2types.InstanceTypeInfo) []resource.Snapshot {
	values := []resource.Snapshot{}
	instanceTypes := map[ec2types.InstanceType]ec2types.InstanceTypeInfo{}
	if len(instanceTypeSets) > 0 && instanceTypeSets[0] != nil {
		instanceTypes = instanceTypeSets[0]
	}
	if output == nil {
		return values
	}
	for _, instance := range output.DBInstances {
		raw, _ := json.Marshal(instance)
		instanceClass := awssdk.ToString(instance.DBInstanceClass)
		value := resource.Snapshot{
			ResourceType:   "rds",
			ExternalID:     awssdk.ToString(instance.DBInstanceIdentifier),
			Name:           awssdk.ToString(instance.DBInstanceIdentifier),
			Region:         region,
			Zone:           awssdk.ToString(instance.AvailabilityZone),
			CloudStatus:    awssdk.ToString(instance.DBInstanceStatus),
			Engine:         awssdk.ToString(instance.Engine),
			EngineVersion:  awssdk.ToString(instance.EngineVersion),
			InstanceType:   instanceClass,
			StorageType:    awssdk.ToString(instance.StorageType),
			StorageSizeGiB: int64(awssdk.ToInt32(instance.AllocatedStorage)),
			RawAttributes:  raw,
			// AWS 会持续推进可时间点恢复的最新时间，它属于观测信息而非实例配置变化。
			VolatileRawAttributeKeys: []string{"LatestRestorableTime"},
		}
		if info, exists := instanceTypes[ec2types.InstanceType(strings.TrimPrefix(instanceClass, "db."))]; exists {
			if info.VCpuInfo != nil {
				value.VCPU = int(awssdk.ToInt32(info.VCpuInfo.DefaultVCpus))
			}
			if info.MemoryInfo != nil {
				value.Memory = awssdk.ToInt64(info.MemoryInfo.SizeInMiB)
			}
		}
		processor := map[string]int{}
		for _, feature := range instance.ProcessorFeatures {
			processor[awssdk.ToString(feature.Name)], _ = strconv.Atoi(awssdk.ToString(feature.Value))
		}
		if processor["coreCount"] > 0 && processor["threadsPerCore"] > 0 {
			value.VCPU = processor["coreCount"] * processor["threadsPerCore"]
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
func tagName(tags []ec2types.Tag) string {
	for _, tag := range tags {
		if awssdk.ToString(tag.Key) == "Name" {
			return awssdk.ToString(tag.Value)
		}
	}
	return ""
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
