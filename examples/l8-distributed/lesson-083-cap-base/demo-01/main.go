package main

import "fmt"

// Quorum 描述一份数据的副本分布与读写所需确认数
type Quorum struct {
	N int // 副本总数
	W int // 一次写入需要的确认副本数
	R int // 一次读取需要查询的副本数
}

// StrongConsistent 用 R + W > N 与 W > N/2 判断读写是否满足强一致。
// 两个条件同时成立，读集合与写集合必有交集，且不会出现两个并发写都成功。
func (q Quorum) StrongConsistent() bool {
	return q.R+q.W > q.N && q.W > q.N/2
}

func main() {
	configs := []Quorum{
		{N: 3, W: 2, R: 2}, // 经典多数派，可容忍 1 个副本失联
		{N: 3, W: 1, R: 1}, // 追求低延迟，读写集合可能不相交
		{N: 3, W: 3, R: 1}, // 写全同步、读任意，写入可用性最低
	}
	for _, q := range configs {
		fmt.Printf("N=%d W=%d R=%d 强一致=%v\n", q.N, q.W, q.R, q.StrongConsistent())
	}
}
