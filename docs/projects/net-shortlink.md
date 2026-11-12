---
title: 第 105 讲 · 实战 03 · 短链服务
description: 用发号器与 Base62、缓存旁路、301/302 取舍、TTL 与开放重定向防护，设计高并发读低延迟跳转服务
---

<StageHeader stage="L10 · 工程实战" level="★★☆☆☆" time="55 分钟" goal="设计高并发读、低延迟跳转的服务" prereq="第 104 讲 · 实战 02 · protoc 插件与代码生成器" />

# 第 105 讲 · 实战 03 · 短链服务

> 短链读远多于写：一次创建对应上万次点击。本讲回答短码从哪来、跳转查得多快、失败会把用户带到哪里，全程标准库可运行。

## 这一讲你会得到什么
<CheckList id="l105-goals" title="本讲目标" :items='["理解发号器加 Base62 为什么比哈希更可控","画出读路径缓存旁路，说清击穿与空值缓存","分清 301 与 302 在可改目标上的代价","识别开放重定向与过期链接两类边界"]' />

## 1. 项目目标与约束

| 维度 | 目标 | 约束与取舍 |
| --- | --- | --- |
| 读写比 | 假定千比一，读优先 | 写可慢，读不能多一次往返 |
| 短码 | 短、不可枚举、稳定 | 自增 ID 加 Base62 |
| 目标可改 | 运营要能换落地页 | 不能用被永久缓存的 301 |
| 过期 | 支持 TTL | 404 与 410 语义不同 |
| 安全 | 不接受任意目标 | 必须做 scheme 白名单 |

<Callout type="tip" title="为什么不用哈希短码" text="哈希要处理冲突且不利扩容；自增 ID 加 Base62 把唯一性与短编码拆成两件事。" />

最易被忽略的是目标可改：营销短链今天指向 A、明天指向 B，一旦用了 301，浏览器永久缓存旧映射，改了后端用户也回不来。

## 2. 架构蓝图

<ConceptDiagram type="cache-aside" title="短链读路径：缓存旁路" caption="读先查缓存，未命中才回源并回填；写只落库并让缓存失效，避免双写不一致" />

读写路径不对称：写只发号、编码、落库，不预热缓存以免留下幽灵数据；读未命中才回源回填，缓存崩了只影响延迟。发号器重启必须从持久化恢复游标，否则从头发号会覆盖历史。

## 3. 关键模块实现

### 3.1 发号与 Base62：可验证的短码

```go
package main

import (
	"fmt"
	"strings"
	"sync"
)

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// Encode62 把非负整数转成 Base62；0 单独处理，避免返回空串。
func Encode62(n uint64) string {
	if n == 0 {
		return string(alphabet[0])
	}
	var sb strings.Builder
	for n > 0 {
		sb.WriteByte(alphabet[n%62])
		n /= 62
	}
	// 低位先算出，反转后才是高位在前。
	b := []byte(sb.String())
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

func Decode62(s string) (uint64, bool) {
	var n uint64
	for i := 0; i < len(s); i++ {
		idx := strings.IndexByte(alphabet, s[i])
		if idx < 0 {
			return 0, false // 出现字母表外的字符，判定为非法短码
		}
		n = n*62 + uint64(idx)
	}
	return n, true
}

// Allocator 用互斥锁保护自增发号，进程内严格不重复。
type Allocator struct {
	mu  sync.Mutex
	cur uint64
}

func (a *Allocator) Next() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cur++
	return a.cur
}

func main() {
	a := &Allocator{}
	for i := 0; i < 3; i++ {
		id := a.Next()
		code := Encode62(id)
		back, ok := Decode62(code)
		fmt.Printf("id=%d code=%s 回解=%d 一致=%v\n", id, code, back, ok && back == id)
	}
	_, bad := Decode62("a!b")
	fmt.Println("含非法字符:", bad)
}
```

短码来自 URL 路径，是完全不受信任的输入；先做字符白名单再查存储能挡掉大量扫描，编解码互为逆运算最适合表驱动测试。

### 3.2 带 TTL 的存储：把过期做成一等公民

```go
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrExpired = errors.New("短链已过期")

type Entry struct {
	URL     string
	Expires time.Time // 零值表示永不过期
}

type Store struct {
	mu   sync.RWMutex
	data map[string]Entry
}

func NewStore() *Store {
	return &Store{data: make(map[string]Entry)}
}

func (s *Store) Put(code, url string, ttl time.Duration) {
	e := Entry{URL: url}
	if ttl > 0 {
		e.Expires = time.Now().Add(ttl)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[code] = e
}

// Get 用读锁；过期判断放在锁内，避免返回一条本该失效的数据。
func (s *Store) Get(code string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.data[code]
	if !ok {
		return "", errors.New("短链不存在")
	}
	if !e.Expires.IsZero() && time.Now().After(e.Expires) {
		return "", ErrExpired
	}
	return e.URL, nil
}

func main() {
	s := NewStore()
	s.Put("aB3", "https://example.com/a", 50*time.Millisecond)
	u, err := s.Get("aB3")
	fmt.Println("立即读:", u, err)
	time.Sleep(60 * time.Millisecond)
	u, err = s.Get("aB3")
	fmt.Println("过期后:", u, err, "判定=", errors.Is(err, ErrExpired))
}
```

用 RWMutex 是因为读占绝对多数，读锁之间不互斥。过期判断放在锁内而非返回后再判，避免刚读出就被别的 goroutine 判定过期的时序缝隙。生产通常惰性判断加后台清理，不做全表扫描。

### 3.3 HTTP 层：302、410 与开放重定向

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

// memStore 是本文件自带的最小实现，便于独立编译运行。
type memStore struct {
	mu   sync.Mutex
	data map[string]link
	next uint64
}

type link struct {
	target  string
	expires time.Time
}

func (m *memStore) Create(target string, ttl time.Duration) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	code := fmt.Sprintf("%d", m.next) // 演示用；生产换成 Base62
	m.data[code] = link{target: target, expires: time.Now().Add(ttl)}
	return code
}

func (m *memStore) Get(code string) (string, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.data[code]
	if !ok {
		return "", http.StatusNotFound
	}
	if !l.expires.IsZero() && time.Now().After(l.expires) {
		return "", http.StatusGone
	}
	return l.target, http.StatusOK
}

// safeTarget 只放行 http/https，堵住 javascript 与协议相对地址造成的开放重定向。
func safeTarget(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("非法目标地址")
	}
	return u.String(), nil
}

func newHandler(m *memStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/shorten":
			var req struct {
				URL string `json:"url"`
			}
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err := json.Unmarshal(body, &req); err != nil {
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
			fmt.Fprintf(w, `{"code":"%s"}`, code)
		case r.Method == http.MethodGet:
			code := strings.TrimPrefix(r.URL.Path, "/")
			if code == "" {
				http.NotFound(w, r)
				return
			}
			target, status := m.Get(code)
			if status != http.StatusOK {
				http.Error(w, http.StatusText(status), status)
				return
			}
			// 302 而非 301：目标可改、点击可统计，浏览器不永久缓存旧映射。
			http.Redirect(w, r, target, http.StatusFound)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		}
	})
}

func main() {
	m := &memStore{data: make(map[string]link)}
	srv := httptest.NewServer(newHandler(m))
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/shorten", "application/json",
		strings.NewReader(`{"url":"https://go.dev/doc/"}`))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Println("创建:", resp.StatusCode, string(body))

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	var out struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &out)
	r2, _ := client.Get(srv.URL + "/" + out.Code)
	fmt.Println("跳转:", r2.StatusCode, r2.Header.Get("Location"))
	r2.Body.Close()

	// 直接验证过期分支：TTL 极短，稍等后应得到 410。
	c2 := m.Create("https://example.com", time.Millisecond)
	time.Sleep(2 * time.Millisecond)
	_, status := m.Get(c2)
	fmt.Println("过期状态:", status)

	_, err := safeTarget("javascript:alert(1)")
	fmt.Println("危险目标:", err)
}
```

safeTarget 是安全底线：只按看起来像 URL 放行，javascript 或协议相对地址会让域名变成别人的钓鱼跳板。410 与 404 分开，因为过期与不存在含义不同。

## 4. 失败路径与边界条件

短码冲突靠发号器唯一性；热点过期靠空值缓存或互斥回源；不存在的码靠布隆过滤；危险协议在写路径拒绝；过期返回 410；发号器重启游标归零会覆盖历史。最贵的一个错是 301：浏览器永久缓存，改目标与回收短码全部失效。短码也要限制字符集与长度。

## 5. 测试与压测思路

编解码用表驱动测试覆盖零值、边界值与非法字符；HTTP 层用 httptest 断言创建、跳转、404、410、405 与危险目标被拒，客户端须设置 CheckRedirect 才能看到 302 与 Location；读压测只打 GET，观察 p99、命中率与 RWMutex 竞争，目标是命中率与 p99 而非 QPS 绝对值。

压测要模拟真实读写比：预热后命中率接近满分、p99 稳定在亚毫秒级；再逐步降低命中率观察回源放大系数。

## 6. 可继续扩展的方向

接 Redis 做 cache-aside 并补空值缓存；发号器换成号段或雪花算法支持多实例，见第 86 讲；跳转事件进队列再聚合，正是第 107 讲 ETL 的输入。

## 随堂检测

<Quiz no="105-1" q="短链通常选 302 而不是 301，为什么？" :options='["302 更省带宽","302 每次回源，目标可修改且点击可统计","301 不被浏览器支持"]' answer="1" explain="301 会被浏览器长期缓存，服务端改映射无法生效；302 保留每次回源。" />

## 一页纸速记

<KnowledgeCard :points='["自增 ID 加 Base62 把唯一性与短编码拆成两件事","读路径 cache-aside：命中直接 302，未命中回源回填","302 保留可改目标与点击统计，301 几乎不可逆"]' :pitfalls='["只做字符串校验会留下开放重定向","发号器重启游标归零会覆盖历史短码"]' summary="短链的设计重心是把读路径做薄、把失败路径想全。" />

## 小结与下一讲

- 发号器负责唯一，Base62 负责短，解耦后可独立测试与扩容。
- cache-aside 让缓存崩了只影响延迟；302 与 410 守住可改性与过期语义。
- scheme 白名单、限流与短码字符集校验是公网暴露前的必备边界。

[下一讲：实战 04 · 图片服务与对象存储](/projects/net-image-server)
