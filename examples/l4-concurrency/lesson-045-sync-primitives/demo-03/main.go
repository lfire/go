package main

import (
	"fmt"
	"sync"
	"time"
)

type Signal struct {
	mu    sync.Mutex
	cond  *sync.Cond
	ready bool
}

func NewSignal() *Signal {
	s := &Signal{}
	s.cond = sync.NewCond(&s.mu) // 绑定一把锁
	return s
}

func (s *Signal) Publish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = true
	s.cond.Broadcast() // 广播唤醒所有等待者
}

func (s *Signal) WaitReady() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for !s.ready { // 必须 for 复查，防虚假唤醒
		s.cond.Wait() // 释放锁挂起，唤醒后重新持锁返回
	}
}

func main() {
	s := NewSignal()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.WaitReady()
			fmt.Println("等待者", id, "被唤醒")
		}(i)
	}
	time.Sleep(50 * time.Millisecond) // 让等待者先挂起
	s.Publish()
	wg.Wait()
}
