package main

import (
	"errors"
	"fmt"
	"sync"
)

// Segment 表示一个号段区间，本地内存发号，用完再取
type Segment struct {
	mu   sync.Mutex
	cur  int64
	max  int64
	step int64
	// fetch 模拟从数据库原子取号段：UPDATE max_id = max_id + step
	fetch func(step int64) (int64, error)
}

func (s *Segment) load() error {
	max, err := s.fetch(s.step)
	if err != nil {
		return err // 数据库不可用，取号段失败
	}
	s.cur = max - s.step
	s.max = max
	return nil
}

// Next 发一个号；号段用尽时尝试加载下一段
func (s *Segment) Next() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur >= s.max {
		if err := s.load(); err != nil {
			return 0, errors.New("号段用尽且数据库不可用：" + err.Error())
		}
	}
	id := s.cur
	s.cur++
	return id, nil
}

func main() {
	dbMax := int64(1000)
	seg := &Segment{
		step: 5,
		fetch: func(step int64) (int64, error) {
			dbMax += step
			return dbMax, nil
		},
	}
	for i := 0; i < 7; i++ {
		id, err := seg.Next()
		if err != nil {
			fmt.Println("发号失败:", err)
			continue
		}
		fmt.Println("发号:", id)
	}

	// 注入故障：数据库不可用，首段都领不到
	seg2 := &Segment{step: 5,
		fetch: func(step int64) (int64, error) {
			return 0, errors.New("数据库连接超时")
		}}
	_, err := seg2.Next()
	fmt.Println("取号段失败:", err != nil)
}
