// 本文件负责阿里云四类负载均衡的独立分页采集和官方产品快照转换。
package aliyun

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"cmdb/internal/resource"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/alb"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/gwlb"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/nlb"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/slb"
)

// slbCollectAPI 保证传统负载均衡只有列表和监听详情均成功后才能输出快照。
type slbCollectAPI interface {
	slbProbeAPI
	DescribeLoadBalancerAttribute(*slb.DescribeLoadBalancerAttributeRequest) (*slb.DescribeLoadBalancerAttributeResponse, error)
}

// albProbeAPI 将应用型负载均衡探测限定为列表首页。
type albProbeAPI interface {
	ListLoadBalancers(*alb.ListLoadBalancersRequest) (*alb.ListLoadBalancersResponse, error)
}

// albCollectAPI 增加完整同步所需的监听器读取权限。
type albCollectAPI interface {
	albProbeAPI
	ListListeners(*alb.ListListenersRequest) (*alb.ListListenersResponse, error)
}

// nlbProbeAPI 将网络型负载均衡探测限定为列表首页。
type nlbProbeAPI interface {
	ListLoadBalancers(*nlb.ListLoadBalancersRequest) (*nlb.ListLoadBalancersResponse, error)
}

// nlbCollectAPI 增加完整同步所需的监听器读取权限。
type nlbCollectAPI interface {
	nlbProbeAPI
	ListListeners(*nlb.ListListenersRequest) (*nlb.ListListenersResponse, error)
}

// gwlbProbeAPI 只读取网关型负载均衡列表，不假设其存在监听器 API。
type gwlbProbeAPI interface {
	ListLoadBalancers(*gwlb.ListLoadBalancersRequest) (*gwlb.ListLoadBalancersResponse, error)
}

// gwlbCollectAPI 与轻量探测共用列表接口，完整同步另行遍历全部分页。
type gwlbCollectAPI interface{ gwlbProbeAPI }

// readLoadBalancerPages 只有空令牌表示成功结束；检测所有已用令牌，防止跨页循环被当作完整采集。
func readLoadBalancerPages[T any](read func(string) ([]T, string, error)) ([]T, error) {
	values := []T{}
	seen := map[string]bool{}
	for token := ""; ; {
		seen[token] = true
		items, next, err := read(token)
		if err != nil {
			return nil, err
		}
		values = append(values, items...)
		if next == "" {
			return values, nil
		}
		if seen[next] {
			return nil, errors.New("负载均衡分页令牌重复，采集未完成")
		}
		token = next
	}
}

// collectSLB 按页获取传统负载均衡，并为每个实例读取实际监听器。
func collectSLB(client slbCollectAPI, region string) ([]slb.LoadBalancer, map[string][]slb.ListenerPortAndProtocol, error) {
	values := []slb.LoadBalancer{}
	ports := map[string][]slb.ListenerPortAndProtocol{}
	totalCount := 0
	for page := 1; ; page++ {
		request := slb.CreateDescribeLoadBalancersRequest()
		request.RegionId = region
		request.PageNumber = requests.Integer(strconv.Itoa(page))
		request.PageSize = "100"
		response, err := client.DescribeLoadBalancers(request)
		if err != nil {
			return nil, nil, err
		}
		if response == nil {
			return nil, nil, errors.New("SLB 列表响应为空")
		}
		items := response.LoadBalancers.LoadBalancer
		values = append(values, items...)
		if response.TotalCount > totalCount {
			totalCount = response.TotalCount
		}
		if len(values) >= totalCount {
			break
		}
		// 云端声明仍有资源却提前返回空页时，必须丢弃局部列表，避免错误推进该类型生命周期。
		if len(items) == 0 {
			return nil, nil, errors.New("SLB 列表分页未完整返回")
		}
	}
	for _, item := range values {
		request := slb.CreateDescribeLoadBalancerAttributeRequest()
		request.LoadBalancerId = item.LoadBalancerId
		response, err := client.DescribeLoadBalancerAttribute(request)
		if err != nil {
			return nil, nil, err
		}
		if response == nil {
			return nil, nil, errors.New("SLB 监听器响应为空")
		}
		ports[item.LoadBalancerId] = response.ListenerPortsAndProtocol.ListenerPortAndProtocol
	}
	return values, ports, nil
}

// collectALB 独立遍历实例与各实例监听器；任一步失败都不输出局部快照。
func collectALB(client albCollectAPI) ([]alb.LoadBalancer, map[string][]alb.Listener, error) {
	values, err := readLoadBalancerPages(func(token string) ([]alb.LoadBalancer, string, error) {
		request := alb.CreateListLoadBalancersRequest()
		request.MaxResults, request.NextToken = "100", token
		response, err := client.ListLoadBalancers(request)
		if err != nil {
			return nil, "", err
		}
		if response == nil {
			return nil, "", errors.New("ALB 列表响应为空")
		}
		return response.LoadBalancers, response.NextToken, nil
	})
	if err != nil {
		return nil, nil, err
	}
	listeners := map[string][]alb.Listener{}
	for _, item := range values {
		items, err := readLoadBalancerPages(func(token string) ([]alb.Listener, string, error) {
			request := alb.CreateListListenersRequest()
			ids := []string{item.LoadBalancerId}
			request.LoadBalancerIds = &ids
			request.MaxResults, request.NextToken = "100", token
			response, err := client.ListListeners(request)
			if err != nil {
				return nil, "", err
			}
			if response == nil {
				return nil, "", errors.New("ALB 监听器响应为空")
			}
			return response.Listeners, response.NextToken, nil
		})
		if err != nil {
			return nil, nil, err
		}
		listeners[item.LoadBalancerId] = items
	}
	return values, listeners, nil
}

// collectNLB 独立遍历网络型负载均衡和监听器，失败时保留同步前完整快照。
func collectNLB(client nlbCollectAPI) ([]nlb.LoadbalancerInfo, map[string][]nlb.ListenerInfo, error) {
	values, err := readLoadBalancerPages(func(token string) ([]nlb.LoadbalancerInfo, string, error) {
		request := nlb.CreateListLoadBalancersRequest()
		request.MaxResults, request.NextToken = "100", token
		response, err := client.ListLoadBalancers(request)
		if err != nil {
			return nil, "", err
		}
		if response == nil {
			return nil, "", errors.New("NLB 列表响应为空")
		}
		if err := validateNLBResponse(response.Success, response.Code, response.HttpStatusCode); err != nil {
			return nil, "", err
		}
		return response.LoadBalancers, response.NextToken, nil
	})
	if err != nil {
		return nil, nil, err
	}
	listeners := map[string][]nlb.ListenerInfo{}
	for _, item := range values {
		items, err := readLoadBalancerPages(func(token string) ([]nlb.ListenerInfo, string, error) {
			request := nlb.CreateListListenersRequest()
			ids := []string{item.LoadBalancerId}
			request.LoadBalancerIds = &ids
			request.MaxResults, request.NextToken = "100", token
			response, err := client.ListListeners(request)
			if err != nil {
				return nil, "", err
			}
			if response == nil {
				return nil, "", errors.New("NLB 监听器响应为空")
			}
			if err := validateNLBResponse(response.Success, response.Code, response.HttpStatusCode); err != nil {
				return nil, "", err
			}
			return response.Listeners, response.NextToken, nil
		})
		if err != nil {
			return nil, nil, err
		}
		listeners[item.LoadBalancerId] = items
	}
	return values, listeners, nil
}

// validateNLBResponse 防止 SDK 将 HTTP 2xx 内的业务失败当作成功空页；只用业务码和状态分类，不接收或回显 Message。
func validateNLBResponse(success bool, code string, status int) error {
	if classified := classifyAliyunAccessError(errors.New(code)); classified != nil {
		return classified
	}
	if status == 401 {
		return resource.ErrAuthenticationFailed
	}
	if status == 403 {
		return resource.ErrPermissionDenied
	}
	validCode := code == "" || code == "200" || strings.EqualFold(code, "success")
	validStatus := status == 0 || (status >= 200 && status < 300)
	if !success || !validCode || !validStatus {
		return errors.New("NLB API 返回业务失败")
	}
	return nil
}

// collectGWLB 只遍历网关型负载均衡列表，地址直接来自 Zone Mapping。
func collectGWLB(client gwlbCollectAPI) ([]gwlb.Data, error) {
	return readLoadBalancerPages(func(token string) ([]gwlb.Data, string, error) {
		request := gwlb.CreateListLoadBalancersRequest()
		request.MaxResults, request.NextToken = "100", token
		response, err := client.ListLoadBalancers(request)
		if err != nil {
			return nil, "", err
		}
		if response == nil {
			return nil, "", errors.New("GWLB 列表响应为空")
		}
		return response.LoadBalancers, response.NextToken, nil
	})
}

// appendLoadBalancerEndpoint 以地址、端口及协议去重，保留同端口的 TCP/UDP 等不同监听。
func appendLoadBalancerEndpoint(values []resource.EndpointSnapshot, kind, address string, port int, protocol string) []resource.EndpointSnapshot {
	if address == "" {
		return values
	}
	protocol = strings.ToLower(protocol)
	for _, value := range values {
		if value.Kind == kind && value.Address == address && value.Port == port && value.Protocol == protocol {
			return values
		}
	}
	return append(values, resource.EndpointSnapshot{Kind: kind, Address: address, Port: port, Protocol: protocol})
}

func loadBalancerEndpointKind(addressType string) string {
	if strings.EqualFold(addressType, "internet") {
		return "public"
	}
	return "private"
}

// slbSnapshots 保留传统负载均衡官方身份，原始属性只使用列表项。
func slbSnapshots(loadBalancers []slb.LoadBalancer, portMaps ...map[string][]slb.ListenerPortAndProtocol) []resource.Snapshot {
	ports := map[string][]slb.ListenerPortAndProtocol{}
	if len(portMaps) > 0 {
		ports = portMaps[0]
	}
	values := []resource.Snapshot{}
	for _, item := range loadBalancers {
		raw, _ := json.Marshal(item)
		kind := loadBalancerEndpointKind(item.AddressType)
		endpoints := []resource.EndpointSnapshot{}
		if len(ports[item.LoadBalancerId]) == 0 {
			endpoints = appendLoadBalancerEndpoint(endpoints, kind, item.Address, 0, "")
		}
		for _, listener := range ports[item.LoadBalancerId] {
			endpoints = appendLoadBalancerEndpoint(endpoints, kind, item.Address, listener.ListenerPort, listener.ListenerProtocol)
		}
		values = append(values, resource.Snapshot{ResourceType: "slb", ExternalID: item.LoadBalancerId, Name: item.LoadBalancerName, Region: item.RegionId, Zone: item.MasterZoneId, CloudStatus: item.LoadBalancerStatus, NetworkType: item.AddressType, RawAttributes: raw, Endpoints: endpoints})
	}
	return values
}

// albSnapshots 列表不提供地域，使用接入源地域；无监听器时只保留 DNS 地址。
func albSnapshots(items []alb.LoadBalancer, listeners map[string][]alb.Listener, region string) []resource.Snapshot {
	values := []resource.Snapshot{}
	for _, item := range items {
		raw, _ := json.Marshal(item)
		value := resource.Snapshot{ResourceType: "alb", ExternalID: item.LoadBalancerId, Name: item.LoadBalancerName, Region: region, CloudStatus: item.LoadBalancerStatus, NetworkType: item.AddressType, RawAttributes: raw}
		kind := loadBalancerEndpointKind(item.AddressType)
		if len(listeners[item.LoadBalancerId]) == 0 {
			value.Endpoints = appendLoadBalancerEndpoint(value.Endpoints, kind, item.DNSName, 0, "")
		}
		for _, listener := range listeners[item.LoadBalancerId] {
			value.Endpoints = appendLoadBalancerEndpoint(value.Endpoints, kind, item.DNSName, listener.ListenerPort, listener.ListenerProtocol)
		}
		values = append(values, value)
	}
	return values
}

// nlbSnapshots 使用实际 DNS 和监听配置，列表地域缺省时继承接入源地域。
func nlbSnapshots(items []nlb.LoadbalancerInfo, listeners map[string][]nlb.ListenerInfo, region string) []resource.Snapshot {
	values := []resource.Snapshot{}
	for _, item := range items {
		raw, _ := json.Marshal(item)
		value := resource.Snapshot{ResourceType: "nlb", ExternalID: item.LoadBalancerId, Name: item.LoadBalancerName, Region: item.RegionId, CloudStatus: item.LoadBalancerStatus, NetworkType: item.AddressType, RawAttributes: raw}
		if len(item.ZoneMappings) > 0 {
			value.Zone = item.ZoneMappings[0].ZoneId
		}
		if value.Region == "" {
			value.Region = region
		}
		kind := loadBalancerEndpointKind(item.AddressType)
		if len(listeners[item.LoadBalancerId]) == 0 {
			value.Endpoints = appendLoadBalancerEndpoint(value.Endpoints, kind, item.DNSName, 0, "")
		}
		for _, listener := range listeners[item.LoadBalancerId] {
			value.Endpoints = appendLoadBalancerEndpoint(value.Endpoints, kind, item.DNSName, listener.ListenerPort, listener.ListenerProtocol)
		}
		values = append(values, value)
	}
	return values
}

// gwlbSnapshots 只将 Zone Mapping 的实际私网 IPv4 转为端点，不虚构端口、协议或 DNS。
func gwlbSnapshots(items []gwlb.Data, region string) []resource.Snapshot {
	values := []resource.Snapshot{}
	for _, item := range items {
		raw, _ := json.Marshal(item)
		value := resource.Snapshot{ResourceType: "gwlb", ExternalID: item.LoadBalancerId, Name: item.LoadBalancerName, Region: item.RegionId, Zone: item.ZoneId, CloudStatus: item.LoadBalancerStatus, NetworkType: "intranet", RawAttributes: raw}
		if value.Zone == "" && len(item.ZoneMappings) > 0 {
			value.Zone = item.ZoneMappings[0].ZoneId
		}
		if value.Region == "" {
			value.Region = region
		}
		for _, zone := range item.ZoneMappings {
			for _, address := range zone.LoadBalancerAddresses {
				value.Endpoints = appendLoadBalancerEndpoint(value.Endpoints, "private", address.PrivateIpv4Address, 0, "")
			}
		}
		values = append(values, value)
	}
	return values
}
