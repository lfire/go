package main

import (
	"fmt"
	"sync"
)

// ServiceSet 维护一份实例列表，并支持订阅者 watch 变更。
type ServiceSet struct {
	mu   sync.RWMutex
	list []string
	subs []chan []string
}

func (s *ServiceSet) Update(list []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := append([]string(nil), list...)
	s.list = cp
	for _, ch := range s.subs { // 推送快照，避免订阅者拿到共享底层数组
		ch <- append([]string(nil), cp...)
	}
}

func (s *ServiceSet) Watch() <-chan []string {
	ch := make(chan []string, 1)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	ch <- append([]string(nil), s.list...) // 订阅即给一份当前快照
	s.mu.Unlock()
	return ch
}

func (s *ServiceSet) Snapshot() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.list...)
}

func main() {
	set := &ServiceSet{list: []string{"10.0.0.1:9000"}}
	ch := set.Watch()
	fmt.Println("客户端发现 初始:", <-ch)

	// 注册中心推送一次变更，客户端本地缓存随之更新，无需重新拉取
	set.Update([]string{"10.0.0.1:9000", "10.0.0.3:9000"})
	fmt.Println("收到推送:", <-ch)
	fmt.Println("本地快照:", set.Snapshot())
}
