// 本文件使用 AWS 官方 SDK 采集 EC2、RDS 和 ELB，并转换为共享资源快照。
package aws

import (
	"context"
	"encoding/json"
	"errors"
	"net"
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
type Collector struct{}

// NewCollector 创建 AWS 采集器。
func NewCollector() *Collector { return &Collector{} }

type credential struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
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
	results := []resource.CollectionResult{}
	ec2Output, err := collectEC2(ctx, ec2.NewFromConfig(configuration))
	if authenticationError(err) {
		return nil, resource.ErrAuthenticationFailed
	}
	results = append(results, resource.CollectionResult{ResourceType: "ec2", Snapshots: ec2Snapshots(ec2Output, source.Region), Err: err})
	rdsOutput, err := collectRDS(ctx, rds.NewFromConfig(configuration))
	if authenticationError(err) {
		return nil, resource.ErrAuthenticationFailed
	}
	results = append(results, resource.CollectionResult{ResourceType: "rds", Snapshots: rdsSnapshots(rdsOutput, source.Region), Err: err})
	elbOutput, listeners, err := collectELB(ctx, elasticloadbalancingv2.NewFromConfig(configuration))
	if authenticationError(err) {
		return nil, resource.ErrAuthenticationFailed
	}
	results = append(results, resource.CollectionResult{ResourceType: "elb", Snapshots: elbSnapshots(elbOutput, listeners, source.Region), Err: err})
	for resultIndex := range results {
		for snapshotIndex := range results[resultIndex].Snapshots {
			resolveEndpoints(ctx, &results[resultIndex].Snapshots[snapshotIndex])
		}
	}
	return results, nil
}

func collectEC2(ctx context.Context, client *ec2.Client) (*ec2.DescribeInstancesOutput, error) {
	output := &ec2.DescribeInstancesOutput{}
	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return output, err
		}
		output.Reservations = append(output.Reservations, page.Reservations...)
	}
	return output, nil
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

func ec2Snapshots(output *ec2.DescribeInstancesOutput, region string) []resource.Snapshot {
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
			value := resource.Snapshot{ResourceType: "ec2", ExternalID: awssdk.ToString(instance.InstanceId), Name: tagName(instance.Tags), Region: region, CloudStatus: status, RawAttributes: raw}
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
		value := resource.Snapshot{ResourceType: "rds", ExternalID: awssdk.ToString(instance.DBInstanceIdentifier), Name: awssdk.ToString(instance.DBInstanceIdentifier), Region: region, Zone: awssdk.ToString(instance.AvailabilityZone), CloudStatus: awssdk.ToString(instance.DBInstanceStatus), Engine: awssdk.ToString(instance.Engine), EngineVersion: awssdk.ToString(instance.EngineVersion), RawAttributes: raw}
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
func authenticationError(err error) bool {
	if err == nil {
		return false
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		code := strings.ToLower(apiError.ErrorCode())
		return strings.Contains(code, "auth") || strings.Contains(code, "accessdenied") || strings.Contains(code, "invalidclienttoken") || strings.Contains(code, "signature")
	}
	return false
}
