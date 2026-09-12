// 本文件实现 AWS 凭证格式校验和 STS 账号识别，不承担资源同步、权限或审计职责。
package aws

import (
	"context"
	"encoding/json"
	"regexp"

	"cmdb/internal/resource"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// 编译期确认 AWS 模块完整实现资源核心定义的平台适配器契约。
var _ resource.ProviderAdapter = (*Collector)(nil)

var awsAccountIDPattern = regexp.MustCompile(`^[0-9]{12}$`)

// awsIdentityAPI 约束账号识别只调用 STS GetCallerIdentity，便于隔离真实云端请求。
type awsIdentityAPI interface {
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// newAWSSTSClient 使用静态凭证配置创建官方 STS 客户端。
func newAWSSTSClient(configuration awssdk.Config) awsIdentityAPI {
	return sts.NewFromConfig(configuration)
}

// ValidateCredential 只允许 AWS AccessKey 的两个必填字段及可选会话令牌进入凭证加密流程。
func (c *Collector) ValidateCredential(raw json.RawMessage) error {
	_, err := resource.DecodeStrictStringObject(raw, []string{"access_key_id", "secret_access_key"}, []string{"session_token"})
	return err
}

// NormalizeStoredCredential 兼容旧版前端曾保存的空 Session Token；该空值只在历史密文读取时移除。
func (c *Collector) NormalizeStoredCredential(raw json.RawMessage) (json.RawMessage, error) {
	return resource.NormalizeStoredStringObject(raw, []string{"access_key_id", "secret_access_key"}, []string{"session_token"})
}

// ValidateConfig 首期 AWS 模块不定义额外采集配置，只允许空对象。
func (c *Collector) ValidateConfig(raw json.RawMessage) error {
	return resource.ValidateEmptyConfig(raw)
}

// ResourceTypes 返回首期 AWS 采集范围，顺序与同步结果保持一致。
func (c *Collector) ResourceTypes() []string {
	return []string{"ec2", "rds", "elb"}
}

// ResolveCloudAccountID 通过 STS 确认凭证所属账号，原始 SDK 错误不得离开平台模块。
func (c *Collector) ResolveCloudAccountID(ctx context.Context, source resource.Source, plain []byte) (string, error) {
	values, err := resource.DecodeStrictStringObject(json.RawMessage(plain), []string{"access_key_id", "secret_access_key"}, []string{"session_token"})
	if err != nil {
		return "", err
	}
	configuration, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(source.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(values["access_key_id"], values["secret_access_key"], values["session_token"])),
	)
	if err != nil {
		return "", classifyAWSIdentityError(err)
	}
	newClient := c.newSTSClient
	if newClient == nil {
		newClient = newAWSSTSClient
	}
	client := newClient(configuration)
	if client == nil {
		return "", resource.ErrCloudNetwork
	}
	response, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", classifyAWSIdentityError(err)
	}
	if response == nil {
		return "", resource.ErrCloudAuthentication
	}
	accountID := awssdk.ToString(response.Account)
	if !awsAccountIDPattern.MatchString(accountID) {
		return "", resource.ErrCloudAuthentication
	}
	return accountID, nil
}

// classifyAWSIdentityError 将 STS SDK 失败收敛为有限的安全分类，避免泄露原始请求和凭证。
func classifyAWSIdentityError(err error) error {
	if classified := classifyAWSAccessError(err); classified != nil {
		return classified
	}
	// STS 未能安全识别的底层错误统一按网络失败处理，不能透传 SDK 文本。
	return resource.ErrCloudNetwork
}
