package main

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

type Instance struct {
	ID       string
	Addr     string
	Healthy  bool
	lastBeat time.Time
}

// Registry 是最小的服务注册表：按服务名分组的实例表加一把读写锁。
type Registry struct {
	mu       sync.RWMutex
	services map[string]map[string]*Instance
	ttl      time.Duration
}

func NewRegistry(ttl time.Duration) *Registry {
	return &Registry{services: make(map[string]map[string]*Instance), ttl: ttl}
}

func (r *Registry) Register(service, id, addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.services[service] == nil {
		r.services[service] = make(map[string]*Instance)
	}
	r.services[service][id] = &Instance{ID: id, Addr: addr, Healthy: true, lastBeat: time.Now()}
}

// Beat 续约：刷新最后心跳时间；实例不存在说明已摘除，返回 false 让客户端重新注册。
func (r *Registry) Beat(service, id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst := r.services[service][id]
	if inst == nil {
		return false
	}
	inst.lastBeat = time.Now()
	inst.Healthy = true
	return true
}

func (r *Registry) Deregister(service, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.services[service], id)
}

// Resolve 做一次发现：只返回健康实例，排序后结果可预测。
func (r *Registry) Resolve(service string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, inst := range r.services[service] {
		if inst.Healthy {
			out = append(out, inst.Addr)
		}
	}
	sort.Strings(out)
	return out
}

// sweep 巡检：超过 ttl 没心跳的实例直接摘除。
func (r *Registry) sweep() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for _, set := range r.services {
		for id, inst := range set {
			if now.Sub(inst.lastBeat) > r.ttl {
				delete(set, id)
			}
		}
	}
}

func (r *Registry) StartSweep(interval time.Duration, stop <-chan struct{}) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				r.sweep()
			case <-stop:
				return
			}
		}
	}()
}

func main() {
	reg := NewRegistry(300 * time.Millisecond)
	stop := make(chan struct{})
	reg.StartSweep(100*time.Millisecond, stop)
	defer close(stop)

	reg.Register("user", "u1", "10.0.0.1:9000")
	reg.Register("user", "u2", "10.0.0.2:9000")
	fmt.Println("初始实例:", reg.Resolve("user"))

	// 只有 u1 持续续约；u2 静默，应当被巡检摘除
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				reg.Beat("user", "u1")
			case <-done:
				return
			}
		}
	}()
	time.Sleep(700 * time.Millisecond)
	close(done)
	fmt.Println("巡检后:", reg.Resolve("user"))
}
