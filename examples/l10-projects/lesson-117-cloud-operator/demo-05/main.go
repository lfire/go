// 文件：internal/queue/ratelimit.go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type item struct {
	retries int
	at      time.Time
}

// Queue 用两个集合保证同一 key 不会被并发处理，dirty 是待办、processing 是在途。
type Queue struct {
	mu         sync.Mutex
	cond       *sync.Cond
	dirty      map[string]*item
	processing map[string]bool
	dead       []string
	closed     bool
	maxRetries int
}

func NewQueue(maxRetries int) *Queue {
	q := &Queue{
		dirty:      make(map[string]*item),
		processing: make(map[string]bool),
		maxRetries: maxRetries,
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func backoff(n int) time.Duration {
	d := time.Duration(5) * time.Millisecond
	for i := 0; i < n; i++ {
		d *= 2
		if d > time.Second {
			return time.Second
		}
	}
	return d
}

// Add 幂等入队：同一 key 只在 dirty 里记一次，重复事件不会放大重试。
func (q *Queue) Add(key string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.dirty[key]; !ok {
		q.dirty[key] = &item{}
	}
	q.cond.Signal()
}

// addAfter 把失败项按退避时间放回 dirty，并到点唤醒等待的 worker。
func (q *Queue) addAfter(key string, retries int, d time.Duration) {
	q.mu.Lock()
	q.dirty[key] = &item{retries: retries, at: time.Now().Add(d)}
	q.mu.Unlock()
	time.AfterFunc(d, func() {
		q.mu.Lock()
		q.cond.Broadcast()
		q.mu.Unlock()
	})
}

// Get 阻塞取一个到期的可执行 key，未到退避时间的项跳过。
func (q *Queue) Get() (string, int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		for k, it := range q.dirty {
			if q.processing[k] || time.Now().Before(it.at) {
				continue
			}
			delete(q.dirty, k)
			q.processing[k] = true
			return k, it.retries, true
		}
		if q.closed {
			return "", 0, false
		}
		q.cond.Wait()
	}
}

// Done 处理成功则清除计数，失败按重试预算退避或进死信。
func (q *Queue) Done(key string, err error, retries int) {
	q.mu.Lock()
	delete(q.processing, key)
	q.mu.Unlock()
	if err == nil {
		return
	}
	if retries+1 >= q.maxRetries {
		q.mu.Lock()
		q.dead = append(q.dead, key)
		q.mu.Unlock()
		fmt.Println("dead letter:", key)
		return
	}
	q.addAfter(key, retries+1, backoff(retries))
}

func (q *Queue) Dead() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.dead)
}

func (q *Queue) Close() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

type ReconcileFn func(context.Context, string) error

// Run 启动 workers 个串行消费者，并周期性全量 resync 兜底事件丢失。
func Run(ctx context.Context, q *Queue, workers int, fn ReconcileFn, resync time.Duration, listKeys func() []string) {
	for i := 0; i < workers; i++ {
		go func() {
			for {
				key, retries, ok := q.Get()
				if !ok {
					return
				}
				q.Done(key, fn(ctx, key), retries)
			}
		}()
	}
	t := time.NewTicker(resync)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			q.Close()
			return
		case <-t.C:
			for _, k := range listKeys() {
				q.Add(k)
			}
		}
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	q := NewQueue(8)
	q.Add("prod/kv")
	var mu sync.Mutex
	failLeft := 2
	fn := func(_ context.Context, key string) error {
		mu.Lock()
		defer mu.Unlock()
		if failLeft > 0 {
			failLeft--
			return fmt.Errorf("apiserver timeout for %s", key)
		}
		fmt.Println("converged:", key)
		return nil
	}
	Run(ctx, q, 2, fn, 50*time.Millisecond, func() []string { return []string{"prod/kv"} })
	time.Sleep(10 * time.Millisecond)
	fmt.Println("dead letters:", q.Dead())
}
