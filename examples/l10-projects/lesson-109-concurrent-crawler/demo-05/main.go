package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"time"
)

var re = regexp.MustCompile(`href="(/[a-z0-9]*)"`)

// Pool 是 V2 并发版：N 个 worker 共享 jobs 与一个全局限速器。
type Pool struct {
	client  *http.Client
	base    string
	mu      sync.Mutex
	seen    map[string]bool
	jobs    chan string
	results chan string
	wg      sync.WaitGroup
	limiter <-chan time.Time
	ticker  *time.Ticker
	max     int
}

func NewPool(base string, client *http.Client, max, workers int, rate time.Duration) *Pool {
	t := time.NewTicker(rate)
	return &Pool{
		client:  client,
		base:    base,
		seen:    make(map[string]bool),
		jobs:    make(chan string, workers*4),
		results: make(chan string, workers*4),
		ticker:  t,
		limiter: t.C,
		max:     max,
	}
}

// enqueue 在派发时就把 URL 记为已调度，避免同一 URL 多次入队。
func (p *Pool) enqueue(path string) {
	p.mu.Lock()
	if p.seen[path] || len(p.seen) >= p.max {
		p.mu.Unlock()
		return
	}
	p.seen[path] = true
	p.wg.Add(1) // Add 必须在对应 Done 之前
	p.mu.Unlock()
	p.jobs <- path // 发送放在锁外
}

// handle 用 defer 保证任意路径都归还计数。
func (p *Pool) handle(ctx context.Context, path string) {
	defer p.wg.Done()
	select {
	case <-ctx.Done():
		return
	case <-p.limiter: // 所有 worker 共享一个 ticker，全局限速
	}
	resp, err := p.client.Get(p.base + path)
	if err != nil {
		return // 部分失败：本页跳过，但计数已归还
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		p.enqueue(m[1])
	}
	select {
	case p.results <- path:
	case <-ctx.Done():
	}
}

func (p *Pool) worker(ctx context.Context) {
	for path := range p.jobs {
		p.handle(ctx, path)
	}
}

func (p *Pool) Run(ctx context.Context, seed string, workers int) []string {
	p.enqueue(seed)
	for i := 0; i < workers; i++ {
		go p.worker(ctx)
	}
	go func() {
		p.wg.Wait() // 归零意味着系统 quiescent，可以关闭
		close(p.jobs)
		close(p.results)
	}()
	var visited []string
	for r := range p.results {
		visited = append(visited, r)
	}
	p.ticker.Stop()
	return visited
}

func main() {
	pages := map[string]string{
		"/":  `<a href="/a">a</a><a href="/b">b</a>`,
		"/a": `<a href="/d">d</a>`,
		"/b": `<a href="/d">d</a>`,
		"/d": `end`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, ok := pages[r.URL.Path]; ok {
			fmt.Fprint(w, b)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	p := NewPool(srv.URL, srv.Client(), 10, 4, time.Millisecond)
	visited := p.Run(context.Background(), "/", 4)
	fmt.Println("V2 抓取数:", len(visited), "（顺序不固定）")
}
