---
title: 第 70 讲 · 认证授权：Session / JWT / OAuth2 / RBAC
description: 讲清认证与授权分离、Session 与 JWT 的取舍、令牌刷新与吊销难题、OAuth2 流程与 RBAC 权限模型，并点名常见安全漏洞
---

<StageHeader stage="L6 · 服务端与数据" level="★★★★☆" time="50 分钟" goal="设计安全且可演进的身份体系" prereq="第 69 讲 · Elasticsearch" />

# 第 70 讲 · 认证授权：Session / JWT / OAuth2 / RBAC

> 前面所有接口都默认「谁来都能调」。真实服务必须先回答两个不同的问题：**你是谁**（认证，Authentication）和**你能做什么**（授权，Authorization）。把这两件事混在一层里，是权限系统腐化的起点。这一讲先用标准库写一套 Session 服务，再手写一个 JWT 把「无状态」的代价摊开看，最后直面那个绕不过去的难题——签发出去的令牌怎么吊销，以及 OAuth2 与 RBAC 各自解决哪一段问题。

## 这一讲你会得到什么

<CheckList id="70-goal" title="本讲目标" :items='["能分清认证与授权，会用中间件把两层分开","理解 Session 与 JWT 的状态归属差异及各自适用场景","能签发并校验 JWT，说明刷新轮换与吊销为什么互相矛盾","了解 OAuth2 授权码流程、RBAC 模型与常见漏洞的防护"]' />

## 1. 先建立直觉

认证像前台查身份证，授权像门禁卡决定你能进哪几间房。查身份证只做一次，门禁判断却要在每个受保护资源前重复，所以代码上天然分层：认证中间件在外层确认身份，授权逻辑贴近具体 Handler。HTTP 也因此给了两个不同状态码：**401** 表示「没带或带了无效身份」，**403** 表示「身份有效但权限不够」，混用会让客户端无法决定该重新登录还是提示无权限。

Session 与 JWT 的根本差别只有一句话：**状态放在哪**。Session 把状态留在服务端，客户端只拿一个随机 ID；JWT 把状态签进令牌里，服务端不存。前者「想踢谁就踢谁」，后者「横向扩展不用共享存储」——而一旦业务要求「退出登录立刻全局失效」，无状态方案就不得不把状态请回来。

<ConceptDiagram type="middleware-onion" title="认证与授权是包在业务 Handler 外面的两层" caption="外层认证确认身份，内层授权判断角色，最内层才执行业务逻辑；任何一层失败都提前返回" />

## 2. 语法与规则

第一段代码用零依赖的标准库实现完整 Session 服务：登录签发、HttpOnly Cookie、认证与授权两层中间件、登出销毁。

```go
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

type Session struct {
	UserID string
	Role   string
	Expiry time.Time
}

type Store struct {
	mu   sync.RWMutex
	data map[string]Session
}

func (s *Store) Create(userID, role string, ttl time.Duration) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf) // 用 CSPRNG，绝不用 math/rand 生成令牌
	sid := hex.EncodeToString(buf)
	s.mu.Lock()
	s.data[sid] = Session{
		UserID: userID, Role: role, Expiry: time.Now().Add(ttl),
	}
	s.mu.Unlock()
	return sid
}

func (s *Store) Get(sid string) (Session, bool) {
	s.mu.RLock()
	v, ok := s.data[sid]
	s.mu.RUnlock()
	if !ok || time.Now().After(v.Expiry) {
		return Session{}, false
	}
	return v, true
}

func (s *Store) Delete(sid string) {
	s.mu.Lock()
	delete(s.data, sid)
	s.mu.Unlock()
}

var store = &Store{data: make(map[string]Session)}

const cookieName = "sid"

type ctxKey string

const userKey ctxKey = "user"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", login)
	mux.HandleFunc("POST /logout", logout)
	mux.HandleFunc("GET /me", requireAuth(me))
	mux.HandleFunc("GET /admin",
		requireAuth(requireRole("admin", admin)))
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User string `json:"user"`
		Pass string `json:"pass"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	// 演示用；生产应查库并用 bcrypt 比对密码哈希。
	if req.User != "gopher" || req.Pass != "secret" {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	sid := store.Create("u-1", "admin", 30*time.Minute)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,                 // JS 读不到，缓解 XSS 窃取
		Secure:   r.TLS != nil,         // 生产 HTTPS 下必须为 true
		SameSite: http.SameSiteLaxMode, // 缓解 CSRF
		Expires:  time.Now().Add(30 * time.Minute),
	})
	w.WriteHeader(http.StatusNoContent)
}

func logout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(cookieName)
	if err == nil {
		store.Delete(c.Value) // 服务端删状态，令牌立即失效
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true,
	})
	w.WriteHeader(http.StatusNoContent)
}

// requireAuth 是认证：确认「你是谁」。
func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		sess, ok := store.Get(c.Value)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// 身份只放进本次请求作用域的 context，随请求结束而消失。
		ctx := context.WithValue(r.Context(), userKey, sess)
		next(w, r.WithContext(ctx))
	}
}

// requireRole 是授权：确认「你能做什么」。
func requireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := r.Context().Value(userKey).(Session)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if sess.Role != role {
			http.Error(w, "forbidden", http.StatusForbidden) // 403 不是 401
			return
		}
		next(w, r)
	}
}

func me(w http.ResponseWriter, r *http.Request) {
	sess := r.Context().Value(userKey).(Session)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"user_id": sess.UserID, "role": sess.Role,
	})
}

func admin(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("admin area"))
}
```

<OutputBox title="Session 的关键行为" output="POST /login 返回 204，并下发 HttpOnly 的 sid Cookie\nGET /me 带上 Cookie 返回用户身份\nGET /admin 角色不是 admin 时返回 403\nPOST /logout 后服务端删除会话，旧 sid 立刻失效" note="退出登录之所以能即时生效，是因为状态始终在服务端；这正是 Session 相对 JWT 的核心优势" />

<LineByLine code="parts := strings.Split(token, dot)\nif len(parts) != 3 {\nreturn err\n}\ncheckAlg(parts[0])\nverifySig(parts[0], parts[1], parts[2])\nclaims := decode(parts[1])\ncheckExp(claims)\nreturn claims" :notes='["JWT 由头、载荷、签名三段组成，用点号分隔","段数不对直接拒绝，不要尝试容错","固定校验 alg 为 HS256，防止算法混淆攻击","用恒定时间比较重算签名与携带签名","载荷只是 base64 编码，不是加密，可明文读出","必须校验 exp，否则过期令牌被永久接受","还可以校验 iss、aud 与黑名单 jti"]' title="JWT 校验逐行看" />

第二段代码手写一个 HMAC-SHA256 的 JWT，并搭出完整服务：登录签发短效 access 与长效 refresh、鉴权中间件、角色授权。

```go
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

var secret = []byte("replace-with-a-long-random-secret")

type Claims struct {
	Sub  string `json:"sub"`
	Role string `json:"role"`
	JTI  string `json:"jti"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

func sign(head, payload string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(head + "." + payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func Issue(c Claims) (string, error) {
	headBytes, _ := json.Marshal(map[string]string{
		"alg": "HS256", "typ": "JWT",
	})
	bodyBytes, _ := json.Marshal(c)
	head := base64.RawURLEncoding.EncodeToString(headBytes)
	body := base64.RawURLEncoding.EncodeToString(bodyBytes)
	return head + "." + body + "." + sign(head, body), nil
}

func Verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("malformed token")
	}
	// 必须固定算法：否则攻击者可把 alg 改成 none 绕过签名。
	headBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, err
	}
	var head struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headBytes, &head); err != nil {
		return Claims{}, err
	}
	if head.Alg != "HS256" {
		return Claims{}, fmt.Errorf("unexpected alg")
	}
	// hmac.Equal 是恒定时间比较，避免时序侧信道。
	want := sign(parts[0], parts[1])
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return Claims{}, fmt.Errorf("bad signature")
	}
	bodyBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, err
	}
	var c Claims
	if err := json.Unmarshal(bodyBytes, &c); err != nil {
		return Claims{}, err
	}
	if time.Now().Unix() > c.Exp {
		return Claims{}, fmt.Errorf("token expired")
	}
	return c, nil
}

type ctxKey string

const claimsKey ctxKey = "claims"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", login)
	mux.HandleFunc("GET /me", authMiddleware(me))
	mux.HandleFunc("GET /admin",
		authMiddleware(requireRole("admin", admin)))
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func login(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	jti := fmt.Sprint(now.UnixNano())
	access, _ := Issue(Claims{
		Sub: "u-1", Role: "admin", JTI: "a-" + jti,
		Iat: now.Unix(), Exp: now.Add(15 * time.Minute).Unix(),
	})
	refresh, _ := Issue(Claims{
		Sub: "u-1", JTI: "r-" + jti,
		Iat: now.Unix(), Exp: now.Add(7 * 24 * time.Hour).Unix(),
	})
	_ = json.NewEncoder(w).Encode(map[string]string{
		"access_token": access, "refresh_token": refresh,
	})
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		token := strings.TrimPrefix(h, "Bearer ")
		if token == h { // 没有 Bearer 前缀
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		c, err := Verify(token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, c)
		next(w, r.WithContext(ctx))
	}
}

func requireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := r.Context().Value(claimsKey).(Claims)
		if !ok || c.Role != role {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func me(w http.ResponseWriter, r *http.Request) {
	c := r.Context().Value(claimsKey).(Claims)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"sub": c.Sub, "role": c.Role,
	})
}

func admin(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("admin area"))
}
```

这里有几个必须守住的点：签名密钥要足够长且从配置注入，不能硬编码；`alg` 必须白名单校验，不能信任令牌头自己声明的算法；`exp` 必须校验；载荷只是 base64 编码而非加密，**绝不能放密码、身份证号等敏感信息**。

## 3. 深入一步

<Term word="RBAC" text="基于角色的访问控制：用户关联角色、角色关联权限，权限判断只认角色；比给用户逐条配权限更易维护，可再加资源归属做细粒度校验" />

| 维度 | Session | JWT |
| --- | --- | --- |
| 状态位置 | 服务端存储 | 令牌自包含 |
| 即时吊销 | 删除会话即可 | 难，需要黑名单或版本号 |
| 横向扩展 | 需共享存储或粘性会话 | 天然无状态 |
| 每次请求开销 | 一次存储查询 | 一次签名校验 |
| 令牌体积 | 极小随机 ID | 较大，含声明 |
| 适合场景 | 后台管理、需强管控 | 微服务、多端、第三方 API |

**吊销难题**值得单独说透：JWT 的全部卖点是服务端不存状态，可「退出登录、改密码、封禁账号」都要求让已签发的令牌立刻失效，这本身就是要求状态。三条出路各有代价：把 access 有效期压到很短（如 15 分钟），用 refresh 令牌续期，把「不可吊销窗口」缩到可接受；维护 jti 黑名单或用户令牌版本号，校验时查一次存储——此时 JWT 已经不再无状态；或直接改用 Session。所以准确的说法是：**JWT 不是不能吊销，而是吊销会让它失去无状态这个优势**，选型时要按业务能否容忍分钟级失效窗口来定。

| 漏洞 | 成因 | 防护 |
| --- | --- | --- |
| 算法混淆 | 信任令牌头里的 alg，接受 none | 服务端固定算法白名单 |
| XSS 窃取令牌 | 把 JWT 放 localStorage | 优先 HttpOnly Cookie，配合 CSP |
| CSRF | Cookie 自动携带 | SameSite、CSRF Token |
| 重放 | 令牌被截获后重复使用 | 短有效期、jti、TLS |
| 越权 | 只认证不校验资源归属 | 授权层校验角色与归属 |

<Callout type="warn" title="反模式：把 context 存进结构体" text="认证中间件的常见写法是把用户身份放进 r.Context()，这没问题；错的是把 context.Context 或请求作用域数据提升为结构体字段长期持有，例如定义带 ctx 字段的 Service 并在构造时赋值。context 应作为函数第一个参数显式传递，只在单次请求作用域内有效；存进结构体后取消信号失效、生命周期错乱，还让并发复用变得危险。前几讲反复强调的这条规则，在身份体系里最容易破防。" />

<Callout type="idea" title="OAuth2 与 OIDC" text="OAuth2 是授权框架，解决「第三方应用在不拿到你密码的前提下获得有限访问权限」；授权码流程是：重定向到授权服务器、用户登录并同意、回调带一次性 code、服务端用 code 加 client_secret 换 access_token、再凭 token 取资源。OIDC 在 OAuth2 之上加了 ID Token 补上身份认证。自己实现登录用 Session 或 JWT 即可，接第三方登录才需要 OAuth2，别把它当登录方案滥用。" />

第三段代码演示吊销与刷新轮换：黑名单、过期清理、以及 refresh 重放检测。

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Token struct {
	JTI string
	Exp time.Time
}

type Revoker struct {
	mu      sync.Mutex
	blocked map[string]time.Time // jti -> 该令牌本身的过期时间
	used    map[string]bool      // 已使用过的 refresh jti
}

func (r *Revoker) Revoke(t Token) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocked[t.JTI] = t.Exp
	fmt.Println("加入黑名单:", t.JTI)
}

func (r *Revoker) IsRevoked(jti string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	exp, ok := r.blocked[jti]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(r.blocked, jti) // 令牌本就过期了，黑名单项无需保留
		return false
	}
	return true
}

// Rotate 刷新令牌轮换：旧的 refresh 立即作废，返回新的一对令牌。
func (r *Revoker) Rotate(oldJTI string) (Token, Token) {
	r.mu.Lock()
	if r.used[oldJTI] {
		// 同一个 refresh 被用第二次，极可能是泄露，应撤销整条令牌链。
		fmt.Println("检测到 refresh 重放，撤销该用户全部令牌")
	}
	r.used[oldJTI] = true
	r.mu.Unlock()

	now := time.Now()
	suffix := fmt.Sprint(now.UnixNano())
	return Token{JTI: "access-" + suffix,
		Exp: now.Add(15 * time.Minute)},
		Token{JTI: "refresh-" + suffix,
			Exp: now.Add(7 * 24 * time.Hour)}
}

func main() {
	r := &Revoker{
		blocked: map[string]time.Time{}, used: map[string]bool{},
	}
	now := time.Now()
	access := Token{JTI: "access-1", Exp: now.Add(15 * time.Minute)}
	refresh := Token{JTI: "refresh-1", Exp: now.Add(7 * 24 * time.Hour)}

	fmt.Println("吊销前是否可用:", !r.IsRevoked(access.JTI))
	r.Revoke(access) // 退出登录：把 access 的 jti 加入黑名单
	fmt.Println("吊销后是否可用:", !r.IsRevoked(access.JTI))

	a2, f2 := r.Rotate(refresh.JTI)
	fmt.Println("轮换得到新 access 与新 refresh:", a2.JTI != "", f2.JTI != "")
	r.Rotate(refresh.JTI) // 再次使用旧 refresh，触发重放检测
}
```

黑名单的关键优化是**只保留到令牌自身过期为止**：令牌过期后校验本来就会拒绝，再留着黑名单项纯属浪费内存。生产里这份状态通常放 Redis 并设相同 TTL。刷新轮换则同时解决「refresh 长期有效太危险」和「泄露检测」：每次刷新都换发新 refresh，旧的一旦被二次使用，就说明可能已经泄露，应撤销该用户整条令牌链。

### 协议与契约：令牌结构、刷新与吊销的推导

**JWT 的三段结构决定了它的安全边界。** 一个 JWT 是 `base64url(header).base64url(payload).base64url(signature)`：header 声明 `alg` 与 `typ`，payload 承载 `sub`、`exp`、`iat`、`jti`、`iss`、`aud` 等声明，signature 用 HMAC 或 RSA 对前两段签名。最关键的一句推导是：base64url 只是**编码，不是加密**，payload 任何人都能解开读明文，所以绝不能放密码、身份证号、手机号；要保密就得用 JWE 或干脆只放一个无意义的 ID。验签顺序也有讲究：先校验段数为 3，再**固定算法白名单**（否则攻击者把 `alg` 改成 `none` 绕过签名，或把 RS256 混淆成 HS256 用公钥当密钥伪造），再用 `hmac.Equal` 做恒定时间比较（防时序侧信道），最后才解码 payload 并校验 `exp`、`nbf`、`iss`、`aud` 与 `jti` 黑名单。顺序反了会给未验签的数据白白做解析与存储查询。

**Cookie 属性不是可选项，每一条都对应一种攻击。** `HttpOnly` 让 JS 读不到 Cookie，缓解 XSS 窃取；`Secure` 保证只在 HTTPS 上发送，防明文链路嗅探；`SameSite=Lax/Strict` 阻止跨站请求自动携带 Cookie，缓解 CSRF；`Path` 与 `Domain` 限定作用域；`Expires` / `MaxAge` 控制寿命。会话 ID 必须用 `crypto/rand` 生成，因为它来自 CSPRNG、不可预测；`math/rand` 是确定性伪随机数，种子可由时间推断，攻击者能推算后续 ID 进而劫持会话。HTTP 也把认证与授权分成两个状态码：401 表示身份缺失或无效（去登录），403 表示身份有效但权限不足（提示无权限），混用会让客户端无法决定下一步。

**刷新与吊销的矛盾可以从状态归属直接推出。** JWT 的全部卖点是服务端不存状态；可「退出登录、改密码、封禁账号」都要求让已签发令牌立刻失效，这本身就是在要求状态。三条出路各有代价：把 access 压到很短（如 15 分钟）并用 refresh 续期，把「不可吊销窗口」缩到可接受；维护 jti 黑名单或用户令牌版本号，校验时查一次存储——此时 JWT 已不再无状态；或直接改用 Session。所以准确说法是「JWT 不是不能吊销，而是吊销会让它失去无状态这个优势」。黑名单还有一条可推导的优化：只保留到令牌自身过期为止，过期后校验本来就会拒绝，再留着纯属浪费内存，因此 TTL 设为令牌剩余存活时间。刷新轮换进一步用「旧 refresh 一旦被二次使用就撤销整条令牌链」来检测泄露。

**OAuth2 为什么用一次性 code 而不是直接给 token。** 授权码流程里，浏览器只拿到一个一次性 `code`，由应用后端用 `code` 加 `client_secret` 换取 access_token；这样令牌不出现在浏览器地址栏、Referer 与前端代码里，且能验证是哪一个客户端在换。PKCE 再补上「code 被截获也无法换取令牌」的证明。OAuth2 本质是**授权框架**（第三方应用在不拿密码的前提下获得有限权限），不是认证协议；OIDC 在其上用 ID Token 补上身份认证。自己实现登录用 Session 或 JWT 即可，接第三方登录才需要 OAuth2。

### 设计取舍与替代方案

| 决策点 | 方案 A | 方案 B（视场景） | 方案 A 的代价 |
| --- | --- | --- | --- |
| 身份方案 | 服务端 Session | 短效 JWT 加 refresh 轮换 | Session 需共享存储或粘性会话，扩展稍麻烦 |
| 吊销能力 | 无状态 JWT 不做黑名单 | 短 access 加黑名单或令牌版本号 | 前者改密码后旧令牌最长仍可用到过期 |
| 令牌存放 | localStorage | HttpOnly 加 Secure 加 SameSite Cookie | 前者 XSS 可直接读到令牌 |
| 会话 ID | `math/rand` | `crypto/rand` | 前者可预测，存在会话劫持风险 |
| 密码存储 | MD5 / 明文 | bcrypt / argon2 加盐 | 彩虹表与撞库直接破解 |
| 数据级权限 | 给每个资源造角色 | 授权层校验资源归属，或 ABAC | 角色爆炸，模型错配 |

### 失败矩阵

| 失败场景 | 触发条件 | 可观测现象 | 定位手段 | 对策 |
| --- | --- | --- | --- | --- |
| 超时 | 黑名单存储查询慢 | 鉴权延迟毛刺 | 鉴权耗时分布 | 本地缓存加短 TTL |
| 重试放大 | refresh 并发到期触发刷新风暴 | 认证服务 QPS 尖峰 | refresh 请求曲线 | 单飞、抖动、错峰过期 |
| 幂等缺失 | 同一 refresh 被并发使用 | 换发多对令牌、误判泄露 | refresh 重放计数 | 轮换加原子标记 |
| 缓存与 DB 不一致 | 黑名单只写了一个实例 | 部分节点仍接受已吊销令牌 | 各实例黑名单比对 | 集中存 Redis 并设相同 TTL |
| 连接池耗尽 | 每次请求都查黑名单且无缓存 | 存储等待队列上涨 | 存储 `WaitCount` | 短 access 加本地缓存 |
| 慢查询 | 黑名单表无索引 | 鉴权变慢 | `EXPLAIN` | 按 jti 建索引 |
| 消息重复 | 吊销事件重投 | 重复写黑名单无业务损害 | 事件 ID 去重 | 写操作幂等 |
| 算法混淆 | 信任令牌头的 alg | 伪造令牌被接受 | 审计日志里的 alg 值 | 服务端固定算法白名单 |
| 越权 | 只认证不校验资源归属 | 用户能读他人订单 | 归属断言日志 | 授权层校验归属 |

<SequenceDiagram title="登录、刷新轮换与吊销" :actors='["Client","Auth","TokenStore","Redis","Resource"]' :steps='[{"from":"Client","to":"Auth","label":"登录","note":"提交凭据","kind":"call"},{"from":"Auth","to":"TokenStore","label":"校验密码","note":"bcrypt 比对","kind":"call"},{"from":"TokenStore","to":"Auth","label":"通过","note":"生成用户标识","kind":"return"},{"from":"Auth","to":"Client","label":"双令牌","note":"短 access 加长效 refresh","kind":"return"},{"from":"Client","to":"Resource","label":"带令牌访问","note":"Authorization 头","kind":"call"},{"from":"Resource","to":"Redis","label":"查黑名单","note":"校验 jti","kind":"call"},{"from":"Redis","to":"Resource","label":"未吊销","note":"放行","kind":"return"},{"from":"Client","to":"Auth","label":"access 过期","note":"请求刷新","kind":"call"},{"from":"Auth","to":"Redis","label":"标记旧 refresh","note":"原子轮换","kind":"call"},{"from":"Auth","to":"Client","label":"新令牌对","note":"旧 refresh 作废","kind":"return"},{"from":"Client","to":"Auth","label":"重放旧 refresh","note":"疑似泄露","kind":"call"},{"from":"Auth","to":"Redis","label":"撤销令牌链","note":"整条链作废","kind":"async"}]' />

## 4. 工程实践

### 可验证实验：把 Cookie 属性、状态码与 JWT 编码跑出来看

**实验一：curl 观察 401 / 403 与 Cookie 属性。** 启动第 2 节的 Session 服务，依次发三条请求：

```bash
curl -i -X POST http://127.0.0.1:8080/login -d '{"user":"gopher","pass":"secret"}'
curl -i http://127.0.0.1:8080/me
curl -i http://127.0.0.1:8080/admin
```

<OutputBox title="三次请求的关键状态行与头" output="POST /login → 204 No Content\nSet-Cookie: sid=...; Path=/; HttpOnly; Secure; SameSite=Lax\nGET /me 无 Cookie → 401 Unauthorized\nGET /admin 普通角色 → 403 Forbidden" note="204 表示登录成功且无响应体；三个头各挡一类攻击" />

逐字段解释：`204 No Content` 是登录成功的正确状态码（没有内容可返回）；`HttpOnly` 让 JS 读不到 `sid`，XSS 拿不到会话；`Secure` 保证 Cookie 只在 HTTPS 上发送；`SameSite=Lax` 阻止跨站请求自动携带，缓解 CSRF；`401` 说明没带或带了无效身份，客户端该去登录；`403` 说明身份有效但角色不够，客户端该提示无权限——两者绝不能互换。把 `Secure` 去掉再在 HTTP 下抓包，会看到 Cookie 以明文出现在链路上，这条属性的作用就无需记忆了。

**实验二：go test -race 证明载荷只是编码、并挡住 alg=none。** 下面这段完整可编译的测试文件把「base64url 不是加密」与「固定算法白名单能挡住算法混淆」变成断言：

```go
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

var secret = []byte("test-secret")

func sign(head, payload string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(head + "." + payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verify(token string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("malformed")
	}
	headBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return err
	}
	var head struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headBytes, &head); err != nil {
		return err
	}
	if head.Alg != "HS256" { // 固定算法白名单
		return fmt.Errorf("unexpected alg: %s", head.Alg)
	}
	if !hmac.Equal([]byte(sign(parts[0], parts[1])), []byte(parts[2])) {
		return fmt.Errorf("bad signature")
	}
	return nil
}

func TestPayloadIsEncodedNotEncrypted(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"sub": "u-1", "role": "admin"})
	payload := base64.RawURLEncoding.EncodeToString(body)
	decoded, _ := base64.RawURLEncoding.DecodeString(payload)
	if !strings.Contains(string(decoded), "admin") {
		t.Fatal("载荷应可明文读出")
	}
}

func TestAlgNoneIsRejected(t *testing.T) {
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u-1"}`))
	if err := verify(head + "." + body + "."); err == nil {
		t.Fatal("alg=none 竟然被接受了")
	}
}

func TestValidTokenPasses(t *testing.T) {
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u-1"}`))
	if err := verify(head + "." + body + "." + sign(head, body)); err != nil {
		t.Fatalf("合法令牌被拒: %v", err)
	}
}
```

```bash
go test -race -run 'TestPayloadIsEncodedNotEncrypted|TestAlgNoneIsRejected|TestValidTokenPasses' -v ./...
```

<OutputBox title="go test -race 预期输出" output="=== RUN   TestPayloadIsEncodedNotEncrypted\n--- PASS: TestPayloadIsEncodedNotEncrypted (0.00s)\n=== RUN   TestAlgNoneIsRejected\n--- PASS: TestAlgNoneIsRejected (0.00s)\n=== RUN   TestValidTokenPasses\n--- PASS: TestValidTokenPasses (0.00s)\nPASS" note="载荷可明文读出，alg=none 被拒，合法 HS256 令牌通过" />

逐字段解释：第一个 `--- PASS` 证明 payload 经 base64url 解码后能直接读到 `admin`，因此「看不懂就是加密」的直觉是错的，敏感信息绝不能放进去；第二个 `--- PASS` 证明把 `alg` 改成 `none`、签名留空的伪造令牌被固定算法白名单挡下，这正是算法混淆攻击的最小复现；第三个 `--- PASS` 证明正常 HS256 令牌仍能通过，说明拒绝不是「把校验写死成全拒」。`hmac.Equal` 的恒定时间比较也在这段里，换成普通的 `==` 会引入时序侧信道。把 `exp` 校验补进 `verify` 再跑一个过期用例，就能把第 2 节「必须校验 exp」同样变成断言。

<Predict q="把 JWT 的 alg 改成 none 并把签名段留空，一个固定校验 HS256 的服务端会怎样？" :options='["正常放行，因为 none 是合法算法","在算法白名单处拒绝","自动降级为不校验","报 base64 解码错误"]' answer="1" output="服务端读到 alg=none 与白名单不符，直接返回 unexpected alg，令牌被拒" explain="alg 来自令牌头、属于不可信输入，必须由服务端固定为预期算法；信任它等于把验签方式的决定权交给攻击者。" />

### 两条真实工程误解

**误解一：JWT 的 payload 看起来是一串乱码，说明它是加密的，放点身份证号也没关系。** 错。base64url 是可逆编码，`base64 -d` 或任何一行 Go 代码都能还原出明文 JSON；JWT 的签名只保证「没被篡改」，不保证「没被看见」。要放敏感信息就得用 JWE 加密，或者只在 payload 里放一个无意义的用户 ID，其余信息回服务端查。

**误解二：用 `math/rand` 生成会话 ID 也够随机，反正长度够长。** 错。`math/rand` 是确定性伪随机数生成器，默认种子固定、或可由启动时间推断，攻击者一旦观测到若干 ID 就能推算后续值并劫持会话；长度再长也救不了可预测的种子。会话 ID、CSRF Token、密码重置 Token 这类安全随机值一律用 `crypto/rand`（CSPRNG），这正是第 2 节 `rand.Read(buf)` 而非 `rand.Intn` 的原因。

### 与后续阶段的呼应

- 本讲的认证授权分层在 /microservice/gateway 会被收进统一入口做鉴权，在 /microservice/grpc-advanced 变成 metadata 透传身份与拦截器统一校验；/microservice/tracing 把 `sub` 与 `jti` 放进 span，便于跨服务追溯一次请求的身份。
- 令牌黑名单与版本号本质是把状态请回来，属于 /distributed/cap-base 的一致性取舍；refresh 重放检测与去重表状态机在 /distributed/distributed-lock 继续展开。
- L10 的 /projects/micro-ecommerce 里用户、角色、订单归属三层权限正好覆盖本讲的 RBAC 与数据级归属；/projects/ai-gateway 的令牌与限流把「短有效期加刷新」用在按量计费的接口上。

权限模型上，RBAC 适合大多数后台系统：用户 → 角色 → 权限，改权限只动角色表。但「用户只能看自己的订单」这类数据级权限，RBAC 表达不了，需要在授权层额外校验资源归属；别试图为此给每个资源造一个角色，那是模型错配，应引入 ABAC 或策略引擎。

<Compare title="按吊销需求选身份方案" :panels='[{"label":"场景 A · 需要即时吊销","code":"后台管理 / 金融 / 企业内系统\n要求退出登录立刻全局生效","note":"选服务端 Session：状态在自己手里，删除即失效，实现最直接"},{"label":"场景 B · 无状态横向扩展","code":"微服务 / 多端 / 第三方 API\n服务端不想共享会话存储","note":"选短效 JWT 加 refresh 轮换，并接受黑名单带来的状态成本"}]' />

<FlowTimeline title="OAuth2 授权码流程" :steps='[{"title":"用户点登录","desc":"应用重定向到授权服务器","tone":"blue"},{"title":"认证与同意","desc":"用户在授权方登录并确认授权范围","tone":"purple"},{"title":"回调带 code","desc":"一次性授权码回到应用后端","tone":"orange"},{"title":"换 token","desc":"后端用 code 加 client_secret 换取令牌","tone":"green"},{"title":"访问资源","desc":"凭 access_token 调用资源接口","tone":"gray"}]' direction="horizontal" />

还有几个反模式要避开：用 `math/rand` 生成会话 ID（可预测，必须用 `crypto/rand`）；把 access token 有效期设成几天甚至几周；把权限判断只放在前端（前端只是体验，服务端必须独立校验）；以及在日志里打印完整令牌。密码存储一律用 bcrypt/argon2 加盐哈希，绝不存明文或简单 MD5。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l6-server/lesson-070-auth`](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-070-auth)。clone 仓库后进入 `examples/l6-server`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给 JWT 鉴权加黑名单检查" task="改造 authMiddleware：Verify 通过后再查一次 jti 黑名单，命中则按未认证处理。请写出关键骨架，并说明黑名单项该保留多久。" :hints='["先 Verify 再查黑名单，顺序反了会给无效令牌白白做一次存储查询","从 Claims 里取 JTI，不要重新解析一遍 token","黑名单 TTL 设成该令牌剩余的存活时间即可，过期自动清理"]' solution="c, err := Verify(token)\nif err != nil {\nreturn unauthorized\n}\nif revoker.IsRevoked(c.JTI) {\nreturn unauthorized\n}\nreturn next(w, r.WithContext(ctxWithClaims(c)))" explain="黑名单让 JWT 具备了吊销能力，代价是每次请求多一次存储查询；它也说明「无状态」与「即时吊销」本质上不可兼得。" />

## 随堂检测

<Quiz no="70-1" q="用户已登录但访问了无权限的管理接口，应返回哪个状态码？" :options='["401 Unauthorized","403 Forbidden","400 Bad Request","404 Not Found"]' answer="1" explain="401 表示身份缺失或无效，403 表示身份有效但权限不足；用 404 隐藏存在性只在特定安全场景下才考虑。" />

<Predict no="70-2" q="无状态 JWT 的 access 有效期设为 7 天，且没有任何黑名单，用户改密码后会发生什么？" :options='["旧令牌立即失效","旧令牌在最长 7 天内仍可正常使用","服务端自动广播吊销","令牌会自动缩短有效期"]' answer="1" output="已签发的旧令牌仍带有效签名与未过期的 exp，最长 7 天内都会被服务端接受" explain="这正是 JWT 吊销难题：服务端不存状态就无法主动作废。解法是缩短有效期、加 refresh 轮换，或引入黑名单与令牌版本号。" />

## 一页纸速记

<KnowledgeCard :points='["认证确认你是谁（401），授权确认你能做什么（403），中间件分两层","Session 状态在服务端、可即时吊销；JWT 自包含、易扩展","JWT 吊销必然引入状态：短 access 加 refresh 轮换，或 jti 黑名单","固定校验 alg、exp，密钥外置；载荷是编码不是加密","OAuth2 是第三方授权框架，RBAC 管角色，数据级权限另需归属校验"]' :pitfalls='["把 context.Context 存进结构体字段长期持有","把密码、身份证等敏感信息放进 JWT 载荷","信任令牌头里的 alg，接受 none 算法","把 access token 放 localStorage 或设超长有效期","用 math/rand 生成会话 ID，或只靠前端做权限控制"]' summary="身份体系的核心权衡只有一个：状态放服务端换来可控，放进令牌换来扩展，二者不可兼得。" />

## 小结与下一讲

- 认证与授权必须分层，401 与 403 各司其职；RBAC 之上还要处理数据级归属。
- Session 可即时吊销，JWT 易于横向扩展；所谓吊销方案本质都是把状态请回来。
- 校验 JWT 要固定算法、验签、验过期；密钥外置、载荷不放敏感信息、Cookie 加 HttpOnly 与 SameSite。

L6 的最后一讲转向文件：大文件怎么上传、怎么存、断了怎么续。之后我们就进入 RPC 与微服务阶段。

[下一讲：文件、对象存储与断点续传](/server/file-storage)
