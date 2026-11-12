package main

import (
	"fmt"
	"sync/atomic"
)

// Resolver 把服务名解析成地址列表，并在变化时推送；对应 gRPC resolver.Builder 与 Resolver。
type Resolver interface {
	Resolve(service string) []string
	Watch(service string) <-chan []string
}

// Balancer 从地址列表里挑一个；对应 gRPC balancer.Picker 的 Pick 方法。
type Balancer interface {
	Pick(addrs []string) string
}

type pickFirst struct{}

func (pickFirst) Pick(addrs []string) string {
	if len(addrs) == 0 {
		return ""
	}
	return addrs[0] // 只认第一个，简单但不分摊流量
}

type roundRobin struct{ n atomic.Uint64 }

func (r *roundRobin) Pick(addrs []string) string {
	if len(addrs) == 0 {
		return ""
	}
	i := r.n.Add(1) - 1 // 从下标 0 开始
	return addrs[i%uint64(len(addrs))]
}

type staticResolver struct{ table map[string][]string }

func (s *staticResolver) Resolve(service string) []string { return s.table[service] }
func (s *staticResolver) Watch(service string) <-chan []string {
	ch := make(chan []string, 1)
	ch <- s.table[service]
	return ch
}

func main() {
	res := &staticResolver{table: map[string][]string{
		"user": {"10.0.0.1:9000", "10.0.0.2:9000", "10.0.0.3:9000"},
	}}
	addrs := res.Resolve("user")
	fmt.Println("解析到地址:", addrs)
	fmt.Println("pick_first 选中:", (pickFirst{}).Pick(addrs))

	var lb Balancer = &roundRobin{}
	for i := 0; i < 6; i++ {
		fmt.Println("round_robin 选中:", lb.Pick(addrs))
	}
}
