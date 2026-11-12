// 文件：binlog_sync.go —— 订阅 binlog 幂等写 ES：版本挡乱序、位点可重放
package main

import (
	"fmt"
	"sync"
)

type RowVersion struct {
	SKU     string
	Title   string
	Price   int64
	Version int64 // binlog 单调递增版本，可用 GTID 或 updated_at 加序号
}

// ESIndex 用 map 模拟 ES 文档，external version 保证只有更新的版本能覆盖。
type ESIndex struct {
	mu      sync.Mutex
	docs    map[string]RowVersion
	checkpt int64
	dlq     []string
}

func NewESIndex() *ESIndex {
	return &ESIndex{docs: make(map[string]RowVersion)}
}

// Upsert 用 external version 做幂等：版本小于等于当前直接丢弃，
// 这样重放、乱序、重复事件都不会把旧数据写回去。
func (e *ESIndex) Upsert(row RowVersion) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if cur, ok := e.docs[row.SKU]; ok && row.Version <= cur.Version {
		return false
	}
	e.docs[row.SKU] = row
	return true
}

func (e *ESIndex) Advance(offset int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if offset > e.checkpt {
		e.checkpt = offset
	}
}

func (e *ESIndex) DeadLetter(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dlq = append(e.dlq, id)
}

type BinlogEvent struct {
	Offset int64
	Row    RowVersion
	Bad    bool
}

// Consume 处理一批事件：解析失败进死信且不推进位点，
// 成功才推进 checkpoint，保证至少一次加幂等下不丢数据。
func (e *ESIndex) Consume(events []BinlogEvent) (applied, skipped, poisoned int) {
	for _, ev := range events {
		if ev.Bad {
			e.DeadLetter(fmt.Sprintf("offset-%d", ev.Offset))
			poisoned++
			continue // 不推进位点，等待人工介入后重放
		}
		if e.Upsert(ev.Row) {
			applied++
		} else {
			skipped++
		}
		e.Advance(ev.Offset)
	}
	return applied, skipped, poisoned
}

func (e *ESIndex) Snapshot() (int, int64, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.docs), e.checkpt, len(e.dlq)
}

func main() {
	es := NewESIndex()
	events := []BinlogEvent{
		{Offset: 101, Row: RowVersion{SKU: "S1", Title: "手机", Price: 199900, Version: 5}},
		{Offset: 102, Row: RowVersion{SKU: "S1", Title: "手机 Pro", Price: 299900, Version: 7}},
		{Offset: 103, Row: RowVersion{SKU: "S1", Title: "手机", Price: 199900, Version: 5}}, // 乱序旧版本
		{Offset: 104, Bad: true}, // 毒事件
		{Offset: 105, Row: RowVersion{SKU: "S2", Title: "耳机", Price: 39900, Version: 2}},
	}
	a, s, p := es.Consume(events)
	docs, cp, dlq := es.Snapshot()
	fmt.Printf("applied=%d skipped=%d poisoned=%d docs=%d checkpoint=%d dlq=%d\n", a, s, p, docs, cp, dlq)
}
