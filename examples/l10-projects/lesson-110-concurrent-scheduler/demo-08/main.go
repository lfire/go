package main

import (
	"context"
	"fmt"
	"time"
)

// Loop 演示 Master 主循环：抢主、周期触发、监听失主、优雅退位。
type Loop struct {
	node   string
	leader bool
	epoch  int64
	done   chan struct{}
}

func NewLoop(node string) *Loop {
	return &Loop{node: node, done: make(chan struct{})}
}

// Campaign 简化选主：成功即持有 epoch。
func (l *Loop) Campaign() {
	l.leader = true
	l.epoch++
	fmt.Println(l.node, "当选 leader, epoch=", l.epoch)
}

// Resign 主动退位：停派发、等在途收尾、释放资格。
func (l *Loop) Resign() {
	fmt.Println(l.node, "停止派发，等待在途任务收尾")
	time.Sleep(10 * time.Millisecond) // 模拟 drain 在途
	l.leader = false
	close(l.done)
	fmt.Println(l.node, "已退位，跟随者可立即接管")
}

// Serve 抢主后进入派发循环，收到 done 信号立即退出。
func (l *Loop) Serve(ctx context.Context, ticks int) {
	l.Campaign()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	fired := 0
	for {
		select {
		case <-ctx.Done():
			l.Resign()
			return
		case <-l.done:
			return // 失主信号：一次关闭永久可读
		case <-ticker.C:
			if !l.leader {
				return
			}
			fired++
			fmt.Println("  派发第", fired, "批 (epoch=", l.epoch, ")")
			if fired >= ticks {
				l.Resign()
				return
			}
		}
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	l := NewLoop("master-1")
	l.Serve(ctx, 3)
	fmt.Println("主流程结束，派发资格已释放")
}
