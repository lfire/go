---
title: 第 108 讲 · 实战 06 · CSV / SQL 查询工具
description: 用一套表达式 AST 与执行计划适配多种数据源，以流式结果和统一测试策略支撑 CSV 与 SQL 查询
---

<StageHeader stage="L10 · 工程实战" level="★★☆☆☆" time="55 分钟" goal="一套 AST 适配多种数据源" prereq="第 107 讲 · 实战 05 · 日志分析 ETL 管道" />

# 第 108 讲 · 实战 06 · CSV / SQL 查询工具

> 窗口统计的结果总要被人查。数据可能在 CSV、内存或数据库里，每种来源写一套 WHERE 判断，逻辑立刻散成三份。本讲用表达式 AST、执行计划与数据源接口三层统一它们。

## 这一讲你会得到什么
<CheckList id="l108-goals" title="本讲目标" :items='["用接口定义表达式 AST，让每个节点自己求值","把查询拆成扫描、过滤、投影三段执行计划","用 DataSource 接口让 CSV 与内存表复用同一逻辑","以回调逐行吐出结果"]' />

## 1. 项目目标与约束

| 维度 | 目标 | 约束与取舍 |
| --- | --- | --- |
| 语法 | 等号、不等号、大于与 AND/OR | 不做完整 SQL 解析器 |
| 数据源 | CSV 与内存表可互换 | 统一 Row 为字符串映射 |
| 结果 | 流式逐行输出 | 不攒全量结果集 |
| 类型 | 数值比较前显式转型 | 失败判为不匹配 |

<Callout type="tip" title="与第 104 讲的 AST 不是一回事" text="第 104 讲用 go/ast 解析已存在的 Go 源码以生成新代码；这一讲定义自定义查询表达式的 AST 用于对数据求值。一个读语法树，一个建语法树。" />

真正的 SQL 解析器要处理词法、优先级与子查询，那是独立项目；本讲只建表达式树加执行计划这个最稳定的内核。

## 2. 架构蓝图

<ConceptDiagram type="architecture-layer" title="查询工具的分层" caption="AST 描述查什么，执行计划描述怎么查，数据源接口屏蔽从哪查，上层只依赖下层接口" />

依赖方向单向：AST 不认识数据源，执行计划不认识 CSV，数据源不认识过滤条件。加运算符改 AST 层，加 LIMIT 改计划层，接 PostgreSQL 只改数据源层。

## 3. 关键模块实现

### 3.1 表达式 AST：每个节点自己求值

```go
package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Row 是一行数据，列名到字符串值的映射。
type Row map[string]string

// Expr 是表达式节点接口；每种节点自己负责求值。
type Expr interface {
	Eval(row Row) bool
}

type Compare struct {
	Field string
	Op    string // = != > >=
	Value string
}

func (c Compare) Eval(row Row) bool {
	left := row[c.Field]
	if c.Op == "=" {
		return left == c.Value
	}
	if c.Op == "!=" {
		return left != c.Value
	}
	// 数值比较：先尝试转成整数，转型失败就判为不匹配。
	a, err1 := strconv.Atoi(left)
	b, err2 := strconv.Atoi(c.Value)
	if err1 != nil || err2 != nil {
		return false
	}
	switch c.Op {
	case ">":
		return a > b
	case ">=":
		return a >= b
	default:
		return false
	}
}

type And struct{ Left, Right Expr }
func (a And) Eval(row Row) bool { return a.Left.Eval(row) && a.Right.Eval(row) }

type Or struct{ Left, Right Expr }
func (o Or) Eval(row Row) bool { return o.Left.Eval(row) || o.Right.Eval(row) }

func main() {
	// 手写一棵 WHERE (status = 200 AND size >= 1000) 的 AST。
	where := And{
		Left:  Compare{Field: "status", Op: "=", Value: "200"},
		Right: Compare{Field: "size", Op: ">=", Value: "1000"},
	}
	rows := []Row{
		{"status": "200", "size": "2048"},
		{"status": "200", "size": "64"},
		{"status": "500", "size": "4096"},
	}
	for _, r := range rows {
		fmt.Println(r, "->", where.Eval(r), strings.Repeat("-", 8))
	}
}
```

Expr 只有一个方法，正是小接口原则：新增 Like 或 In 只需实现 Eval，不用改已有节点。转型失败返回 false 而非报错，因为 CSV 常有空串与脏值；但绝不能静默当成 0 参与比较。

### 3.2 执行计划：扫描、过滤、投影

```go
package main

import (
	"fmt"
	"io"
)

type Row map[string]string

type Predicate func(Row) bool

// Plan 把查询拆成三个可组合的阶段，执行顺序固定。
type Plan struct {
	Table   string
	Where   Predicate
	Columns []string
}

func (p Plan) Project(r Row) Row {
	if len(p.Columns) == 0 {
		return r // 空列清单表示取全部列
	}
	out := make(Row, len(p.Columns))
	for _, c := range p.Columns {
		out[c] = r[c]
	}
	return out
}

// Execute 以回调方式逐行吐出结果，不构造中间结果集。
func Execute(rows []Row, p Plan, emit func(Row) error) (int, error) {
	n := 0
	for _, r := range rows {
		if p.Where != nil && !p.Where(r) {
			continue
		}
		if err := emit(p.Project(r)); err != nil {
			return n, fmt.Errorf("写出第 %d 行: %w", n, err)
		}
		n++
	}
	return n, nil
}

// CSVWriter 输出到任意 io.Writer，演示「流式结果不绑定具体终端」。
func CSVWriter(w io.Writer) func(Row) error {
	return func(r Row) error {
		_, err := fmt.Fprintf(w, "%s,%s\n", r["status"], r["size"])
		return err
	}
}

func main() {
	rows := []Row{
		{"status": "200", "size": "2048"},
		{"status": "500", "size": "64"},
		{"status": "200", "size": "128"},
	}
	p := Plan{
		Table:   "access",
		Where:   func(r Row) bool { return r["status"] == "200" },
		Columns: []string{"status", "size"},
	}
	n, err := Execute(rows, p, CSVWriter(io.Discard))
	fmt.Println("输出行数:", n, "错误:", err)
}
```

emit 返回 error 很关键：下游写失败时执行器立刻停止并把第几行包进错误，而不是继续白跑几百万行。这是流式执行相对先攒 slice 再统一写的另一个优势，失败点精确。

### 3.3 数据源适配：接口让实现可插拔

```go
package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

type Row map[string]string

// DataSource 屏蔽「数据从哪来」；执行计划只依赖这个接口。
type DataSource interface {
	Scan(emit func(Row) error) error
	Columns() []string
}

// SliceSource 内存数据源，便于测试。
type SliceSource struct {
	cols []string
	rows []Row
}

func (s SliceSource) Columns() []string { return s.cols }
func (s SliceSource) Scan(emit func(Row) error) error {
	for _, r := range s.rows {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

// CSVSource 流式读取，不在内存里攒全表。
type CSVSource struct {
	r      io.Reader
	header []string
}

func (c *CSVSource) Columns() []string { return c.header }
func (c *CSVSource) Scan(emit func(Row) error) error {
	cr := csv.NewReader(c.r)
	head, err := cr.Read()
	if err != nil {
		return fmt.Errorf("读表头: %w", err)
	}
	c.header = head
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读数据行: %w", err)
		}
		row := make(Row, len(head))
		for i, h := range head {
			row[h] = rec[i]
		}
		if err := emit(row); err != nil {
			return err
		}
	}
}

func CountBy(src DataSource, field, want string) (int, error) {
	n := 0
	err := src.Scan(func(r Row) error {
		if r[field] == want {
			n++
		}
		return nil
	})
	return n, err
}

func main() {
	csvText := "status,size\n200,2048\n500,64\n200,128\n"
	sources := []DataSource{
		SliceSource{cols: []string{"status", "size"}, rows: []Row{{"status": "200", "size": "1"}}},
		&CSVSource{r: strings.NewReader(csvText)},
	}
	for i, s := range sources {
		n, err := CountBy(s, "status", "200")
		fmt.Printf("数据源 %d 列=%v 命中=%d 错误=%v\n", i, s.Columns(), n, err)
	}
}
```

CountBy 完全不知道数据来自内存还是 CSV，只调用 Scan。encoding/csv 的 Reader 默认流式，一次只读一条记录，还会自动校验每行字段数与表头一致。将来接数据库只需写 SQLSource，把 rows.Next 包成同样的 emit 回调。

## 4. 失败路径与边界条件

字段数与表头不符由 csv.Reader 校验；列缺失取到空串会被当合法值，应用 comma-ok 判存在；空文件要区分读失败；下游写失败靠 emit 立即中断；接数据库一律参数化查询。最隐蔽的是把列缺失当空串、转型失败当 0，静默给出错误结果。

## 5. 测试与压测思路

AST 用表驱动覆盖正常值、边界值、脏值与缺失列；用 t.Run 让 CSV 与 SliceSource 跑同一组期望，这是验证接口抽象最直接的方式；构造只接受 3 行就返回错误的 emit，断言立即停止并带行号。

测试核心是同一份期望跑在多个数据源上，新增 SQLSource 可零成本复用用例。压测盯 allocs/op，每行只该有一次 Row 分配。

## 6. 可继续扩展的方向

加词法与语法分析把 SELECT WHERE 编译成本讲 AST；把过滤条件下推给数据源；新增 SQLSource 用 database/sql 的 Rows 包成 emit，沿用第 65 讲连接池；加 LIMIT 与 GROUP BY 作为计划新阶段；把第 107 讲窗口结果写成 CSV 用本工具直接查询，形成完整数据链路。

## 随堂检测

<Quiz no="108-1" q="为什么把结果设计成 emit 回调？" :options='["回调写起来更短","内存与总行数无关，下游失败能立刻中断并定位行号","Go 不支持返回切片"]' answer="1" explain="流式回调只持有一行，避免大结果集撑爆内存；emit 返回 error 还能让执行器立刻停止并带出行号。" />

## 一页纸速记

<KnowledgeCard :points='["小接口 Expr 让每个节点自己求值，新增算子零改动","执行计划固定扫描、过滤、投影三阶段","DataSource 屏蔽来源；emit 回调逐行输出"]' :pitfalls='["列缺失当空串、转型失败当 0 会静默给出错误结果","扫描后才填 header 会让扫描前列校验失效"]' summary="把查什么、怎么查、从哪查拆成三层，工具才不会腐烂。" />

## 小结与下一讲

- 表达式 AST 是查什么的稳定内核，小接口让算子扩展近乎零成本。
- 执行计划把过滤与投影串成流水线，回调输出保证流式与精确失败。
- DataSource 接口是复用关键，同一批表驱动测试能覆盖所有实现。

[下一讲：实战 07 · 并发爬虫：单机 → 并发 → 分布式](/projects/concurrent-crawler)
