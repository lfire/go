package main

import "fmt"

// Value 是带类型标签的单元格；Valid 为假表示 NULL，绝不退化成零值。
type Value struct {
	Kind  string
	Num   float64
	Str   string
	Valid bool
}

// Row 是一行数据，列名到 Value 的映射。
type Row map[string]Value

// Emit 是流式结果的唯一出口；返回 error 时执行器立即停止。
type Emit func(Row) error

// DataSource 屏蔽数据从哪读；执行计划只依赖这个接口。
type DataSource interface {
	Columns() []string
	Scan(Emit) error
	SupportsPushdown() bool
}

// memorySource 是最小的接口实现，用于编译期检查契约。
type memorySource struct {
	cols []string
	rows []Row
}

func (m memorySource) Columns() []string      { return m.cols }
func (m memorySource) SupportsPushdown() bool { return false }
func (m memorySource) Scan(emit Emit) error {
	for _, r := range m.rows {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	var ds DataSource = memorySource{cols: []string{"status"}, rows: []Row{{"status": {Kind: "int", Num: 200, Valid: true}}}}
	n := 0
	_ = ds.Scan(func(Row) error { n++; return nil })
	fmt.Println("契约就绪，扫描行数:", n, "支持下推:", ds.SupportsPushdown())
}
