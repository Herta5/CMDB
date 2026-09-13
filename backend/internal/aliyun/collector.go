// 本文件使用阿里云官方 SDK 采集 ECS、RDS 和负载均衡资源。
package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"

	"cmdb/internal/resource"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/rds"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/slb"
)

// Collector 是阿里云独立平台采集器。
type Collector struct {
	newSTSClient stsClientFactory
}

// NewCollector 创建阿里云采集器。
func NewCollector() *Collector { return &Collector{newSTSClient: newAliyunSTSClient} }

type credential struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
}

// ecsProbeAPI 约束连接测试只调用实例与云盘列表的最小页面，便于使用模拟响应验证轻量行为。
type ecsProbeAPI interface {
	DescribeInstances(*ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error)
	DescribeDisks(*ecs.DescribeDisksRequest) (*ecs.DescribeDisksResponse, error)
}

// ecsCollectAPI 约束 ECS 完整采集同时读取实例和已挂载云盘，便于使用模拟响应验收。
type ecsCollectAPI interface {
	DescribeInstances(*ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error)
	DescribeDisks(*ecs.DescribeDisksRequest) (*ecs.DescribeDisksResponse, error)
}

// rdsProbeAPI 约束连接测试调用 RDS 列表首页，并在存在实例时验证规格详情权限。
type rdsProbeAPI interface {
	DescribeDBInstances(*rds.DescribeDBInstancesRequest) (*rds.DescribeDBInstancesResponse, error)
	DescribeDBInstanceAttribute(*rds.DescribeDBInstanceAttributeRequest) (*rds.DescribeDBInstanceAttributeResponse, error)
}

// rdsCollectAPI 约束 RDS 完整采集同时读取实例、网络和规格详情，便于使用模拟响应验收。
type rdsCollectAPI interface {
	rdsProbeAPI
	DescribeDBInstanceNetInfo(*rds.DescribeDBInstanceNetInfoRequest) (*rds.DescribeDBInstanceNetInfoResponse, error)
}

// slbProbeAPI 约束连接测试只调用负载均衡列表首页。
type slbProbeAPI interface {
	DescribeLoadBalancers(*slb.DescribeLoadBalancersRequest) (*slb.DescribeLoadBalancersResponse, error)
}

// Collect 创建三个产品客户端并按资源类型隔离采集失败。
func (c *Collector) Collect(ctx context.Context, source resource.Source, plain []byte) ([]resource.CollectionResult, error) {
	var auth credential
	if json.Unmarshal(plain, &auth) != nil || auth.AccessKeyID == "" || auth.AccessKeySecret == "" || source.Region == "" {
		return nil, resource.ErrAuthenticationFailed
	}
	ecsClient, err := ecs.NewClientWithAccessKey(source.Region, auth.AccessKeyID, auth.AccessKeySecret)
	if err != nil {
		return nil, resource.ErrAuthenticationFailed
	}
	rdsClient, err := rds.NewClientWithAccessKey(source.Region, auth.AccessKeyID, auth.AccessKeySecret)
	if err != nil {
		return nil, resource.ErrAuthenticationFailed
	}
	slbClient, err := slb.NewClientWithAccessKey(source.Region, auth.AccessKeyID, auth.AccessKeySecret)
	if err != nil {
		return nil, resource.ErrAuthenticationFailed
	}
	results, err := resource.CollectByType(ctx, []resource.TypeCollection{
		{ResourceType: "ecs", Collect: func(context.Context) ([]resource.Snapshot, error) {
			items, disks, collectErr := collectECS(ecsClient, source.Region)
			if collectErr != nil {
				return nil, collectErr
			}
			return ecsSnapshots(items, disks), nil
		}},
		{ResourceType: "rds", Collect: func(context.Context) ([]resource.Snapshot, error) {
			items, networks, attributes, collectErr := collectRDS(rdsClient)
			if collectErr != nil {
				return nil, collectErr
			}
			return rdsSnapshots(items, networks, attributes), nil
		}},
		{ResourceType: "slb", Collect: func(context.Context) ([]resource.Snapshot, error) {
			items, ports, collectErr := collectSLB(slbClient, source.Region)
			if collectErr != nil {
				return nil, collectErr
			}
			return slbSnapshots(items, ports), nil
		}},
	}, classifyAliyunAccessError)
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

// Probe 通过三类资源的最小分页请求验证认证、权限和网络，不读取详情或解析动态地址。
func (c *Collector) Probe(ctx context.Context, source resource.Source, plain []byte) ([]resource.CollectionResult, error) {
	var auth credential
	if json.Unmarshal(plain, &auth) != nil || auth.AccessKeyID == "" || auth.AccessKeySecret == "" || source.Region == "" {
		return nil, resource.ErrAuthenticationFailed
	}
	ecsClient, err := ecs.NewClientWithAccessKey(source.Region, auth.AccessKeyID, auth.AccessKeySecret)
	if err != nil {
		return nil, resource.ErrAuthenticationFailed
	}
	rdsClient, err := rds.NewClientWithAccessKey(source.Region, auth.AccessKeyID, auth.AccessKeySecret)
	if err != nil {
		return nil, resource.ErrAuthenticationFailed
	}
	slbClient, err := slb.NewClientWithAccessKey(source.Region, auth.AccessKeyID, auth.AccessKeySecret)
	if err != nil {
		return nil, resource.ErrAuthenticationFailed
	}
	return probeAliyunAccess(ctx, ecsClient, rdsClient, slbClient, source.Region)
}

// probeAliyunAccess 每类资源只取一条数据；返回内容仅表达 API 是否可达，不携带云端资源。
func probeAliyunAccess(ctx context.Context, ecsClient ecsProbeAPI, rdsClient rdsProbeAPI, slbClient slbProbeAPI, region string) ([]resource.CollectionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ecsRequest := ecs.CreateDescribeInstancesRequest()
	ecsRequest.RegionId = region
	ecsRequest.PageNumber = "1"
	ecsRequest.PageSize = "1"
	_, ecsErr := ecsClient.DescribeInstances(ecsRequest)
	if ecsErr == nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		diskRequest := ecs.CreateDescribeDisksRequest()
		diskRequest.RegionId = region
		diskRequest.DiskType = "all"
		diskRequest.Status = "In_use"
		diskRequest.PageNumber = "1"
		diskRequest.PageSize = "1"
		_, ecsErr = ecsClient.DescribeDisks(diskRequest)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rdsRequest := rds.CreateDescribeDBInstancesRequest()
	rdsRequest.PageNumber = "1"
	rdsRequest.PageSize = "1"
	rdsResponse, rdsErr := rdsClient.DescribeDBInstances(rdsRequest)
	if rdsErr == nil && len(rdsResponse.Items.DBInstance) > 0 {
		attributeRequest := rds.CreateDescribeDBInstanceAttributeRequest()
		attributeRequest.DBInstanceId = rdsResponse.Items.DBInstance[0].DBInstanceId
		_, rdsErr = rdsClient.DescribeDBInstanceAttribute(attributeRequest)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slbRequest := slb.CreateDescribeLoadBalancersRequest()
	slbRequest.RegionId = region
	slbRequest.PageNumber = "1"
	slbRequest.PageSize = "1"
	_, slbErr := slbClient.DescribeLoadBalancers(slbRequest)

	results := []resource.CollectionResult{{ResourceType: "ecs", Err: ecsErr}, {ResourceType: "rds", Err: rdsErr}, {ResourceType: "slb", Err: slbErr}}
	for _, result := range results {
		if accessErr := classifyAliyunAccessError(result.Err); accessErr != nil {
			return nil, accessErr
		}
	}
	return results, nil
}

func collectECS(client ecsCollectAPI, region string) ([]ecs.Instance, map[string][]ecs.Disk, error) {
	values := []ecs.Instance{}
	disksByInstance := make(map[string][]ecs.Disk)
	for page := 1; ; page++ {
		request := ecs.CreateDescribeInstancesRequest()
		request.RegionId = region
		request.PageNumber = requests.Integer(strconv.Itoa(page))
		request.PageSize = "100"
		response, err := client.DescribeInstances(request)
		if err != nil {
			return values, disksByInstance, err
		}
		values = append(values, response.Instances.Instance...)
		if page*response.PageSize >= response.TotalCount || len(response.Instances.Instance) == 0 {
			break
		}
	}
	instanceIDs := make(map[string]struct{}, len(values))
	for _, instance := range values {
		vcpu := instance.Cpu
		if vcpu == 0 {
			vcpu = instance.CPU
		}
		if instance.InstanceType == "" || vcpu <= 0 || instance.Memory <= 0 {
			return values, disksByInstance, errors.New("ECS 实例规格信息不完整")
		}
		instanceIDs[instance.InstanceId] = struct{}{}
	}
	// 云盘需要独立 API 获取；任何失败都使 ECS 类型失败，避免以空磁盘覆盖完整快照。
	for page := 1; ; page++ {
		request := ecs.CreateDescribeDisksRequest()
		request.RegionId = region
		request.DiskType = "all"
		request.Status = "In_use"
		request.PageNumber = requests.Integer(strconv.Itoa(page))
		request.PageSize = "100"
		response, err := client.DescribeDisks(request)
		if err != nil {
			return values, disksByInstance, err
		}
		if err := validateAliyunDiskEncryptionPresence(response); err != nil {
			return values, disksByInstance, err
		}
		for _, disk := range response.Disks.Disk {
			// DescribeDisks 的筛选结果仍在本地收敛，避免云端忽略筛选参数时混入未挂载盘或本地盘。
			if disk.Status != "In_use" || isAliyunLocalDiskCategory(disk.Category) {
				continue
			}
			if !isAliyunCloudDiskCategory(disk.Category) || disk.DiskId == "" || disk.InstanceId == "" || (disk.Type != "system" && disk.Type != "data") || disk.Size <= 0 || disk.Device == "" {
				return values, disksByInstance, errors.New("ECS 云盘信息不完整")
			}
			if _, collected := instanceIDs[disk.InstanceId]; collected {
				disksByInstance[disk.InstanceId] = append(disksByInstance[disk.InstanceId], disk)
			}
		}
		if page*response.PageSize >= response.TotalCount || len(response.Disks.Disk) == 0 {
			return values, disksByInstance, nil
		}
	}
}

// validateAliyunDiskEncryptionPresence 从 SDK 保留的原始响应区分“未加密”和“字段缺失”；手工构造的测试响应没有原始正文时由结构化字段测试覆盖。
func validateAliyunDiskEncryptionPresence(response *ecs.DescribeDisksResponse) error {
	if response == nil {
		return errors.New("ECS 云盘响应为空")
	}
	raw := response.GetHttpContentBytes()
	if len(raw) == 0 {
		return nil
	}
	var envelope struct {
		Disks struct {
			Disk []struct {
				Encrypted *bool `json:"Encrypted"`
			} `json:"Disk"`
		} `json:"Disks"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Disks.Disk) != len(response.Disks.Disk) {
		return errors.New("ECS 云盘响应不完整")
	}
	for index, disk := range envelope.Disks.Disk {
		structured := response.Disks.Disk[index]
		if structured.Status != "In_use" || isAliyunLocalDiskCategory(structured.Category) {
			continue
		}
		if disk.Encrypted == nil {
			return errors.New("ECS 云盘加密状态缺失")
		}
	}
	return nil
}

// isAliyunCloudDiskCategory 仅接受具有稳定云盘 ID 的云盘类别，排除实例规格附带的本地盘。
func isAliyunCloudDiskCategory(category string) bool {
	return category == "cloud" || (strings.HasPrefix(category, "cloud_") && len(category) > len("cloud_")) || (strings.HasPrefix(category, "elastic_ephemeral_disk_") && len(category) > len("elastic_ephemeral_disk_"))
}

func isAliyunLocalDiskCategory(category string) bool {
	return category == "ephemeral" || strings.HasPrefix(category, "ephemeral_") || strings.HasPrefix(category, "local_")
}

func collectRDS(client rdsCollectAPI) ([]rds.DBInstance, map[string][]rds.DBInstanceNetInfo, map[string]rds.DBInstanceAttribute, error) {
	values := []rds.DBInstance{}
	networks := map[string][]rds.DBInstanceNetInfo{}
	attributes := map[string]rds.DBInstanceAttribute{}
	for page := 1; ; page++ {
		request := rds.CreateDescribeDBInstancesRequest()
		request.PageNumber = requests.Integer(strconv.Itoa(page))
		request.PageSize = "100"
		response, err := client.DescribeDBInstances(request)
		if err != nil {
			return values, networks, attributes, err
		}
		values = append(values, response.Items.DBInstance...)
		if page*response.PageRecordCount >= response.TotalRecordCount || len(response.Items.DBInstance) == 0 {
			break
		}
	}
	for _, instance := range values {
		request := rds.CreateDescribeDBInstanceNetInfoRequest()
		request.DBInstanceId = instance.DBInstanceId
		response, err := client.DescribeDBInstanceNetInfo(request)
		if err != nil {
			return values, networks, attributes, err
		}
		networks[instance.DBInstanceId] = response.DBInstanceNetInfos.DBInstanceNetInfo

		attributeRequest := rds.CreateDescribeDBInstanceAttributeRequest()
		attributeRequest.DBInstanceId = instance.DBInstanceId
		attributeResponse, err := client.DescribeDBInstanceAttribute(attributeRequest)
		if err != nil {
			return values, networks, attributes, err
		}
		if len(attributeResponse.Items.DBInstanceAttribute) != 1 || attributeResponse.Items.DBInstanceAttribute[0].DBInstanceId != instance.DBInstanceId {
			return values, networks, attributes, errors.New("阿里云 RDS 实例详情不完整")
		}
		attributes[instance.DBInstanceId] = attributeResponse.Items.DBInstanceAttribute[0]
	}
	return values, networks, attributes, nil
}
func collectSLB(client *slb.Client, region string) ([]slb.LoadBalancer, map[string][]slb.ListenerPortAndProtocol, error) {
	values := []slb.LoadBalancer{}
	ports := map[string][]slb.ListenerPortAndProtocol{}
	for page := 1; ; page++ {
		request := slb.CreateDescribeLoadBalancersRequest()
		request.RegionId = region
		request.PageNumber = requests.Integer(strconv.Itoa(page))
		request.PageSize = "100"
		response, err := client.DescribeLoadBalancers(request)
		if err != nil {
			return values, ports, err
		}
		values = append(values, response.LoadBalancers.LoadBalancer...)
		if page*response.PageSize >= response.TotalCount || len(response.LoadBalancers.LoadBalancer) == 0 {
			break
		}
	}
	for _, item := range values {
		request := slb.CreateDescribeLoadBalancerAttributeRequest()
		request.LoadBalancerId = item.LoadBalancerId
		response, err := client.DescribeLoadBalancerAttribute(request)
		if err != nil {
			return values, ports, err
		}
		ports[item.LoadBalancerId] = response.ListenerPortsAndProtocol.ListenerPortAndProtocol
	}
	return values, ports, nil
}

func ecsSnapshots(instances []ecs.Instance, disksByInstance map[string][]ecs.Disk) []resource.Snapshot {
	values := []resource.Snapshot{}
	for _, instance := range instances {
		raw, _ := json.Marshal(instance)
		vcpu := instance.Cpu
		if vcpu == 0 {
			vcpu = instance.CPU
		}
		value := resource.Snapshot{ResourceType: "ecs", ExternalID: instance.InstanceId, Name: instance.InstanceName, Region: instance.RegionId, Zone: instance.ZoneId, CloudStatus: instance.Status, InstanceType: instance.InstanceType, VCPU: vcpu, Memory: int64(instance.Memory), RawAttributes: raw}
		add := func(kind, address string) {
			if address != "" && !contains(value.Endpoints, kind, address, 0) {
				value.Endpoints = append(value.Endpoints, resource.EndpointSnapshot{Kind: kind, Address: address})
			}
		}
		for _, address := range instance.InnerIpAddress.IpAddress {
			add("private", address)
		}
		for _, address := range instance.VpcAttributes.PrivateIpAddress.IpAddress {
			add("private", address)
		}
		for _, address := range instance.PublicIpAddress.IpAddress {
			add("public", address)
		}
		add("public", instance.EipAddress.IpAddress)
		seenDisks := make(map[string]struct{})
		for _, disk := range disksByInstance[instance.InstanceId] {
			if disk.DiskId == "" {
				continue
			}
			if _, exists := seenDisks[disk.DiskId]; exists {
				continue
			}
			seenDisks[disk.DiskId] = struct{}{}
			value.Disks = append(value.Disks, resource.ServerDisk{ID: disk.DiskId, Kind: disk.Type, Type: disk.Category, SizeGiB: int64(disk.Size), Device: disk.Device, Encrypted: disk.Encrypted})
		}
		sort.Slice(value.Disks, func(left, right int) bool { return value.Disks[left].ID < value.Disks[right].ID })
		values = append(values, value)
	}
	return values
}
func rdsSnapshots(instances []rds.DBInstance, networks map[string][]rds.DBInstanceNetInfo, attributeSets ...map[string]rds.DBInstanceAttribute) []resource.Snapshot {
	values := []resource.Snapshot{}
	attributes := map[string]rds.DBInstanceAttribute{}
	if len(attributeSets) > 0 && attributeSets[0] != nil {
		attributes = attributeSets[0]
	}
	for _, instance := range instances {
		raw, _ := json.Marshal(instance)
		value := resource.Snapshot{ResourceType: "rds", ExternalID: instance.DBInstanceId, Name: instance.DBInstanceDescription, Region: instance.RegionId, Zone: instance.ZoneId, CloudStatus: instance.DBInstanceStatus, Engine: instance.Engine, EngineVersion: instance.EngineVersion, RawAttributes: raw}
		if attribute, exists := attributes[instance.DBInstanceId]; exists {
			value.InstanceType = attribute.DBInstanceClass
			value.VCPU, _ = strconv.Atoi(attribute.DBInstanceCPU)
			value.Memory = attribute.DBInstanceMemory
			value.StorageType = attribute.DBInstanceStorageType
			value.StorageSizeGiB = int64(attribute.DBInstanceStorage)
		}
		for _, network := range networks[instance.DBInstanceId] {
			port, _ := strconv.Atoi(network.Port)
			kind := "private"
			if strings.EqualFold(network.IPType, "public") {
				kind = "public"
			}
			value.Endpoints = append(value.Endpoints, resource.EndpointSnapshot{Kind: kind, Address: network.ConnectionString, Port: port, Protocol: "tcp"})
		}
		values = append(values, value)
	}
	return values
}
func slbSnapshots(loadBalancers []slb.LoadBalancer, portMaps ...map[string][]slb.ListenerPortAndProtocol) []resource.Snapshot {
	ports := map[string][]slb.ListenerPortAndProtocol{}
	if len(portMaps) > 0 {
		ports = portMaps[0]
	}
	values := []resource.Snapshot{}
	for _, item := range loadBalancers {
		raw, _ := json.Marshal(item)
		kind := "private"
		if strings.EqualFold(item.AddressType, "internet") {
			kind = "public"
		}
		endpoints := []resource.EndpointSnapshot{}
		if len(ports[item.LoadBalancerId]) == 0 {
			endpoints = append(endpoints, resource.EndpointSnapshot{Kind: kind, Address: item.Address})
		} else {
			for _, listener := range ports[item.LoadBalancerId] {
				endpoints = append(endpoints, resource.EndpointSnapshot{Kind: kind, Address: item.Address, Port: listener.ListenerPort, Protocol: listener.ListenerProtocol})
			}
		}
		values = append(values, resource.Snapshot{ResourceType: "slb", ExternalID: item.LoadBalancerId, Name: item.LoadBalancerName, Region: item.RegionId, Zone: item.MasterZoneId, CloudStatus: item.LoadBalancerStatus, NetworkType: item.AddressType, RawAttributes: raw, Endpoints: endpoints})
	}
	return values
}
func contains(values []resource.EndpointSnapshot, kind, address string, port int) bool {
	for _, value := range values {
		if value.Kind == kind && value.Address == address && value.Port == port {
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

// classifyAliyunAccessError 将阿里云原始错误收敛为可安全展示的认证或权限分类。
func classifyAliyunAccessError(err error) error {
	if err == nil {
		return nil
	}
	value := strings.ToLower(err.Error())
	if strings.Contains(value, "forbidden") || strings.Contains(value, "not authorized") || strings.Contains(value, "permission") {
		return resource.ErrPermissionDenied
	}
	if strings.Contains(value, "invalidaccesskey") || strings.Contains(value, "signature") || strings.Contains(value, "unauthorized") {
		return resource.ErrAuthenticationFailed
	}
	return nil
}
