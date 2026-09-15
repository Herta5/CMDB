// 本文件在资源写入前确定同步终态，只允许完整、无重复的类型结果进入生命周期事务。
package resource

import (
	"context"
	"errors"
	"strings"
)

// SyncDecision 保存经完整性校验的成功类型、初始统计和可公开摘要；不携带整体云错误。
type SyncDecision struct {
	Status       string
	Successful   []CollectionResult
	Statistics   map[string]map[string]int
	ErrorSummary string
}

// DecideSyncResult 以平台声明的预期类型为准，结构无效或整体失败时丢弃全部局部结果。
func DecideSyncResult(expectedTypes []string, results []CollectionResult, collectErr error) SyncDecision {
	invalid := SyncDecision{Status: "failed", ErrorSummary: "采集结果不完整或类型重复"}
	if collectErr != nil {
		return SyncDecision{Status: "failed", ErrorSummary: safeCollectionError(collectErr)}
	}
	if len(expectedTypes) == 0 || len(results) != len(expectedTypes) {
		return invalid
	}
	expected := make(map[string]struct{}, len(expectedTypes))
	for _, resourceType := range expectedTypes {
		if _, exists := expected[resourceType]; exists || resourceType == "" {
			return invalid
		}
		expected[resourceType] = struct{}{}
	}
	byType := make(map[string]CollectionResult, len(results))
	for _, result := range results {
		if _, ok := expected[result.ResourceType]; !ok {
			return invalid
		}
		if _, exists := byType[result.ResourceType]; exists {
			return invalid
		}
		byType[result.ResourceType] = result
	}
	decision := SyncDecision{Status: "success", Statistics: make(map[string]map[string]int, len(results))}
	summaries := make([]string, 0)
	// 使用适配器顺序保证统计分类与写入顺序不受云端结果顺序影响。
	for _, resourceType := range expectedTypes {
		result := byType[resourceType]
		counts := map[string]int{"added": 0, "updated": 0, "restored": 0, "lost": 0, "deleted": 0, "failed": 0}
		if result.Err != nil {
			counts["failed"] = 1
			summaries = append(summaries, resourceType+"："+safeCollectionError(result.Err))
		} else {
			decision.Successful = append(decision.Successful, result)
		}
		decision.Statistics[resourceType] = counts
	}
	if len(decision.Successful) == 0 {
		decision.Status = "failed"
	} else if len(summaries) > 0 {
		decision.Status = "partial_success"
	}
	decision.ErrorSummary = strings.Join(summaries, "；")
	return decision
}

// safeCollectionError 只使用有限领域分类，不将 SDK 文本、请求正文或凭证片段带入任务和审计。
func safeCollectionError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "同步执行超时，可重新执行"
	case errors.Is(err, context.Canceled):
		return "同步执行已中断，可重新执行"
	case errors.Is(err, ErrCloudAuthentication):
		return "凭证认证失败"
	case errors.Is(err, ErrCloudPermission):
		return "云账号权限不足，请授予资源只读权限"
	case errors.Is(err, ErrCloudNetwork):
		return "网络连接失败"
	default:
		return "资源采集失败"
	}
}
