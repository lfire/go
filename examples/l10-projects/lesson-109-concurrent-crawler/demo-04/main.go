package main

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RobotRules 保存单 host 的禁止前缀；生产需完整解析 Allow/Disallow。
type RobotRules struct {
	disallow []string
	delay    time.Duration
}

func (r RobotRules) Allowed(path string) bool {
	for _, p := range r.disallow {
		if strings.HasPrefix(path, p) {
			return false
		}
	}
	return true
}

// TokenBucket 是每域名独立限速：容量等于每秒补充速率。
type TokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	rate     float64
	last     time.Time
}

func NewTokenBucket(rate float64) *TokenBucket {
	return &TokenBucket{tokens: rate, capacity: rate, rate: rate, last: time.Now()}
}

// Wait 阻塞到拿到一枚令牌，等待放在锁外循环里。
func (b *TokenBucket) Wait() time.Duration {
	for {
		b.mu.Lock()
		now := time.Now()
		b.tokens = min(b.capacity, b.tokens+now.Sub(b.last).Seconds()*b.rate)
		b.last = now
		if b.tokens >= 1 {
			b.tokens--
			b.mu.Unlock()
			return 0
		}
		wait := time.Duration((1 - b.tokens) / b.rate * float64(time.Second))
		b.mu.Unlock()
		time.Sleep(wait)
	}
}

// HostLimiter 按 host 维护令牌桶与 robots 规则。
type HostLimiter struct {
	mu      sync.Mutex
	buckets map[string]*TokenBucket
	rules   map[string]RobotRules
	qps     float64
}

func NewHostLimiter(qps float64) *HostLimiter {
	return &HostLimiter{buckets: map[string]*TokenBucket{}, rules: map[string]RobotRules{}, qps: qps}
}

func (h *HostLimiter) SetRules(host string, r RobotRules) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rules[host] = r
}

func (h *HostLimiter) Acquire(host, path string) bool {
	h.mu.Lock()
	if r, ok := h.rules[host]; ok && !r.Allowed(path) {
		h.mu.Unlock()
		return false // 被 robots 禁止
	}
	b, ok := h.buckets[host]
	if !ok {
		b = NewTokenBucket(h.qps)
		h.buckets[host] = b
	}
	h.mu.Unlock()
	b.Wait() // 锁外等待，不阻塞其它 host
	return true
}

// BuildClient 配置连接池与 DNS，给整条链路设超时边界。
func BuildClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   64,
		MaxConnsPerHost:       64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

func main() {
	limiter := NewHostLimiter(5)
	limiter.SetRules("shop.example", RobotRules{disallow: []string{"/admin", "/cart"}, delay: time.Second})
	client := BuildClient(8 * time.Second)
	fmt.Println("单请求超时:", client.Timeout)
	for _, path := range []string{"/item/1", "/admin/secret", "/item/2"} {
		ok := limiter.Acquire("shop.example", path)
		fmt.Printf("path=%s 允许=%v\n", path, ok)
	}
}
