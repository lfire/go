---
title: 第 077 讲 · 配置中心与动态配置
description: 自建命名空间、长轮询、原子热更新与灰度回滚，做到配置变更不重启
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★☆☆" time="40 分钟" goal="做到配置变更不重启" prereq="第 076 讲 · 负载均衡与 resolver 源码" />

# 第 077 讲 · 配置中心与动态配置

> 改一个限流阈值，几十个服务、上百个实例要重新打包发布一遍——这是把配置和代码绑死最直接的代价。配置中心要做的是把「会变的值」从二进制里抽出来，集中存放、按环境隔离、变更时实时推到进程内，并保留每一次发布的历史以便一键回滚。本讲用标准库把命名空间、长轮询、原子热更新、灰度、校验与客户端容错各造一遍，最后对照 Nacos、Apollo、etcd 的实现取舍。理解之后你会清楚：热更新不是「把值换掉」这么简单，能不能不重启地生效，取决于配置本身是否允许动态加载。

## 这一讲你会得到什么
<CheckList id="l77-goals" title="本讲目标" :items='["用命名空间、dataId 与版本号建立可治理的配置模型，并保留历史用于回滚","用携带版本号的长轮询替代高频空轮询，理解它与监听推送的差别","用 atomic.Value 做无锁热更新，配合校验、灰度与本地缓存保证安全","区分可热更与不可热更的配置，知道哪些变更仍必须重启"]' />

## 1. 先建立直觉

把配置中心想成小区公告栏。物业（配置中心）只管贴公告，每户（服务实例）自己来看。最笨的办法是每户每分钟下楼看一次（定时轮询），大部分时候白跑；聪明的办法是住户把「我上次看到的是第几版」告诉物业，公告没换就让他先在大厅等着，一换立刻通知（长轮询）；再进一步是物业直接给每户装个门铃，换公告就按铃（监听推送）。三种方式的差别，全在「实时性」与「服务端压力」之间。

而「公告换了之后家里要怎么调整」才是难点。把 `timeout_ms` 从 500 改成 800，进程里读配置的那个对象必须能原地换成新值，且换的瞬间不能有 goroutine 读到撕裂的半成品——这正是 `atomic.Value` 或读写锁的用武之地。版本历史则让「改错了」变成一条回滚命令，而不是又一次全量发布。

<ConceptDiagram type="discovery" title="配置中心与客户端监听" caption="配置中心按命名空间存放带版本的配置，客户端通过长轮询或 watch 拿到变更并原地热更新" />

<FlowTimeline :steps='[{"title":"发布配置","desc":"按命名空间与 dataId 写入新值，版本号递增","tone":"blue"},{"title":"灰度","desc":"先对少量实例生效，观察指标","tone":"blue"},{"title":"客户端等待","desc":"带上已知版本长轮询或订阅 watch","tone":"green"},{"title":"校验","desc":"对新值做类型与范围校验，非法则拒收","tone":"green"},{"title":"原子热更","desc":"一次性替换配置快照，业务无锁读取","tone":"orange"},{"title":"回滚","desc":"异常时按历史版本一键恢复","tone":"orange"}]' direction="horizontal" />

## 2. 语法与规则

### 2.1 配置模型、版本与原子热更新

一份配置至少要有三要素：命名空间（隔离环境与服务）、dataId（唯一定位一份配置）、版本号（判定新旧与回滚）。存储侧用 `atomic.Value` 保存当前快照，读多写少时业务侧无锁读取；每次发布把旧值存入历史，回滚就是把它重新挂回去。

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Config 是一份不可变快照；Namespace 用环境与服务两级隔离。
type Config struct {
	Namespace string
	Version   int
	Data      map[string]string
}

// Store 保存当前值与历史版本，支持热更新与按版本回滚。
type Store struct {
	mu      sync.RWMutex
	current atomic.Value // 存 *Config，读路径完全无锁
	history []*Config
}

func NewStore(init *Config) *Store {
	s := &Store{}
	s.current.Store(init)
	s.history = append(s.history, init)
	return s
}

func (s *Store) Get() *Config { return s.current.Load().(*Config) }

func (s *Store) Publish(data map[string]string) *Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.Get()
	next := &Config{Namespace: old.Namespace, Version: old.Version + 1, Data: data}
	s.current.Store(next) // 原子替换整份快照，业务无需停服
	s.history = append(s.history, next)
	return next
}

func (s *Store) Rollback(version int) (*Config, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.history {
		if c.Version == version {
			s.current.Store(c)
			return c, true
		}
	}
	return nil, false
}

func main() {
	s := NewStore(&Config{Namespace: "prod/order", Version: 1,
		Data: map[string]string{"timeout_ms": "500"}})
	fmt.Println("初始:", s.Get().Data["timeout_ms"], "版本", s.Get().Version)
	s.Publish(map[string]string{"timeout_ms": "800"})
	fmt.Println("热更后:", s.Get().Data["timeout_ms"], "版本", s.Get().Version)
	c, _ := s.Rollback(1)
	fmt.Println("回滚后:", c.Data["timeout_ms"], "版本", c.Version)
}
```

配置的组织方式通常写成一份清单，命名空间、灰度比例与内容一目了然：

```yaml
# 命名空间用环境与服务两级隔离，dataId 唯一定位一份配置
namespace: prod
service: order
dataId: order-server.yaml
content:
  timeout_ms: 800
  max_retry: 3
gray:
  enabled: true
  percent: 10        # 先对 10% 实例生效
  whitelist: [order-7]
```

<OutputBox title="运行结果" output="初始: 500 版本 1\n热更后: 800 版本 2\n回滚后: 500 版本 1" note="每次 Publish 版本号递增并留档，回滚只是把某个历史快照重新挂回 current，秒级完成。" />

### 2.2 长轮询：带版本地等变更

客户端轮询最怕空转：配置一天不变，却要拉上万次。长轮询的做法是请求里带上自己已知的版本号，服务端发现版本没变就把请求挂住，直到配置更新或超时才返回。这样既近乎实时，又不产生大量无效请求。

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Config struct {
	Version int
	Data    map[string]string
}

// Watcher 用「携带版本号的等待」模拟长轮询：没变更就挂住，变了立刻返回新快照。
type Watcher struct {
	mu      sync.Mutex
	version int
	data    map[string]string
	waiters []chan Config
}

func (w *Watcher) LongPoll(knownVersion int) <-chan Config {
	ch := make(chan Config, 1)
	w.mu.Lock()
	if w.version > knownVersion { // 已有更新，立即返回
		ch <- Config{Version: w.version, Data: w.data}
	} else {
		w.waiters = append(w.waiters, ch) // 挂住，等变更唤醒
	}
	w.mu.Unlock()
	return ch
}

func (w *Watcher) Update(data map[string]string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.version++
	w.data = data
	for _, ch := range w.waiters {
		ch <- Config{Version: w.version, Data: w.data}
	}
	w.waiters = nil
}

func main() {
	w := &Watcher{version: 1, data: map[string]string{"rate": "10"}}
	// 客户端带上版本 1 来等，此时无更新，请求被挂住
	pending := w.LongPoll(1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		w.Update(map[string]string{"rate": "20"})
	}()
	cfg := <-pending // 变更发生时才返回
	fmt.Printf("长轮询收到 版本=%d rate=%s\n", cfg.Version, cfg.Data["rate"])
}
```

<LineByLine code="old := s.Get()\nnext := &Config{Namespace: old.Namespace, Version: old.Version + 1, Data: data}\ns.current.Store(next)" :notes='["先取出当前快照，用于继承命名空间并推算版本号","构造一份全新的不可变快照，版本号加一，绝不原地修改旧对象","原子替换 current，读侧永远看到完整的一份，不会读到改到一半的状态"]' title="原子热更新逐行拆解" />

## 3. 深入一步

### 3.1 客户端容错、校验与灰度

配置中心也是个会挂的服务，SDK 必须能扛住它抖动：拉不到就沿用本地缓存。新值上线前还要做类型与范围校验，一次把 `timeout_ms` 误填成 `abc` 的发布不该打挂全站。灰度则用稳定的实例 ID 取模，保证同一实例每次判定一致，先放 10% 观察指标再全量。

```go
package main

import (
	"fmt"
	"strconv"
)

type Client struct{ cache map[string]string }

// Fetch 模拟从远端拉取；err 非空时由 Load 回退本地缓存。
func Fetch(remote map[string]string, err error) (map[string]string, error) {
	if err != nil {
		return nil, err
	}
	return remote, nil
}

func (c *Client) Load(remote map[string]string, fetchErr error) map[string]string {
	got, err := Fetch(remote, fetchErr)
	if err != nil {
		fmt.Println("远端不可用，回退本地缓存")
		return c.cache
	}
	// 校验：非法值不覆盖当前配置，避免一次错误发布打挂全站
	if ms, e := strconv.Atoi(got["timeout_ms"]); e != nil || ms <= 0 {
		fmt.Println("配置校验失败，保留旧值")
		return c.cache
	}
	c.cache = got
	return c.cache
}

// InGray 用稳定的实例 ID 取模做灰度，同一实例每次判定一致。
func InGray(instanceID string, percent int) bool {
	h := 0
	for _, b := range []byte(instanceID) {
		h = h*31 + int(b)
	}
	if h < 0 {
		h = -h
	}
	return h%100 < percent
}

func main() {
	c := &Client{cache: map[string]string{"timeout_ms": "500"}}
	fmt.Println("正常发布:", c.Load(map[string]string{"timeout_ms": "800"}, nil))
	fmt.Println("非法值:", c.Load(map[string]string{"timeout_ms": "abc"}, nil))
	fmt.Println("中心失联:", c.Load(nil, fmt.Errorf("connection refused")))
	fmt.Println("灰度命中:", InGray("order-7", 10), InGray("order-8", 10))
}
```

| 变更机制 | 客户端行为 | 实时性 | 服务端压力 | 代表实现 |
| --- | --- | --- | --- | --- |
| 定时轮询 | 固定间隔全量拉取 | 秒到分钟 | 大量空轮询浪费 | 早期自研、cron |
| 长轮询 | 带版本挂住，变更才返回 | 亚秒级 | 占用连接但无空转 | Nacos、Apollo |
| 监听推送 | 建长连接，服务端主动推 | 毫秒级 | 需维护连接与重连 | etcd watch、Consul blocking query |

| 配置类别 | 例子 | 能否热更 | 变更方式 |
| --- | --- | --- | --- |
| 启动参数 | 监听端口、数据库地址 | 否 | 重启进程 |
| 运行时开关 | 限流阈值、功能开关、超时 | 是 | 热更新加灰度 |
| 敏感凭据 | 密钥、证书 | 谨慎 | 轮换并全程审计 |

<Term word="命名空间" text="用环境与服务等维度隔离配置，避免不同集群的同名配置互相污染" />
<Term word="长轮询" text="客户端携带已知版本等待，服务端在配置变化或超时后才响应，消除空轮询" />
<Term word="热更新" text="运行时原子替换配置快照而不重启进程，前提是配置本身允许动态加载" />

<Callout type="warn" title="热更新不是万能的" text="监听端口、连接池大小、数据库地址这类只在启动时读取一次的参数，改了配置也不会生效，硬做热更反而制造「配置与运行不一致」的假象。另外一份配置被多个 goroutine 读取时，务必整体替换不可变快照，切忌逐字段原地赋值。" />

### 3.2 为什么配置要外置，以及热更新到底能改什么

把配置和代码绑死，代价不是「改起来麻烦」这么轻：改一个限流阈值，几十个服务、上百个实例都要重新打包、重新发布、重新回滚一遍，变更窗口长、风险集中，而且一次发布里往往夹带无关改动，出错时很难定位。配置中心的动机就是把「会变的值」从二进制里抽出来，让它们变成可独立发布、可灰度、可回滚的数据。

但「热更新」有严格的物理前提，理解它才不会做出假热更。一个值想要不重启就生效，必须同时满足两点：它在运行时被反复读取，而不是只在启动时读一次；并且读取点换成新值不会破坏正在进行的逻辑。监听端口、连接池大小、数据库地址这类参数，进程启动时就 bind 了端口、建好了连接池，之后改配置根本没有任何代码再去读它——硬做热更只会制造「配置面板显示 800，实际进程还跑在 500」的假象，排查时极具误导性。所以配置要分级：能热更的放配置中心，不能热更的留在启动参数。

这也解释了为什么热更新要用 `atomic.Value` 整份替换不可变快照。假设一份配置有 timeout 与 retry 两个字段，逐字段原地赋值时，一个并发读者完全可能读到「timeout 已是新值、retry 还是旧值」的撕裂组合；而整份构造新对象、一次性 Store，读者 Load 到的永远是一份完整、自洽的快照。与读写锁相比，`atomic.Value` 让读路径完全无锁，特别适合读多写少的配置读取。

<SequenceDiagram title="长轮询到原子热更新再到回滚" caption="带版本等待变更，校验灰度后整份替换快照，异常时按历史版本回滚" :actors='["Admin","Center","Client","App"]' :steps='[{"from":"Admin","to":"Center","label":"发布新配置","note":"写入新值","kind":"call"},{"from":"Center","to":"Center","label":"版本号递增","note":"留档待回滚","kind":"self"},{"from":"Client","to":"Center","label":"带版本长轮询","note":"没变更就挂住","kind":"call"},{"from":"Center","to":"Client","label":"变更才返回","note":"推送新快照","kind":"call"},{"from":"Client","to":"Client","label":"类型范围校验","note":"非法则拒收","kind":"self"},{"from":"Client","to":"Client","label":"灰度判定","note":"稳定哈希","kind":"self"},{"from":"Client","to":"App","label":"原子替换快照","note":"整份 Store","kind":"call"},{"from":"App","to":"App","label":"无锁读取","note":"Load 完整快照","kind":"self"},{"from":"Admin","to":"Center","label":"异常触发回滚","note":"一键恢复","kind":"call"},{"from":"Center","to":"Client","label":"推送历史版本","note":"版本 N 减一","kind":"call"}]' />

### 3.3 替代方案取舍：变更机制、更新方式与灰度判定

「配置变了怎么让客户端知道」至少有四种走法，差别全在实时性与服务端压力之间：

| 变更机制 | 实时性 | 服务端压力 | 失败边界 | 代表实现 |
| --- | --- | --- | --- | --- |
| 定时轮询 | 秒到分钟 | 大量空轮询浪费 | 间隔内变更不可见 | cron、早期自研 |
| 长轮询 | 亚秒级 | 占用连接但无空转 | 断线要退避重连并全量校准 | Nacos、Apollo |
| 监听推送 | 毫秒级 | 需维护长连接与重连 | 连接风暴、推送丢失 | etcd watch、Consul blocking query |
| GitOps 声明式 | 分钟级 | 低，靠控制器收敛 | 收敛有延迟，需声明与实际比对 | Argo CD、Helm |

长轮询之所以「近乎实时又不空转」，是因为请求里带了客户端已知的版本号：版本没变就把连接挂住，变更发生或超时才响应，把「反复问」变成「带条件地等」。进程内怎么换值同样有取舍：

| 更新方式 | 一致性 | 读路径开销 | 适用 |
| --- | --- | --- | --- |
| atomic.Value 整份替换 | 强，读者只见完整快照 | 无锁 | 读多写少的配置 |
| 逐字段加锁赋值 | 需读者也持锁才安全 | 每读加锁 | 字段少、读写均衡 |
| 每次读时拉取远端 | 取决于远端 | 每次网络开销 | 几乎不推荐 |

灰度判定也有一处容易踩的取舍：必须用实例 ID 或固定 hash，保证同一实例每次结论一致；用随机数会让同一实例两次请求命中不同配置，指标忽上忽下且无法复现。

### 3.4 可验证实验：竞态撕裂与长轮询观测

「逐字段原地改会撕裂」可以用竞态检测器直接看到。下面这段故意让一个 goroutine 改配置、另一个同时读，复现原文警告的场景：

```go
package main

import (
	"sync"
	"testing"
)

type Config struct {
	Timeout int
	Retry   int
}

var cfg Config

func mutate() {
	cfg.Timeout = 800
	cfg.Retry = 3
}

func TestTornConfig(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			mutate()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = cfg.Timeout + cfg.Retry
		}
	}()
	wg.Wait()
}
```

```bash
go test -race -run TestTornConfig
```

```text
WARNING: DATA RACE
Read at 0x00c0000120a8 by goroutine 7:
  main.TestTornConfig.func2()
      torn_test.go:29
Previous write at 0x00c0000120a8 by goroutine 6:
  main.mutate()
      torn_test.go:20
```

逐字段解释：读 goroutine 在 `cfg.Timeout + cfg.Retry` 处读，写 goroutine 在 `mutate` 里逐字段写，两个字段属于同一结构体却没有同步，检测器报出读写冲突；地址与行号每次运行略有不同属正常。修复方式不是给这两个字段各加一把锁，而是像 2.1 那样整份构造新 `Config` 再 `atomic.Value.Store`——读者永远拿到一份完整快照，从根上消除撕裂。

长轮询的行为也能用 curl 直接观察：请求带上已知版本，服务端在配置变更或超时后才返回。

```bash
curl -s 'http://localhost:8080/config?dataId=order&version=5' --max-time 30
```

```text
{"version":6,"content":{"timeout_ms":800}}
```

逐字段解释：`version=5` 是客户端当前已知版本；返回的 `version: 6` 说明期间发生过发布，`content` 里的 `timeout_ms: 800` 是热更后的新值。若 30 秒内没有变更，连接挂满 `--max-time` 后由服务端返回空或 304，客户端随即发起下一轮带同一版本的长轮询——这正是「不产生空轮询又接近实时」的具体形态。

## 4. 工程实践

配置中心上线后，治理比功能更重要。每次发布都要留操作人、时间、旧值与新值，形成可审计的变更记录；发布流程走「校验 → 灰度 → 观察 → 全量 → 留档」，回滚必须是现成的一条命令而不是临时改回去。敏感配置别明文放在通用配置里，走独立的密钥管理与加密存储。

<Compare :panels='[{"label":"重启生效","code":"cfg := loadFromEnv()\nport := cfg.Port\nstart(port)","note":"改一个阈值就要全量重启，变更慢、风险集中，回滚也得再滚一次"},{"label":"热更新生效","code":"store.Watch(func(c Config) {\n    apply(c)\n})","note":"订阅变更即原地切换，秒级生效且可按版本一键回滚，但要求配置可动态加载"}]' title="配置变更的两种生效方式" />

还有几条落地经验。把配置分级：能热更的放配置中心，不能热更的留在启动参数，别混在一起让运维猜。客户端一定要有本地快照文件，进程冷启动时配置中心恰好不可用，也要能用上次的值先跑起来。监听断线要带退避重连，重连成功后做一次全量校准，避免错过断线期间的推送。灰度判定用实例 ID 或固定 hash，别用随机数，否则同一实例两次请求可能命中不同配置。最后，配置中心不是数据库：别把高频变化的业务状态塞进去，那会把它变成全链路的单点与瓶颈。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l7-microservice/lesson-077-config-center`](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-077-config-center)。clone 仓库后进入 `examples/l7-microservice`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

### 4.2 配置中心的失败矩阵

配置是「一份值推给上百个进程」，任何一环失守都会把小失误放大成全站事故：

| 故障 | 触发点 | 现象 | 防线 |
| --- | --- | --- | --- |
| 配置中心抖动 | 拉取失败 | 进程冷启动即崩或全站停在旧值 | 本地快照文件 + 回退缓存 |
| 错误配置打挂全站 | 把 timeout_ms 误填成 abc | 非法值未拦截就全量生效 | 发布前类型范围校验 + 灰度 |
| 灰度判定不一致 | 用随机数判定 | 同一实例两次请求命中不同配置 | 用实例 ID 或固定 hash |
| 监听断线漏变更 | 断线期间的推送丢失 | 客户端长期停在旧版本 | 退避重连 + 重连后全量校准 |
| 配置与运行不一致 | 给不可热更参数做热更 | 配置改了却不生效，误导排查 | 能热更与不能热更分级管理 |
| 变成全链路单点 | 把高频业务状态塞进配置中心 | 读写压力集中、故障面放大 | 只放低频配置，业务状态走存储 |

### 4.3 两条真实工程误解

**误解一：所有配置都能热更新。** 热更新的前提是值在运行时被反复读取，且替换它不破坏一致性。监听端口、连接池大小、数据库地址这类只在启动时读取一次并绑定、建池的参数，改了配置根本没有任何代码再去读；硬做热更只会让配置面板显示 800、实际进程还跑在 500，排查时极具误导性。

<Callout type="warn" title="不是所有配置都能热更" text="把配置分级：限流阈值、功能开关、超时这类可热更的放配置中心，监听端口、连接池、数据库地址这类只在启动时读取的留在启动参数并重启生效，别混在一起让运维猜。" />

**误解二：灰度用 rand 判定就行，反正按比例。** 随机判定会让同一实例在两次请求里命中不同配置，行为抖动、指标忽上忽下，出了问题还无法复现到具体实例。灰度必须用稳定的实例 ID 或固定 hash，保证同一实例每次结论一致，先放 10% 观察指标再全量。

<Compare :panels='[{"label":"错误：随机灰度","code":"if rand.Intn(100) 命中灰度 { 用新配置 }","note":"同一实例两次请求可能命中不同配置，行为抖动难排查"},{"label":"正确：稳定哈希","code":"if inGray(instanceID) { 用新配置 }","note":"同一实例判定恒定，可灰度观察、可复现"}]' title="灰度该用什么判定" />

### 4.4 与后续阶段的呼应

配置外置在 L8 会换成更系统的形态：[/distributed/kubernetes](/distributed/kubernetes) 的 ConfigMap 与 Secret 把配置做成集群资源，[/distributed/gitops](/distributed/gitops) 的 Helm 与 Argo CD 把「带版本、可回滚」升级成声明式发布与灰度，[/distributed/sre](/distributed/sre) 则把「变更引发故障」纳入稳定性治理与错误预算。到 L10，[/projects/micro-seckill](/projects/micro-seckill) 需要不重启就调限流阈值来应对突发流量，[/projects/micro-ecommerce](/projects/micro-ecommerce) 的功能开关也依赖本讲的原子热更新与灰度发布——配置中心从此不再是可选组件，而是变更安全的基础设施。

<Quiz q="为什么监听端口这类配置不能靠配置中心热更新生效？" :options='["端口数值太大","它只在进程启动时被读取一次并绑定，运行时没有代码再读它","配置中心不支持数字类型","热更新只支持字符串"]' answer="1" explain="热更新要求值在运行时被反复读取；端口在启动 bind 时读一次就固定了，之后改配置不会有任何代码重新读取，硬做只会造成配置与运行不一致。" />

<Predict q="客户端带着 version=5 发起长轮询，期间配置发布了一次把 timeout_ms 改成 800，请求最可能返回什么？" :options='["立刻返回旧值 version 5","挂住直到变更，返回新版本与新值","等满超时返回空","直接报错"]' answer="1" output="返回 version 6，content 里 timeout_ms 为 800" explain="长轮询把请求挂住直到版本变化；发布使版本从 5 升到 6，服务端随即返回新快照，客户端校验后原子替换。这正是带版本等待消除空轮询的效果。" />

## 动手练一练
<Exercise title="给热更新加上校验失败告警与自动回滚" task="扩展 2.1 的 Store：Publish 前先校验新值，校验失败拒绝发布并保留旧版本；再实现「发布后若检测到异常指标，自动回滚到上一版本」的钩子。" :hints='["Publish 里先调用一个 validate(data) bool，不通过直接返回当前 Config","回滚目标取当前 Version 减一，复用已有的 history","把校验与回滚结果打日志，便于审计"]' solution="要点一：Publish 开头执行校验，非法值不递增版本、不入 history，直接返回 s.Get()。\n要点二：新增 RollbackPrev，目标版本为 s.Get().Version - 1，在 history 中找到并 Store 回去。\n要点三：把每次发布、校验拒绝、自动回滚都记一条带操作人与时间戳的日志，形成可审计轨迹。" explain="校验把错误挡在发布前，自动回滚把风险限制在发布后的一小段窗口，两者合起来才敢开全量热更新。" />

## 随堂检测
<Quiz no="077-1" q="长轮询相比固定间隔轮询，最核心的改进是什么？" :options='["每次拉取的配置体积更小","客户端带上已知版本挂住请求，没变更不返回，消除大量空轮询","配置在传输中自动加密","服务端不再需要保存历史版本"]' answer="1" explain="长轮询把「轮询」变成「带条件的等待」：版本没变就挂住连接，变更才响应，因此既接近实时又不产生海量无效请求。" />

<Predict no="077-2" q="代码 3.1 里 Client.Load 依次收到合法值 800、非法值 abc、以及 connection refused，最终 cache 里的 timeout_ms 是多少？" :options='["abc","800","500","空字符串"]' answer="1" output="正常发布生效为 800；非法值被校验挡住、中心失联回退缓存，二者都不会覆盖 800，最终仍为 800" explain="合法发布把值更新成 800；非法值校验失败保留旧值，远端报错回退本地缓存，这两次失败都不会把已经生效的 800 改掉。" />

## 一页纸速记
<KnowledgeCard :points='["配置模型三要素：命名空间隔离环境、dataId 定位配置、版本号判定新旧与回滚","atomic.Value 存不可变快照，整份替换实现无锁热更新，切忌逐字段原地改","长轮询带已知版本挂住请求，变更才返回；监听推送实时性更高但要维护连接","客户端要有本地缓存、类型校验、灰度判定与断线重连，配置中心抖动时进程照常工作"]' :pitfalls='["把只在启动时读取的端口、DB 地址也做成热更新，造成配置与运行不一致","发布不留版本历史与审计，出错只能靠重新改回去","配置中心失联时不做本地回退，一次抖动拖垮全部实例"]' summary="一句话记住：配置中心 = 带版本的存储 + 变更推送 + 校验灰度回滚，热更新只负责「安全地换值」。" />

## 小结与下一讲

- **模型与更新**：命名空间、dataId、版本历史构成治理基础；`atomic.Value` 整份替换实现无锁热更新，回滚即挂回历史快照。
- **变更机制**：定时轮询、长轮询、监听推送在实时性与服务端压力间取舍，长轮询用版本号消除空转。
- **客户端容错**：本地缓存、校验、灰度、断线重连四件套，是配置中心可用性的真正兜底。

[下一讲：API 网关、鉴权与限流](/microservice/gateway)
