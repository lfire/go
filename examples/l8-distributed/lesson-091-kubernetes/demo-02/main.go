package main

import "fmt"

type shutdownState string

const (
	stateServing  shutdownState = "Serving"
	stateDraining shutdownState = "Draining"
	stateStopping shutdownState = "Stopping"
	stateExited   shutdownState = "Exited"
)

// Transition 按固定顺序推进状态，返回经过的状态序列
func Transition(sigterm bool) []shutdownState {
	seq := []shutdownState{stateServing}
	if !sigterm {
		return seq
	}
	seq = append(seq, stateDraining) // 先置未就绪，摘流
	seq = append(seq, stateStopping) // 再关连接，等存量请求
	seq = append(seq, stateExited)
	return seq
}

func main() {
	for _, s := range Transition(true) {
		fmt.Println("状态:", s)
	}
	fmt.Println("正确顺序: 先摘流再停服，避免存量请求被切断")
}
