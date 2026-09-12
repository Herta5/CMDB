// 本文件用纯决策表验证采集完整性、同步终态和安全错误分类，不依赖数据库或云账号。
package resource

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestDecideSyncResult 防止空结果、缺类、重复类或整体错误中的局部快照被误认为成功。
func TestDecideSyncResult(t *testing.T) {
	for _, tc := range []struct {
		name       string
		expected   []string
		results    []CollectionResult
		err        error
		status     string
		successful int
		statistics int
		summary    string
	}{
		{name: "全成功", results: []CollectionResult{{ResourceType: "rds"}, {ResourceType: "ec2"}}, status: "success", successful: 2, statistics: 2},
		{name: "一成功一失败", results: []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "rds", Err: ErrCloudNetwork}}, status: "partial_success", successful: 1, statistics: 2, summary: "rds：网络连接失败"},
		{name: "全部类型失败", results: []CollectionResult{{ResourceType: "ec2", Err: ErrCloudAuthentication}, {ResourceType: "rds", Err: errors.New("虚构原始云响应")}}, status: "failed", statistics: 2, summary: "ec2：凭证认证失败；rds：资源采集失败"},
		{name: "空结果", status: "failed", summary: "采集结果不完整或类型重复"},
		{name: "缺少预期类型", results: []CollectionResult{{ResourceType: "ec2"}}, status: "failed", summary: "采集结果不完整或类型重复"},
		{name: "重复类型", results: []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "ec2"}}, status: "failed", summary: "采集结果不完整或类型重复"},
		{name: "额外类型", results: []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "rds"}, {ResourceType: "虚构敏感类型"}}, status: "failed", summary: "采集结果不完整或类型重复"},
		{name: "未知类型替换预期类型", results: []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "虚构敏感类型"}}, status: "failed", summary: "采集结果不完整或类型重复"},
		{name: "整体认证失败覆盖局部成功", results: []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "rds"}}, err: fmt.Errorf("虚构凭证载荷：%w", ErrCloudAuthentication), status: "failed", summary: "凭证认证失败"},
		{name: "整体网络失败", err: fmt.Errorf("虚构网络载荷：%w", ErrCloudNetwork), status: "failed", summary: "网络连接失败"},
		{name: "整体权限失败", err: ErrCloudPermission, status: "failed", summary: "云账号权限不足，请授予资源只读权限"},
		{name: "整体未知错误", err: errors.New("虚构原始云错误"), status: "failed", summary: "资源采集失败"},
		{name: "预期类型为空", expected: []string{}, status: "failed", summary: "采集结果不完整或类型重复"},
		{name: "预期类型重复", expected: []string{"ec2", "ec2"}, results: []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "rds"}}, status: "failed", summary: "采集结果不完整或类型重复"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expected := tc.expected
			if expected == nil {
				expected = []string{"ec2", "rds"}
			}
			decision := DecideSyncResult(expected, tc.results, tc.err)
			if decision.Status != tc.status || len(decision.Successful) != tc.successful || len(decision.Statistics) != tc.statistics {
				t.Fatalf("终态、成功结果或统计范围错误：%+v", decision)
			}
			if len(decision.Successful) == 0 && decision.Status != "failed" {
				t.Fatalf("没有成功类型时必须失败：%+v", decision)
			}
			if decision.ErrorSummary != tc.summary || strings.Contains(decision.ErrorSummary, "虚构") {
				t.Fatalf("错误摘要必须只保留安全分类：%s", decision.ErrorSummary)
			}
			for _, result := range tc.results {
				if counts, ok := decision.Statistics[result.ResourceType]; ok {
					failed := 0
					if result.Err != nil {
						failed = 1
					}
					if !reflect.DeepEqual(counts, map[string]int{"added": 0, "updated": 0, "restored": 0, "lost": 0, "deleted": 0, "failed": failed}) {
						t.Fatal("纯决策只允许记录类型失败，不得提前产生资产变更统计")
					}
				}
			}
			if tc.name == "全成功" && (decision.Successful[0].ResourceType != "ec2" || decision.Successful[1].ResourceType != "rds") {
				t.Fatal("成功结果应按平台声明顺序稳定处理")
			}
		})
	}
}
