// 本文件统一约束阿里云官方 SDK 的外部传输协议，避免各产品继承 SDK v1 的 HTTP 默认值。
package aliyun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	aliyuncredentials "github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
)

var errAliyunHTTPSRequired = errors.New("阿里云外部请求必须使用 HTTPS")

// aliyunHTTPSOnlyTransport 在实际拨号前拒绝 HTTP，包括 HTTPS 端点返回的降级重定向。
type aliyunHTTPSOnlyTransport struct {
	base http.RoundTripper
	ctx  context.Context
}

func (transport aliyunHTTPSOnlyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil || !strings.EqualFold(request.URL.Scheme, "https") {
		return nil, errAliyunHTTPSRequired
	}
	base := transport.base
	if base == nil {
		base = http.DefaultTransport
	}
	if transport.ctx == nil {
		return base.RoundTrip(request)
	}
	if err := transport.ctx.Err(); err != nil {
		return nil, err
	}
	// SDK v1 不接收 context；在最终传输边界同时保留原请求取消和整次采集取消。
	ctx, cancel := context.WithCancel(request.Context())
	stop := context.AfterFunc(transport.ctx, cancel)
	cleanup := sync.OnceFunc(func() {
		stop()
		cancel()
	})
	response, err := base.RoundTrip(request.Clone(ctx))
	if err != nil || response == nil || response.Body == nil {
		cleanup()
		return response, err
	}
	// 返回响应头并不代表请求结束；取消必须持续覆盖 SDK 解码响应体的全过程。
	response.Body = &aliyunContextBody{ReadCloser: response.Body, cleanup: cleanup}
	return response, nil
}

// aliyunContextBody 在响应读完或关闭时释放任务取消订阅，避免请求完成后继续持有任务上下文。
type aliyunContextBody struct {
	io.ReadCloser
	cleanup func()
}

func (body *aliyunContextBody) Read(buffer []byte) (int, error) {
	n, err := body.ReadCloser.Read(buffer)
	if err != nil {
		body.cleanup()
	}
	return n, err
}

func (body *aliyunContextBody) Close() error {
	defer body.cleanup()
	return body.ReadCloser.Close()
}

// newAliyunHTTPSConfig 统一 HTTPS 与单请求上限；采集上下文覆盖所有分页、详情和补充调用。
func newAliyunHTTPSConfig(contexts ...context.Context) *sdk.Config {
	var ctx context.Context
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	config := sdk.NewConfig().WithScheme(requests.HTTPS).WithTimeout(30 * time.Second)
	config.Transport = aliyunHTTPSOnlyTransport{base: http.DefaultTransport, ctx: ctx}
	return config
}

// newAliyunSDKCredential 将已校验的短期内存凭证交给官方 SDK，不在配置中复制保存。
func newAliyunSDKCredential(accessKeyID, accessKeySecret string) *aliyuncredentials.AccessKeyCredential {
	return aliyuncredentials.NewAccessKeyCredential(accessKeyID, accessKeySecret)
}
