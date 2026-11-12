// 错误栈：在错误产生处记录一次调用点，便于线上定位
package main

import (
	"fmt"
	"runtime"
)

// StackError 在包装时附带一次调用点信息
type StackError struct {
	msg   string
	frame string
}

func (e *StackError) Error() string {
	return e.msg + " @ " + e.frame
}

// here 跳过自身与包装函数，记录真正触发错误的调用者
func here() string {
	pcs := make([]uintptr, 1)
	n := runtime.Callers(3, pcs)
	if n == 0 {
		return "unknown"
	}
	return runtime.FuncForPC(pcs[0]).Name()
}

func NewStack(msg string) error {
	return &StackError{msg: msg, frame: here()}
}

func main() {
	fmt.Println(NewStack("读取配置失败"))
}
