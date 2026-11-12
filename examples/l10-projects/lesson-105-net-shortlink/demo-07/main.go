package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("短链不存在")
	ErrExpired  = errors.New("短链已过期")
)

type link struct {
	target  string
	expires time.Time
}

type memStore struct {
	mu   sync.RWMutex
	data map[string]link
	next uint64
}

func (m *memStore) Create(target string, ttl time.Duration) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	code := fmt.Sprintf("%d", m.next)
	l := link{target: target}
	if ttl > 0 {
		l.expires = time.Now().Add(ttl)
	}
	m.data[code] = l
	return code
}

func (m *memStore) Get(code string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	l, ok := m.data[code]
	if !ok {
		return "", ErrNotFound
	}
	if !l.expires.IsZero() && time.Now().After(l.expires) {
		return "", ErrExpired
	}
	return l.target, nil
}

func safeTarget(raw string) (string, error) {
	if !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "http://") {
		return "", fmt.Errorf("仅支持 http 与 https 绝对地址")
	}
	return raw, nil
}

// trace 中间件为每次请求注入 trace_id，并把它写进结构化日志。
func trace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := r.Header.Get("X-Trace-Id")
		if tid == "" {
			tid = fmt.Sprintf("t%d", time.Now().UnixNano())
		}
		ctx := context.WithValue(r.Context(), "trace_id", tid)
		w.Header().Set("X-Trace-Id", tid)
		start := time.Now()
		next.ServeHTTP(w, r.WithContext(ctx))
		slog.Info("request", "trace_id", tid, "path", r.URL.Path,
			"cost_ms", time.Since(start).Milliseconds())
	})
}

func newHandler(m *memStore) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/shorten", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			URL string `json:"url"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "请求体不是合法 JSON", http.StatusBadRequest)
			return
		}
		target, err := safeTarget(req.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		code := m.Create(target, 24*time.Hour)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
			return
		}
		code := strings.TrimPrefix(r.URL.Path, "/")
		if code == "" {
			http.NotFound(w, r)
			return
		}
		target, err := m.Get(code)
		switch {
		case errors.Is(err, ErrNotFound):
			http.Error(w, "短链不存在", http.StatusNotFound)
		case errors.Is(err, ErrExpired):
			http.Error(w, "短链已过期", http.StatusGone)
		case err != nil:
			http.Error(w, "服务暂时不可用", http.StatusServiceUnavailable)
		default:
			// 302 而非 301：目标可改、点击可统计、浏览器不永久缓存旧映射。
			http.Redirect(w, r, target, http.StatusFound)
		}
	})
	return trace(mux)
}

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("监听失败:", err)
		return
	}
	m := &memStore{data: make(map[string]link)}
	srv := &http.Server{Handler: newHandler(m), ReadHeaderTimeout: 2 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	base := "http://" + ln.Addr().String()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Post(base+"/shorten", "application/json",
		strings.NewReader(`{"url":"https://go.dev/doc/"}`))
	if err != nil {
		fmt.Println("创建失败:", err)
		return
	}
	var out struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()

	r2, _ := client.Get(base + "/" + out.Code)
	fmt.Println("跳转:", r2.StatusCode, "Location=", r2.Header.Get("Location"))
	_ = r2.Body.Close()
	r3, _ := client.Get(base + "/not-exist")
	fmt.Println("不存在:", r3.StatusCode)
	_ = r3.Body.Close()

	// 优雅退出：生产中由 signal.NotifyContext 触发，这里手动演示 Shutdown。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		fmt.Println("关闭超时:", err)
	}
	fmt.Println("服务已优雅退出")
}
