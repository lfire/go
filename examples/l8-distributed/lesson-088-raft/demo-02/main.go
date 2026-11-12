package main

import "fmt"

// LogEntry 是一条日志，Term 记录它由哪个任期的 Leader 写入
type LogEntry struct {
	Index int
	Term  int
	Cmd   string
}

// RaftLog 用副本复制进度演示多数派提交
type RaftLog struct {
	entries  []LogEntry
	commit   int
	replicas map[string]int
}

func NewRaftLog() *RaftLog {
	return &RaftLog{replicas: make(map[string]int)}
}

func (l *RaftLog) Append(term int, cmd string) LogEntry {
	e := LogEntry{Index: len(l.entries) + 1, Term: term, Cmd: cmd}
	l.entries = append(l.entries, e)
	return e
}

func (l *RaftLog) Replicate(node string, index int) {
	if index > l.replicas[node] {
		l.replicas[node] = index
	}
}

// TryCommit 从后往前找最高的一条「被多数派复制」的日志并提交
func (l *RaftLog) TryCommit(total int) {
	for i := len(l.entries); i > l.commit; i-- {
		copies := 1 // Leader 自己
		for _, idx := range l.replicas {
			if idx >= i {
				copies++
			}
		}
		if copies > total/2 {
			l.commit = i
			fmt.Printf("索引 %d 已复制到 %d/%d 个节点，提交\n", i, copies, total)
			return
		}
	}
	fmt.Println("没有日志达到多数派，本次不提交")
}

func main() {
	l := NewRaftLog()
	e1 := l.Append(2, "set x=1")
	e2 := l.Append(2, "set y=2")

	// 只把第一条复制到 n2，凑成多数派
	l.Replicate("n2", e1.Index)
	l.TryCommit(3)
	fmt.Println("当前提交索引:", l.commit, "，未提交命令:", e2.Cmd)

	// Leader 在同步第二条之前宕机：n3 完全落后
	fmt.Println("n3 的复制进度:", l.replicas["n3"], "（落后副本）")

	// 分区恢复后把两条都补齐，再提交
	l.Replicate("n3", e2.Index)
	l.TryCommit(3)
	fmt.Println("补齐后提交索引:", l.commit)
}
