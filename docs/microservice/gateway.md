---
title: 第 078 讲 · API 网关、鉴权与限流
description: 自建轻量网关，做路由转发、协议转换、统一鉴权与令牌桶限流，守住系统边界
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★★☆" time="50 分钟" goal="设计统一入口并守住边界" prereq="第 077 讲 配置中心与动态配置" />

# 第 078 讲 · API 网关、鉴权与限流

> 服务拆细之后，外部客户端不该知道背后有几十个实例、更不该逐个去对接鉴权和限流。API 网关就是那个统一入口：把请求按路径路由到正确服务，把外部 HTTP/JSON 翻译成内部 RPC，顺手完成鉴权、限流、日志与协议转换。它让内部服务专注于业务，也把「边界」收敛到一个可以严防死守的位置。本讲用标准库搭一个能跑的轻量网关，亲手实现令牌桶限流与协议桥接，再对照 Kong、APISIX 的取舍——目标是知道该自建还是该选型，以及无论用哪种，哪些职责绝不能放进网关。

## 这一讲你会得到什么
<CheckList id="l78-goals" title="本讲目标" :items='["用 ReverseProxy 与路由表自建网关，理解转发、路径匹配与中间件顺序","实现统一的鉴权中间件，并说清为什么内部服务仍要保留一层校验","用标准库实现令牌桶限流，区分它与固定窗口、漏桶的突发与平滑特性","做一次 HTTP 到内部 RPC 的协议转换，知道网关该做与不该做的事"]' />

## 1. 先建立直觉

把网关想成写字楼前台。访客（外部请求）不直接上楼找人，而是先到前台登记：出示证件（鉴权）、说明要找哪个部门（路由）、前台按当天的接待能力放人（限流），必要时还帮忙把口语转成内部工单格式（协议转换）。楼里各部门（内部服务）因此不用各配一套保安，也不必关心访客从哪来。

网关的威力与风险都来自「所有流量都过它」。它天然适合放横切逻辑：认证、限流、灰度、日志、证书卸载。但也正因如此，它一旦变成单点、或塞进业务逻辑，就会成为整站最脆弱也最难改的一块。所以设计原则很朴素：网关只做与业务无关的边界治理，且自身必须无状态、可水平扩容。

<ConceptDiagram type="gateway" title="API 网关的职责边界" caption="外部请求经网关完成鉴权、限流、路由与协议转换，再转发到内部服务集群" />

<FlowTimeline :steps='[{"title":"接入请求","desc":"外部 HTTP 请求抵达统一入口","tone":"blue"},{"title":"统一鉴权","desc":"校验 token，失败直接短路返回 401","tone":"blue"},{"title":"限流判定","desc":"令牌桶取令牌，无令牌返回 429","tone":"blue"},{"title":"路由匹配","desc":"按路径前缀找到目标服务","tone":"green"},{"title":"协议转换","desc":"REST 路径与 JSON 转成内部方法调用","tone":"green"},{"title":"反向代理","desc":"转发到健康实例并回写响应","tone":"orange"}]' direction="horizontal" />

## 2. 语法与规则

### 2.1 路由表 + 反向代理 + 鉴权中间件

`net/http/httputil` 的 `ReverseProxy` 已经把转发细节做完了，网关要补的只是「路径前缀 → 后端」的一张表，以及挂在最前面的鉴权中间件。注意顺序：鉴权在外、转发在内，未授权请求根本到不了后端。

```go
package main

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/http/httptest"
	"net/url"
	"strings"
)

// Gateway 把路径前缀映射到后端，并在转发前统一做鉴权。
type Gateway struct {
	routes map[string]*httputil.ReverseProxy
	tokens map[string]bool
}

func NewGateway() *Gateway {
	return &Gateway{
		routes: make(map[string]*httputil.ReverseProxy),
		tokens: map[string]bool{"abc123": true},
	}
}

func (g *Gateway) Register(prefix, target string) error {
	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	g.routes[prefix] = httputil.NewSingleHostReverseProxy(u)
	return nil
}

func (g *Gateway) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !g.tokens[tok] {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for prefix, proxy := range g.routes {
		if strings.HasPrefix(r.URL.Path, prefix) {
			proxy.ServeHTTP(w, r)
			return
		}
	}
	http.NotFound(w, r)
}

func main() {
	// 模拟一个下游订单服务
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "order service got %s", r.URL.Path)
	}))
	defer backend.Close()

	g := NewGateway()
	_ = g.Register("/api/order", backend.URL)
	front := httptest.NewServer(g.auth(http.HandlerFunc(g.ServeHTTP)))
	defer front.Close()

	client := &http.Client{}
	resp, _ := client.Get(front.URL + "/api/order/123") // 无 token
	fmt.Println("无鉴权:", resp.StatusCode)
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodGet, front.URL+"/api/order/123", nil)
	req.Header.Set("Authorization", "Bearer abc123")
	resp2, _ := client.Do(req)
	fmt.Println("带 token:", resp2.StatusCode)
	resp2.Body.Close()
}
```

路由与治理规则通常抽成配置，避免每加一个服务就改代码：

```yaml
routes:
  - path: /api/order
    service: order-service
    auth: jwt
    rate_limit: 100/s
  - path: /api/user
    service: user-service
    auth: jwt
    rate_limit: 50/s
```

<OutputBox title="运行结果" output="无鉴权: 401\n带 token: 200" note="鉴权中间件在最外层短路，未授权请求完全没有到达下游；带合法 token 的请求被 ReverseProxy 转发成功。" />

### 2.2 令牌桶限流

限流最经典的坑是固定窗口在临界点放行双倍流量。令牌桶用「按速率补充、最多存 capacity 个」解决了它：既把平均速率压在 rate，又允许一定量的突发，正是 API 网关的主流选择。

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

// TokenBucket 令牌桶：按固定速率补充令牌，拿到令牌才放行，允许一定突发。
type TokenBucket struct {
	mu       sync.Mutex
	capacity float64
	rate     float64 // 每秒补充的令牌数
	tokens   float64
	last     time.Time
}

func NewTokenBucket(capacity, rate float64) *TokenBucket {
	return &TokenBucket{capacity: capacity, rate: rate, tokens: capacity, last: time.Now()}
}

func (b *TokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens += elapsed * b.rate
	if b.tokens > b.capacity {
		b.tokens = b.capacity // 桶满则截断，避免无限累积
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func main() {
	b := NewTokenBucket(3, 2) // 容量 3，每秒补充 2 个
	for i := 0; i < 5; i++ {
		fmt.Printf("第 %d 次请求 放行=%v\n", i+1, b.Allow())
	}
	fmt.Println("等待令牌补充...")
	time.Sleep(1100 * time.Millisecond)
	fmt.Println("补充后 放行=", b.Allow())
}
```

<LineByLine code="b.mu.Lock()\ndefer b.mu.Unlock()\nnow := time.Now()\nelapsed := now.Sub(b.last).Seconds()" :notes='["加锁保护共享的 tokens，让多个 goroutine 可以并发判定","defer 保证函数返回时一定释放锁，不会漏解锁","取当前时间作为本次补充的基准","算出距上次放行经过了多少秒，用于按速率补充令牌"]' title="Allow 开头的加锁与计时逐行拆解" />

## 3. 深入一步

### 3.1 协议转换：HTTP 到内部 RPC

网关另一项核心职责是协议适配：外部是 RESTful 的 `/api/order/10086`，内部却是 `Order.Get` 这样的方法调用。桥接层把路径与参数翻译成方法名与入参，调完再把结果包回 JSON，让下游服务完全不必懂 HTTP。

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

// RPCFunc 抽象一次内部调用：网关把 HTTP 请求翻译成方法与参数。
type RPCFunc func(method string, params map[string]any) (any, error)

func invoke(method string, params map[string]any) (any, error) {
	switch method {
	case "Order.Get":
		return map[string]any{"order_id": params["id"], "status": "paid"}, nil
	default:
		return nil, fmt.Errorf("unknown method")
	}
}

// ProtocolBridge 做协议转换：REST 路径转 RPC 方法，结果再包成 JSON 回写。
func ProtocolBridge(invoke RPCFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := map[string]any{}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 2 {
			params["id"] = parts[len(parts)-1] // 取路径末段作为订单号
		}
		result, err := invoke("Order.Get", params)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}

func main() {
	ts := httptest.NewServer(ProtocolBridge(invoke))
	defer ts.Close()
	resp, _ := http.Get(ts.URL + "/api/order/10086")
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	fmt.Printf("协议转换结果: 状态=%d 数据=%v\n", resp.StatusCode, out)
}
```

| 算法 | 允许突发 | 平滑度 | 边界问题 | 适用场景 |
| --- | --- | --- | --- | --- |
| 固定窗口计数 | 否 | 低 | 窗口临界可能双倍流量 | 粗粒度兜底 |
| 滑动窗口计数 | 较平滑 | 中 | 存储与精度取舍 | 通用接口 |
| 漏桶 | 否，恒定流出 | 高 | 突发被排队或丢弃 | 保护脆弱下游 |
| 令牌桶 | 是 | 高 | capacity 与 rate 需调 | API 网关主流 |

| 维度 | Kong | APISIX | 自研轻量网关 |
| --- | --- | --- | --- |
| 底座 | Nginx 加 Lua | Nginx 加 etcd | net/http 标准库 |
| 配置方式 | 声明式，可落库 | 声明式，etcd 热更 | 代码或本地文件 |
| 插件生态 | 成熟丰富 | 丰富且支持热加载 | 按需自己写 |
| 适用场景 | 成熟企业网关 | 云原生、高频变更 | 内部小集群、教学与定制 |

<Term word="反向代理" text="代表客户端把请求转发到后端并把响应送回，客户端只感知网关这一个地址" />
<Term word="令牌桶" text="按固定速率产令牌、桶满截断，取到令牌才放行，兼顾平均速率与可控突发" />
<Term word="协议转换" text="把外部 HTTP 与 JSON 映射成内部 RPC 方法调用，让下游不必感知外部协议" />

<Callout type="warn" title="网关是边界，不是业务层" text="别把订单状态机、数据聚合这类业务逻辑写进网关，否则它会变成谁都不敢改的巨石。也别只在网关做鉴权：一旦请求从内网绕过网关直达服务，单层防护就全失效，内部服务至少还要保留一次轻量校验。" />

### 3.2 为什么需要统一入口，以及网关的能力边界从哪来

服务拆细之后，如果让外部客户端直接对接内部服务，它就得知道几十个实例地址，还得每个服务各自实现一遍鉴权、限流与日志。网关的动机是把这些横切能力收敛到唯一入口：外部只认一个域名，内部服务的拓扑变化对外不可见。它的威力与风险都来自同一句话——「所有流量都过它」。

由此能推出两条硬边界。第一，网关只做与业务无关的边界治理：认证、限流、灰度、日志、证书卸载、协议转换都合适；订单状态机、跨服务数据聚合这类业务编排一旦写进网关，它就会变成谁都不敢改的巨石，因为最常变的业务代码压在了最不该动的层里。第二，网关自身必须无状态、可水平扩容：既然是唯一入口，单副本就是全站单点，本地内存里存限流计数或会话都会让扩容失效。协议转换的边界也在这里——把 RESTful 的路径与 JSON 翻译成内部方法调用是「翻译」，替下游编排多个服务是「编排」，后者不该做。

限流算法的选择则是另一处可以算清楚的推导。固定窗口计数最简单，却有著名的临界双倍问题：限 100 次每分钟，若 59.9 秒来了 100 次、60.1 秒又来 100 次，两个窗口各自合法，但在这 0.2 秒的临界区间里实际放行了 200 次。令牌桶用「按速率补充、最多存 capacity 个」解决它：`tokens += elapsed * rate`，超过 capacity 截断，取到令牌才放行。capacity 决定闲置后一次最多能放行多少（突发），rate 决定稳态平均 QPS，两者独立，才既能限平均又能容忍突发。

<SequenceDiagram title="一次请求穿过网关的治理链" caption="鉴权限流在前、路由协议转换在中、透传链路上下文后转发到后端" :actors='["Client","Gateway","Auth","Limiter","Backend"]' :steps='[{"from":"Client","to":"Gateway","label":"HTTP 请求","note":"抵达统一入口","kind":"call"},{"from":"Gateway","to":"Auth","label":"校验 token","note":"失败短路","kind":"call"},{"from":"Auth","to":"Gateway","label":"通过或 401","note":"带身份","kind":"return"},{"from":"Gateway","to":"Limiter","label":"取令牌","note":"按用户维度","kind":"call"},{"from":"Limiter","to":"Gateway","label":"放行或 429","note":"带 Retry-After","kind":"return"},{"from":"Gateway","to":"Gateway","label":"匹配路由前缀","note":"查路由表","kind":"self"},{"from":"Gateway","to":"Gateway","label":"透传 trace-id","note":"避免断链","kind":"self"},{"from":"Gateway","to":"Backend","label":"协议转换转发","note":"REST 转 RPC","kind":"call"},{"from":"Backend","to":"Gateway","label":"回写结果","note":"内部响应","kind":"return"},{"from":"Gateway","to":"Client","label":"返回响应","note":"包回 JSON","kind":"return"}]' />

### 3.3 替代方案取舍：限流算法与网关形态

限流算法不止令牌桶一种，各自的突发与平滑特性决定适用场景：

| 算法 | 允许突发 | 平滑度 | 边界问题 | 适用 |
| --- | --- | --- | --- | --- |
| 固定窗口计数 | 否 | 低 | 窗口临界可能双倍流量 | 粗粒度兜底 |
| 滑动窗口计数 | 较平滑 | 中 | 存储与精度取舍 | 通用接口 |
| 漏桶 | 否，恒定流出 | 高 | 突发被排队或丢弃 | 保护脆弱下游 |
| 令牌桶 | 是 | 高 | capacity 与 rate 需联调 | API 网关主流 |
| 分布式集中计数 | 视算法 | 高 | 依赖 Redis，多一次网络 | 多副本配额一致 |

网关本身也有多种形态，选型决定了运维与故障面：

| 形态 | 底座 | 配置方式 | 优点 | 失败边界 |
| --- | --- | --- | --- | --- |
| 自研轻量网关 | net/http 标准库 | 代码或本地文件 | 可控、可教学、定制自由 | 能力少，生态靠自己 |
| Kong | Nginx 加 Lua | 声明式，可落库 | 插件成熟、企业生态 | 运维重，Lua 定制门槛 |
| APISIX | Nginx 加 etcd | 声明式，etcd 热更 | 高频变更、插件热加载 | 依赖 etcd，组件多 |
| Service Mesh Ingress | Envoy 数据面 | 控制面下发 CRD | 与集群治理一体 | 多一跳，排障链路长 |

还有一处只在多副本下暴露的取舍：限流计数放进程内内存时，N 个网关副本各自维护一份桶，实际放行量约为单副本的 N 倍，且各实例看到的配额不一致。要么用 Redis 集中计数（多一次网络、Redis 成为依赖），要么按实例数把配额均分并接受误差——没有两全，只有按业务容忍度选。

### 3.4 可验证实验：429 响应与令牌桶并发竞态

限流是否真的生效，用 curl 看状态码最直接。容量 3、每秒补 2 个令牌时连续快速请求：

```bash
for i in $(seq 1 6); do
  curl -s -o /dev/null -w "%{http_code}\n" \
    -H 'Authorization: Bearer abc123' http://localhost:8080/api/order/1
done
```

```text
200
200
200
429
429
200
```

逐字段解释：前三个请求取走了初始的 3 个令牌，返回 200；第四、五个因桶已空返回 429，配合 Retry-After 让客户端知道何时重试；第六个发生在令牌按速率补充之后，重新放行。若把限流改成固定窗口计数，在窗口临界处会看到短时间内连续两个 200 突发，正好对比出两种算法的差别。

令牌桶本身是共享状态，并发正确性可以用竞态检测器验证。下面这段故意**不加锁**，复现「多个 goroutine 同时改 tokens」会怎样：

```go
package main

import (
	"sync"
	"testing"
)

type Bucket struct{ tokens float64 }

func (b *Bucket) Allow() bool {
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func TestBucketRace(t *testing.T) {
	b := &Bucket{tokens: 1000}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.Allow()
		}()
	}
	wg.Wait()
}
```

```bash
go test -race -run TestBucketRace
```

```text
WARNING: DATA RACE
Read at 0x00c0000b4010 by goroutine 9:
  main.(*Bucket).Allow()
      bucket_test.go:12
Previous write at 0x00c0000b4010 by goroutine 8:
  main.(*Bucket).Allow()
      bucket_test.go:14
```

逐字段解释：`b.tokens < 1` 的读与 `b.tokens--` 的写在两个 goroutine 间无同步，检测器报出冲突，说明无锁版本会算错令牌、放行量失控。原文 2.2 的 `TokenBucket` 用 `sync.Mutex` 把整段 `Allow` 保护起来，正是为了让判定与扣减成为一个原子操作——这也是任何带共享计数的限流器不能省的一把锁。

## 4. 工程实践

限流要分层：网关做粗粒度的全局兜底（防刷、防突发），服务侧再做细粒度（按用户、按租户、按接口）。维度上优先用「用户 ID 加接口」而非单纯 IP，避免 NAT 后大量用户共享一个桶相互挤占。被限流的请求应返回 429 并带上 Retry-After，让客户端知道何时重试，而不是一律 500。

<Compare :panels='[{"label":"只放网关","code":"gateway.Auth(next)\n// 内部服务默认互信","note":"入口统一、改造成本低，但一旦被绕过或内网失守，内部服务毫无防护"},{"label":"网关加服务双层","code":"gateway.Auth(next)\nhandler.Auth(next)","note":"网关挡外部、服务再校验一次，纵深防御；代价是每服务都要带一份校验逻辑"}]' title="鉴权放在哪一层" />

还有几条落地经验。网关自身必须无状态并多副本部署，限流计数若放本地内存，多实例间配额会不一致，要么用 Redis 集中计数、要么按实例数把配额均分并接受误差。路由与限流规则走配置中心热更新，和上一讲的能力接起来就能不重启地调阈值。转发时记得透传 trace-id 等 metadata，否则链路追踪会在网关这一跳断掉；同时设置合理的超时与连接池，别让下游慢拖垮网关。选型上，小集群或强定制需求用自研轻量网关足够，需要成熟插件、可视化与多租户时再上 Kong 或 APISIX——功能越多，运维与故障面也越大。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l7-microservice/lesson-078-gateway`](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-078-gateway)。clone 仓库后进入 `examples/l7-microservice`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

### 4.2 网关的失败矩阵

网关是唯一入口，它的故障天然是全站级别的，更要把边界一条条钉死：

| 故障 | 触发点 | 现象 | 防线 |
| --- | --- | --- | --- |
| 网关单点 | 只部署单副本 | 入口一挂全站不可用 | 无状态多副本 + 前置 LB |
| 限流击穿 | 固定窗口临界 | 瞬时间隔内双倍流量打到下游 | 令牌桶或滑动窗口 |
| 配额不一致 | 本地内存计数 + 多副本 | 各实例阈值叠加或不一致 | Redis 集中计数或按实例均分 |
| trace 断链 | 转发未透传 metadata | 链路追踪缺网关这一跳 | 显式透传 trace-id |
| 单层鉴权 | 内网可绕过网关直达服务 | 内部服务零防护 | 网关 + 服务双层校验 |
| 下游慢拖垮网关 | 未设转发超时与连接池 | 连接堆积、goroutine 暴涨 | 超时 + 连接池 + 熔断 |

### 4.3 两条真实工程误解

**误解一：只在网关做鉴权就够了，内部服务默认互信。** 网关挡住了外部流量，但内网里服务间调用、运维直连、配置错误都可能让请求绕过网关直达后端。一旦单层防护被绕过，内部服务毫无校验。正确做法是纵深防御：网关做统一鉴权，服务侧保留一次轻量校验，代价是每服务带一份校验逻辑，换来的却是内网失守时仍有一道底线。

<Compare :panels='[{"label":"错误：单层鉴权","code":"gateway.Auth(next)\n后端默认互信","note":"一旦请求从内网绕过网关，内部服务毫无防护"},{"label":"正确：双层校验","code":"gateway.Auth(next)\nhandler.Auth(next)","note":"网关挡外部、服务再校验一次，纵深防御"}]' title="鉴权放在哪一层" />

**误解二：令牌桶本地计数在多副本下配额天然一致。** 每个网关副本各维护一份进程内的桶，N 个副本的实际放行量约为单副本的 N 倍，而且彼此看到的余量不同，用户换一个副本就可能从被限流变成被放行。要么用 Redis 集中计数（多一次网络、Redis 成为新依赖），要么按实例数把配额均分并接受误差——没有既零依赖又全局精确的方案。

<Callout type="warn" title="本地令牌桶多副本会失准" text="限流计数放进程内内存时，N 个副本各自计一份，总配额被放大约 N 倍；要么集中到 Redis，要么按副本数均分配额并接受误差，别默认本地计数能全局一致。" />

### 4.4 与后续阶段的呼应

网关在 L8 会融入更大的接入体系：[/distributed/kubernetes](/distributed/kubernetes) 的 Ingress 与 Service 负责把流量引进集群并暴露服务，[/distributed/high-availability](/distributed/high-availability) 的多活架构要求入口本身也能跨故障域容灾，[/distributed/sre](/distributed/sre) 的容量规划则直接决定限流阈值怎么定。到 L10，[/projects/micro-seckill](/projects/micro-seckill) 把网关限流当作防刷与削峰的第一道闸，[/projects/micro-ecommerce](/projects/micro-ecommerce) 用统一入口收敛鉴权与协议转换——本讲「只做边界治理、自身无状态」的原则，到那里就是能不能扛住峰值的前提。

<Quiz q="多副本网关把令牌桶计数放在各自进程内存里，最可能出现什么问题？" :options='["各副本限流更精确","实际总放行量约为单副本的 N 倍，且各实例配额不一致","令牌会在副本间自动同步","不会有任何影响"]' answer="1" explain="每个副本独立计一份桶，总配额被放大约 N 倍且彼此不一致；要全局一致就得集中计数到 Redis，或按副本数均分配额并接受误差。" />

<Predict q="令牌桶容量 3、每秒补 2 个，连续快速发 6 个请求，返回的 HTTP 状态码序列最可能是？" :options='["全是 200","200 200 200 429 429 200","全是 429","200 429 200 429 200 429"]' answer="1" output="200 200 200 429 429 200" explain="前三个请求取走初始 3 个令牌返回 200，第四五个因桶空返回 429 并带 Retry-After，第六个在令牌补充后重新放行。" />

## 动手练一练
<Exercise title="给网关加上按用户维度的限流与 429 响应" task="扩展 2.1 的 Gateway：用 map[string]*TokenBucket 为每个用户 ID 维护一个独立令牌桶，在鉴权通过后按用户限流，超限时返回 429 与 Retry-After 头。" :hints='["用户 ID 可在鉴权成功后写入 context，或在 token 映射里带出","每个用户首次访问时懒创建令牌桶，用一把锁保护 map","限流失败时 http.Error 之前先设置 Retry-After 头"]' solution="要点一：Gateway 增加 limits map[string]*TokenBucket 与 mu，鉴权成功后取用户 ID。\n要点二：getBucket 懒创建，用户首次请求时用 NewTokenBucket 初始化。\n要点三：Allow 为 false 时先 Set Retry-After，再 WriteHeader(429) 返回，不放行到 ReverseProxy。" explain="按用户隔离配额才不会让一个高频用户挤占所有人的令牌；明确的 429 与 Retry-After 让客户端能退避重试而不是盲目打爆网关。" />

## 随堂检测
<Quiz no="078-1" q="令牌桶限流里，capacity 与 rate 分别控制什么？" :options='["capacity 控制补充速度，rate 控制最大并发","capacity 控制允许的最大突发量，rate 控制长期平均放行速率","两者都控制超时时间","capacity 是窗口大小，rate 是错误码"]' answer="1" explain="桶容量决定闲置后一次最多能放行多少请求（突发），补充速率决定稳态下的平均 QPS；二者独立，才既能限平均又能容忍突发。" />

<Predict no="078-2" q="代码 2.2 里令牌桶初始容量 3、每秒补 2 个，连续 5 次几乎无间隔地调用 Allow，结果是？" :options='["5 次全放行","前 3 次放行，后 2 次拒绝","前 2 次放行，后 3 次拒绝","全部拒绝"]' answer="1" output="第 1 到 3 次请求 放行=true；第 4、5 次请求 放行=false" explain="初始令牌 3 个，连续调用几乎不产生时间差，补充量可忽略；前 3 次各取走 1 个后耗尽，后 2 次因 tokens 小于 1 被拒。" />

## 一页纸速记
<KnowledgeCard :points='["网关 = 路由表 + ReverseProxy + 前置中间件，鉴权在外、转发在内，未授权请求到不了后端","令牌桶按速率补充、桶满截断，capacity 管突发、rate 管平均，避免固定窗口临界双倍流量","协议转换把 REST 路径与 JSON 映射成内部方法调用，让下游服务不必懂 HTTP","网关只做边界治理，必须无状态、可水平扩容，且自身也要被限流与监控"]' :pitfalls='["把业务逻辑塞进网关，让它变成难以变更的巨石","只在网关鉴权、内部服务完全互信，绕过网关即全线失守","限流计数放本地内存却多副本部署，各实例配额不一致"]' summary="一句话记住：网关守住入口，把鉴权、限流、路由、协议转换收成一层，但别让它长出业务。" />

## 小结与下一讲

- **网关骨架**：路由表加 ReverseProxy 完成转发，鉴权中间件在外层短路，规则抽成配置便于热更。
- **限流**：令牌桶兼顾平均速率与可控突发，按用户维度隔离配额，超限返回 429 加 Retry-After。
- **边界**：协议转换屏蔽内部 RPC，网关只做横切治理且自身无状态；小集群可自研，复杂生态再选 Kong/APISIX。

[下一讲：熔断、降级、重试与雪崩防护](/microservice/resilience)
