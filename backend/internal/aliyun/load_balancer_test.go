// 本文件通过官方 SDK 模拟响应验收负载均衡分页、监听端点及类型失败隔离，不访问真实账号。
package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"cmdb/internal/resource"
	aliyunresponses "github.com/aliyun/alibaba-cloud-sdk-go/sdk/responses"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/alb"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/gwlb"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/nlb"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/slb"
)

type albCollectStub struct {
	list      func(*alb.ListLoadBalancersRequest) (*alb.ListLoadBalancersResponse, error)
	listeners func(*alb.ListListenersRequest) (*alb.ListListenersResponse, error)
}

func (s *albCollectStub) ListLoadBalancers(r *alb.ListLoadBalancersRequest) (*alb.ListLoadBalancersResponse, error) {
	if s.list != nil {
		return s.list(r)
	}
	return alb.CreateListLoadBalancersResponse(), nil
}
func (s *albCollectStub) ListListeners(r *alb.ListListenersRequest) (*alb.ListListenersResponse, error) {
	if s.listeners != nil {
		return s.listeners(r)
	}
	return alb.CreateListListenersResponse(), nil
}

type nlbCollectStub struct {
	list      func(*nlb.ListLoadBalancersRequest) (*nlb.ListLoadBalancersResponse, error)
	listeners func(*nlb.ListListenersRequest) (*nlb.ListListenersResponse, error)
}

func (s *nlbCollectStub) ListLoadBalancers(r *nlb.ListLoadBalancersRequest) (*nlb.ListLoadBalancersResponse, error) {
	if s.list != nil {
		return s.list(r)
	}
	return successfulNLBListResponse(), nil
}
func (s *nlbCollectStub) ListListeners(r *nlb.ListListenersRequest) (*nlb.ListListenersResponse, error) {
	if s.listeners != nil {
		return s.listeners(r)
	}
	return successfulNLBListenersResponse(), nil
}

// successfulNLBListResponse 显式表达 HTTP 和业务均成功，避免 Success 零值掩盖错误响应。
func successfulNLBListResponse() *nlb.ListLoadBalancersResponse {
	r := nlb.CreateListLoadBalancersResponse()
	r.Success, r.Code, r.HttpStatusCode = true, "200", http.StatusOK
	return r
}

func successfulNLBListenersResponse() *nlb.ListListenersResponse {
	r := nlb.CreateListListenersResponse()
	r.Success, r.Code, r.HttpStatusCode = true, "200", http.StatusOK
	return r
}

type gwlbCollectStub struct {
	list func(*gwlb.ListLoadBalancersRequest) (*gwlb.ListLoadBalancersResponse, error)
}

func (s *gwlbCollectStub) ListLoadBalancers(r *gwlb.ListLoadBalancersRequest) (*gwlb.ListLoadBalancersResponse, error) {
	if s.list != nil {
		return s.list(r)
	}
	return gwlb.CreateListLoadBalancersResponse(), nil
}

type slbCollectStub struct {
	list      func(*slb.DescribeLoadBalancersRequest) (*slb.DescribeLoadBalancersResponse, error)
	attribute func(*slb.DescribeLoadBalancerAttributeRequest) (*slb.DescribeLoadBalancerAttributeResponse, error)
}

func (s *slbCollectStub) DescribeLoadBalancers(r *slb.DescribeLoadBalancersRequest) (*slb.DescribeLoadBalancersResponse, error) {
	if s.list != nil {
		return s.list(r)
	}
	return slb.CreateDescribeLoadBalancersResponse(), nil
}
func (s *slbCollectStub) DescribeLoadBalancerAttribute(r *slb.DescribeLoadBalancerAttributeRequest) (*slb.DescribeLoadBalancerAttributeResponse, error) {
	if s.attribute != nil {
		return s.attribute(r)
	}
	return slb.CreateDescribeLoadBalancerAttributeResponse(), nil
}

// TestCollectALBReadsAllPagesAndListeners 防止列表或单实例监听器漏页，保留相同端口的不同协议。
func TestCollectALBReadsAllPagesAndListeners(t *testing.T) {
	client := &albCollectStub{
		list: func(r *alb.ListLoadBalancersRequest) (*alb.ListLoadBalancersResponse, error) {
			if r.MaxResults != "100" {
				t.Fatalf("ALB 列表页大小错误：%s", r.MaxResults)
			}
			response := alb.CreateListLoadBalancersResponse()
			switch r.NextToken {
			case "":
				response.LoadBalancers = []alb.LoadBalancer{{LoadBalancerId: "alb-1", DNSName: "alb.example.invalid", AddressType: "Internet"}}
				response.NextToken = "第二页"
			case "第二页":
				response.LoadBalancers = []alb.LoadBalancer{{LoadBalancerId: "alb-2"}}
			default:
				t.Fatalf("ALB 未使用云端分页令牌：%s", r.NextToken)
			}
			return response, nil
		},
		listeners: func(r *alb.ListListenersRequest) (*alb.ListListenersResponse, error) {
			if r.MaxResults != "100" || r.LoadBalancerIds == nil || len(*r.LoadBalancerIds) != 1 {
				t.Fatalf("ALB 监听器必须按实例分页：%+v", r)
			}
			response := alb.CreateListListenersResponse()
			id := (*r.LoadBalancerIds)[0]
			if r.NextToken == "" {
				response.Listeners = []alb.Listener{{LoadBalancerId: id, ListenerPort: 443, ListenerProtocol: "HTTPS"}}
				response.NextToken = "监听下一页"
			} else if r.NextToken == "监听下一页" {
				response.Listeners = []alb.Listener{{LoadBalancerId: id, ListenerPort: 443, ListenerProtocol: "HTTP"}}
			} else {
				t.Fatal("ALB 监听分页令牌错误")
			}
			return response, nil
		},
	}
	items, listeners, err := collectALB(client)
	if err != nil || len(items) != 2 || len(listeners["alb-1"]) != 2 || len(listeners["alb-2"]) != 2 {
		t.Fatalf("ALB 必须读取完整列表和监听器：%v %v %v", items, listeners, err)
	}
	values := albSnapshots(items, listeners, "cn-hangzhou")
	want := []resource.EndpointSnapshot{{Kind: "public", Address: "alb.example.invalid", Port: 443, Protocol: "https"}, {Kind: "public", Address: "alb.example.invalid", Port: 443, Protocol: "http"}}
	if !reflect.DeepEqual(values[0].Endpoints, want) || values[0].Region != "cn-hangzhou" {
		t.Fatalf("ALB DNS 端口协议或地域错误：%+v", values[0])
	}
}

// TestCollectNLBReadsAllPagesAndListeners 防止 NLB 列表与监听器分页遗漏。
func TestCollectNLBReadsAllPagesAndListeners(t *testing.T) {
	client := &nlbCollectStub{
		list: func(r *nlb.ListLoadBalancersRequest) (*nlb.ListLoadBalancersResponse, error) {
			if r.MaxResults != "100" {
				t.Fatal("NLB 列表页大小必须为 100")
			}
			response := successfulNLBListResponse()
			if r.NextToken == "" {
				response.LoadBalancers = []nlb.LoadbalancerInfo{{LoadBalancerId: "nlb-1", DNSName: "nlb.example.invalid", AddressType: "Intranet"}}
				response.NextToken = "第二页"
			} else if r.NextToken == "第二页" {
				response.LoadBalancers = []nlb.LoadbalancerInfo{{LoadBalancerId: "nlb-2"}}
			} else {
				t.Fatal("NLB 列表分页令牌错误")
			}
			return response, nil
		},
		listeners: func(r *nlb.ListListenersRequest) (*nlb.ListListenersResponse, error) {
			if r.MaxResults != "100" || r.LoadBalancerIds == nil || len(*r.LoadBalancerIds) != 1 {
				t.Fatal("NLB 监听器必须按实例分页")
			}
			response := successfulNLBListenersResponse()
			id := (*r.LoadBalancerIds)[0]
			if r.NextToken == "" {
				response.Listeners = []nlb.ListenerInfo{{LoadBalancerId: id, ListenerPort: 53, ListenerProtocol: "TCP"}}
				response.NextToken = "监听下一页"
			} else if r.NextToken == "监听下一页" {
				response.Listeners = []nlb.ListenerInfo{{LoadBalancerId: id, ListenerPort: 53, ListenerProtocol: "UDP"}}
			} else {
				t.Fatal("NLB 监听分页令牌错误")
			}
			return response, nil
		},
	}
	items, listeners, err := collectNLB(client)
	if err != nil || len(items) != 2 || len(listeners["nlb-1"]) != 2 || len(listeners["nlb-2"]) != 2 {
		t.Fatalf("NLB 必须读取完整列表和监听器：%v %v %v", items, listeners, err)
	}
	want := []resource.EndpointSnapshot{{Kind: "private", Address: "nlb.example.invalid", Port: 53, Protocol: "tcp"}, {Kind: "private", Address: "nlb.example.invalid", Port: 53, Protocol: "udp"}}
	if got := nlbSnapshots(items, listeners, "cn-hangzhou")[0].Endpoints; !reflect.DeepEqual(got, want) {
		t.Fatalf("NLB DNS 端口协议错误：%+v", got)
	}
}

// TestCollectGWLBReadsAllPages 防止 GWLB 只采集首页。
func TestCollectGWLBReadsAllPages(t *testing.T) {
	client := &gwlbCollectStub{list: func(r *gwlb.ListLoadBalancersRequest) (*gwlb.ListLoadBalancersResponse, error) {
		if r.MaxResults != "100" {
			t.Fatal("GWLB 列表页大小必须为 100")
		}
		response := gwlb.CreateListLoadBalancersResponse()
		if r.NextToken == "" {
			response.LoadBalancers = []gwlb.Data{{LoadBalancerId: "gwlb-1"}}
			response.NextToken = "第二页"
		} else if r.NextToken == "第二页" {
			response.LoadBalancers = []gwlb.Data{{LoadBalancerId: "gwlb-2"}}
		} else {
			t.Fatal("GWLB 分页令牌错误")
		}
		return response, nil
	}}
	items, err := collectGWLB(client)
	if err != nil || len(items) != 2 || items[1].LoadBalancerId != "gwlb-2" {
		t.Fatalf("GWLB 必须保留全部分页：%v %v", items, err)
	}
}

// TestAliyunLoadBalancerSnapshotsUseOfficialTypes 防止产品身份混用、原始列表项被监听器污染或 GWLB 虚构端点协议。
func TestAliyunLoadBalancerSnapshotsUseOfficialTypes(t *testing.T) {
	s := slb.LoadBalancer{LoadBalancerId: "lb-slb", Address: "198.51.100.2", AddressType: "internet"}
	a := alb.LoadBalancer{LoadBalancerId: "alb-alb", DNSName: "alb.example.invalid", AddressType: "Internet"}
	n := nlb.LoadbalancerInfo{LoadBalancerId: "nlb-nlb", DNSName: "nlb.example.invalid", AddressType: "Intranet"}
	g := gwlb.Data{LoadBalancerId: "gwlb-gwlb", ZoneMappings: []gwlb.ZoneEniModel{{LoadBalancerAddresses: []gwlb.EniModels{{PrivateIpv4Address: "10.0.0.8"}, {PrivateIpv4Address: "10.0.0.8"}, {}}}}}
	values := []resource.Snapshot{slbSnapshots([]slb.LoadBalancer{s}, map[string][]slb.ListenerPortAndProtocol{"lb-slb": {{ListenerPort: 80, ListenerProtocol: "HTTP"}, {ListenerPort: 80, ListenerProtocol: "HTTP"}}})[0], albSnapshots([]alb.LoadBalancer{a}, nil, "cn-hangzhou")[0], nlbSnapshots([]nlb.LoadbalancerInfo{n}, nil, "cn-hangzhou")[0], gwlbSnapshots([]gwlb.Data{g}, "cn-hangzhou")[0]}
	wantTypes := []string{"slb", "alb", "nlb", "gwlb"}
	wantIDs := []string{"lb-slb", "alb-alb", "nlb-nlb", "gwlb-gwlb"}
	for i, original := range []any{s, a, n, g} {
		raw, _ := json.Marshal(original)
		if values[i].ResourceType != wantTypes[i] || values[i].ExternalID != wantIDs[i] || string(values[i].RawAttributes) != string(raw) {
			t.Fatalf("官方类型、身份或原始列表项错误：%+v", values[i])
		}
	}
	if len(values[0].Endpoints) != 1 || values[0].Endpoints[0].Protocol != "http" {
		t.Fatalf("SLB 监听端点必须去重并转换小写协议：%v", values[0].Endpoints)
	}
	want := []resource.EndpointSnapshot{{Kind: "private", Address: "10.0.0.8"}}
	if !reflect.DeepEqual(values[3].Endpoints, want) {
		t.Fatalf("GWLB 必须只保留实际私网地址：%v", values[3].Endpoints)
	}
	if values[1].Endpoints[0].Port != 0 || values[1].Endpoints[0].Protocol != "" || values[2].Endpoints[0].Port != 0 {
		t.Fatal("没有监听器时不得虚构端口或协议")
	}
}

// TestAliyunLoadBalancerTypeFailureKeepsOtherTypes 防止单类列表或监听器失败污染其他类型，认证失败除外。
func TestAliyunLoadBalancerTypeFailureKeepsOtherTypes(t *testing.T) {
	for _, product := range []string{"slb", "alb", "nlb", "gwlb"} {
		for _, failure := range []string{"列表权限", "列表网络", "监听器权限", "认证"} {
			if product == "gwlb" && failure == "监听器权限" {
				continue
			}
			t.Run(product+"/"+failure, func(t *testing.T) {
				cloudErr := errors.New("Forbidden")
				if failure == "列表网络" {
					cloudErr = errors.New("网络中断")
				}
				if failure == "认证" {
					cloudErr = errors.New("InvalidAccessKeyId.NotFound")
				}
				s, a, n, g := &slbCollectStub{}, &albCollectStub{}, &nlbCollectStub{}, &gwlbCollectStub{}
				s.list = func(*slb.DescribeLoadBalancersRequest) (*slb.DescribeLoadBalancersResponse, error) {
					r := slb.CreateDescribeLoadBalancersResponse()
					r.LoadBalancers.LoadBalancer = []slb.LoadBalancer{{LoadBalancerId: "lb-slb"}}
					if product == "slb" && failure != "监听器权限" {
						return nil, cloudErr
					}
					return r, nil
				}
				s.attribute = func(*slb.DescribeLoadBalancerAttributeRequest) (*slb.DescribeLoadBalancerAttributeResponse, error) {
					if product == "slb" && failure == "监听器权限" {
						return nil, cloudErr
					}
					return slb.CreateDescribeLoadBalancerAttributeResponse(), nil
				}
				a.list = func(*alb.ListLoadBalancersRequest) (*alb.ListLoadBalancersResponse, error) {
					r := alb.CreateListLoadBalancersResponse()
					r.LoadBalancers = []alb.LoadBalancer{{LoadBalancerId: "alb-alb"}}
					if product == "alb" && failure != "监听器权限" {
						return nil, cloudErr
					}
					return r, nil
				}
				a.listeners = func(*alb.ListListenersRequest) (*alb.ListListenersResponse, error) {
					if product == "alb" && failure == "监听器权限" {
						return nil, cloudErr
					}
					return alb.CreateListListenersResponse(), nil
				}
				n.list = func(*nlb.ListLoadBalancersRequest) (*nlb.ListLoadBalancersResponse, error) {
					r := successfulNLBListResponse()
					r.LoadBalancers = []nlb.LoadbalancerInfo{{LoadBalancerId: "nlb-nlb"}}
					if product == "nlb" && failure != "监听器权限" {
						return nil, cloudErr
					}
					return r, nil
				}
				n.listeners = func(*nlb.ListListenersRequest) (*nlb.ListListenersResponse, error) {
					if product == "nlb" && failure == "监听器权限" {
						return nil, cloudErr
					}
					return successfulNLBListenersResponse(), nil
				}
				g.list = func(*gwlb.ListLoadBalancersRequest) (*gwlb.ListLoadBalancersResponse, error) {
					r := gwlb.CreateListLoadBalancersResponse()
					r.LoadBalancers = []gwlb.Data{{LoadBalancerId: "gwlb-gwlb"}}
					if product == "gwlb" {
						return nil, cloudErr
					}
					return r, nil
				}
				results, err := collectAliyunResources(context.Background(), &ecsProbeStub{}, &rdsCollectStub{}, s, a, n, g, "cn-hangzhou")
				if failure == "认证" {
					if !errors.Is(err, resource.ErrAuthenticationFailed) || results != nil {
						t.Fatalf("认证失败必须整体终止：%v %v", results, err)
					}
					return
				}
				if err != nil || len(results) != 6 {
					t.Fatalf("单类失败必须保留六类结果：%v %v", results, err)
				}
				for i, wantType := range []string{"ecs", "rds", "slb", "alb", "nlb", "gwlb"} {
					result := results[i]
					if result.ResourceType != wantType {
						t.Fatalf("类型顺序错误：%v", results)
					}
					if wantType == product {
						if result.Err == nil || len(result.Snapshots) != 0 {
							t.Fatalf("失败类型不得输出局部快照：%v", result)
						}
					} else if result.Err != nil || (i >= 2 && len(result.Snapshots) != 1) {
						t.Fatalf("其他类型必须保留成功快照：%v", result)
					}
				}
			})
		}
	}
}

// TestAliyunLoadBalancerRejectsRepeatedTokens 防止列表与监听分页陷入循环，包括跨多页重用令牌。
func TestAliyunLoadBalancerRejectsRepeatedTokens(t *testing.T) {
	for _, product := range []string{"alb", "nlb", "gwlb"} {
		for _, stage := range []string{"列表", "监听器"} {
			if product == "gwlb" && stage == "监听器" {
				continue
			}
			t.Run(product+"/"+stage, func(t *testing.T) {
				calls := 0
				next := func() string {
					calls++
					if calls > 4 {
						t.Fatal("分页必须检测循环并有限终止")
					}
					if calls%2 == 1 {
						return "甲"
					}
					return "乙"
				}
				a := &albCollectStub{list: func(*alb.ListLoadBalancersRequest) (*alb.ListLoadBalancersResponse, error) {
					r := alb.CreateListLoadBalancersResponse()
					r.LoadBalancers = []alb.LoadBalancer{{LoadBalancerId: "alb"}}
					if stage == "列表" {
						r.NextToken = next()
					}
					return r, nil
				}, listeners: func(*alb.ListListenersRequest) (*alb.ListListenersResponse, error) {
					r := alb.CreateListListenersResponse()
					r.NextToken = next()
					return r, nil
				}}
				n := &nlbCollectStub{list: func(*nlb.ListLoadBalancersRequest) (*nlb.ListLoadBalancersResponse, error) {
					r := successfulNLBListResponse()
					r.LoadBalancers = []nlb.LoadbalancerInfo{{LoadBalancerId: "nlb"}}
					if stage == "列表" {
						r.NextToken = next()
					}
					return r, nil
				}, listeners: func(*nlb.ListListenersRequest) (*nlb.ListListenersResponse, error) {
					r := successfulNLBListenersResponse()
					r.NextToken = next()
					return r, nil
				}}
				g := &gwlbCollectStub{list: func(*gwlb.ListLoadBalancersRequest) (*gwlb.ListLoadBalancersResponse, error) {
					r := gwlb.CreateListLoadBalancersResponse()
					r.NextToken = next()
					return r, nil
				}}
				var err error
				switch product {
				case "alb":
					_, _, err = collectALB(a)
				case "nlb":
					_, _, err = collectNLB(n)
				case "gwlb":
					_, err = collectGWLB(g)
				}
				if err == nil {
					t.Fatal("重复分页令牌必须使该类型失败")
				}
			})
		}
	}
}

// TestAliyunLoadBalancerProbeUsesOnlyFirstListPage 防止轻量探测进入分页、监听器或返回资源快照。
func TestAliyunLoadBalancerProbeUsesOnlyFirstListPage(t *testing.T) {
	a := &albCollectStub{list: func(r *alb.ListLoadBalancersRequest) (*alb.ListLoadBalancersResponse, error) {
		if r.MaxResults != "1" || r.NextToken != "" {
			t.Fatal("ALB 探测只能读取一条列表数据")
		}
		v := alb.CreateListLoadBalancersResponse()
		v.NextToken = "不应读取"
		v.LoadBalancers = []alb.LoadBalancer{{LoadBalancerId: "alb", DNSName: "alb.example.invalid"}}
		return v, nil
	}, listeners: func(*alb.ListListenersRequest) (*alb.ListListenersResponse, error) {
		t.Fatal("ALB 探测不得读取监听器")
		return nil, nil
	}}
	n := &nlbCollectStub{list: func(r *nlb.ListLoadBalancersRequest) (*nlb.ListLoadBalancersResponse, error) {
		if r.MaxResults != "1" || r.NextToken != "" {
			t.Fatal("NLB 探测只能读取一条列表数据")
		}
		v := successfulNLBListResponse()
		v.NextToken = "不应读取"
		v.LoadBalancers = []nlb.LoadbalancerInfo{{LoadBalancerId: "nlb", DNSName: "nlb.example.invalid"}}
		return v, nil
	}, listeners: func(*nlb.ListListenersRequest) (*nlb.ListListenersResponse, error) {
		t.Fatal("NLB 探测不得读取监听器")
		return nil, nil
	}}
	g := &gwlbCollectStub{list: func(r *gwlb.ListLoadBalancersRequest) (*gwlb.ListLoadBalancersResponse, error) {
		if r.MaxResults != "1" || r.NextToken != "" {
			t.Fatal("GWLB 探测只能读取一条列表数据")
		}
		v := gwlb.CreateListLoadBalancersResponse()
		v.NextToken = "不应读取"
		return v, nil
	}}
	results, err := probeAliyunAccess(context.Background(), &ecsProbeStub{}, &rdsProbeStub{}, &slbProbeStub{}, a, n, g, "cn-hangzhou")
	if err != nil || len(results) != 6 {
		t.Fatalf("六类型探测失败：%v %v", results, err)
	}
	for _, result := range results {
		if result.Err != nil || len(result.Snapshots) != 0 {
			t.Fatalf("探测只能表达可达性：%v", result)
		}
	}
}

// decodeNLBHTTP200 使用 SDK 真实反序列化路径复现 HTTP 成功但业务失败、Go error 为 nil 的响应。
func decodeNLBHTTP200(t *testing.T, response aliyunresponses.AcsResponse, body string) {
	t.Helper()
	if err := aliyunresponses.Unmarshal(response, &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, "JSON"); err != nil {
		t.Fatalf("模拟 HTTP 200 响应应由 SDK 正常解码：%v", err)
	}
}

// TestNLBHTTP200BusinessFailure 防止业务失败空列表推进生命周期、监听失败输出局部快照或探测误报可达。
func TestNLBHTTP200BusinessFailure(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{name: "权限错误码", body: `{"Success":false,"Code":"Forbidden.RAM","HttpStatusCode":200,"Message":"不应泄露的响应正文"}`, want: resource.ErrPermissionDenied},
		{name: "认证错误码", body: `{"Success":false,"Code":"InvalidAccessKeyId.NotFound","HttpStatusCode":200,"Message":"不应泄露的响应正文"}`, want: resource.ErrAuthenticationFailed},
		{name: "未知业务错误", body: `{"Success":false,"Code":"InternalError","HttpStatusCode":200,"Message":"不应泄露的响应正文"}`},
		{name: "缺失成功标记", body: `{"Code":"200","HttpStatusCode":200,"Message":"不应泄露的响应正文"}`},
		{name: "业务权限状态", body: `{"Success":false,"Code":"","HttpStatusCode":403,"Message":"不应泄露的响应正文"}`, want: resource.ErrPermissionDenied},
		{name: "业务认证状态", body: `{"Success":false,"Code":"","HttpStatusCode":401,"Message":"不应泄露的响应正文"}`, want: resource.ErrAuthenticationFailed},
		{name: "成功标记与错误码矛盾", body: `{"Success":true,"Code":"Forbidden.RAM","HttpStatusCode":200,"Message":"不应泄露的响应正文"}`, want: resource.ErrPermissionDenied},
	} {
		for _, stage := range []string{"列表", "监听器"} {
			t.Run(tc.name+"/"+stage, func(t *testing.T) {
				client := &nlbCollectStub{
					list: func(*nlb.ListLoadBalancersRequest) (*nlb.ListLoadBalancersResponse, error) {
						r := successfulNLBListResponse()
						if stage == "列表" {
							r = nlb.CreateListLoadBalancersResponse()
							decodeNLBHTTP200(t, r, tc.body)
						} else {
							r.LoadBalancers = []nlb.LoadbalancerInfo{{LoadBalancerId: "nlb-1"}}
						}
						return r, nil
					},
					listeners: func(request *nlb.ListListenersRequest) (*nlb.ListListenersResponse, error) {
						r := successfulNLBListenersResponse()
						if request.NextToken == "" {
							r.Listeners = []nlb.ListenerInfo{{LoadBalancerId: "nlb-1", ListenerPort: 443, ListenerProtocol: "TCP"}}
							r.NextToken = "第二页"
						} else {
							r = nlb.CreateListListenersResponse()
							decodeNLBHTTP200(t, r, tc.body)
						}
						return r, nil
					},
				}
				results, err := collectAliyunResources(context.Background(), &ecsProbeStub{}, &rdsCollectStub{}, &slbCollectStub{}, &albCollectStub{}, client, &gwlbCollectStub{}, "cn-hangzhou")
				if errors.Is(tc.want, resource.ErrAuthenticationFailed) {
					if !errors.Is(err, tc.want) || results != nil {
						t.Fatalf("NLB 业务认证失败必须整体终止：结果=%v 错误=%v", results, err)
					}
				} else {
					if err != nil || len(results) != 6 {
						t.Fatalf("NLB 业务失败必须保留其他类型：%v %v", results, err)
					}
					for _, result := range results {
						if result.ResourceType != "nlb" {
							if result.Err != nil {
								t.Fatalf("NLB 失败不得影响其他类型：%v", result)
							}
							continue
						}
						if result.Err == nil || len(result.Snapshots) != 0 {
							t.Fatalf("NLB 业务失败不能输出成功或局部快照：%v", result)
						}
						if tc.want != nil && !errors.Is(result.Err, tc.want) {
							t.Fatalf("NLB 业务错误分类错误：%v", result.Err)
						}
						if strings.Contains(result.Err.Error(), "不应泄露") {
							t.Fatal("不得回显 NLB Message")
						}
					}
				}
				if stage == "列表" {
					results, err = probeAliyunAccess(context.Background(), &ecsProbeStub{}, &rdsProbeStub{}, &slbProbeStub{}, &albCollectStub{}, client, &gwlbCollectStub{}, "cn-hangzhou")
					if tc.want != nil {
						if !errors.Is(err, tc.want) {
							t.Fatalf("Probe 必须保留安全访问错误分类：%v %v", results, err)
						}
					} else if err != nil || len(results) != 6 || results[4].Err == nil {
						t.Fatalf("Probe 不得将 NLB 业务失败标记可达：%v %v", results, err)
					}
				}
			})
		}
	}
}
