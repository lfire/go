---
title: 第 75 讲 · 服务注册发现与健康检查
description: 自建注册表、心跳续约与健康检查，让服务实例上下线对调用方彻底透明
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★★☆" time="45 分钟" goal="让服务实例变化对调用方透明" prereq="第 74 讲 gRPC 拦截器、metadata、错误与超时" />

# 第 75 讲 · 服务注册发现与健康检查

> 前面几讲的 RPC 都假设「对端地址已知」。可一旦服务多副本、弹性扩缩容、容器随时被调度重启，写死 IP 就立刻失效：新实例没人知道，宕机的实例还在被反复调用。服务注册发现要解决的就是这件事——把「谁在哪、还活着吗」从每个调用方的配置文件里，收进一个可动态变化的组件。本讲不只介绍 Consul、etcd、Nacos 怎么用，而是用标准库把注册表、心跳续约、故障摘除、健康检查和变更推送各写一遍，再对照工业方案的取舍。只有亲手实现过，才会明白 TTL 该设多大、为什么优雅退出要先反注册、以及客户端缓存为什么必须容错。

## 这一讲你会得到什么
<CheckList id="l75-goals" title="本讲目标" :items='["用读写锁与后台巡检自建一个支持注册、续约、摘除与发现的最小注册表","区分心跳续约、主动健康探测与租约三种存活判定方式及其代价","说清客户端发现与服务端发现两种模式的调用链差异","理解 watch 推送与本地缓存，以及注册中心不可用时的降级策略"]' />

## 1. 先建立直觉

把注册中心想成小区的物业通讯录。每户人家（服务实例）入住时登记门牌与电话（注册），之后每隔一段时间给物业报个平安（心跳）；谁连续几次没报平安，物业就把他划掉（摘除）。住户想找维修工时，不用挨家敲门，直接翻通讯录（发现），而且物业会在名单变动时主动通知订阅了的人（watch）。这样扩缩容、重启、宕机都只是通讯录里的一行改动，调用方几乎无感。

这里最微妙的是「怎么算死了」。心跳是实例自证活着，成本低但可能「进程还在、业务已卡死」；主动探测由注册中心定期打 /health，更贴近真实可用性但带来额外流量；租约（lease）把存活绑定在一个带 TTL 的 key 上，续约即续命，是 etcd 一类 CP 系统的做法。三种方式常常叠加使用：心跳快速续约，健康检查兜底业务级故障。

<ConceptDiagram type="discovery" title="注册、心跳、发现与摘除" caption="实例注册并定期续约，调用方订阅变更拿到健康列表，失效实例被巡检摘除" />

<FlowTimeline :steps='[{"title":"实例启动","desc":"向注册中心登记服务名、ID 与地址","tone":"blue"},{"title":"定期心跳","desc":"按间隔续约，刷新最后上报时间","tone":"blue"},{"title":"调用方订阅","desc":"watch 服务名，拿到当前健康实例列表","tone":"green"},{"title":"变更推送","desc":"实例上下线时把新列表推给所有订阅者","tone":"green"},{"title":"故障摘除","desc":"超过 TTL 未续约的实例被巡检删除","tone":"orange"},{"title":"本地容错","desc":"注册中心短暂不可用时沿用最后一份缓存","tone":"orange"}]' direction="horizontal" />

## 2. 语法与规则

### 2.1 最小注册表：注册、续约、巡检、发现

一张按服务名分组的 map 加一把读写锁，就撑起了注册中心的骨架。`Register` 登记实例，`Beat` 刷新心跳，`StartSweep` 起后台 goroutine 定期把超时未续约的实例摘掉，`Resolve` 只返回健康地址。为让输出稳定，发现结果按字典序排一下。

```go
package main

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

type Instance struct {
	ID       string
	Addr     string
	Healthy  bool
	lastBeat time.Time
}

// Registry 是最小的服务注册表：按服务名分组的实例表加一把读写锁。
type Registry struct {
	mu       sync.RWMutex
	services map[string]map[string]*Instance
	ttl      time.Duration
}

func NewRegistry(ttl time.Duration) *Registry {
	return &Registry{services: make(map[string]map[string]*Instance), ttl: ttl}
}

func (r *Registry) Register(service, id, addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.services[service] == nil {
		r.services[service] = make(map[string]*Instance)
	}
	r.services[service][id] = &Instance{ID: id, Addr: addr, Healthy: true, lastBeat: time.Now()}
}

// Beat 续约：刷新最后心跳时间；实例不存在说明已摘除，返回 false 让客户端重新注册。
func (r *Registry) Beat(service, id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst := r.services[service][id]
	if inst == nil {
		return false
	}
	inst.lastBeat = time.Now()
	inst.Healthy = true
	return true
}

func (r *Registry) Deregister(service, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.services[service], id)
}

// Resolve 做一次发现：只返回健康实例，排序后结果可预测。
func (r *Registry) Resolve(service string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, inst := range r.services[service] {
		if inst.Healthy {
			out = append(out, inst.Addr)
		}
	}
	sort.Strings(out)
	return out
}

// sweep 巡检：超过 ttl 没心跳的实例直接摘除。
func (r *Registry) sweep() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for _, set := range r.services {
		for id, inst := range set {
			if now.Sub(inst.lastBeat) > r.ttl {
				delete(set, id)
			}
		}
	}
}

func (r *Registry) StartSweep(interval time.Duration, stop <-chan struct{}) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				r.sweep()
			case <-stop:
				return
			}
		}
	}()
}

func main() {
	reg := NewRegistry(300 * time.Millisecond)
	stop := make(chan struct{})
	reg.StartSweep(100*time.Millisecond, stop)
	defer close(stop)

	reg.Register("user", "u1", "10.0.0.1:9000")
	reg.Register("user", "u2", "10.0.0.2:9000")
	fmt.Println("初始实例:", reg.Resolve("user"))

	// 只有 u1 持续续约；u2 静默，应当被巡检摘除
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				reg.Beat("user", "u1")
			case <-done:
				return
			}
		}
	}()
	time.Sleep(700 * time.Millisecond)
	close(done)
	fmt.Println("巡检后:", reg.Resolve("user"))
}
```

<OutputBox title="运行结果" output="初始实例: [10.0.0.1:9000 10.0.0.2:9000]\n巡检后: [10.0.0.1:9000]" note="u2 超过 TTL 未续约被摘除；u1 一直续约保持健康。Resolve 排序后输出稳定，便于断言。" />

### 2.2 主动健康探测

心跳只能证明「进程还活着」，证明不了「业务还能服务」。所以工业方案还会做主动探测：注册中心或网关定期请求实例的 /health，连续失败就标记不健康。用标准库几行就能复刻。

```go
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
)

// probe 主动探测实例健康；真实 Consul 支持 HTTP、TCP、脚本等多种检查。
func probe(client *http.Client, addr string) bool {
	resp, err := client.Get(addr + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func main() {
	// 两个后端：一个返回 200，一个返回 503 模拟业务不可用
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	client := &http.Client{Timeout: 500 * time.Millisecond}
	fmt.Println("健康实例 probe:", probe(client, good.URL))
	fmt.Println("故障实例 probe:", probe(client, bad.URL))
}
```

<LineByLine code="func (r *Registry) Beat(service, id string) bool {\n    inst := r.services[service][id]\n    if inst == nil {\n        return false\n    }\n    inst.lastBeat = time.Now()\n    inst.Healthy = true\n    return true\n}" :notes='["Beat 是续约入口，返回是否已登记","按服务名与实例 ID 定位到具体实例","取不到说明已被巡检摘除，返回 false 让调用方重新注册","刷新最后心跳时间，这是判定存活的唯一依据","顺便把健康标记恢复为真","返回 true 表示本次续约成功","函数以右括号收尾"]' title="心跳续约逐行拆解" />

## 3. 深入一步

### 3.1 watch 推送与客户端发现

发现有两种组织方式。**客户端发现**：调用方自己查注册表、自己选节点，少一跳、策略可定制，代价是每个语言都要实现一遍 SDK。**服务端发现**：调用方只认网关或 LB 的单一地址，由中间层查表转发，语言无关、治理集中，代价是多一跳且入口必须高可用。gRPC 的 resolver 属于前者，Kubernetes Service 属于后者。无论哪种，变更都靠 watch 推送而非每次拉取。

```go
package main

import (
	"fmt"
	"sync"
)

// ServiceSet 维护一份实例列表，并支持订阅者 watch 变更。
type ServiceSet struct {
	mu   sync.RWMutex
	list []string
	subs []chan []string
}

func (s *ServiceSet) Update(list []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := append([]string(nil), list...)
	s.list = cp
	for _, ch := range s.subs { // 推送快照，避免订阅者拿到共享底层数组
		ch <- append([]string(nil), cp...)
	}
}

func (s *ServiceSet) Watch() <-chan []string {
	ch := make(chan []string, 1)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	ch <- append([]string(nil), s.list...) // 订阅即给一份当前快照
	s.mu.Unlock()
	return ch
}

func (s *ServiceSet) Snapshot() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.list...)
}

func main() {
	set := &ServiceSet{list: []string{"10.0.0.1:9000"}}
	ch := set.Watch()
	fmt.Println("客户端发现 初始:", <-ch)

	// 注册中心推送一次变更，客户端本地缓存随之更新，无需重新拉取
	set.Update([]string{"10.0.0.1:9000", "10.0.0.3:9000"})
	fmt.Println("收到推送:", <-ch)
	fmt.Println("本地快照:", set.Snapshot())
}
```

| 维度 | Consul | etcd | Nacos |
| --- | --- | --- | --- |
| 一致性 | CP 为主，Raft | 强一致 CP，Raft | AP 与 CP 可切换 |
| 存活判定 | 内置 HTTP/TCP/脚本检查 | 租约 lease 加 keepalive | 心跳加主动探测 |
| 发现方式 | DNS 或 HTTP API，支持 watch | watch key 前缀变更 | 长轮询加 UDP 推送 |
| 额外能力 | 多数据中心、服务网格 | KV、选主、K8s 底座 | 配置中心一体 |
| 典型场景 | 多语言混合部署 | 自研控制面、K8s 生态 | Java 与 Spring 生态 |

| 发现模式 | 谁查注册表 | 谁选节点 | 优点 | 代价 |
| --- | --- | --- | --- | --- |
| 客户端发现 | 调用方 SDK | 调用方负载均衡器 | 少一跳、策略可定制 | 每种语言都要实现 SDK |
| 服务端发现 | 网关或 LB | 中间层 | 语言无关、治理集中 | 多一跳、入口需高可用 |

<Term word="心跳续约" text="实例定期上报刷新存活时间，超过 TTL 未续约即被判定失效并摘除" />
<Term word="租约 lease" text="把存活绑定在带 TTL 的 key 上，续约即续命，是 etcd 常用的强一致做法" />
<Term word="优雅退出" text="进程停止前先反注册并停止接新请求，避免摘除窗口内仍有流量打进来" />

<Callout type="warn" title="TTL 与心跳间隔是一场权衡" text="TTL 太短，一次 GC 停顿或网络抖动就会误摘健康实例；TTL 太长，故障实例又要很久才被清掉。经验是心跳间隔取 TTL 的三分之一左右，配合连续多次失败才摘除。另外务必先反注册再关进程，否则依赖超时摘除的那段时间里，请求还会打到已经停服的地址上。" />

### 3.2 为什么写死 IP 必然失效，以及存活判定为何要叠加

把地址写进配置文件，隐含了一个前提：实例的地址在服务的整个生命周期里不变。微服务里这个前提第一条就不成立——弹性扩缩容会随时加实例，容器被调度重启后 IP 会变，实例崩溃后地址直接作废。于是「谁在哪、还活着吗」不再是配置常量，而是一份持续变化的动态数据。注册中心的本质，就是把这份动态数据从每个调用方的配置文件里收拢成一个可订阅的数据源；调用方不再持有地址，而是持有一个「服务名」并在运行时解析它。

真正微妙的是「怎么算死了」，三种判定各有盲区，所以生产里几乎总是叠加使用：

| 判定方式 | 谁证明活着 | 能发现 | 盲区 | 代价 |
| --- | --- | --- | --- | --- |
| 心跳续约 | 实例自证 | 进程崩溃、网络断开 | 进程还在但业务已卡死（如死锁、依赖挂了） | 低，仅周期上报 |
| 主动健康探测 | 注册中心定期打 health 接口 | 业务级不可用 | 探测流量本身、探测路径未必覆盖真实依赖 | 中，额外请求 |
| 租约 lease | 带 TTL 的 key 是否续约 | 进程或会话消失 | 与心跳类似，仍不查业务状态 | 依赖 CP 存储 |

心跳负责快速续约、兜住进程级故障；健康检查负责兜住「进程活着但业务不可用」，例如数据库连不上时实例应主动在 /health 报不健康，把流量让给别的副本。这就是二者的分工。

TTL 的取值是一场能算清楚的权衡。设心跳间隔为 h、TTL 为 t：t 太短，一次几百毫秒的 GC 停顿或网络抖动就可能让心跳迟到，健康实例被误摘；t 太长，故障实例要在 t 之后才被清掉，这段时间里请求持续打到死地址。工程经验是让 h 约为 t 的三分之一，并配合「连续多次失败才摘除」的阈值——用时间窗口滤掉单点抖动，而不是一次超时就删除。

<SequenceDiagram title="注册续约摘除与本地缓存的完整时序" caption="实例注册并续约，巡检按 TTL 摘除，watch 推送变更，调用方回退本地缓存" :actors='["Instance","Registry","Watcher","Client"]' :steps='[{"from":"Instance","to":"Registry","label":"注册实例","note":"登记服务名与地址","kind":"call"},{"from":"Instance","to":"Registry","label":"周期心跳","note":"刷新存活时间","kind":"call"},{"from":"Watcher","to":"Registry","label":"订阅变更","note":"watch 服务名","kind":"call"},{"from":"Registry","to":"Watcher","label":"推送列表","note":"健康实例快照","kind":"call"},{"from":"Instance","to":"Instance","label":"停止心跳","note":"进程崩溃","kind":"self"},{"from":"Registry","to":"Registry","label":"超 TTL 摘除","note":"巡检删除","kind":"self"},{"from":"Registry","to":"Watcher","label":"推送新列表","note":"移除故障实例","kind":"call"},{"from":"Watcher","to":"Client","label":"回退本地缓存","note":"沿用可用地址","kind":"call"}]' />

### 3.3 替代方案取舍：发现模式与一致性侧重

「谁查注册表、谁选节点」有不止一种组织方式，选错会直接决定跳数与治理成本：

| 发现模式 | 谁查注册表 | 谁选节点 | 优点 | 失败边界 |
| --- | --- | --- | --- | --- |
| 客户端发现 | 调用方 SDK | 调用方负载均衡器 | 少一跳、策略可定制 | 每种语言都要实现 SDK，升级要全量发版 |
| 服务端发现 | 网关或 LB | 中间层 | 语言无关、治理集中 | 多一跳，入口必须高可用 |
| DNS 发现 | 递归解析器 | DNS 轮询 | 与语言完全解耦 | 受 TTL 缓存约束，故障摘除慢 |
| Service Mesh | 控制面下发 | sidecar 代理 | 应用零感知、策略集中 | 多一跳、运维与排障复杂度高 |

还有一层更根本的取舍是注册中心自身的一致性侧重，这正是后面 L8 会展开的 CAP 在发现场景的具体投影：

| 侧重 | 代表 | 分区时的表现 | 业务更怕什么时选它 |
| --- | --- | --- | --- |
| CP 强一致 | etcd、Consul（Raft） | 可能拒绝写入，新实例注册不上 | 更怕调到错误或过期的地址 |
| AP 高可用 | Nacos AP 模式、Eureka | 可能短暂返回已下线的旧地址 | 更怕分区时根本注册不上、服务起不来 |

没有绝对正确的选择：结算类服务往往更怕「调到旧地址写错数据」而偏 CP，入口网关往往更怕「分区时新实例注册不上、容量起不来」而偏 AP。

### 3.4 可验证实验：缓存回退与健康列表观测

「注册中心挂了怎么办」不能只靠口头承诺，用一段可编译代码就能验证回退逻辑：

```go
package main

import "fmt"

// resolve 在注册中心报错时回退到最后一份本地缓存。
func resolve(remote []string, err error, cache []string) []string {
	if err != nil {
		return cache
	}
	return remote
}

func main() {
	cache := []string{"10.0.0.1:9000", "10.0.0.2:9000"}
	fmt.Println("正常:", resolve([]string{"10.0.0.1:9000"}, nil, cache))
	fmt.Println("抖动:", resolve(nil, fmt.Errorf("registry timeout"), cache))
}
```

<OutputBox title="运行结果" output="正常: [10.0.0.1:9000]\n抖动: [10.0.0.1:9000 10.0.0.2:9000]" note="正常路径用实时列表；注册中心报错时沿用缓存，调用方仍在可用地址上继续工作。" />

逐字段解释：正常那行只返回实时解析到的一个地址，说明缓存没有被无脑覆盖；抖动那行 `err` 非空，`resolve` 直接返回缓存里的两个地址——这份缓存就是注册中心短暂失联时全站不至于失去下游的保险。注意真实实现还要给缓存设一个最大存活时间，避免列表无限期陈旧。

再用 Consul 的健康检查 API 观察「只返回通过检查的实例」：

```bash
curl -s 'http://localhost:8500/v1/health/service/order?passing' | jq '.[].Service.Address'
```

```text
"10.0.0.1"
"10.0.0.2"
```

逐字段解释：`/v1/health/service/order` 查询 order 服务的健康实例，`passing` 参数要求只返回通过了所有健康检查的节点；`jq` 把每项的 `Service.Address` 抽出来，于是输出两行地址。若某实例心跳超时被判不健康，它不会出现在结果里——这正是被动摘除对调用方可见的形式。

并发边界可以用竞态检测器验证：把 2.1 的注册表去掉 `sync.RWMutex` 后，用多个 goroutine 同时 `Register` 与 `Resolve` 跑 `go test -race`，会报出对 `services` 这个 map 的并发读写冲突；原文用读写锁保护它，正是为了让发现路径在并发下安全。

## 4. 工程实践

生产里最容易被忽略的是「注册中心自己挂了怎么办」。客户端必须把最后一次拿到的实例列表缓存在本地，注册中心短暂不可用时沿用旧列表继续调用，而不是让全站瞬间失去所有下游——这就是降级容错。watch 断线要带退避重连，并在重连后做一次全量拉取校准，避免错过期间的变更推送。

<Compare :panels='[{"label":"客户端发现","code":"addrs := resolver.Resolve(service)\npick := balancer.Pick(addrs)\ncall(ctx, pick, req)","note":"调用方自己持有实例表并选节点，少一跳、策略可定制，但每个语言 SDK 都要实现一遍"},{"label":"服务端发现","code":"call(ctx, gatewayAddr, req)\n// 由网关查表并转发","note":"调用方只认一个入口，语言无关、治理集中，但多一跳且网关必须高可用"}]' title="两种发现模式的调用形态" />

几个落地要点：实例 ID 要全局唯一且稳定，别用会随重启变化的随机值，否则上一次的租约与新注册会打架；健康检查接口要区分「进程活着」与「依赖就绪」，数据库连不上时应当主动报不健康，把流量让给别的副本；元数据里带上版本号、权重、地域，供后面的负载均衡与灰度使用；多注册中心或跨机房场景要意识到 CAP 取舍——CP 系统在分区时可能拒绝写入导致新实例注册不上，AP 系统则可能短暂返回已下线的地址，选择取决于业务更怕「注册不上」还是「调到旧地址」。最后，把注册、心跳、反注册收进框架的生命周期里统一管理，业务代码不该感知它们的存在。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l7-microservice/lesson-075-discovery`](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-075-discovery)。clone 仓库后进入 `examples/l7-microservice`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

### 4.2 注册发现的失败矩阵

发现系统的故障几乎都围绕「地址这份动态数据可不可信」展开：

| 故障 | 触发点 | 现象 | 防线 |
| --- | --- | --- | --- |
| 注册中心抖动 | 自身重启或网络分区 | 全站瞬间失去所有下游 | 本地缓存 + 退避重连 |
| 缓存旧地址 | 失联期间沿用了过期列表 | 请求打到已下线实例 | 缓存设最大存活时间 + 快速失败 |
| 心跳风暴 | 上万实例同一时刻续约 | 注册中心被自身流量压垮 | 心跳间隔加抖动 + 分组错峰 |
| GC 停顿误摘 | TTL 设得太短 | 健康实例被误摘，流量抖动 | 心跳取 TTL 三分之一 + 连续失败阈值 |
| 优雅退出漏流量 | 直接 kill 不反注册 | 摘除窗口内请求打到停服实例 | 先反注册再停止接新流量 |
| 网络分区 | CP 与 AP 侧重不同 | 新实例注册不上，或调到旧地址 | 按业务怕哪种失败选一致性侧重 |

### 4.3 两条真实工程误解

**误解一：注册中心挂了，全站立刻不可用。** 这是把注册中心当成了调用链上的实时强依赖。实际上调用方完全可以缓存最后一次拿到的健康列表，注册中心短暂失联时沿用旧列表继续工作，同时带退避重连、重连成功后做一次全量拉取校准。把发现做成「可降级的缓存」而不是「每次必查的强依赖」，是这套系统可用性的底线。

<Compare :panels='[{"label":"错误：无缓存","code":"addrs := resolve(service)\ncall(pick(addrs))","note":"注册中心一抖动 resolve 就失败，全站失去下游"},{"label":"正确：缓存兜底","code":"addrs := resolve(service)\nif err != nil { addrs = cache }\ncall(pick(addrs))","note":"沿用最后一份健康列表，配合退避重连与全量校准"}]' title="注册中心失联时怎么办" />

**误解二：心跳成功就等于服务可用。** 心跳只能证明进程还在并按点上报，证明不了业务还能处理请求：数据库连接池耗尽、依赖的下游全挂、goroutine 死锁时，进程照样心跳，请求却全部失败。健康检查接口必须区分「进程存活」与「依赖就绪」，依赖异常时主动报不健康，把流量让给别的副本；也正因如此，心跳与主动探测在生产里总是叠加而非二选一。

<Callout type="warn" title="心跳活着不等于业务可用" text="让 health 接口检查关键依赖：数据库连不上、缓存不可用或线程池耗尽时主动返回不健康，注册中心据此摘除流量，避免把请求持续送到一个还能心跳却已无法服务的实例。" />

### 4.4 与后续阶段的呼应

本讲反复出现的「租约、watch、CP 侧重」在 L8 会落到具体原理：[/distributed/raft](/distributed/raft) 讲 etcd 的 Leader 选举、日志复制与 watch 机制，正是强一致注册中心的底座；[/distributed/cap-base](/distributed/cap-base) 把 CP 与 AP 的取舍放进 CAP 框架重新审视，能解释为什么分区时有的系统拒绝注册、有的返回旧地址；[/distributed/high-availability](/distributed/high-availability) 的多活与故障域设计，则把「注册中心自己挂了」升级为整体容灾问题。到 L10，[/projects/micro-seckill](/projects/micro-seckill) 秒杀前临时扩容的实例要靠注册发现对调用方透明，[/projects/micro-ecommerce](/projects/micro-ecommerce) 的服务清单则由第 76 讲的 resolver 接着消费。

<Quiz q="注册中心发生网络分区时，CP 系统（如 etcd）相比 AP 系统最典型的表现是什么？" :options='["继续返回可能过期的地址","分区期间可能拒绝写入，新实例注册不上","自动把流量切到另一个机房","直接清空所有实例"]' answer="1" explain="CP 优先保证一致，分区时少数派一侧可能拒绝写入，于是新实例注册不上但已注册地址可信；AP 优先可用，会继续返回可能过期的地址。选哪个取决于业务更怕注册不上还是怕调到旧地址。" />

<Predict q="注册中心调用返回 connection refused，客户端若实现了本地缓存回退，resolve 最终返回什么？" :options='["空列表","最后一次缓存的健康地址列表","直接 panic","只返回第一个地址"]' answer="1" output="正常: [10.0.0.1:9000]；抖动: [10.0.0.1:9000 10.0.0.2:9000]" explain="正常路径用实时解析结果；注册中心报错时回退到缓存里的健康地址，调用方仍能继续工作，这正是本地缓存作为可用性底线的意义。" />

## 动手练一练
<Exercise title="给注册表加上主动摘除与连续失败阈值" task="扩展 2.1 的 Registry：健康检查连续失败 3 次才把实例标记为不健康，而非一次超时就删；再实现 Deregister 的优雅退出路径，并在 Watch 断线时返回最后一份缓存。" :hints='["给 Instance 增加一个 failures 计数，成功心跳时清零","sweep 里用计数判断，达到阈值才 delete，避免抖动误摘","客户端把上一次 Resolve 的结果存在本地，注册中心出错时返回它"]' solution="要点一：Instance 增加 failures 字段，Beat 或 probe 成功时清零，失败时自增。\n要点二：sweep 中仅当 failures 达到 3 才 delete，单有一次超时只标记、不摘除。\n要点三：优雅退出先调 Deregister 再关闭监听；客户端缓存最后一次 Snapshot，请求注册中心报错时回退使用它。" explain="连续失败阈值过滤掉瞬时抖动，本地缓存让调用方在注册中心短暂失联时仍能工作，二者是发现系统可用性的两道保险。" />

## 随堂检测
<Quiz no="75-1" q="实例进程崩溃、来不及反注册，注册中心靠什么把它的地址从列表里清掉？" :options='["客户端报错后主动删除","心跳超过 TTL 未续约，被巡检判定失效并摘除","DNS 缓存自然过期","负载均衡器重试把它拉黑"]' answer="1" explain="心跳是实例自证存活的唯一手段；超过 TTL 没续约，巡检 goroutine 就会把该实例从表里摘除，调用方随后经 watch 拿到新列表。" />

<Predict no="75-2" q="代码 2.1 里 u1 持续心跳、u2 静默，ttl 300 毫秒、巡检 100 毫秒，700 毫秒后 Resolve 返回什么？" :options='["两个地址都在","只剩 u1 的地址","只剩 u2 的地址","返回空列表"]' answer="1" output="巡检后: [10.0.0.1:9000]" explain="u2 超过 TTL 未续约被 sweep 摘除；u1 每 100 毫秒续约一次始终健康，列表经 sort 后稳定输出，只剩那一个地址。" />

## 一页纸速记
<KnowledgeCard :points='["注册表 = 按服务名分组的实例表加读写锁；注册、续约、巡检摘除、发现是四个基本动作","心跳是实例自证存活，主动探测判断业务可用，租约把存活绑在带 TTL 的 key 上","客户端发现由调用方查表选节点，服务端发现由网关或 LB 代劳，二者在跳数与治理集中度上取舍","watch 推送让调用方拿到实时列表，本地缓存让注册中心短暂失联时仍能继续工作"]' :pitfalls='["TTL 设得太短，一次 GC 或网络抖动就误摘健康实例","进程直接退出不反注册，摘除窗口内流量仍打到已停服地址","watch 断线不重连且不回退缓存，注册中心一抖全站失去下游"]' summary="一句话记住：注册中心把「谁在哪、还活着吗」变成可订阅的动态数据，其余都是可靠性补丁。" />

## 小结与下一讲

- **自建骨架**：注册表用读写锁保护实例表，心跳续约刷新存活，巡检按 TTL 摘除失效实例，发现只返回健康地址。
- **存活判定**：心跳、主动探测、租约各有代价，生产常叠加使用，并配连续失败阈值与优雅退出。
- **发现模式**：客户端发现少一跳可定制，服务端发现语言无关治理集中；watch 推送加本地缓存是可用性底线。

[下一讲：负载均衡与 resolver 源码](/microservice/load-balance)
