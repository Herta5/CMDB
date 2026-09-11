// 本文件承载审计查询参数校验，HTTP 和领域仓储共享同一边界。
package audit

import (
	"context"
	"errors"
)

// maxAuditPage 将最大 SQL offset 限制在可控范围，避免极端页码造成整数溢出或无效深翻页。
const maxAuditPage = 1_000_000

var (
	// ErrInvalidFilter 表示时间或分页等审计查询条件无效。
	ErrInvalidFilter = errors.New("审计查询参数无效")
	// ErrRepositoryUnavailable 表示审计仓储未正确装配。
	ErrRepositoryUnavailable = errors.New("审计仓储不可用")
)

// Service 协调审计查询参数与持久化实现。
type Service struct {
	repository *Repository
}

// NewService 创建只读审计查询服务。
func NewService(repository *Repository) *Service { return &Service{repository: repository} }

// List 校验分页和时间边界后返回稳定响应。
func (s *Service) List(ctx context.Context, filter Filter) (Page, error) {
	if filter.Page < 1 || filter.Page > maxAuditPage || filter.PageSize < 1 || filter.PageSize > 100 || (filter.StartAt != nil && filter.EndAt != nil && filter.StartAt.After(*filter.EndAt)) {
		return Page{}, ErrInvalidFilter
	}
	if s == nil || s.repository == nil {
		return Page{}, ErrRepositoryUnavailable
	}
	items, total, snapshotID, err := s.repository.List(ctx, filter)
	if err != nil {
		return Page{}, err
	}
	if items == nil {
		items = []Log{}
	}
	return Page{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize, SnapshotID: snapshotID}, nil
}
