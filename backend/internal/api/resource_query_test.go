// 本文件验证生成查询字段不会在领域参数转换时丢失。
package api

import (
	"cmdb/internal/api/generated"
	"testing"
)

func TestResourceQueryRetainsEngineAndNetworkType(t *testing.T) {
	engine, network := "PostgreSQL", "internet-facing"
	query := resourceQuery(generated.ListAllResourcesParams{Engine: &engine, NetworkType: &network})
	if query.Engine != engine || query.NetworkType != network {
		t.Fatal("资源类型专属筛选必须传给统一查询")
	}
}
