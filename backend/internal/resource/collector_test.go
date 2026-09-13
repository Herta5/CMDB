// 本文件验证跨平台采集编排的整体认证失败和资源类型错误隔离规则。
package resource

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// TestCollectByTypeKeepsPermissionFailureLocal 防止一个资源类型权限不足时阻断后续类型采集。
func TestCollectByTypeKeepsPermissionFailureLocal(t *testing.T) {
	cloudPermission := errors.New("虚构云权限错误")
	rdsCalled := false
	results, err := CollectByType(context.Background(), []TypeCollection{
		{ResourceType: "ec2", Collect: func(context.Context) ([]Snapshot, error) { return nil, cloudPermission }},
		{ResourceType: "rds", Collect: func(context.Context) ([]Snapshot, error) {
			rdsCalled = true
			return []Snapshot{{ResourceType: "rds", ExternalID: "db-1"}}, nil
		}},
	}, func(err error) error {
		if errors.Is(err, cloudPermission) {
			return ErrCloudPermission
		}
		return nil
	})
	if err != nil || !rdsCalled || len(results) != 2 || !errors.Is(results[0].Err, ErrCloudPermission) || results[1].Err != nil || !reflect.DeepEqual(results[1].Snapshots, []Snapshot{{ResourceType: "rds", ExternalID: "db-1"}}) {
		t.Fatalf("权限错误必须只归入对应类型并继续采集：called=%v results=%+v err=%v", rdsCalled, results, err)
	}
}

// TestCollectByTypeStopsOnAuthenticationFailure 防止凭证失效后继续发起无意义的云 API 请求。
func TestCollectByTypeStopsOnAuthenticationFailure(t *testing.T) {
	cloudAuthentication := errors.New("虚构云认证错误")
	laterCalled := false
	results, err := CollectByType(context.Background(), []TypeCollection{
		{ResourceType: "ec2", Collect: func(context.Context) ([]Snapshot, error) { return nil, cloudAuthentication }},
		{ResourceType: "rds", Collect: func(context.Context) ([]Snapshot, error) { laterCalled = true; return nil, nil }},
	}, func(err error) error {
		if errors.Is(err, cloudAuthentication) {
			return ErrCloudAuthentication
		}
		return nil
	})
	if !errors.Is(err, ErrCloudAuthentication) || results != nil || laterCalled {
		t.Fatalf("认证失败必须整体终止采集：called=%v results=%+v err=%v", laterCalled, results, err)
	}
}

// TestCollectByTypeStopsOnDomainAuthenticationError 防止平台已返回统一认证错误时因分类器不识别而降级成单类型失败。
func TestCollectByTypeStopsOnDomainAuthenticationError(t *testing.T) {
	laterCalled := false
	results, err := CollectByType(context.Background(), []TypeCollection{
		{ResourceType: "ecs", Collect: func(context.Context) ([]Snapshot, error) { return nil, ErrCloudAuthentication }},
		{ResourceType: "rds", Collect: func(context.Context) ([]Snapshot, error) { laterCalled = true; return nil, nil }},
	}, func(error) error { return nil })
	if !errors.Is(err, ErrCloudAuthentication) || results != nil || laterCalled {
		t.Fatalf("统一认证错误必须直接整体终止采集：called=%v results=%+v err=%v", laterCalled, results, err)
	}
}
