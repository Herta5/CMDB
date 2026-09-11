// 本文件验证 AWS 账号身份识别只接受严格凭证，并将 STS 失败收敛为安全领域错误。
package aws

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"cmdb/internal/resource"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

// stsIdentityStub 使用模拟 STS 响应验证账号识别，不向真实云账号发起请求。
type stsIdentityStub struct {
	calls    int
	response *sts.GetCallerIdentityOutput
	err      error
}

func (s *stsIdentityStub) GetCallerIdentity(_ context.Context, _ *sts.GetCallerIdentityInput, _ ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	s.calls++
	return s.response, s.err
}

// TestAWSCredentialValidationAllowsOptionalSessionToken 防止缺字段、未知字段或空凭证绕过加密前校验。
func TestAWSCredentialValidationAllowsOptionalSessionToken(t *testing.T) {
	collector := NewCollector()
	for _, value := range []json.RawMessage{
		json.RawMessage(`{"access_key_id":"stub-access-key","secret_access_key":"stub-secret-key"}`),
		json.RawMessage(`{"access_key_id":"stub-access-key","secret_access_key":"stub-secret-key","session_token":"stub-session-token"}`),
	} {
		if err := collector.ValidateCredential(value); err != nil {
			t.Fatal("完整 AWS 凭证必须通过校验")
		}
	}
	for _, value := range []json.RawMessage{
		json.RawMessage(`{}`),
		json.RawMessage(`{"access_key_id":"stub-access-key"}`),
		json.RawMessage(`{"access_key_id":"stub-access-key","secret_access_key":"stub-secret-key","unknown":"value"}`),
		json.RawMessage(`{"access_key_id":"stub-access-key","secret_access_key":" "}`),
	} {
		if err := collector.ValidateCredential(value); !errors.Is(err, resource.ErrInvalidProviderCredential) {
			t.Fatal("不完整、未知或空白 AWS 凭证必须被拒绝")
		}
	}
}

// TestAWSConfigValidationOnlyAllowsEmptyObject 防止 AWS 非敏感配置提前承载未定义或敏感字段。
func TestAWSConfigValidationOnlyAllowsEmptyObject(t *testing.T) {
	collector := NewCollector()
	for _, value := range []json.RawMessage{nil, json.RawMessage(`{}`)} {
		if err := collector.ValidateConfig(value); err != nil {
			t.Fatal("空配置必须通过校验")
		}
	}
	if err := collector.ValidateConfig(json.RawMessage(`{"access_key_id":"stub-access-key"}`)); !errors.Is(err, resource.ErrInvalidProviderConfig) {
		t.Fatal("包含凭证字段的配置必须被拒绝")
	}
}

// TestAWSResourceTypesKeepsInitialScope 防止平台模块超出首期 EC2、RDS 和 ELB 的采集边界。
func TestAWSResourceTypesKeepsInitialScope(t *testing.T) {
	values := NewCollector().ResourceTypes()
	if len(values) != 3 || values[0] != "ec2" || values[1] != "rds" || values[2] != "elb" {
		t.Fatal("AWS 首期资源类型必须固定为 ec2、rds、elb")
	}
}

// TestAWSResolveCloudAccountIDClassifiesSTSFailures 防止原始 STS 错误或无效账号标识进入接入源和审计记录。
func TestAWSResolveCloudAccountIDClassifiesSTSFailures(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		response  *sts.GetCallerIdentityOutput
		stsErr    error
		wantID    string
		wantErr   error
		wantCalls int
	}{
		{name: "返回十二位账号标识", response: &sts.GetCallerIdentityOutput{Account: awssdk.String("100000000001")}, wantID: "100000000001", wantCalls: 1},
		{name: "空身份响应", wantErr: resource.ErrCloudAuthentication, wantCalls: 1},
		{name: "账号标识位数错误", response: &sts.GetCallerIdentityOutput{Account: awssdk.String("10000000001")}, wantErr: resource.ErrCloudAuthentication, wantCalls: 1},
		{name: "账号标识含非数字字符", response: &sts.GetCallerIdentityOutput{Account: awssdk.String("10000000000A")}, wantErr: resource.ErrCloudAuthentication, wantCalls: 1},
		{name: "认证失败", stsErr: &smithy.GenericAPIError{Code: "InvalidClientTokenId", Message: "上游认证失败正文"}, wantErr: resource.ErrCloudAuthentication, wantCalls: 1},
		{name: "权限不足", stsErr: &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "上游权限失败正文"}, wantErr: resource.ErrCloudPermission, wantCalls: 1},
		{name: "网络失败", stsErr: errors.New("上游网络失败正文"), wantErr: resource.ErrCloudNetwork, wantCalls: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stub := &stsIdentityStub{response: testCase.response, err: testCase.stsErr}
			collector := &Collector{newSTSClient: func(configuration awssdk.Config) awsIdentityAPI {
				if configuration.Region != "cn-north-1" {
					t.Fatal("STS 客户端必须使用接入源区域")
				}
				credential, err := configuration.Credentials.Retrieve(context.Background())
				if err != nil || credential.AccessKeyID != "stub-access-key" || credential.SecretAccessKey != "stub-secret-key" || credential.SessionToken != "stub-session-token" {
					t.Fatal("STS 客户端必须使用已校验的静态凭证和可选会话令牌")
				}
				return stub
			}}

			accountID, err := collector.ResolveCloudAccountID(context.Background(), resource.Source{Region: "cn-north-1"}, []byte(`{"access_key_id":"stub-access-key","secret_access_key":"stub-secret-key","session_token":"stub-session-token"}`))
			if accountID != testCase.wantID || !errors.Is(err, testCase.wantErr) {
				t.Fatal("AWS 账号识别必须返回账号标识或安全领域错误")
			}
			if stub.calls != testCase.wantCalls {
				t.Fatal("身份 API 调用次数不符合预期")
			}
			if testCase.stsErr != nil && err != nil && (errors.Is(err, testCase.stsErr) || err.Error() == testCase.stsErr.Error()) {
				t.Fatal("STS 原始错误不得透传")
			}
		})
	}
}
