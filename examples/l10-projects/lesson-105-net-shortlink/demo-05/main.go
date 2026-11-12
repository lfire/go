package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

var ErrNotFound = errors.New("短链不存在")

type call struct {
	wg  sync.WaitGroup
	val string
	err error
}

type group struct {
	mu sync.Mutex
	m  map[string]*call
}

func (g *group) Do(key string, fn func() (string, error)) (string, error) {
	g.mu.Lock()
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &call{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()
	c.val, c.err = fn()
	c.wg.Done()
	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
	return c.val, c.err
}

type item struct {
	val string
	exp time.Time
	nil bool // 空值盾标记
}

type Resolver struct {
	mu      sync.RWMutex
	cache   map[string]item
	group   group
	origin  func(context.Context, string) (string, error)
	srcHits atomic.Int64
}

func NewResolver(origin func(context.Context, string) (string, error)) *Resolver {
	return &Resolver{
		cache:  make(map[string]item),
		group:  group{m: make(map[string]*call)},
		origin: origin,
	}
}

func (r *Resolver) get(key string) (item, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	it, ok := r.cache[key]
	if !ok || time.Now().After(it.exp) {
		return item{}, false
	}
	return it, true
}

func (r *Resolver) set(key, val string, ttl time.Duration, isNil bool) {
	// TTL 抖动抑制空值盾与热点同时过期造成的雪崩。
	jitter := time.Duration(rand.Int63n(int64(ttl / 5)))
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[key] = item{val: val, exp: time.Now().Add(ttl + jitter), nil: isNil}
}

// Resolve 先查缓存，未命中用 singleflight 合并回源，空结果写空值盾。
func (r *Resolver) Resolve(ctx context.Context, code string) (string, error) {
	if it, ok := r.get(code); ok {
		if it.nil {
			return "", ErrNotFound
		}
		return it.val, nil
	}
	val, err := r.group.Do(code, func() (string, error) {
		// context 超时沿回源链路传播，超时不重试。
		qctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		v, e := r.origin(qctx, code)
		r.srcHits.Add(1)
		if errors.Is(e, ErrNotFound) {
			r.set(code, "", 60*time.Second, true)
			return "", ErrNotFound
		}
		if e != nil {
			return "", e
		}
		r.set(code, v, 5*time.Minute, false)
		return v, nil
	})
	return val, err
}

func main() {
	origin := func(ctx context.Context, code string) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Millisecond):
			if code == "known" {
				return "https://go.dev", nil
			}
			return "", ErrNotFound
		}
	}
	r := NewResolver(origin)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Resolve(context.Background(), "ghost")
		}()
	}
	wg.Wait()
	fmt.Println("百并发查同一不存在短码, 真实回源次数:", r.srcHits.Load())
	v, err := r.Resolve(context.Background(), "known")
	fmt.Println("命中已存在短码:", v, err)
	_, err = r.Resolve(context.Background(), "ghost")
	fmt.Println("再次查不存在短码命中空值盾:", errors.Is(err, ErrNotFound))
}
