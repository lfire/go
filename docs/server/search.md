---
title: 第 69 讲 · Elasticsearch 与倒排索引
description: 从倒排索引讲清搜索为何快，覆盖分词与 mapping、查询 DSL、写入与刷新机制以及数据同步与异构，理解何时该用搜索引擎
---

<StageHeader stage="L6 · 服务端与数据" level="★★★★☆" time="45 分钟" goal="理解搜索为何快以及何时该用" prereq="第 68 讲 · 消息队列" />

# 第 69 讲 · Elasticsearch 与倒排索引

> 数据库的 `LIKE '%关键字%'` 为什么慢？因为它只能逐行扫描，再对每行做一次字符串匹配，索引完全用不上。搜索引擎换了个方向：不按「文档里有什么词」来存，而按「每个词出现在哪些文档里」来存。这个方向一换，查询就从「翻遍整本书」变成「查目录再取交集」。这一讲先用几十行 Go 手写一个倒排索引把原理跑通，再接上真实的查询 DSL、写入刷新机制，最后解决最容易被忽略的一环：数据怎么从主库可靠地同步过来。

## 这一讲你会得到什么

<CheckList id="69-goal" title="本讲目标" :items='["能手写一个最小倒排索引，解释搜索为何快于全表扫描","理解分词与 mapping，知道 text 与 keyword 该怎么选","会写 bool、match、term 组合的查询 DSL，并避开深分页","理解近实时刷新与段合并，能设计带版本号的数据同步链路"]' />

## 1. 先建立直觉

正向索引是「文档 → 词」：给你第 3 页，告诉你这页写了什么。倒排索引是「词 → 文档列表」：给你「缓存」这个词，直接列出所有包含它的文档 ID。查询多个词时，只要把各自的 ID 列表取交集，复杂度与文档总数无关，只与命中量有关。这也解释了为什么搜索引擎对「稀有词」特别快——它的倒排列表本来就短。

<ConceptDiagram type="mq-flow" title="异构数据同步：主库变更经消息链路流到搜索引擎" caption="ES 是读模型而非数据源；DB 写成功后通过 binlog 或消息异步同步，同步器要靠版本号抵御乱序与重投" />

分词是倒排索引的前置工序。英文按空格切基本够用，中文必须靠 IK、jieba 这类分词器把「倒排索引」切成有意义的词项；分错了词，后面再怎么优化查询都搜不到。而 `mapping` 就是声明「这一列怎么分词、按什么类型建索引」的 schema，一旦字段类型写错，往往只能新建索引再 reindex。

## 2. 语法与规则

第一段代码用纯标准库实现最小倒排索引：分词、建表、多词交集查询。运行它，就能亲手验证「为什么搜索不随文档数线性变慢」。

```go
package main

import (
	"fmt"
	"sort"
	"strings"
)

type Index struct {
	postings map[string][]int // 词项 -> 文档 ID 列表（天然有序）
	docs     []string
}

func NewIndex() *Index {
	return &Index{postings: make(map[string][]int)}
}

// tokenize 是最朴素的分词：转小写后按空白切分。
// 中文生产环境需要 IK 等分词器，这里用空格分隔的词演示原理。
func tokenize(s string) []string {
	return strings.Fields(strings.ToLower(s))
}

func (idx *Index) Add(doc string) int {
	id := len(idx.docs)
	idx.docs = append(idx.docs, doc)
	for _, term := range tokenize(doc) {
		list := idx.postings[term]
		// 同一文档里重复出现的词只记一次。
		if len(list) == 0 || list[len(list)-1] != id {
			idx.postings[term] = append(list, id)
		}
	}
	return id
}

// Search 对多个词项的倒排列表取交集，实现 AND 语义。
func (idx *Index) Search(query string) []string {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	// 先处理最短的列表，让交集尽快收缩，减少比较次数。
	sort.Slice(terms, func(i, j int) bool {
		return len(idx.postings[terms[i]]) < len(idx.postings[terms[j]])
	})
	hits := idx.postings[terms[0]]
	for _, term := range terms[1:] {
		hits = intersect(hits, idx.postings[term])
		if len(hits) == 0 {
			break
		}
	}
	out := make([]string, 0, len(hits))
	for _, id := range hits {
		out = append(out, idx.docs[id])
	}
	return out
}

// intersect 求两个有序 ID 列表的交集，双指针线性扫描。
func intersect(a, b []int) []int {
	out := make([]int, 0)
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			out = append(out, a[i])
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return out
}

func main() {
	idx := NewIndex()
	idx.Add("go web server with net/http")
	idx.Add("go redis cache for web api")
	idx.Add("elasticsearch inverted index for search")

	fmt.Println("查询 go web:")
	for _, d := range idx.Search("go web") {
		fmt.Println(" -", d)
	}
	fmt.Println("查询 go redis:")
	for _, d := range idx.Search("go redis") {
		fmt.Println(" -", d)
	}
	fmt.Println("倒排表大小:", len(idx.postings))
}
```

<OutputBox title="运行结果" output="查询 go web:\n - go web server with net/http\n - go redis cache for web api\n查询 go redis:\n - go redis cache for web api\n倒排表大小: 13" note="两个词各自命中一个短列表，取交集即得结果；文档总量再大，也只遍历命中的列表" />

<LineByLine code="terms := tokenize(query)\nsortByListLength(terms)\nhits := postings[terms[0]]\nfor _, t := range terms[1:] {\nhits = intersect(hits, postings[t])\n}\nreturn docsOf(hits)" :notes='["把查询串切成词项，与建索引时用同一套分词器","按倒排列表长度升序，先处理最稀有的词","从最短列表出发，作为初始候选集","逐个词项收紧候选集","每轮做有序列表交集，候选只减不增","把最终 ID 换回文档内容返回"]' title="倒排索引查询逐行看" />

真实 ES 还会在倒排表里存词频、位置，用于相关性打分与短语查询；但「词项到文档列表 + 交集」这个骨架，就是它快的根本原因。

第二段代码接上真实客户端：定义 mapping、批量灌入数据，并把 bool 查询包装成一个完整的 HTTP 搜索接口。

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
)

var es *elasticsearch.Client

func main() {
	var err error
	es, err = elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{"http://127.0.0.1:9200"},
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := setup(context.Background(), es); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /search", searchHandler)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// setup 建索引并批量灌入演示数据，只在启动时执行一次。
func setup(ctx context.Context, es *elasticsearch.Client) error {
	// mapping 一旦写入就不能随意改字段类型，改结构通常需要 reindex。
	mapping := `{
		"mappings": {
			"properties": {
				"title":  { "type": "text", "analyzer": "standard" },
				"status": { "type": "keyword" },
				"price":  { "type": "integer" }
			}
		}
	}`
	req := esapi.IndicesCreateRequest{
		Index: "articles", Body: strings.NewReader(mapping),
	}
	res, err := req.Do(ctx, es)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	log.Println("create index status:", res.Status())

	// Bulk 写入：每条文档前一行是 action 元数据，后一行是文档内容。
	var buf bytes.Buffer
	docs := []map[string]any{
		{"title": "Go net/http deep dive", "status": "published", "price": 30},
		{"title": "Go redis cache guide", "status": "published", "price": 20},
	}
	for i, d := range docs {
		meta := map[string]any{
			"index": map[string]any{"_id": fmt.Sprint(i + 1)},
		}
		_ = json.NewEncoder(&buf).Encode(meta)
		_ = json.NewEncoder(&buf).Encode(d)
	}
	bulkRes, err := es.Bulk(bytes.NewReader(buf.Bytes()),
		es.Bulk.WithIndex("articles"),
		es.Bulk.WithRefresh("true")) // 演示用：立即刷新，生产别逐条刷新
	if err != nil {
		return err
	}
	defer bulkRes.Body.Close()
	log.Println("bulk status:", bulkRes.Status())
	return nil
}

// buildQuery 用结构体序列化出 DSL，绝不手工拼接用户输入的字符串。
func buildQuery(q string) string {
	body := map[string]any{
		"query": map[string]any{
			"bool": map[string]any{
				"must": []any{
					map[string]any{"match": map[string]any{"title": q}},
				},
				// filter 不参与打分且可缓存，适合状态这类精确条件。
				"filter": []any{
					map[string]any{"term": map[string]any{"status": "published"}},
				},
			},
		},
	}
	b, _ := json.Marshal(body)
	return string(b)
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}
	res, err := es.Search(
		es.Search.WithContext(r.Context()),
		es.Search.WithIndex("articles"),
		es.Search.WithBody(strings.NewReader(buildQuery(q))),
	)
	if err != nil {
		http.Error(w, "search failed", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()

	var out struct {
		Hits struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
			Hits []struct {
				Source map[string]any `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		http.Error(w, "decode failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"total": out.Hits.Total.Value,
		"docs":  out.Hits.Hits,
	})
}
```

注意 `buildQuery` 用结构体加 `json.Marshal` 生成 DSL，而不是把用户输入拼进 JSON 字符串——查询串里带引号或转义字符时，手工拼接既容易出错又可能注入。启动后访问 `/search?q=go` 就能拿到命中总数与文档列表。

## 3. 深入一步

`text` 与 `keyword` 是最容易选错的一对：`text` 会分词、用于全文检索，不能整体做精确过滤或排序；`keyword` 不分词、原样入索引，适合状态、标签、ID 这类精确匹配与聚合。与之对应，`match` 会对查询串做同样的分词再匹配，`term` 则拿原值去精确比对——对 `text` 字段用 `term` 查「Go Redis」通常查不到，因为索引里存的是分词后的小写词项。

| 概念 | 作用 | 常见误用 |
| --- | --- | --- |
| `text` | 分词后建倒排，支持全文检索与打分 | 拿它做 term 精确过滤 |
| `keyword` | 不分词，整体入索引 | 拿它做 match 全文检索 |
| `match` | 查询串也分词，做相关性匹配 | 期望它精确匹配整个短语 |
| `term` | 不做分词，精确匹配词项 | 用在 text 字段上查整句 |

ES 是**近实时（NRT）**而非实时：写入先落 translog 与内存缓冲，默认每 1 秒 `refresh` 一次生成可搜索的段，所以刚写入的数据可能要等一秒才搜得到。`flush` 把段刷到磁盘并清 translog，后台 `merge` 会合并小段、清理已删除文档。这解释了「写成功却立刻查不到」这个高频困惑，也说明为什么逐条 `WithRefresh("true")` 是写入性能杀手——它会强制频繁生成大量小段。

| 维度 | 数据库全文索引 | Elasticsearch |
| --- | --- | --- |
| 数据角色 | 主数据，强一致、有事务 | 读模型，异步同步、最终一致 |
| 检索能力 | 简单匹配 | 分词、相关性、聚合、高亮 |
| 一致性 | 写入即可见 | 近实时，默认 1 秒可见 |
| 事务与关联 | 强 | 弱，不适合跨文档事务 |

<Term word="倒排索引" text="以词项为键、以包含该词的文档 ID 有序列表为值的数据结构；多词查询转化为列表交集，避免逐行扫描" />

<Callout type="warn" title="四个高频反模式" text="其一，把 ES 当主库：它没有事务、同步有延迟、重建可能丢数据，主数据必须留在数据库。其二，深分页用 from + size：每个分片都要取回 offset 加 size 条再汇总，超过 max_result_window 直接被拒，应改用 search_after 或 scroll。其三，随意改 mapping 字段类型：类型不兼容只能新建索引并 reindex。其四，用前导通配符查询（如 *keyword）：它无法利用倒排索引，会退化成全词项扫描。" />

<Callout type="idea" title="同步链路的取舍" text="双写（应用同时写 DB 和 ES）最直观但存在部分失败窗口；更稳妥的是订阅 binlog 或发领域事件，由独立同步器异步写入 ES。同步器必须处理消息重投与乱序：给文档带版本号，只接受版本更新的写入；失败要退避重试并留死信；全量重建时用别名切换，避免影响在线查询。" />

第三段代码用可运行的纯 Go 演示同步器：版本号防乱序、失败重试、超限兜底。

```go
package main

import (
	"fmt"
	"time"
)

type Doc struct {
	ID      string
	Title   string
	Version int
}

type SearchStore struct {
	docs map[string]Doc
}

// Upsert 只接受版本号更新的写入，乱序到达的旧事件不会覆盖新数据。
func (s *SearchStore) Upsert(d Doc) bool {
	old, ok := s.docs[d.ID]
	if ok && old.Version >= d.Version {
		fmt.Println("丢弃乱序或重复事件:", d.ID, "version", d.Version)
		return false
	}
	s.docs[d.ID] = d
	fmt.Println("写入索引:", d.ID, d.Title, "version", d.Version)
	return true
}

type ChangeEvent struct {
	Doc     Doc
	Retries int
}

type Syncer struct {
	es       *SearchStore
	events   chan ChangeEvent
	maxRetry int
	failOnce bool
}

func (s *Syncer) Run(done chan struct{}) {
	for e := range s.events {
		if err := s.apply(e); err != nil {
			e.Retries++
			if e.Retries > s.maxRetry {
				fmt.Println("同步失败需人工介入:", e.Doc.ID)
				continue
			}
			time.Sleep(10 * time.Millisecond) // 生产用指数退避
			s.events <- e
			continue
		}
	}
	close(done)
}

func (s *Syncer) apply(e ChangeEvent) error {
	if s.failOnce {
		s.failOnce = false
		return fmt.Errorf("temporary failure") // 模拟一次网络抖动
	}
	s.es.Upsert(e.Doc)
	return nil
}

func main() {
	store := &SearchStore{docs: make(map[string]Doc)}
	s := &Syncer{
		es: store, events: make(chan ChangeEvent, 16),
		maxRetry: 3, failOnce: true,
	}
	done := make(chan struct{})
	go s.Run(done)

	// 模拟从 binlog / MQ 收到的变更，顺序可能因重投而打乱。
	s.events <- ChangeEvent{Doc: Doc{ID: "1", Title: "v1", Version: 1}}
	s.events <- ChangeEvent{Doc: Doc{ID: "1", Title: "v3", Version: 3}}
	s.events <- ChangeEvent{Doc: Doc{ID: "1", Title: "v2", Version: 2}}
	time.Sleep(50 * time.Millisecond)
	close(s.events)
	<-done
	fmt.Println("最终文档:", store.docs["1"].Title,
		"version", store.docs["1"].Version)
}
```

第一条事件因模拟故障被重投到队尾，于是 v3 反而先落地，后续 v1、v2 都因版本更旧被丢弃。**重投会改变到达顺序**，所以同步正确性只能靠版本号，绝不能靠「事件按顺序来」这个假设。生产里重试应走独立重试主题而非回灌同一队列，避免毒消息堵住正常流量。

## 4. 工程实践

引入 ES 的完整链路是：主库写入 → 变更捕获（binlog / 领域事件）→ 消息队列 → 同步器 → ES 索引，另有离线全量重建通道做兜底。既然是异步链路，就必须接受秒级不一致，把它写进产品预期；对「刚发布就要立刻搜到」的诉求，可以在写路径里同步刷一次，或在查询层合并主库结果。

<Compare title="分页：深翻页为什么不能用 from + size" :panels='[{"label":"反模式 · from + size","code":"from: 10000\nsize: 10\n每个分片都要取回 10010 条再汇总","note":"页数越深内存与 CPU 开销越夸张，超过 max_result_window 直接被拒绝"},{"label":"正解 · search_after","code":"sort: [id, tiebreaker]\nsearch_after: [上一页最后一条的排序值]","note":"用排序值做游标，成本与页码无关，适合无限滚动与导出"}]' />

<FlowTimeline title="一次异构同步" :steps='[{"title":"主库写入","desc":"业务事务提交，同时记录变更","tone":"blue"},{"title":"变更捕获","desc":"binlog 或领域事件进入消息队列","tone":"purple"},{"title":"同步消费","desc":"按分区键保序，失败退避重试","tone":"orange"},{"title":"版本校验","desc":"旧版本事件直接丢弃","tone":"green"},{"title":"批量写入","desc":"bulk 提交，避免逐条刷新","tone":"blue"},{"title":"刷新可见","desc":"默认 1 秒 refresh 后可被搜到","tone":"gray"}]' direction="horizontal" />

还有一点容易被忽略：**分片数一旦建好就不能改**（改了要 reindex）。分片太少限制横向扩展，太多又带来小分片开销与聚合成本，通常按预估数据量（如单分片 20~40 GB）反推。查询侧要给慢查询、拒绝数与 JVM/堆压力配监控，别等用户反馈「搜不出来」才发现集群早就红了。

## 动手练一练

<Exercise title="给倒排索引加 OR 语义" task="现有 Search 对多个词取交集（AND）。请实现 SearchAny，对任一词命中即返回，且同一文档不重复出现。" :hints='["对每个词项的倒排列表遍历，把文档 ID 放进一个 set","map[int]bool 是最简单的去重结构","最后把 set 里的 ID 统一换成文档内容"]' solution="func (idx *Index) SearchAny(query string) []string {\nseen := map[int]bool{}\nfor _, t := range tokenize(query) {\nfor _, id := range idx.postings[t] {\nseen[id] = true\n}\n}\nvar ids []int\nfor id := range seen {\nids = append(ids, id)\n}\nsort.Ints(ids)\nreturn docsOf(ids)\n}" explain="OR 语义是各列表的并集；用 map 去重后再排序，保证输出稳定。真实 ES 里对应 bool 的 should 子句。" />

## 随堂检测

<Quiz no="69-1" q="关于 text 与 keyword，下列说法正确的是？" :options='["text 不分词，适合精确过滤","keyword 会分词，适合全文检索","text 分词用于全文检索，keyword 不分词用于精确匹配与聚合","两者只是命名不同，行为完全一样"]' answer="2" explain="text 经分词器切成词项建倒排，支持 match 与打分；keyword 原样入索引，适合 term 精确过滤、排序与聚合。" />

<Predict no="69-2" q="应用写入一条文档后立即用 ES 查询，默认配置下最可能的结果是什么？" :options='["立刻可搜到，ES 是强一致的","可能约 1 秒后才可搜到","永远搜不到，需要手动 flush","直接报写入冲突"]' answer="1" output="写入先落 translog 与内存缓冲，默认每 1 秒 refresh 生成可搜索段，因此存在约 1 秒的可见性延迟" explain="ES 是近实时系统；refresh 才让新段可搜索。测试里可用 refresh=true 强制刷新，但生产逐条刷新会严重拖慢写入。" />

## 一页纸速记

<KnowledgeCard :points='["倒排索引是「词项到文档 ID 列表」，多词查询化为有序列表交集","text 分词用于全文检索，keyword 不分词用于精确过滤与聚合","must 参与打分，filter 不打分且可缓存；深分页改用 search_after","ES 是近实时读模型，默认 1 秒 refresh，不能当主库","同步链路靠版本号抵御重投与乱序，全量重建用别名切换"]' :pitfalls='["把 ES 当主数据源，忽略异步延迟与重建风险","from + size 深分页打爆集群","mapping 字段类型随意改，只能 reindex","bulk 里逐条强制 refresh，产生大量小段","前导通配符查询让倒排索引失效"]' summary="搜索引擎快的根源是索引方向反转；用好它的前提是接受「它是最终一致的读模型」。" />

## 小结与下一讲

- 倒排索引把「逐行扫描」换成「查词表取交集」，分词质量决定召回上限。
- `text`/`keyword` 与 `match`/`term` 必须配对使用，`bool` 里区分打分与过滤。
- ES 是近实时读模型，主数据在数据库，同步靠版本号、重试与死信保证最终一致。

服务功能齐了，最后两个问题关乎安全与文件：谁能调用这些接口，以及大文件怎么可靠地存下来。下一讲先解决身份体系。

[下一讲：认证授权：Session / JWT / OAuth2 / RBAC](/server/auth)
