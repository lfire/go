// 工厂、单例与构造注入：装配能力也可以很简单
package main

import (
	"fmt"
	"sync"
)

// Store 是依赖抽象
type Store interface {
	Get() string
}

type memStore struct{ v string }

func (m memStore) Get() string { return m.v }

// 工厂：按类型生产实现
func NewStore(kind string) Store {
	switch kind {
	case "mem":
		return memStore{v: "in-memory"}
	default:
		return memStore{v: "default"}
	}
}

// 单例：sync.Once 保证只初始化一次
var (
	once     sync.Once
	instance Store
)

func SharedStore() Store {
	once.Do(func() {
		instance = NewStore("mem")
	})
	return instance
}

// Service 通过构造函数接收依赖，这是最朴素的依赖注入
type Service struct{ store Store }

func NewService(s Store) *Service { return &Service{store: s} }

func main() {
	svc := NewService(SharedStore())
	fmt.Println(svc.store.Get())
	fmt.Println("单例一致:", SharedStore() == svc.store)
}
