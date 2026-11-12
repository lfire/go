// 文件：internal/gateway/gateway.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Chunk struct {
	Text string
	Done bool
}

// Usage 是供应商在流末尾上报的用量，计费以它为准。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type ChatRequest struct {
	Model    string
	Messages []string
}

type Provider interface {
	Name() string
	Stream(ctx context.Context, req ChatRequest, emit func(Chunk) error) (Usage, error)
}

// Registry 按名字注册供应商，新增一家只改注册表。
type Registry struct {
	mu sync.RWMutex
	m  map[string]Provider
}

func NewRegistry(ps ...Provider) *Registry {
	m := make(map[string]Provider, len(ps))
	for _, p := range ps {
		m[p.Name()] = p
	}
	return &Registry{m: m}
}

func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.m[name]
	return p, ok
}

// bucket 是令牌桶，rate 每秒补充、cap 为突发上限，整体用互斥锁保护。
type bucket struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
	rate   float64
	cap    float64
}

func newBucket(rate, cap float64) *bucket {
	return &bucket{tokens: cap, last: time.Now(), rate: rate, cap: cap}
}

func (b *bucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.cap {
		b.tokens = b.cap
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Gateway 聚合鉴权、按 Key 限流（含优先级）、路由与成本统计。
type Gateway struct {
	reg    *Registry
	keys   map[string]*bucket
	tokens atomic.Int64 // 累计输出量估算，仅用于实时限额
}

func NewGateway(reg *Registry, normalQPS, premiumQPS float64, normalKeys, premiumKeys []string) *Gateway {
	keys := make(map[string]*bucket)
	for _, k := range normalKeys {
		keys[k] = newBucket(normalQPS, normalQPS)
	}
	for _, k := range premiumKeys {
		keys[k] = newBucket(premiumQPS, premiumQPS*2) // 高优先级更大突发
	}
	return &Gateway{reg: reg, keys: keys}
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("X-API-Key")
	b, ok := g.keys[key]
	if !ok {
		http.Error(w, "invalid api key", http.StatusUnauthorized)
		return
	}
	if !b.allow() {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	var body struct {
		Provider string   `json:"provider"`
		Model    string   `json:"model"`
		Messages []string `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p, ok := g.reg.Get(body.Provider)
	if !ok {
		http.Error(w, "unknown provider", http.StatusBadRequest)
		return
	}
	var sb strings.Builder
	usage, err := p.Stream(r.Context(), ChatRequest{Model: body.Model, Messages: body.Messages},
		func(c Chunk) error {
			sb.WriteString(c.Text)
			g.tokens.Add(int64(len([]rune(c.Text)))) // 实时估算，生产以 usage 为准
			return nil
		})
	if err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"text":%q,"prompt":%d,"completion":%d}`,
		sb.String(), usage.PromptTokens, usage.CompletionTokens)
}

// echoProvider 是本段自带的假供应商，取消会沿 ctx 传导并停止出块。
type echoProvider struct{ name string }

func (e echoProvider) Name() string { return e.name }
func (e echoProvider) Stream(ctx context.Context, _ ChatRequest, emit func(Chunk) error) (Usage, error) {
	for _, s := range []string{"你", "好"} {
		select {
		case <-ctx.Done():
			return Usage{}, ctx.Err()
		default:
		}
		if err := emit(Chunk{Text: s}); err != nil {
			return Usage{}, err
		}
	}
	_ = emit(Chunk{Done: true})
	return Usage{PromptTokens: 5, CompletionTokens: 2}, nil
}

func main() {
	reg := NewRegistry(echoProvider{name: "openai"}, echoProvider{name: "anthropic"})
	g := NewGateway(reg, 2, 10, []string{"normal"}, []string{"premium"})
	srv := httptest.NewServer(g)
	defer srv.Close()
	post := func(key, provider string) (int, string) {
		payload := `{"provider":"` + provider + `","model":"m","messages":["hi"]}`
		req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(payload))
		req.Header.Set("X-API-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		buf := make([]byte, 256)
		var sb strings.Builder
		for {
			n, e := resp.Body.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if e != nil {
				break
			}
		}
		return resp.StatusCode, sb.String()
	}
	code, body := post("premium", "openai")
	fmt.Println("ok:", code, "has text:", strings.Contains(body, "你"))
	code, _ = post("bad-key", "openai")
	fmt.Println("unauthorized:", code)
	for i := 0; i < 4; i++ {
		c, _ := post("normal", "openai")
		fmt.Println("normal try:", c)
	}
}
