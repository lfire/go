---
title: 第 68 讲 · 消息队列：Kafka / NATS / 可靠投递
description: 讲清队列与发布订阅、分区与顺序、确认与重试、幂等消费、事务消息与选型对比，用消息解耦并保证不丢不重
---

<StageHeader stage="L6 · 服务端与数据" level="★★★★☆" time="50 分钟" goal="用消息解耦并保证不丢不重" prereq="第 67 讲 · Redis 缓存" />

# 第 68 讲 · 消息队列：Kafka / NATS / 可靠投递

> 同步调用链有个致命特点：下游慢一点、挂一下，上游就跟着卡住或失败。消息队列把「立刻做完」改成「记下来、稍后做」，于是能解耦、削峰、让故障隔离在消费者一侧。但天下没有免费的异步：一旦消息可以重投，业务就必须面对「同一条消息被处理两次」。这一讲把可靠投递拆成三件互相咬合的事——生产确认、Broker 持久化、消费端 ACK 加幂等，并用可运行的代码把「不丢不重」讲成可验证的结论。

## 这一讲你会得到什么

<CheckList id="68-goal" title="本讲目标" :items='["能区分队列与发布订阅，理解分区、消费者组与 offset","能说清三种投递语义的真实含义与 exactly-once 的边界","会用确认、重试与死信队列搭一条可靠消费链","能设计幂等键并写出幂等消费者，按场景做选型"]' />

## 1. 先建立直觉

把消息队列想成邮局。**队列模式**像挂号信：一封信只投递给一个收件人，多开几个收件人就是分摊工作量（竞争消费者）；**发布订阅**像报纸：一份内容印很多份，每个订阅者各拿一份，互不影响。Kafka 用「消费者组」把两者统一了：组内多个实例分摊分区（队列语义），不同组各自独立读全量（发布订阅语义）。

Broker 内部把主题切成多个**分区**，每个分区是一条只能追加、不可修改的日志；消息在分区里的位置叫 offset，顺序只在一个分区内有保证。所以「消息有序」必须先回答「哪些消息要有序」：把同一业务键（订单号、用户 ID）作为分区键，它们就恒定落到同一分区，分区内自然有序；想要全局有序，只能退化成单分区，吞吐也就没了。

<ConceptDiagram type="mq-flow" title="可靠投递链路：确认、持久化、消费、ACK 与重试" caption="生产端要确认、Broker 要持久化、消费端处理成功后才 ACK；任一环节缺失都会丢消息或重复消费" />

## 2. 语法与规则

先用零依赖的纯 Go 把「确认 + 重试 + 死信」这条骨架跑通，理解 Kafka、NATS、RabbitMQ 各自 API 背后共同的东西。

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Message struct {
	ID      int
	Body    string
	Retries int
}

type Broker struct {
	mu       sync.Mutex
	pending  map[int]Message // 已投递但未 ACK 的消息
	dlq      []Message       // 死信队列
	ch       chan Message
	nextID   int
	maxRetry int
}

func NewBroker(maxRetry int) *Broker {
	return &Broker{
		pending:  make(map[int]Message),
		ch:       make(chan Message, 16),
		maxRetry: maxRetry,
	}
}

func (b *Broker) Publish(body string) {
	b.mu.Lock()
	b.nextID++
	m := Message{ID: b.nextID, Body: body}
	b.pending[m.ID] = m
	b.mu.Unlock()
	b.ch <- m
}

// Ack 只有消费成功后才调用，Broker 此时才把消息从待办里移除。
func (b *Broker) Ack(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, id)
}

// Nack 失败重投：未超过次数就重新入队，否则进死信队列。
func (b *Broker) Nack(m Message) {
	m.Retries++
	b.mu.Lock()
	defer b.mu.Unlock()
	if m.Retries > b.maxRetry {
		delete(b.pending, m.ID)
		b.dlq = append(b.dlq, m)
		fmt.Println("进入死信队列:", m.Body)
		return
	}
	go func() { b.ch <- m }() // 异步重投，避免持锁阻塞
}

func main() {
	b := NewBroker(2)
	b.Publish("order-1")
	b.Publish("order-2")

loop:
	for i := 0; i < 10; i++ {
		select {
		case m := <-b.ch:
			if m.Body == "order-2" && m.Retries < 3 {
				fmt.Printf("处理 %s 第 %d 次失败，重投\n", m.Body, m.Retries+1)
				b.Nack(m)
			} else {
				fmt.Println("处理成功:", m.Body)
				b.Ack(m.ID)
			}
		case <-time.After(200 * time.Millisecond):
			break loop // 队列已空
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	fmt.Println("死信条数:", len(b.dlq))
	fmt.Println("未确认条数:", len(b.pending))
}
```

<OutputBox title="运行结果" output="处理成功: order-1\n处理 order-2 第 1 次失败，重投\n处理 order-2 第 2 次失败，重投\n处理 order-2 第 3 次失败，重投\n进入死信队列: order-2\n死信条数: 1\n未确认条数: 0" note="失败消息不会无限重试：达到上限进死信队列留待人工处理，避免拖住整个分区" />

<LineByLine code="m, err := r.FetchMessage(ctx)\nif err != nil {\nreturn err\n}\nhandle(m)\nr.CommitMessages(ctx, m)" :notes='["拉取一条消息，此时 offset 尚未前进","区分暂时性错误与致命错误，前者可重试","失败直接返回，让消息留在分区里等下次投递","业务处理：写库、调下游，都在这里完成","只有处理成功后才提交 offset，崩溃则消息会被重投"]' title="可靠消费循环逐行看" />

注意 `pending` 与 ACK 的分工：Broker 记住「投出去但没确认」的消息，消费者崩溃或超时未 ACK 时会重新投递。这正是 **at-least-once（至少一次）** 的由来——它保证不丢，但必然可能重复。于是消费端幂等不是可选项，而是这套语义的地基。

第二段代码把幂等落到代码上：同一个事件投递三次，业务状态只变更一次。

```go
package main

import (
	"fmt"
	"sync"
)

type Event struct {
	ID     string // 幂等键：业务唯一 ID，不是中间件给的 offset
	Order  string
	Amount int
}

type Consumer struct {
	mu     sync.Mutex
	seen   map[string]bool // 模拟去重表；生产用数据库唯一索引
	orders map[string]int  // 模拟业务状态
}

func NewConsumer() *Consumer {
	return &Consumer{
		seen:   map[string]bool{},
		orders: map[string]int{},
	}
}

// Handle 保证同一 Event.ID 无论投递多少次，业务状态只变更一次。
func (c *Consumer) Handle(e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen[e.ID] {
		fmt.Println("重复投递，直接确认:", e.ID)
		return
	}
	// 生产里「判重 + 写业务 + 记去重」必须放在同一个数据库事务中。
	c.orders[e.Order] += e.Amount
	c.seen[e.ID] = true
	fmt.Println("已处理:", e.ID, "累计金额:", c.orders[e.Order])
}

func main() {
	c := NewConsumer()
	e := Event{ID: "evt-1001", Order: "order-1", Amount: 30}
	c.Handle(e) // 模拟 at-least-once 下的三次重复投递
	c.Handle(e)
	c.Handle(e)
	fmt.Println("最终累计金额:", c.orders["order-1"], "（应为 30 而非 90）")
}
```

幂等键必须来自**业务**而不是中间件：offset 会在重平衡后变化，消息 ID 在重试重发时也可能变，只有「订单号 + 操作类型」这类业务唯一键才稳定。常见的实现是去重表加唯一索引，或 `INSERT ... ON DUPLICATE KEY UPDATE` / 状态机流转；判重与业务写入必须在同一事务里，否则仍存在「业务写成功、去重记录没写上」的窗口。

## 3. 深入一步

| 投递语义 | 保证 | 实现方式 | 代价 |
| --- | --- | --- | --- |
| at-most-once | 至多一次，可能丢 | 收到即 ACK | 实现最简单，允许少量丢失 |
| at-least-once | 至少一次，可能重 | 处理成功后才 ACK | 主流选择，需要消费端幂等 |
| exactly-once | 恰好一次 | Kafka 事务 + 幂等生产者 | 仅限「读 Kafka-处理-写 Kafka」闭环，跨外部系统不成立 |

「exactly-once」最容易被误读。Kafka 的幂等生产者与事务能保证在 Kafka 内部读写不重不漏，但只要消费端要写数据库、调 HTTP、发邮件，这些外部系统不在 Kafka 事务里，就依然要靠业务幂等。所以工程上的准确说法是：**用 at-least-once 保证不丢，用幂等消费把「重」变成无害**。

**事务消息**解决的是另一个问题：「本地事务成功了，消息却没发出去」。常见解法是本地消息表（业务数据与待发消息在同一个数据库事务里写入，再由后台任务投递）或半消息（先发一条对消费者不可见的消息，本地事务成功后再确认）。这套机制属于分布式事务范畴，会在第 85 讲展开，这里只需知道它存在、别自己用「先发消息再写库」的土办法替代。

| 产品 | 模型 | 吞吐 | 顺序 | 典型场景 |
| --- | --- | --- | --- | --- |
| Kafka | 分区日志 / 拉取 | 极高 | 分区内有序 | 日志、流处理、事件溯源 |
| NATS JetStream | 轻量流 / 推送 | 高 | 流内有序 | 云原生内部通信、低延迟 |
| RabbitMQ | 交换机 / 队列 | 中 | 队列内有序 | 复杂路由、传统业务解耦 |
| RocketMQ | 分区队列 | 高 | 分区内有序 | 交易、事务消息、延时消息 |

<Term word="消费者组" text="一组共同消费同一主题的实例；组内核分区分摊负载，不同组各自独立持有全量 offset，从而同时实现队列与发布订阅两种语义" />

<Callout type="warn" title="三个高频反模式" text="其一，开启自动提交 offset：消息刚取出来就提交，业务还没处理完进程崩了，这条消息永久丢失。其二，消费失败后无限重试且不设死信：一条毒消息会反复阻塞整个分区，后面的消息全部堆积。其三，把消息队列当同步 RPC 用：发完消息就阻塞等结果，既丢了削峰与解耦的意义，又引入超时与乱序的复杂度。" />

<Callout type="idea" title="顺序与并发的取舍" text="分区数决定了消费并行度的上限。想让同一订单严格有序，就用订单号作分区键；想让不同订单尽量并行，就把分区数开够。注意增加分区会改变 key 到分区的映射，历史顺序可能被打乱，扩容前要评估。重试时若把失败消息直接塞回队首，还会破坏同一 key 的相对顺序，通常用独立重试主题加退避来规避。" />

第三段代码给出完整、可运行的服务：HTTP 接口生产消息 + 后台 Kafka 消费者手动提交 offset。

```go
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/segmentio/kafka-go"
)

const (
	broker = "127.0.0.1:9092"
	topic  = "orders"
	group  = "order-service"
)

func newWriter() *kafka.Writer {
	return &kafka.Writer{
		Addr:         kafka.TCP(broker),
		Topic:        topic,
		Balancer:     &kafka.Hash{},     // 按 Key 哈希到固定分区，保证同键有序
		RequiredAcks: kafka.RequireAll,  // 所有 ISR 副本确认才算成功
		Async:        false,             // 同步写，失败能立刻返回给调用方
	}
}

func newReader() *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{broker},
		Topic:          topic,
		GroupID:        group,
		MinBytes:       1,
		MaxBytes:       10 << 20,
		CommitInterval: 0, // 关闭自动提交，改为处理成功后手动提交
	})
}

func consume(ctx context.Context, r *kafka.Reader) {
	for {
		m, err := r.FetchMessage(ctx)
		if err != nil {
			log.Printf("fetch: %v", err)
			return
		}
		log.Printf("消费 partition=%d offset=%d value=%s",
			m.Partition, m.Offset, m.Value)
		// 业务处理成功后才提交；崩溃则 offset 不前进，消息会被重投。
		if err := r.CommitMessages(ctx, m); err != nil {
			log.Printf("commit: %v", err)
		}
	}
}

func main() {
	ctx := context.Background()
	w := newWriter()
	defer w.Close()
	r := newReader()
	defer r.Close()
	go consume(ctx, r)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", func(wr http.ResponseWriter, req *http.Request) {
		var body struct {
			OrderID string `json:"order_id"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(wr, "bad json", http.StatusBadRequest)
			return
		}
		// 订单号作 Key：同一订单恒定落同一分区，分区内保持顺序。
		msg := kafka.Message{
			Key:   []byte(body.OrderID),
			Value: []byte(body.OrderID),
		}
		if err := w.WriteMessages(req.Context(), msg); err != nil {
			http.Error(wr, "publish failed", http.StatusBadGateway)
			return
		}
		wr.WriteHeader(http.StatusAccepted) // 202：已接收，尚未处理完
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

三个参数值得单独记：`RequiredAcks = RequireAll` 是生产端「不丢」的开关（代价是延迟）；`Balancer = Hash` 把顺序责任交给分区键；`CommitInterval = 0` 关掉自动提交，把 ACK 时机交回业务。注意接口返回 202 而不是 200——消息「已收下」和「已处理完」是两件事，契约上必须区分。

## 4. 工程实践

生产端还有一处细节：数据库事务与发消息不是原子的。若先写库再发消息，进程在中间崩溃就会丢消息；反过来先发消息再写库，库失败消息却已经发出去了。正确姿势是本地消息表或事务消息，把「业务变更」与「待发消息」放进同一个本地事务，再由独立投递器保证最终发出。

<Compare title="offset 什么时候提交" :panels='[{"label":"反模式 · 自动提交","code":"FetchMessage(ctx)\n// offset 已被自动提交\nprocess(m)","note":"业务还没处理完 offset 就前进，进程一崩这条消息永久丢失"},{"label":"正解 · 处理成功后提交","code":"m := FetchMessage(ctx)\nprocess(m)\nCommitMessages(ctx, m)","note":"崩溃时 offset 未前进，消息会重投；配合消费端幂等才是完整方案"}]' />

<FlowTimeline title="一条消息的可靠旅程" :steps='[{"title":"生产确认","desc":"acks=all，等 ISR 副本都写入","tone":"blue"},{"title":"分区持久化","desc":"按 Key 落到固定分区，写入日志","tone":"purple"},{"title":"拉取消费","desc":"组内实例按分区分摊","tone":"orange"},{"title":"幂等处理","desc":"查业务幂等键，重复则直接确认","tone":"green"},{"title":"提交 offset","desc":"成功后才 ACK，失败进重试","tone":"blue"},{"title":"重试与死信","desc":"退避重投，超限进死信待人工处理","tone":"gray"}]' direction="horizontal" />

监控上要盯住**堆积量（lag）**：消费速度持续低于生产速度，lag 会单调上涨，说明该扩分区或扩消费者了；同时要给重试次数、死信条数、处理耗时配告警。还有一个反模式是**消费者里做长耗时同步操作**（大文件处理、慢 HTTP 调用），它会让分区长时间被占用；这类工作应当只把「任务描述」放进消息，实际执行交给独立的 worker pool。

## 动手练一练

<Exercise title="把消费者改造成幂等" task="现有 Handle 每收到一条消息就给订单累加金额，在 at-least-once 下会重复计费。请改写它，要求同一事件 ID 只生效一次，并说明生产环境该把去重状态放哪里。" :hints='["用业务事件 ID 作幂等键，不要用 offset 或中间件消息 ID","判重、业务写入、记录已处理必须在同一个事务里","命中去重表时仍要向 Broker 确认这条消息，否则会被反复重投"]' solution="func (c *Consumer) Handle(e Event) {\nc.mu.Lock()\ndefer c.mu.Unlock()\nif c.seen[e.ID] {\nreturn\n}\napply(e)\nc.seen[e.ID] = true\n}" explain="生产里 seen 应是带唯一索引的去重表，且与业务写入同事务；命中重复时直接返回成功让上层 ACK，绝不能返回错误触发无意义重投。" />

## 随堂检测

<Quiz no="68-1" q="关于 Kafka 的顺序保证，下列说法正确的是？" :options='["同一主题全局有序","同一分区内有序，用分区键把相关消息归到同一分区","消费者组内所有实例看到完全相同顺序","只要开了事务就全局有序"]' answer="1" explain="Kafka 只保证分区内有序；把订单号等业务键作为消息 Key，同键消息恒定落同一分区即可获得所需的局部顺序。" />

<Predict no="68-2" q="消费者开启自动提交 offset，取出消息后业务处理到一半进程崩溃，会发生什么？" :options='["重启后会重新消费这条消息","这条消息永久丢失","Broker 自动补发到死信队列","offset 回滚到上一条"]' answer="1" output="该 offset 已被自动提交，重启后从下一条开始消费，这条消息的业务结果永久缺失" explain="自动提交把「拿到消息」误当成「处理完成」。要关掉自动提交，在业务成功后手动 CommitMessages。" />

## 一页纸速记

<KnowledgeCard :points='["组内分摊分区是队列语义，不同组各读全量是发布订阅语义","顺序只在分区内成立，用业务键作分区键来获得局部有序","at-least-once 不丢但会重，消费端幂等是必需而非可选","关自动提交、处理成功后才 ACK，失败退避重试并设死信队列","本地消息表或事务消息解决「库写了消息没发」的原子性问题"]' :pitfalls='["开启自动提交 offset，业务崩溃即丢消息","用 offset 或中间件 ID 当幂等键，重平衡后失效","失败消息无限重试，阻塞整个分区","把 MQ 当同步 RPC，发完阻塞等结果","先发消息再写库，库失败却已对外产生副作用"]' summary="可靠投递不是某个开关，而是生产确认、持久化、手动 ACK、幂等消费与死信重试共同凑成的一条链。" />

## 小结与下一讲

- 分区与消费者组统一了队列和发布订阅，顺序保证需要业务键参与设计。
- 「不丢」靠 acks=all 加持久化加手动 ACK，「不重」靠业务幂等键，两者缺一不可。
- 事务消息、本地消息表解决跨系统的原子性，重试要有退避与死信兜底。

数据量上来之后，数据库的模糊查询与多条件筛选会先扛不住。下一讲看看搜索引擎为什么能快，以及怎么把数据同步过去。

[下一讲：Elasticsearch 与倒排索引](/server/search)
