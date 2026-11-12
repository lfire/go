package main

import (
	"fmt"
	"strings"
)

// LogicalPlan 只描述查什么，与数据源无关。
type LogicalPlan struct {
	Table   string
	Filter  string
	Columns []string
	Limit   int
}

// PhysicalPlan 描述怎么查：是否下推、投影列、是否流式、LIMIT 位置。
type PhysicalPlan struct {
	Table            string
	ScanWithPushdown bool
	PushdownExpr     string
	ProjectColumns   []string
	Limit            int
	Streaming        bool
}

// Optimize 把逻辑计划降级成物理计划，核心规则是谓词下推与列裁剪。
func Optimize(lp LogicalPlan, canPushdown bool) PhysicalPlan {
	pp := PhysicalPlan{
		Table:          lp.Table,
		ProjectColumns: lp.Columns,
		Limit:          lp.Limit,
		Streaming:      true,
	}
	if canPushdown && lp.Filter != "" {
		pp.ScanWithPushdown = true
		pp.PushdownExpr = lp.Filter
	}
	return pp
}

// Explain 输出可读计划，是 REPL 里排查慢查询的第一入口。
func (p PhysicalPlan) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project %s\n", strings.Join(p.ProjectColumns, ","))
	if p.Limit > 0 {
		fmt.Fprintf(&b, "  Limit %d\n", p.Limit)
	}
	if p.ScanWithPushdown {
		fmt.Fprintf(&b, "  Filter(pushed down) %s\n", p.PushdownExpr)
	} else {
		b.WriteString("  Filter(in executor)\n")
	}
	fmt.Fprintf(&b, "  TableScan %s streaming=%v", p.Table, p.Streaming)
	return b.String()
}

func main() {
	lp := LogicalPlan{Table: "access", Filter: "status = 200", Columns: []string{"status", "size"}, Limit: 100}
	fmt.Println("== 数据源支持下推 ==")
	fmt.Println(Optimize(lp, true).Explain())
	fmt.Println("== 数据源不支持下推 ==")
	fmt.Println(Optimize(lp, false).Explain())
}
