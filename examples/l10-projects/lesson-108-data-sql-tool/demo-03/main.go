package main

import "fmt"

// Tri 是 SQL 三值逻辑；WHERE 只放行 True。
type Tri int8

const (
	Unknown Tri = iota
	False
	True
)

// Value 用 Valid 表示非 NULL。
type Value struct {
	Kind  string
	Num   float64
	Str   string
	Valid bool
}

func (v Value) IsNull() bool { return !v.Valid }

type Row map[string]Value

// Expr 是小接口：新增算子只需实现 Eval，不改动已有节点。
type Expr interface {
	Eval(Row) Tri
}

// Compare 比较列与常量；列缺失或任一为 NULL 时返回 Unknown。
type Compare struct {
	Field string
	Op    string
	Const Value
}

func (c Compare) Eval(r Row) Tri {
	left, ok := r[c.Field]
	if !ok || left.IsNull() || c.Const.IsNull() {
		return Unknown
	}
	var cmp int
	if left.Kind == "int" {
		switch {
		case left.Num < c.Const.Num:
			cmp = -1
		case left.Num > c.Const.Num:
			cmp = 1
		}
	} else {
		cmp = strCompare(left.Str, c.Const.Str)
	}
	switch c.Op {
	case "=":
		return triOf(cmp == 0)
	case "!=":
		return triOf(cmp != 0)
	case ">":
		return triOf(cmp > 0)
	case ">=":
		return triOf(cmp >= 0)
	case "<":
		return triOf(cmp < 0)
	case "<=":
		return triOf(cmp <= 0)
	default:
		return Unknown
	}
}

func strCompare(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func triOf(b bool) Tri {
	if b {
		return True
	}
	return False
}

// And 与 Or 严格遵循三值逻辑真值表。
type And struct{ L, R Expr }

func (a And) Eval(r Row) Tri {
	l, rr := a.L.Eval(r), a.R.Eval(r)
	if l == False || rr == False {
		return False
	}
	if l == Unknown || rr == Unknown {
		return Unknown
	}
	return True
}

type Or struct{ L, R Expr }

func (o Or) Eval(r Row) Tri {
	l, rr := o.L.Eval(r), o.R.Eval(r)
	if l == True || rr == True {
		return True
	}
	if l == Unknown || rr == Unknown {
		return Unknown
	}
	return False
}

func main() {
	where := And{
		L: Compare{Field: "status", Op: "=", Const: Value{Kind: "int", Num: 200, Valid: true}},
		R: Compare{Field: "size", Op: ">=", Const: Value{Kind: "int", Num: 1000, Valid: true}},
	}
	rows := []Row{
		{"status": {Kind: "int", Num: 200, Valid: true}, "size": {Kind: "int", Num: 2048, Valid: true}},
		{"status": {Kind: "int", Num: 200, Valid: true}},
		{"status": {Kind: "int", Num: 500, Valid: true}, "size": {Kind: "int", Num: 4096, Valid: true}},
	}
	for i, r := range rows {
		fmt.Printf("行 %d WHERE=%v\n", i, where.Eval(r))
	}
}
