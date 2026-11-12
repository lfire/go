package main

import (
	"fmt"
	"sync"
)

// Zone 是一个可用区，Healthy 决定是否接流量
type Zone struct {
	Name    string
	Healthy bool
	Weight  int
}

// Router 只在健康区里轮询分配流量
type Router struct {
	mu    sync.Mutex
	zones []Zone
}

// Pick 全部不健康才返回空，避免把请求打向已死的域
func (r *Router) Pick(req int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	healthy := make([]int, 0, len(r.zones))
	for i, z := range r.zones {
		if z.Healthy {
			healthy = append(healthy, i)
		}
	}
	if len(healthy) == 0 {
		return ""
	}
	idx := healthy[req%len(healthy)]
	return r.zones[idx].Name
}

// FailZone 把某个可用区整体摘除，模拟机房级故障
func (r *Router) FailZone(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.zones {
		if r.zones[i].Name == name {
			r.zones[i].Healthy = false
			fmt.Println("摘除故障域:", name)
		}
	}
}

func main() {
	r := &Router{zones: []Zone{
		{"zone-a", true, 1}, {"zone-b", true, 1}, {"zone-c", true, 1},
	}}
	for i := 0; i < 3; i++ {
		fmt.Println("请求", i, "路由到:", r.Pick(i))
	}
	// 模拟一个机房整体宕机
	r.FailZone("zone-b")
	for i := 0; i < 4; i++ {
		fmt.Println("故障后请求", i, "路由到:", r.Pick(i))
	}
	fmt.Println("剩余健康区承担全部流量，需要提前留足容量余量")
}
