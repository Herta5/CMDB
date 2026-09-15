// 本文件把来源读取与已装配平台能力连接起来，HTTP 层不访问服务内部适配器。
package resource

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrSourceReadFailed 区分动作执行前读取失败，调用方不得伪装成云调用失败。
	ErrSourceReadFailed = errors.New("接入源读取失败")
	// ErrCollectorUnavailable 表示接入平台能力未装配。
	ErrCollectorUnavailable = errors.New("平台采集器暂不可用")
)

// SyncSource 在项目归属校验后使用当前平台适配器受理手工同步。
func (s *Service) SyncSource(ctx context.Context, projectID, sourceID uint64) (*SyncJob, error) {
	source, err := s.FindSourceForProject(ctx, projectID, sourceID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceReadFailed, err)
	}
	collector := s.adapters[source.Provider]
	if collector == nil {
		return nil, ErrCollectorUnavailable
	}
	return s.EnqueueSync(ctx, source.ID, "manual", collector)
}

// ProbeSource 在项目归属校验后调用轻量探测，不改变资产生命周期。
func (s *Service) ProbeSource(ctx context.Context, projectID, sourceID uint64) (*ConnectionTestResult, error) {
	source, err := s.FindSourceForProject(ctx, projectID, sourceID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceReadFailed, err)
	}
	collector := s.adapters[source.Provider]
	if collector == nil {
		return nil, ErrCollectorUnavailable
	}
	return s.TestConnection(ctx, projectID, sourceID, collector)
}
