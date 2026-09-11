// 本文件定义所有云平台采集模块必须实现的统一快照契约。
package resource

import (
	"context"
	"errors"
)

var (
	// ErrInvalidProviderCredential 表示凭证 JSON 不符合平台字段和非空约束。
	ErrInvalidProviderCredential = errors.New("云平台凭证格式无效")
	// ErrInvalidProviderConfig 表示非敏感配置包含未定义字段或敏感信息。
	ErrInvalidProviderConfig = errors.New("云平台配置无效")
	// ErrCloudAuthentication 表示云平台拒绝凭证认证，不携带原始响应。
	ErrCloudAuthentication = errors.New("接入源认证失败")
	// ErrCloudPermission 表示凭证有效但缺少云资源只读权限。
	ErrCloudPermission = errors.New("云账号权限不足")
	// ErrCloudNetwork 表示云平台轻量调用遇到网络故障。
	ErrCloudNetwork = errors.New("网络连接失败")
)

// 旧名称保留为同一错误实例，确保既有同步和 HTTP 分类在迁移适配器时不改变语义。
var (
	ErrAuthenticationFailed = ErrCloudAuthentication
	ErrPermissionDenied     = ErrCloudPermission
)

// Snapshot 是平台采集器输出的单个云端资源事实。
type Snapshot struct {
	ResourceType  string
	ExternalID    string
	Name          string
	Region        string
	Zone          string
	CloudStatus   string
	Engine        string
	EngineVersion string
	NetworkType   string
	RawAttributes []byte
	// VolatileRawAttributeKeys 声明原始 JSON 中只用于观测、不得触发配置更新统计的顶层键；原始值仍会持久化。
	VolatileRawAttributeKeys []string
	Endpoints                []EndpointSnapshot
}

// EndpointSnapshot 表示采集时观察到的原始地址和动态解析结果。
type EndpointSnapshot struct {
	Kind        string   `json:"kind"`
	Address     string   `json:"address"`
	Port        int      `json:"port"`
	Protocol    string   `json:"protocol"`
	ResolvedIPs []string `json:"resolved_ips"`
}

// CollectionResult 按资源类型隔离成功与失败，失败类型不得触发失联判断。
type CollectionResult struct {
	ResourceType string
	Snapshots    []Snapshot
	Err          error
}

// Collector 由阿里云和 AWS 模块分别实现。
type Collector interface {
	Collect(context.Context, Source, []byte) ([]CollectionResult, error)
	// Probe 只验证各资源 API 的认证、权限和网络可达性，不得遍历或返回完整资产。
	Probe(context.Context, Source, []byte) ([]CollectionResult, error)
}
