// 本文件统一约束阿里云官方 SDK 的外部传输协议，避免各产品继承 SDK v1 的 HTTP 默认值。
package aliyun

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	aliyuncredentials "github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
)

var errAliyunHTTPSRequired = errors.New("阿里云外部请求必须使用 HTTPS")

// aliyunHTTPSOnlyTransport 在实际拨号前拒绝 HTTP，包括 HTTPS 端点返回的降级重定向。
type aliyunHTTPSOnlyTransport struct {
	base http.RoundTripper
}

func (transport aliyunHTTPSOnlyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil || !strings.EqualFold(request.URL.Scheme, "https") {
		return nil, errAliyunHTTPSRequired
	}
	base := transport.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(request)
}

// newAliyunHTTPSConfig 为所有阿里云产品客户端提供统一的 HTTPS 默认协议。
func newAliyunHTTPSConfig() *sdk.Config {
	config := sdk.NewConfig().WithScheme(requests.HTTPS)
	config.Transport = aliyunHTTPSOnlyTransport{base: http.DefaultTransport}
	return config
}

// newAliyunSDKCredential 将已校验的短期内存凭证交给官方 SDK，不在配置中复制保存。
func newAliyunSDKCredential(accessKeyID, accessKeySecret string) *aliyuncredentials.AccessKeyCredential {
	return aliyuncredentials.NewAccessKeyCredential(accessKeyID, accessKeySecret)
}
