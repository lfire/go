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

## 4. 工程实践

生产里最容易被忽略的是「注册中心自己挂了怎么办」。客户端必须把最后一次拿到的实例列表缓存在本地，注册中心短暂不可用时沿用旧列表继续调用，而不是让全站瞬间失去所有下游——这就是降级容错。watch 断线要带退避重连，并在重连后做一次全量拉取校准，避免错过期间的变更推送。

<Compare :panels='[{"label":"客户端发现","code":"addrs := resolver.Resolve(service)\npick := balancer.Pick(addrs)\ncall(ctx, pick, req)","note":"调用方自己持有实例表并选节点，少一跳、策略可定制，但每个语言 SDK 都要实现一遍"},{"label":"服务端发现","code":"call(ctx, gatewayAddr, req)\n// 由网关查表并转发","note":"调用方只认一个入口，语言无关、治理集中，但多一跳且网关必须高可用"}]' title="两种发现模式的调用形态" />

几个落地要点：实例 ID 要全局唯一且稳定，别用会随重启变化的随机值，否则上一次的租约与新注册会打架；健康检查接口要区分「进程活着」与「依赖就绪」，数据库连不上时应当主动报不健康，把流量让给别的副本；元数据里带上版本号、权重、地域，供后面的负载均衡与灰度使用；多注册中心或跨机房场景要意识到 CAP 取舍——CP 系统在分区时可能拒绝写入导致新实例注册不上，AP 系统则可能短暂返回已下线的地址，选择取决于业务更怕「注册不上」还是「调到旧地址」。最后，把注册、心跳、反注册收进框架的生命周期里统一管理，业务代码不该感知它们的存在。

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
