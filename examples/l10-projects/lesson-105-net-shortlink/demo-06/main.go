package main

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	mu     sync.Mutex
	tokens float64
	rate   float64
	burst  float64
	last   time.Time
}

func newBucket(rate, burst float64) *bucket {
	return &bucket{tokens: burst, rate: rate, burst: burst, last: time.Now()}
}

func (b *bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type Guard struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	allowHost map[string]bool
	denyHost  []string
}

func NewGuard() *Guard {
	return &Guard{
		buckets:   make(map[string]*bucket),
		allowHost: map[string]bool{"go.dev": true, "example.com": true},
		denyHost:  []string{"evil.", "phishing."},
	}
}

func (g *Guard) Allow(client string) bool {
	g.mu.Lock()
	b, ok := g.buckets[client]
	if !ok {
		b = newBucket(5, 10) // 每秒 5 次，突发 10 次
		g.buckets[client] = b
	}
	g.mu.Unlock()
	return b.Allow()
}

// SafeTarget 只放行白名单内的 http/https 绝对地址，堵住开放重定向。
func (g *Guard) SafeTarget(raw string) (string, error) {
	if len(raw) > 2048 {
		return "", fmt.Errorf("目标地址超长")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("目标地址无法解析")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("仅支持 http 与 https")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("目标地址缺少域名")
	}
	for _, p := range g.denyHost {
		if strings.HasPrefix(host, p) {
			return "", fmt.Errorf("目标域名在黑名单内")
		}
	}
	if !g.allowHost[host] {
		return "", fmt.Errorf("目标域名不在白名单内")
	}
	return u.String(), nil
}

func main() {
	g := NewGuard()
	pass := 0
	for i := 0; i < 15; i++ {
		if g.Allow("1.2.3.4") {
			pass++
		}
	}
	fmt.Println("同客户端 15 次请求放行:", pass)
	for _, raw := range []string{
		"https://go.dev/doc/",
		"javascript:alert(1)",
		"//evil.example.com/x",
		"https://phishing.example.com/x",
		"https://unknown.com/x",
	} {
		out, err := g.SafeTarget(raw)
		fmt.Printf("目标=%s 结果=%s 错误=%v\n", raw, out, err)
	}
}
