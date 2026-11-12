package main

import (
	"errors"
	"fmt"
	"sync"
)

type status int

const (
	statusInit status = iota
	statusDoing
	statusDone
)

// record 是去重表里的一条记录，state 构成状态机
type record struct {
	state  status
	result string
}

type idempotent struct {
	mu     sync.Mutex
	table  map[string]*record
	charge func(key string) string // 真实扣款，只允许执行一次
	called int
}

// Handle 用幂等键去重：已完成直接回放，处理中拒绝并发，未开始才执行
func (s *idempotent) Handle(key string) (string, error) {
	s.mu.Lock()
	r, ok := s.table[key]
	if ok {
		switch r.state {
		case statusDone:
			s.mu.Unlock()
			return r.result, nil // 重复请求回放上次结果
		case statusDoing:
			s.mu.Unlock()
			return "", errors.New("处理中，请稍后重试")
		}
	} else {
		r = &record{state: statusDoing}
		s.table[key] = r
	}
	s.mu.Unlock()

	// 临界区外执行业务：这里可能中途失败，状态机要能承接
	res := s.charge(key)
	s.called++

	s.mu.Lock()
	r.state = statusDone
	r.result = res
	s.mu.Unlock()
	return res, nil
}

func main() {
	s := &idempotent{
		table:  make(map[string]*record),
		charge: func(key string) string { return "扣款成功:" + key },
	}
	key := "pay:order-2026:req-9527"
	r1, _ := s.Handle(key)
	r2, _ := s.Handle(key) // 超时后的重试
	r3, _ := s.Handle(key) // 客户端再次重放
	fmt.Println("首次:", r1)
	fmt.Println("重试:", r2)
	fmt.Println("重放:", r3)
	fmt.Println("真实扣款执行次数:", s.called)
}
