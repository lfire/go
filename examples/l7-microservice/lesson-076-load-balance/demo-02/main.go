package main

import "fmt"

type Node struct {
	Addr    string
	Weight  int // 配置权重，固定不变
	current int // 当前权重，每轮变化
}

type smoothWeighted struct {
	nodes []*Node
	total int
}

func newSmoothWeighted(nodes []*Node) *smoothWeighted {
	total := 0
	cur := make([]*Node, len(nodes))
	for i, n := range nodes {
		total += n.Weight
		cur[i] = &Node{Addr: n.Addr, Weight: n.Weight}
	}
	return &smoothWeighted{nodes: cur, total: total}
}

func (s *smoothWeighted) Pick() string {
	var best *Node
	for _, n := range s.nodes {
		n.current += n.Weight
		if best == nil || n.current > best.current {
			best = n
		}
	}
	best.current -= s.total
	return best.Addr
}

func main() {
	lb := newSmoothWeighted([]*Node{
		{Addr: "A", Weight: 5},
		{Addr: "B", Weight: 1},
		{Addr: "C", Weight: 1},
	})
	for i := 0; i < 7; i++ {
		fmt.Print(lb.Pick(), " ")
	}
	fmt.Println()
}
