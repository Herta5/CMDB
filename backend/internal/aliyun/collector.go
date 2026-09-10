// 本文件使用阿里云官方 SDK 采集 ECS、RDS 和负载均衡资源。
package aliyun

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"

	"cmdb/internal/resource"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/rds"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/slb"
)

// Collector 是阿里云独立平台采集器。
type Collector struct{}

// NewCollector 创建阿里云采集器。
func NewCollector() *Collector { return &Collector{} }

type credential struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
}

// ecsProbeAPI 约束连接测试只调用 ECS 列表首页，便于使用模拟响应验证轻量行为。
type ecsProbeAPI interface {
	DescribeInstances(*ecs.DescribeInstancesRequest) (*ecs.DescribeInstancesResponse, error)
}

// rdsProbeAPI 约束连接测试只调用 RDS 列表首页。
type rdsProbeAPI interface {
	DescribeDBInstances(*rds.DescribeDBInstancesRequest) (*rds.DescribeDBInstancesResponse, error)
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
	ecsItems, ecsErr := collectECS(ecsClient, source.Region)
	if accessErr := classifyAliyunAccessError(ecsErr); accessErr != nil {
		return nil, accessErr
	}
	rdsItems, networks, rdsErr := collectRDS(rdsClient)
	if accessErr := classifyAliyunAccessError(rdsErr); accessErr != nil {
		return nil, accessErr
	}
	slbItems, ports, slbErr := collectSLB(slbClient, source.Region)
	if accessErr := classifyAliyunAccessError(slbErr); accessErr != nil {
		return nil, accessErr
	}
	results := []resource.CollectionResult{{ResourceType: "ecs", Snapshots: ecsSnapshots(ecsItems), Err: ecsErr}, {ResourceType: "rds", Snapshots: rdsSnapshots(rdsItems, networks), Err: rdsErr}, {ResourceType: "slb", Snapshots: slbSnapshots(slbItems, ports), Err: slbErr}}
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

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rdsRequest := rds.CreateDescribeDBInstancesRequest()
	rdsRequest.PageNumber = "1"
	rdsRequest.PageSize = "1"
	_, rdsErr := rdsClient.DescribeDBInstances(rdsRequest)

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

func collectECS(client *ecs.Client, region string) ([]ecs.Instance, error) {
	values := []ecs.Instance{}
	for page := 1; ; page++ {
		request := ecs.CreateDescribeInstancesRequest()
		request.RegionId = region
		request.PageNumber = requests.Integer(strconv.Itoa(page))
		request.PageSize = "100"
		response, err := client.DescribeInstances(request)
		if err != nil {
			return values, err
		}
		values = append(values, response.Instances.Instance...)
		if page*response.PageSize >= response.TotalCount || len(response.Instances.Instance) == 0 {
			return values, nil
		}
	}
}
func collectRDS(client *rds.Client) ([]rds.DBInstance, map[string][]rds.DBInstanceNetInfo, error) {
	values := []rds.DBInstance{}
	networks := map[string][]rds.DBInstanceNetInfo{}
	for page := 1; ; page++ {
		request := rds.CreateDescribeDBInstancesRequest()
		request.PageNumber = requests.Integer(strconv.Itoa(page))
		request.PageSize = "100"
		response, err := client.DescribeDBInstances(request)
		if err != nil {
			return values, networks, err
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
			return values, networks, err
		}
		networks[instance.DBInstanceId] = response.DBInstanceNetInfos.DBInstanceNetInfo
	}
	return values, networks, nil
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

func ecsSnapshots(instances []ecs.Instance) []resource.Snapshot {
	values := []resource.Snapshot{}
	for _, instance := range instances {
		raw, _ := json.Marshal(instance)
		value := resource.Snapshot{ResourceType: "ecs", ExternalID: instance.InstanceId, Name: instance.InstanceName, Region: instance.RegionId, Zone: instance.ZoneId, CloudStatus: instance.Status, RawAttributes: raw}
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
		values = append(values, value)
	}
	return values
}
func rdsSnapshots(instances []rds.DBInstance, networks map[string][]rds.DBInstanceNetInfo) []resource.Snapshot {
	values := []resource.Snapshot{}
	for _, instance := range instances {
		raw, _ := json.Marshal(instance)
		value := resource.Snapshot{ResourceType: "rds", ExternalID: instance.DBInstanceId, Name: instance.DBInstanceDescription, Region: instance.RegionId, Zone: instance.ZoneId, CloudStatus: instance.DBInstanceStatus, Engine: instance.Engine, EngineVersion: instance.EngineVersion, RawAttributes: raw}
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
