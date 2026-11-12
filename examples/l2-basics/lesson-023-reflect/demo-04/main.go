package main

import "fmt"

//go:generate stringer -type=Status

// Status 表示任务状态；真实项目里 String() 由 stringer 生成。
type Status int

const (
	Pending Status = iota
	Running
	Done
)

// 这里手写一份，保证本文件可以独立编译运行。
func (s Status) String() string {
	switch s {
	case Pending:
		return "Pending"
	case Running:
		return "Running"
	default:
		return "Done"
	}
}

func main() {
	fmt.Println(Running) // Running
}
