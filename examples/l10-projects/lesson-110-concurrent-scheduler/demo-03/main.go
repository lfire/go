package main

import "fmt"

type State int32

const (
	Pending State = iota
	Scheduled
	Running
	Succeeded
	Failed
	TimedOut
)

func (s State) String() string {
	return [...]string{"Pending", "Scheduled", "Running", "Succeeded", "Failed", "TimedOut"}[s]
}

// allowed 定义合法转换：键为当前状态，值为允许的目标集合。
var allowed = map[State]map[State]bool{
	Pending:   {Scheduled: true},
	Scheduled: {Running: true, Failed: true},
	Running:   {Succeeded: true, Failed: true, TimedOut: true},
	Failed:    {Scheduled: true},
	TimedOut:  {Scheduled: true},
	Succeeded: {},
}

// Machine 持有单个任务的当前状态。
type Machine struct{ state State }

func NewMachine() *Machine { return &Machine{state: Pending} }

func (m *Machine) State() State { return m.state }

// To 执行状态转换，非法转换返回错误且不改变状态。
func (m *Machine) To(next State) error {
	if !allowed[m.state][next] {
		return fmt.Errorf("非法转换: %s -> %s", m.state, next)
	}
	m.state = next
	return nil
}

func trace(steps ...State) {
	m := NewMachine()
	for _, s := range steps {
		if err := m.To(s); err != nil {
			fmt.Println("  拒绝:", err)
			return
		}
		fmt.Println("  进入:", m.State())
	}
}

func main() {
	fmt.Println("正常链路:")
	trace(Scheduled, Running, Succeeded)
	fmt.Println("重试链路:")
	trace(Scheduled, Running, Failed, Scheduled, Running, Succeeded)
	fmt.Println("非法链路:")
	trace(Running) // Pending 直接到 Running 应被拒绝
}
