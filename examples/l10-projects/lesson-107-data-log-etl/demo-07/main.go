package main

import (
	"errors"
	"fmt"
	"math/rand"
	"time"
)

type WindowResult struct {
	WindowStart int64
	Level       string
	Count       int64
	Bytes       int64
	Errors      int64
}

// Sink 必须幂等：seen 模拟数据库唯一键，重放同一批不会新增行。
type Sink struct {
	seen map[string]bool
	fail int
}

func (s *Sink) BatchWrite(results []WindowResult, keys []string) error {
	if s.fail > 0 {
		s.fail--
		return errors.New("下游暂时不可用")
	}
	for i, r := range results {
		if s.seen[keys[i]] {
			continue
		}
		s.seen[keys[i]] = true
		_ = r
	}
	return nil
}

// RetryWrite 带重试预算与指数退避；预算耗尽返回错误，让位点不提交、下次重放整批。
func RetryWrite(s *Sink, results []WindowResult, keys []string, budget int) error {
	var err error
	for attempt := 0; attempt < budget; attempt++ {
		if err = s.BatchWrite(results, keys); err == nil {
			return nil
		}
		backoff := time.Duration(1<<attempt) * 10 * time.Millisecond
		backoff += time.Duration(rand.Intn(10)) * time.Millisecond
		time.Sleep(backoff)
	}
	return fmt.Errorf("批写失败，重试 %d 次仍不成功: %w", budget, err)
}

func main() {
	s := &Sink{seen: make(map[string]bool), fail: 2}
	results := []WindowResult{{WindowStart: 1700000000, Level: "INFO", Count: 3}}
	keys := []string{"1700000000-INFO"}
	if err := RetryWrite(s, results, keys, 4); err != nil {
		fmt.Println("放弃:", err)
		return
	}
	_ = RetryWrite(s, results, keys, 4)
	fmt.Println("去重后落库行数:", len(s.seen))
}
