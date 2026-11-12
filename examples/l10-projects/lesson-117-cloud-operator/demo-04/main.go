// 文件：internal/informer/store.go
package main

import (
	"fmt"
	"sync"
)

// Object 是缓存里的对象投影，RV 是 apiserver 的 resourceVersion。
type Object struct {
	Key             string
	Namespace       string
	ResourceVersion string
	Replicas        int
}

func (o Object) DeepCopy() Object { return o }

// Store 是线程安全的本地缓存：主键索引加命名空间二级索引。
type Store struct {
	mu   sync.RWMutex
	byK  map[string]Object
	byNS map[string]map[string]struct{}
}

func NewStore() *Store {
	return &Store{
		byK:  make(map[string]Object),
		byNS: make(map[string]map[string]struct{}),
	}
}

// Add 覆盖写入并维护索引；写前 DeepCopy，避免外部持有同一底层数据。
func (s *Store) Add(o Object) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byK[o.Key] = o.DeepCopy()
	set := s.byNS[o.Namespace]
	if set == nil {
		set = make(map[string]struct{})
		s.byNS[o.Namespace] = set
	}
	set[o.Key] = struct{}{}
}

// Update 拒绝乱序事件：RV 不大于当前值就丢弃，返回是否接受。
func (s *Store) Update(o Object) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.byK[o.Key]; ok && cur.ResourceVersion >= o.ResourceVersion {
		return false
	}
	s.byK[o.Key] = o.DeepCopy()
	return true
}

func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.byK[key]; ok {
		delete(s.byK, key)
		if set := s.byNS[o.Namespace]; set != nil {
			delete(set, key)
		}
	}
}

// Get 返回副本，调用方修改不影响缓存，是 DeepCopy 纪律的关键落点。
func (s *Store) Get(key string) (Object, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.byK[key]
	return o.DeepCopy(), ok
}

func (s *Store) ListNS(ns string) []Object {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Object, 0)
	for k := range s.byNS[ns] {
		if o, ok := s.byK[k]; ok {
			out = append(out, o.DeepCopy())
		}
	}
	return out
}

func main() {
	s := NewStore()
	s.Add(Object{Key: "prod/kv", Namespace: "prod", ResourceVersion: "100", Replicas: 3})
	// 迟到的旧事件必须被丢弃
	fmt.Println("stale accepted:", s.Update(Object{Key: "prod/kv", Namespace: "prod", ResourceVersion: "99", Replicas: 1}))
	fmt.Println("fresh accepted:", s.Update(Object{Key: "prod/kv", Namespace: "prod", ResourceVersion: "101", Replicas: 5}))
	o, _ := s.Get("prod/kv")
	o.Replicas = 999 // 修改副本，不影响缓存
	o2, _ := s.Get("prod/kv")
	fmt.Println("cache replicas:", o2.Replicas, "ns objects:", len(s.ListNS("prod")))
}
