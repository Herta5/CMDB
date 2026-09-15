// 本文件用进程内 TLS 服务验证阿里云任务取消会终止实际请求及响应体读取。
package aliyun

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"cmdb/internal/resource"
)

// aliyunRedirectTransport 只在测试中将 SDK 云端地址映射到受信任的本地 TLS 服务。
type aliyunRedirectTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (transport aliyunRedirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.URL.Scheme = transport.target.Scheme
	request.URL.Host = transport.target.Host
	return transport.base.RoundTrip(request)
}

// TestAliyunCancellationStopsCloudRequests 覆盖列表、分页、补充 API 和返回头后的响应体读取，防止旧 SDK 丢失任务取消。
func TestAliyunCancellationStopsCloudRequests(t *testing.T) {
	for _, test := range []struct {
		name, action, version string
		page, body, probe     bool
	}{
		{name: "同步列表", action: "DescribeInstances"},
		{name: "同步响应体", action: "DescribeInstances", body: true},
		{name: "同步分页", action: "DescribeInstances", page: true},
		{name: "同步云盘补充", action: "DescribeDisks"},
		{name: "连接探测", action: "DescribeInstances", probe: true},
		{name: "同步末尾类型", action: "ListLoadBalancers", version: "2024-04-15"},
		{name: "探测末尾类型", action: "ListLoadBalancers", version: "2024-04-15", probe: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started, disconnected, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer close(release)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				if r.Form.Get("Action") == test.action && (test.version == "" || r.Form.Get("Version") == test.version) && (!test.page || r.Form.Get("PageNumber") == "2") {
					if test.body {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						_, _ = io.WriteString(w, `{"TotalCount":`)
						w.(http.Flusher).Flush()
					}
					close(started)
					select {
					case <-r.Context().Done():
						close(disconnected)
					case <-release:
					}
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if r.Form.Get("Action") == "DescribeInstances" && test.page {
					_, _ = io.WriteString(w, `{"PageSize":1,"TotalCount":2,"Instances":{"Instance":[{"InstanceId":"i-test","InstanceType":"ecs.test","Cpu":2,"Memory":1024}]}}`)
				} else {
					_, _ = io.WriteString(w, `{"Success":true,"HttpStatusCode":200}`)
				}
			}))
			defer server.Close()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			base := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
			defer base.CloseIdleConnections()
			target, _ := url.Parse(server.URL)
			original := http.DefaultTransport
			http.DefaultTransport = aliyunRedirectTransport{target: target, base: base}
			defer func() { http.DefaultTransport = original }()
			type outcome struct {
				results []resource.CollectionResult
				err     error
			}
			done := make(chan outcome, 1)
			go func() {
				collector := NewCollector()
				run := collector.Collect
				if test.probe {
					run = collector.Probe
				}
				results, err := run(ctx, resource.Source{Region: "cn-hangzhou"}, []byte(`{"access_key_id":"fake-key","access_key_secret":"fake-secret"}`))
				done <- outcome{results: results, err: err}
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("SDK 未发出预期云请求")
			}
			cancel()
			select {
			case <-disconnected:
			case <-time.After(300 * time.Millisecond):
				// 失败路径也解除本地服务阻塞，避免回归测试遗留网络请求。
				server.CloseClientConnections()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("解除测试网络阻塞后采集仍未结束")
				}
				t.Fatal("取消任务必须中断实际 TLS 请求或响应体读取")
			}
			select {
			case got := <-done:
				if got.err == nil || len(got.results) != 0 {
					t.Fatalf("取消任务不得返回可应用快照：结果数=%d，错误=%v", len(got.results), got.err)
				}
			case <-time.After(time.Second):
				t.Fatal("实际请求取消后采集必须结束")
			}
		})
	}
}

// TestAliyunResponseBodyRemainsReadable 保证任务仍有效时，返回响应头不会提前取消响应体读取。
func TestAliyunResponseBodyRemainsReadable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-release:
			_, _ = io.WriteString(w, "完整云响应")
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	base := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	defer base.CloseIdleConnections()
	config := newAliyunHTTPSConfig(ctx)
	transport := config.Transport.(aliyunHTTPSOnlyTransport)
	transport.base = base
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	response, err := transport.RoundTrip(request)
	close(release)
	if err != nil {
		t.Fatalf("任务未取消时请求应成功：%v", err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil || string(content) != "完整云响应" {
		t.Fatalf("响应头返回后必须完整读取响应体：%q，%v", content, err)
	}
}

// TestAliyunTransportPreservesRequestCancellation 防止绑定采集上下文后丢失 HTTP 原请求的取消信号。
func TestAliyunTransportPreservesRequestCancellation(t *testing.T) {
	taskCtx, cancelTask := context.WithCancel(context.Background())
	defer cancelTask()
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	request, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, "https://aliyun.example.invalid", nil)
	response, err := newAliyunHTTPSConfig(taskCtx).Transport.RoundTrip(request)
	if err == nil || response != nil {
		t.Fatal("已取消的原请求必须失败")
	}
}
