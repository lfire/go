package main

import (
	"fmt"
	"io"
	"runtime"
)

type Row map[string]string
type Emit func(Row) error

// Source 是流式数据源：逐行调用 emit，不预先攒全表。
type Source func(Emit) error

// Execute 把源、过滤、输出串成流水线，任一环节都只持有一行。
func Execute(src Source, where func(Row) bool, emit Emit) (int, error) {
	n := 0
	err := src(func(r Row) error {
		if where != nil && !where(r) {
			return nil
		}
		if err := emit(r); err != nil {
			return fmt.Errorf("写出第 %d 行: %w", n, err)
		}
		n++
		return nil
	})
	if err != nil {
		return n, err
	}
	return n, nil
}

// genSource 造一百万行但不落盘、不存切片，演示源侧也是流式的。
func genSource(total int) Source {
	return func(emit Emit) error {
		for i := 0; i < total; i++ {
			if err := emit(Row{"status": "200", "size": "128"}); err != nil {
				return err
			}
		}
		return nil
	}
}

// CSVWriter 把结果写到任意 io.Writer，输出侧同样只持有一行。
func CSVWriter(w io.Writer) Emit {
	return func(r Row) error {
		_, err := fmt.Fprintf(w, "%s,%s\n", r["status"], r["size"])
		return err
	}
}

func main() {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	n, err := Execute(genSource(1000000), func(r Row) bool { return r["status"] == "200" }, CSVWriter(io.Discard))
	runtime.GC()
	runtime.ReadMemStats(&after)
	delta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if delta < 0 {
		delta = 0
	}
	errNote := "无"
	if err != nil {
		errNote = err.Error()
	}
	fmt.Printf("输出行数=%d 错误=%s 执行后常驻堆增量=%d KB\n", n, errNote, delta/1024)
}
