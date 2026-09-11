// 本文件验证阿里云账号身份识别只接受严格凭证，并将 STS 失败收敛为安全领域错误。
package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"cmdb/internal/resource"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/sts"
)

// stsIdentityStub 使用模拟 STS 响应验证账号识别，不向真实云账号发起请求。
type stsIdentityStub struct {
	calls    int
	response *sts.GetCallerIdentityResponse
	err      error
}

func (s *stsIdentityStub) GetCallerIdentity(*sts.GetCallerIdentityRequest) (*sts.GetCallerIdentityResponse, error) {
	s.calls++
	return s.response, s.err
}

// TestAliyunCredentialValidationRejectsUnexpectedFields 防止凭证缺字段、未知字段或空字段绕过加密前校验。
func TestAliyunCredentialValidationRejectsUnexpectedFields(t *testing.T) {
	collector := NewCollector()
	if err := collector.ValidateCredential(json.RawMessage(`{"access_key_id":"test-id","access_key_secret":"test-secret"}`)); err != nil {
		t.Fatalf("完整阿里云凭证必须通过校验：%v", err)
	}
	for _, value := range []json.RawMessage{
		json.RawMessage(`{"access_key_id":"test-id"}`),
		json.RawMessage(`{"access_key_id":"test-id","access_key_secret":"test-secret","unknown":"value"}`),
		json.RawMessage(`{"access_key_id":"test-id","access_key_secret":" "}`),
	} {
		if err := collector.ValidateCredential(value); !errors.Is(err, resource.ErrInvalidProviderCredential) {
			t.Fatalf("不完整或包含未知字段的凭证必须被拒绝：%v", err)
		}
	}
}

// TestAliyunConfigValidationOnlyAllowsEmptyObject 防止阿里云非敏感配置提前承载未定义或敏感字段。
func TestAliyunConfigValidationOnlyAllowsEmptyObject(t *testing.T) {
	collector := NewCollector()
	for _, value := range []json.RawMessage{nil, json.RawMessage(`{}`)} {
		if err := collector.ValidateConfig(value); err != nil {
			t.Fatalf("空配置必须通过校验：%v", err)
		}
	}
	if err := collector.ValidateConfig(json.RawMessage(`{"region":"cn-hangzhou"}`)); !errors.Is(err, resource.ErrInvalidProviderConfig) {
		t.Fatalf("非空配置必须被拒绝：%v", err)
	}
}

// TestAliyunResourceTypesKeepsInitialScope 防止平台模块超出首期 ECS、RDS 和 SLB 的采集边界。
func TestAliyunResourceTypesKeepsInitialScope(t *testing.T) {
	values := NewCollector().ResourceTypes()
	if len(values) != 3 || values[0] != "ecs" || values[1] != "rds" || values[2] != "slb" {
		t.Fatalf("阿里云首期资源类型必须固定为 ecs、rds、slb：%v", values)
	}
}

// TestAliyunResolveCloudAccountIDClassifiesSTSFailures 防止 STS 原始错误或空账号标识进入同步结果与审计。
func TestAliyunResolveCloudAccountIDClassifiesSTSFailures(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		response  *sts.GetCallerIdentityResponse
		stsErr    error
		wantID    string
		wantErr   error
		wantCalls int
	}{
		{name: "返回账号标识", response: &sts.GetCallerIdentityResponse{AccountId: "100000000001"}, wantID: "100000000001", wantCalls: 1},
		{name: "空账号标识", response: &sts.GetCallerIdentityResponse{}, wantErr: resource.ErrCloudAuthentication, wantCalls: 1},
		{name: "认证失败", stsErr: errors.New("InvalidAccessKeyId.NotFound"), wantErr: resource.ErrCloudAuthentication, wantCalls: 1},
		{name: "权限不足", stsErr: errors.New("Forbidden.RAM: User not authorized"), wantErr: resource.ErrCloudPermission, wantCalls: 1},
		{name: "网络失败", stsErr: errors.New("dial tcp: network is unreachable"), wantErr: resource.ErrCloudNetwork, wantCalls: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stub := &stsIdentityStub{response: testCase.response, err: testCase.stsErr}
			collector := &Collector{newSTSClient: func(region, accessKeyID, accessKeySecret string) (aliyunIdentityAPI, error) {
				if region != "cn-hangzhou" || accessKeyID != "test-id" || accessKeySecret != "test-secret" {
					t.Fatal("STS 客户端必须使用接入源区域和已校验凭证")
				}
				return stub, nil
			}}

			accountID, err := collector.ResolveCloudAccountID(context.Background(), resource.Source{Region: "cn-hangzhou"}, []byte(`{"access_key_id":"test-id","access_key_secret":"test-secret"}`))
			if accountID != testCase.wantID || !errors.Is(err, testCase.wantErr) {
				t.Fatalf("账号识别结果错误：账号=%q 错误=%v", accountID, err)
			}
			if stub.calls != testCase.wantCalls {
				t.Fatalf("身份 API 调用次数错误：got=%d want=%d", stub.calls, testCase.wantCalls)
			}
			if err != nil && strings.Contains(err.Error(), "test-id") {
				t.Fatalf("安全错误不得泄露虚构凭证标识：%v", err)
			}
		})
	}
}
