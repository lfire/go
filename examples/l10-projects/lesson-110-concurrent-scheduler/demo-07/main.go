package main

import (
	"fmt"
	"sync"
)

// Executions 记录每个任务已接受的最大 epoch，写入端校验 fencing token。
type Executions struct {
	mu   sync.Mutex
	done map[string]int64
}

func NewExecutions() *Executions {
	return &Executions{done: make(map[string]int64)}
}

// TryRun 只接受 epoch 更大的派发，挡住旧 leader 复活与重复重派。
func (e *Executions) TryRun(job string, epoch int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if prev, ok := e.done[job]; ok && prev >= epoch {
		return false
	}
	e.done[job] = epoch
	return true
}

// Dispatch 按分片过滤后带 epoch 派发，返回实际执行数。
func Dispatch(jobs []string, shards, owner int, ex *Executions, epoch int64, tag string) int {
	n := 0
	for _, j := range jobs {
		shard := 0
		for i := 0; i < len(j); i++ {
			shard += int(j[i])
		}
		if shard%shards != owner {
			continue
		}
		if ex.TryRun(j, epoch) {
			fmt.Printf("  %s 执行 %s (epoch=%d)\n", tag, j, epoch)
			n++
		} else {
			fmt.Printf("  %s 拒绝重复 %s (epoch=%d)\n", tag, j, epoch)
		}
	}
	return n
}

func main() {
	ex := NewExecutions()
	jobs := []string{"report", "cleanup", "heartbeat", "sync", "settle"}
	fmt.Println("旧 leader 首次派发 (epoch=1):")
	Dispatch(jobs, 3, 0, ex, 1, "leader-1")
	fmt.Println("新 leader 故障转移重派 (epoch=2):")
	Dispatch(jobs, 3, 0, ex, 2, "leader-2")
	fmt.Println("旧 leader 复活重派 (epoch=1):")
	Dispatch(jobs, 3, 0, ex, 1, "leader-1-old")
}
