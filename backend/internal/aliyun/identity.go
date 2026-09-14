// 本文件实现阿里云凭证格式校验和 STS 账号识别，不承担资源同步、权限或审计职责。
package aliyun

import (
	"context"
	"encoding/json"
	"strings"

	"cmdb/internal/resource"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/sts"
)

// 编译期确认阿里云模块完整实现资源核心定义的平台适配器契约。
var _ resource.ProviderAdapter = (*Collector)(nil)

// aliyunIdentityAPI 约束账号识别只调用 STS GetCallerIdentity，便于隔离真实云端请求。
type aliyunIdentityAPI interface {
	GetCallerIdentity(*sts.GetCallerIdentityRequest) (*sts.GetCallerIdentityResponse, error)
}

// stsClientFactory 创建 STS 客户端；测试以模拟实现替代网络调用。
type stsClientFactory func(region, accessKeyID, accessKeySecret string) (aliyunIdentityAPI, error)

// newAliyunSTSClient 使用接入源区域及已校验凭证创建官方 STS 客户端。
func newAliyunSTSClient(region, accessKeyID, accessKeySecret string) (aliyunIdentityAPI, error) {
	return sts.NewClientWithOptions(region, newAliyunHTTPSConfig(), newAliyunSDKCredential(accessKeyID, accessKeySecret))
}

// ValidateCredential 只允许阿里云 AccessKey 的两个必填字段进入凭证加密流程。
func (c *Collector) ValidateCredential(raw json.RawMessage) error {
	_, err := resource.DecodeStrictStringObject(raw, []string{"access_key_id", "access_key_secret"}, nil)
	return err
}

// ValidateConfig 首期阿里云模块不定义额外采集配置，只允许空对象。
func (c *Collector) ValidateConfig(raw json.RawMessage) error {
	return resource.ValidateEmptyConfig(raw)
}

// ResourceTypes 返回阿里云固定资源类型；旧 elb 不属于阿里云类型且不再产生。
func (c *Collector) ResourceTypes() []string {
	return []string{"ecs", "rds", "slb", "alb", "nlb", "gwlb"}
}

// ResolveCloudAccountID 通过 STS 确认凭证所属账号，原始 SDK 错误不得离开平台模块。
func (c *Collector) ResolveCloudAccountID(_ context.Context, source resource.Source, plain []byte) (string, error) {
	values, err := resource.DecodeStrictStringObject(json.RawMessage(plain), []string{"access_key_id", "access_key_secret"}, nil)
	if err != nil {
		return "", err
	}
	factory := c.newSTSClient
	if factory == nil {
		factory = newAliyunSTSClient
	}
	client, err := factory(source.Region, values["access_key_id"], values["access_key_secret"])
	if err != nil || client == nil {
		return "", classifyAliyunIdentityError(err)
	}
	request := sts.CreateGetCallerIdentityRequest()
	// 阿里云 STS 已拒绝明文 HTTP；显式指定 HTTPS，避免依赖 SDK 的历史默认协议。
	request.Scheme = requests.HTTPS
	response, err := client.GetCallerIdentity(request)
	if err != nil {
		return "", classifyAliyunIdentityError(err)
	}
	if response == nil || strings.TrimSpace(response.AccountId) == "" {
		return "", resource.ErrCloudAuthentication
	}
	return strings.TrimSpace(response.AccountId), nil
}

// classifyAliyunIdentityError 将 STS SDK 失败收敛为有限的安全分类，避免泄露原始请求和凭证。
func classifyAliyunIdentityError(err error) error {
	if classified := classifyAliyunAccessError(err); classified != nil {
		return classified
	}
	// STS 未能安全识别的底层错误统一按网络失败处理，不能透传 SDK 文本。
	return resource.ErrCloudNetwork
}
