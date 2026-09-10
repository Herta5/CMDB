// 本文件提供三类资产表的统一路由和行模型，避免同步与生命周期逻辑重复。
package resource

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetRow 覆盖三张表的字段超集，仅在资源核心内部用于统一读写。
type assetRow struct {
	AssetBase
	PrivateIPs json.RawMessage `gorm:"type:json"`
	PublicIPs  json.RawMessage `gorm:"type:json"`
	Endpoints  json.RawMessage `gorm:"type:json"`
}

// assetTableForType 将受支持的云产品路由到固定表名，表名不会来自外部输入。
func assetTableForType(resourceType string) (string, error) {
	switch strings.ToLower(resourceType) {
	case "ecs", "ec2":
		return "resources_servers", nil
	case "rds":
		return "resources_databases", nil
	case "slb", "elb", "alb", "nlb":
		return "resources_load_balancers", nil
	default:
		return "", fmt.Errorf("不支持的资源类型：%s", resourceType)
	}
}

// endpointColumns 把采集端点转换为目标表的 JSON 字段。
func endpointColumns(table string, endpoints []EndpointSnapshot) map[string]any {
	if table != "resources_servers" {
		encoded, _ := json.Marshal(endpoints)
		return map[string]any{"endpoints": encoded}
	}
	privateIPs, publicIPs := make([]string, 0), make([]string, 0)
	for _, endpoint := range endpoints {
		if endpoint.Kind == "private" {
			privateIPs = append(privateIPs, endpoint.Address)
		} else if endpoint.Kind == "public" {
			publicIPs = append(publicIPs, endpoint.Address)
		}
	}
	privateJSON, _ := json.Marshal(privateIPs)
	publicJSON, _ := json.Marshal(publicIPs)
	return map[string]any{"private_ips": privateJSON, "public_ips": publicJSON}
}

// resourceFromRow 将各表的地址字段还原为稳定的统一 API 视图。
func resourceFromRow(row assetRow, table string) Resource {
	endpoints := make([]EndpointSnapshot, 0)
	if table == "resources_servers" {
		var privateIPs, publicIPs []string
		_ = json.Unmarshal(row.PrivateIPs, &privateIPs)
		_ = json.Unmarshal(row.PublicIPs, &publicIPs)
		for _, address := range privateIPs {
			endpoints = append(endpoints, EndpointSnapshot{Kind: "private", Address: address})
		}
		for _, address := range publicIPs {
			endpoints = append(endpoints, EndpointSnapshot{Kind: "public", Address: address})
		}
	} else if len(row.Endpoints) > 0 {
		_ = json.Unmarshal(row.Endpoints, &endpoints)
	}
	return Resource{AssetBase: row.AssetBase, Endpoints: endpoints}
}
