// 本文件定义所有云平台采集模块必须实现的统一快照契约。
package resource

import (
	"context"
	"errors"
)

// ErrAuthenticationFailed 表示接入凭证无效，公共同步服务据此保护现有资源状态。
var ErrAuthenticationFailed = errors.New("接入源认证失败")

// Snapshot 是平台采集器输出的单个云端资源事实。
type Snapshot struct {
	ResourceType  string
	ExternalID    string
	Name          string
	Region        string
	Zone          string
	CloudStatus   string
	RawAttributes []byte
	Endpoints     []EndpointSnapshot
}

// EndpointSnapshot 表示采集时观察到的原始地址和动态解析结果。
type EndpointSnapshot struct {
	Kind        string
	Address     string
	Port        int
	Protocol    string
	ResolvedIPs []string
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
}
