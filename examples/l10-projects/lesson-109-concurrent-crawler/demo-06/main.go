package main

import (
	"container/heap"
	"fmt"
)

// Item 是队列元素：Priority 越小越先出，Depth 抑制无限下钻。
type Item struct {
	URL      string
	Priority int
	Depth    int
	index    int
}

// PriorityQueue 用最小堆实现，支持按 URL 更新优先级。
type PriorityQueue struct {
	items []*Item
	pos   map[string]*Item
}

func NewPriorityQueue() *PriorityQueue {
	pq := &PriorityQueue{pos: map[string]*Item{}}
	heap.Init(pq)
	return pq
}

func (pq *PriorityQueue) Len() int           { return len(pq.items) }
func (pq *PriorityQueue) Less(i, j int) bool { return pq.items[i].Priority < pq.items[j].Priority }
func (pq *PriorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[i].index = i
	pq.items[j].index = j
}
func (pq *PriorityQueue) Push(x any) {
	it := x.(*Item)
	it.index = len(pq.items)
	pq.items = append(pq.items, it)
	pq.pos[it.URL] = it
}
func (pq *PriorityQueue) Pop() any {
	old := pq.items
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	pq.items = old[:n-1]
	delete(pq.pos, it.URL)
	return it
}

// Upsert 首次入队或发现更高优先级时更新，不产生重复元素。
func (pq *PriorityQueue) Upsert(url string, priority, depth int) {
	if it, ok := pq.pos[url]; ok {
		if priority < it.Priority {
			it.Priority = priority
			heap.Fix(pq, it.index)
		}
		return
	}
	heap.Push(pq, &Item{URL: url, Priority: priority, Depth: depth})
}

func (pq *PriorityQueue) PopTop() (Item, bool) {
	if pq.Len() == 0 {
		return Item{}, false
	}
	return *heap.Pop(pq).(*Item), true
}

func main() {
	pq := NewPriorityQueue()
	pq.Upsert("/checkout", 9, 3)
	pq.Upsert("/home", 1, 0)
	pq.Upsert("/list", 5, 1)
	pq.Upsert("/home", 0, 0) // 更高优先级，原地更新
	for {
		it, ok := pq.PopTop()
		if !ok {
			break
		}
		fmt.Printf("出队 url=%s 优先级=%d 深度=%d\n", it.URL, it.Priority, it.Depth)
	}
}
