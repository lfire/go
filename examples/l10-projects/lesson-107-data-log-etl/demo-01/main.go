package main

import "fmt"

// WindowResult 是落地契约的最小单元，字段与 window_agg 表列一一对应。
type WindowResult struct {
	WindowStart int64
	Level       string
	Count       int64
	Bytes       int64
	Errors      int64
}

// Sink 是落地层对上暴露的唯一接口；聚合与窗口层只依赖它，测试时可换成内存 fake。
type Sink interface {
	BatchWrite(results []WindowResult, keys []string) error
	Close() error
}

// noopSink 用于编译期检查接口实现；真实实现见第 6.6 节。
type noopSink struct{}

func (noopSink) BatchWrite([]WindowResult, []string) error { return nil }
func (noopSink) Close() error                              { return nil }

func main() {
	var s Sink = noopSink{}
	if err := s.BatchWrite([]WindowResult{{WindowStart: 1, Level: "INFO"}}, []string{"k"}); err != nil {
		fmt.Println("批写失败:", err)
	}
	fmt.Println("契约就绪:", s != nil)
}
