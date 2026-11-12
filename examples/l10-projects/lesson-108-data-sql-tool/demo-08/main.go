package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

type Row map[string]string

// Expr 是表达式 AST 的最小接口，本例只实现一个等值比较节点。
type Expr interface {
	Eval(Row) bool
}

type Eq struct {
	Field string
	Value string
}

func (e Eq) Eval(r Row) bool {
	v, ok := r[e.Field]
	return ok && v == e.Value
}

// LogicalPlan 描述查什么；Filter 为 nil 表示全表。
type LogicalPlan struct {
	Filter  Expr
	Columns []string
	Limit   int
}

// Run 把 CSV 扫描、谓词过滤、列投影、流式输出串成一条可运行链路。
func Run(csvText string, lp LogicalPlan, w io.Writer) (int, error) {
	cr := csv.NewReader(strings.NewReader(csvText))
	head, err := cr.Read()
	if err != nil {
		return 0, fmt.Errorf("读表头: %w", err)
	}
	cols := lp.Columns
	if len(cols) == 0 {
		cols = head
	}
	n := 0
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, fmt.Errorf("读数据行: %w", err)
		}
		row := make(Row, len(head))
		for i, h := range head {
			if i < len(rec) {
				row[h] = rec[i]
			}
		}
		if lp.Filter != nil && !lp.Filter.Eval(row) {
			continue
		}
		parts := make([]string, len(cols))
		for i, c := range cols {
			parts[i] = row[c]
		}
		if _, err := fmt.Fprintf(w, "%s\n", strings.Join(parts, ",")); err != nil {
			return n, fmt.Errorf("写出第 %d 行: %w", n, err)
		}
		n++
		if lp.Limit > 0 && n >= lp.Limit {
			break
		}
	}
	return n, nil
}

func main() {
	csvText := "status,size\n200,2048\n500,64\n200,128\n"
	lp := LogicalPlan{Filter: Eq{Field: "status", Value: "200"}, Columns: []string{"status", "size"}, Limit: 10}
	n, err := Run(csvText, lp, io.Discard)
	fmt.Println("输出行数:", n, "错误:", err)
}
