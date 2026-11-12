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

## 4. 工程实践

权限模型上，RBAC 适合大多数后台系统：用户 → 角色 → 权限，改权限只动角色表。但「用户只能看自己的订单」这类数据级权限，RBAC 表达不了，需要在授权层额外校验资源归属；别试图为此给每个资源造一个角色，那是模型错配，应引入 ABAC 或策略引擎。

<Compare title="按吊销需求选身份方案" :panels='[{"label":"场景 A · 需要即时吊销","code":"后台管理 / 金融 / 企业内系统\n要求退出登录立刻全局生效","note":"选服务端 Session：状态在自己手里，删除即失效，实现最直接"},{"label":"场景 B · 无状态横向扩展","code":"微服务 / 多端 / 第三方 API\n服务端不想共享会话存储","note":"选短效 JWT 加 refresh 轮换，并接受黑名单带来的状态成本"}]' />

<FlowTimeline title="OAuth2 授权码流程" :steps='[{"title":"用户点登录","desc":"应用重定向到授权服务器","tone":"blue"},{"title":"认证与同意","desc":"用户在授权方登录并确认授权范围","tone":"purple"},{"title":"回调带 code","desc":"一次性授权码回到应用后端","tone":"orange"},{"title":"换 token","desc":"后端用 code 加 client_secret 换取令牌","tone":"green"},{"title":"访问资源","desc":"凭 access_token 调用资源接口","tone":"gray"}]' direction="horizontal" />

还有几个反模式要避开：用 `math/rand` 生成会话 ID（可预测，必须用 `crypto/rand`）；把 access token 有效期设成几天甚至几周；把权限判断只放在前端（前端只是体验，服务端必须独立校验）；以及在日志里打印完整令牌。密码存储一律用 bcrypt/argon2 加盐哈希，绝不存明文或简单 MD5。

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
