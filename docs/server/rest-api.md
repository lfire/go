---
title: 第 63 讲 · REST API 设计、校验与 OpenAPI
description: 用资源与状态码表达语义，讲清幂等性、分页过滤、参数校验、版本策略与 OpenAPI 文档生成，设计出稳定可演进的接口
---

<StageHeader stage="L6 · 服务端与数据" level="★★★☆☆" time="45 分钟" goal="设计出稳定、可演进的接口" prereq="第 62 讲 · 路由与框架" />

# 第 63 讲 · REST API 设计、校验与 OpenAPI

> 会用框架把请求分到函数，只解决了「能跑」；接口设计要解决的是「别人愿不愿意用、三年后还改得动」。REST 的真正价值不是几条命名规矩，而是让 HTTP 协议自带的语义替你承担契约：状态码让网关、缓存、监控无需读 body 就能判断成败，幂等性让重试变得安全，OpenAPI 让契约变成机器可读的资产。这一讲把这些逐一落成可运行的 Go 代码。

## 这一讲你会得到什么

<CheckList id="63-goal" title="本讲目标" :items='["能按资源与 HTTP 方法设计一套 CRUD 接口并选对状态码","理解幂等性为什么是重试安全的地基，会设计幂等键","会做分页与过滤、字段级参数校验与统一错误响应","掌握 URL 版本策略与 OpenAPI 文档生成的落地方式"]' />

## 1. 先建立直觉

把 API 想成图书馆的借还台。书是**资源**，用名词 `/users`、`/orders` 命名，不要在路径里塞动词 `/getUser`；动作交给 HTTP 方法表达：GET 取、POST 新建、PUT 整体替换、DELETE 删除。管理员（网关、CDN、监控）不需要听懂你们的业务黑话，只要看状态码就知道发生了什么：2xx 成了、4xx 客户端的错、5xx 服务端的错。

最容易被低估的是**幂等性**：同一个请求执行一次和执行 N 次，对服务端状态的最终影响相同。为什么它值一整节？因为网络会超时、客户端会重试、消息队列会重投。GET、PUT、DELETE 天生幂等，POST 通常不幂等——超时后重发一次下单请求，很可能就多出一单。承认「重试一定会发生」，再为它设计，比祈祷前端别重复点可靠得多。

<ConceptDiagram type="http-chain" title="一次 REST 请求依次穿过版本、校验、幂等与业务" caption="每一层都可能提前返回，越靠前的层越应该便宜；状态码由最终到达的那一层决定" />

## 2. 语法与规则

第一段代码用标准库实现一套完整的 users 资源：分页与过滤、创建返回 201 与 Location、PUT 幂等替换、DELETE 返回 204，以及统一 JSON 响应。

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"
	"sync"
)

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// store 是线程安全的内存仓储，仅用于演示；生产应换成数据库。
type store struct {
	mu     sync.Mutex
	nextID int
	users  map[int]User
}

func (s *store) list(offset, limit int, role string) []User {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]int, 0, len(s.users))
	for id, u := range s.users {
		if role == "" || u.Role == role {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids) // 保证分页稳定；生产里应由 ORDER BY 完成
	if offset > len(ids) {
		offset = len(ids)
	}
	end := offset + limit
	if end > len(ids) {
		end = len(ids)
	}
	out := make([]User, 0, end-offset)
	for _, id := range ids[offset:end] {
		out = append(out, s.users[id])
	}
	return out
}

func (s *store) get(id int) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	return u, ok
}

func (s *store) create(u User) User {
	s.mu.Lock()
	defer s.mu.Unlock()
	u.ID = s.nextID
	s.nextID++
	s.users[u.ID] = u
	return u
}

// put 是幂等的：无论调用多少次，最终状态完全由本次请求体决定。
func (s *store) put(id int, u User) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return User{}, false
	}
	u.ID = id
	s.users[id] = u
	return u, true
}

func (s *store) remove(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return false
	}
	delete(s.users, id)
	return true
}

func parseID(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	return id, err == nil && id > 0
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *store) handleList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	users := s.list(offset, limit, r.URL.Query().Get("role"))
	writeJSON(w, http.StatusOK, map[string]any{
		"data": users, "offset": offset, "limit": limit,
	})
}

func (s *store) handleCreate(w http.ResponseWriter, r *http.Request) {
	var u User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	created := s.create(u)
	// 201 必须带 Location，告诉客户端新资源在哪。
	w.Header().Set("Location", "/v1/users/"+strconv.Itoa(created.ID))
	writeJSON(w, http.StatusCreated, created)
}

func (s *store) handleGet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	u, ok := s.get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *store) handlePut(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var u User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	updated, ok := s.put(id, u)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *store) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if !s.remove(id) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent) // 204 不带响应体
}

func main() {
	s := &store{nextID: 1, users: make(map[int]User)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/users", s.handleList)
	mux.HandleFunc("POST /v1/users", s.handleCreate)
	mux.HandleFunc("GET /v1/users/{id}", s.handleGet)
	mux.HandleFunc("PUT /v1/users/{id}", s.handlePut)
	mux.HandleFunc("DELETE /v1/users/{id}", s.handleDelete)

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

<OutputBox title="典型交互" output="POST /v1/users 返回 201 Created\nLocation: /v1/users/1\n响应体：id 为 1 的用户 JSON\n\nDELETE /v1/users/1 返回 204 No Content，无响应体\nGET /v1/users 带 role 与 limit 查询参数时只返回 admin 角色的前 10 条" note="状态码本身就是契约的一部分，它会被 CDN、网关与监控直接消费" />

<LineByLine code="var dst createReq\nif err := bind(r, dst); err != nil {\nwriteError(w, 400, err)\nreturn\n}\nif errs := dst.validate(); len(errs) != 0 {\nwriteError(w, 422, errs)\nreturn\n}\nwriteJSON(w, 201, store.create(dst))" :notes='["声明与请求体对应的输入结构体，避免直接把 body 灌进领域模型","解码失败属于语法错误，归 400","写统一错误响应后必须 return，防止继续执行","校验失败属于语义错误，归 422 并带上字段级明细","同样立刻返回，两层失败互不干扰","通过校验才进入业务，创建成功返回 201 与新资源"]' title="创建接口的分层校验逐行看" />

注意上面骨架里 `400` 与 `422` 的分工：请求体根本不是合法 JSON，是语法错误，用 400；JSON 合法但字段不合规（邮箱格式错、年龄越界），属于语义校验失败，用 422 更精确。把两者都塞进 400，客户端就无法区分「改格式」和「改内容」。

第二段代码把这件事做成可运行的统一错误契约，包含字段级校验。

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
)

type fieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type apiError struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Details []fieldError `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, status int, e apiError) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(e)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

var emailRe = regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}$`)

type createReq struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

func (r createReq) validate() []fieldError {
	var errs []fieldError
	if len(strings.TrimSpace(r.Name)) < 2 {
		errs = append(errs, fieldError{Field: "name", Message: "至少 2 个字符"})
	}
	if !emailRe.MatchString(r.Email) {
		errs = append(errs, fieldError{Field: "email", Message: "不是合法邮箱"})
	}
	if r.Age < 0 || r.Age > 150 {
		errs = append(errs, fieldError{Field: "age", Message: "需在 0 到 150 之间"})
	}
	return errs
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/users", func(w http.ResponseWriter, r *http.Request) {
		var req createReq
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields() // 未知字段直接拒绝，避免静默吞掉拼写错误
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, apiError{
				Code: "INVALID_JSON", Message: "请求体解析失败",
			})
			return
		}
		if errs := req.validate(); len(errs) > 0 {
			writeError(w, http.StatusUnprocessableEntity, apiError{
				Code: "VALIDATION_FAILED", Message: "参数校验失败", Details: errs,
			})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": req})
	})
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

嗯，最后那行 `_ = errors.New("reserved")` 是为了用上 import，但很别扭。我去掉 errors import 和那行。让我修正：不需要 errors。我会在写入时去掉。

实际上我 Write 的内容要正确。让我在最终内容里去掉 errors import 和那行 dummy。

## 3. 深入一步

幂等性的工程解法是**幂等键**：客户端为一次业务操作生成唯一 ID，放在 `Idempotency-Key` 请求头；服务端在处理前查重，命中就直接返回上次的结果。注意「查重 + 写入」必须原子，通常用数据库唯一索引或 Redis SETNX，否则并发下仍会重复。这与后面消息队列讲的幂等消费是同一个原理。

分页方面，`offset/limit` 实现简单、支持跳页，但深分页时数据库必须扫描并丢弃前 N 行；数据量大的列表（信息流、日志）应改用游标分页：以上一页最后一条的有序键为起点，只取后续 N 条，代价是无法随机跳页。过滤参数要白名单化，绝不能把用户输入直接拼进 SQL。

| 方法 | 语义 | 幂等 | 安全 | 典型状态码 |
| --- | --- | --- | --- | --- |
| GET | 读取资源 | 是 | 是 | 200 / 404 |
| POST | 新建资源 | 否 | 否 | 201 / 400 / 409 |
| PUT | 整体替换 | 是 | 否 | 200 / 201 / 404 |
| PATCH | 局部更新 | 否 | 否 | 200 / 404 |
| DELETE | 删除资源 | 是 | 否 | 204 / 404 |

| 状态码 | 何时用 | 常见误用 |
| --- | --- | --- |
| 400 | JSON 语法错、参数类型错 | 拿它承载所有业务校验失败 |
| 401 / 403 | 未认证 / 已认证但无权限 | 两者混用 |
| 404 | 资源不存在 | 用它表示「不想告诉你」，泄露信息 |
| 409 | 资源冲突，如唯一键重复 | 用 500 顶替 |
| 422 | 语法正确但语义校验失败 | 与 400 混为一谈 |
| 429 | 触发限流 | 用 503 顶替 |

<Term word="OpenAPI" text="一份机器可读的 HTTP 接口契约（JSON/YAML），描述路径、参数、请求响应体与状态码，可用来生成文档、客户端 SDK、Mock 服务与契约测试" />

<Callout type="warn" title="反模式：用 200 包一切" text="把所有响应都写成 200，再在 body 里塞自定义 code 等于 -1，是很多内部系统的通病。后果是 CDN 会缓存错误响应、网关无法按状态做重试与熔断、监控无法按 5xx 告警、客户端必须解析 body 才能知道成败。HTTP 已经给了你一套被全链路理解的状态码，别把它废掉。" />

<Callout type="idea" title="版本策略" text="把版本放在 URL 前缀（/v1、/v2）最直观，网关做灰度与路由也最省事；媒体类型协商更学术但工具链支持弱。只有破坏性变更（删字段、改语义、改必填）才升版本，加可选字段属于兼容演进。弃用旧版本时用 Deprecation 与 Sunset 响应头给出迁移信号，而不是悄悄改语义。" />

第三段代码演示「文档即代码」：用同一份字段元数据生成 OpenAPI 片段，并展示 URL 版本与弃用声明。

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// field 描述一个请求字段；schema 与校验规则共用同一份元数据。
type field struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

func schema(fields []field) map[string]any {
	props := map[string]any{}
	required := []string{}
	for _, f := range fields {
		props[f.Name] = map[string]any{"type": f.Type}
		if f.Required {
			required = append(required, f.Name)
		}
	}
	return map[string]any{
		"type": "object", "properties": props, "required": required,
	}
}

// openapi 由元数据生成文档，避免「实现改了文档没改」。
func openapi() map[string]any {
	userSchema := schema([]field{
		{Name: "name", Type: "string", Required: true},
		{Name: "email", Type: "string", Required: true},
		{Name: "age", Type: "integer"},
	})
	return map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "User API", "version": "1.0.0"},
		"paths": map[string]any{
			"/v1/users": map[string]any{
				"post": map[string]any{
					"summary": "创建用户",
					"requestBody": map[string]any{
						"content": map[string]any{
							"application/json": map[string]any{"schema": userSchema},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{"description": "已创建"},
					},
				},
			},
		},
	}
}

func main() {
	mux := http.NewServeMux()

	// 暴露机器可读契约，供代码生成、Mock 与契约测试消费。
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(openapi())
	})

	// 版本放在 URL 前缀，最易在网关做路由与灰度。
	mux.HandleFunc("GET /v1/users", v1Users)
	mux.HandleFunc("GET /v2/users", v2Users)

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func v1Users(w http.ResponseWriter, r *http.Request) {
	// 弃用声明：给客户端明确的迁移信号，而不是悄悄改语义。
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Sunset", "Wed, 31 Dec 2025 23:59:59 GMT")
	writeList(w, []string{"gopher"})
}

func v2Users(w http.ResponseWriter, r *http.Request) {
	writeList(w, []map[string]any{{"id": 1, "displayName": "gopher"}})
}

func writeList(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{"data": v})
}
```

生产里更常见的做法是用 `swaggo/swag` 从 Handler 上的注解生成 `docs/`，或用「schema 优先」先写 OpenAPI 再生成服务端骨架。无论哪种，目标都是**契约只有一处真相**，而不是靠人肉维护两份。

## 4. 工程实践

接口设计里最贵的错误往往发生在版本演进：把版本号塞进请求体、给同一个字段悄悄换含义、删除字段不通知，都会让老客户端在某次发布后突然崩掉。稳定的做法是「只加不删、新增字段可选、废弃字段先标记再下线」，并用契约测试在 CI 里比对新旧版本差异。

<Compare title="错误契约：要不要用 HTTP 语义" :panels='[{"label":"写法 A · 万物皆 200","code":"w.WriteHeader(200)\njson.NewEncoder(w).Encode(resp)","note":"客户端必须解析 body 里的自定义 code 才能判断成败，缓存、重试、告警全部失效"},{"label":"写法 B · 用 HTTP 语义","code":"w.WriteHeader(status)\njson.NewEncoder(w).Encode(payload)","note":"状态码被 CDN、网关、监控天然理解，客户端可直接按类别处理"}]' />

<FlowTimeline title="一次写请求的处理链" :steps='[{"title":"版本与路由","desc":"按 URL 前缀分到对应版本的 Handler","tone":"blue"},{"title":"解码","desc":"语法错误返回 400","tone":"purple"},{"title":"字段校验","desc":"语义错误返回 422 并带明细","tone":"orange"},{"title":"幂等检查","desc":"查 Idempotency-Key，命中直接回放结果","tone":"green"},{"title":"业务与持久化","desc":"成功返回 201 与 Location","tone":"blue"},{"title":"统一序列化","desc":"错误体与成功体由同一层写出","tone":"gray"}]' direction="horizontal" />

另一个高频反模式是**把幂等性寄托于前端**：靠按钮置灰、靠「用户不会点两次」。网络超时发生在客户端与网关之间时，用户往往什么都没看到，重试是 SDK 与浏览器自动做的。只有服务端用唯一键兜底，才是可靠的。同理，分页的 `limit` 必须设上限（如 100），否则一个 `limit=1000000` 就能把数据库拖垮。

## 动手练一练

<Exercise title="给下单接口加幂等键" task="实现中间件：读取请求头里的 Idempotency-Key，若该键已处理过则直接回放上次响应，否则放行并在业务完成后记录结果。请写出中间件骨架并说明并发下要注意什么。" :hints='["命中缓存时不要再调用 next，直接写回上次的 status 与 body","查重与记录必须原子；内存 map 要加锁，生产用 Redis SETNX 或数据库唯一索引","键要有过期时间，否则去重表会无限膨胀"]' solution="func idempotent(next http.HandlerFunc) http.HandlerFunc {\nreturn func(w http.ResponseWriter, r *http.Request) {\nkey := r.Header.Get(idempotencyHeader)\nif key == empty {\nnext(w, r)\nreturn\n}\nif cached, ok := lookup(key); ok {\nreplay(w, cached)\nreturn\n}\nrec := new(recorder)\nrec.ResponseWriter = w\nnext(rec, r)\nstoreResult(key, rec.status, rec.body)\n}\n}" explain="并发下必须用 Redis SETNX 或唯一索引保证「查重 + 标记」原子，否则两个同时到达的相同键都会放行；回放时要连状态码一起缓存。" />

## 随堂检测

<Quiz no="63-1" q="下列哪一组方法全部是幂等的？" :options='["GET、PUT、DELETE","GET、POST、PATCH","POST、PUT、DELETE","GET、POST、PUT"]' answer="0" explain="GET 只读、PUT 整体替换、DELETE 删除后重复执行结果一致；POST 与 PATCH 通常不幂等。" />

<Predict no="63-2" q="客户端对 POST /v1/orders 请求超时后自动重发一次，服务端没有做幂等处理，最可能发生什么？" :options='["只创建一单，HTTP 层自动去重","创建了两个内容相同的订单","第二次返回 404","第二次返回 409"]' answer="1" output="服务端收到两次合法的创建请求，创建了两个内容相同的订单" explain="超时只说明客户端没收到响应，不代表服务端没执行。POST 不幂等，重试会放大副作用，这正是幂等键存在的原因。" />

<FillBlank no="63-3" tpl="PUT 与 DELETE 是 ___ 的，POST 通常 ___；为了让超时重试安全，客户端应携带 ___ 请求头。" :answers='["幂等~idempotent","非幂等","Idempotency-Key~幂等键"]' hint="想想重复执行对服务端状态的影响" explain="幂等性描述的是「重复执行的影响」，不是「返回内容是否相同」；DELETE 第二次可能返回 404，但资源状态仍然一致，因此依旧幂等。" />

## 一页纸速记

<KnowledgeCard :points='["用名词命名资源，用 HTTP 方法表达动作，用状态码表达结果","POST 不幂等，超时重试要靠服务端 Idempotency-Key 兜底","400 管语法、422 管语义校验，错误体要带字段级明细","分页 limit 必须设上限，深分页改游标；文档由单一元数据生成"]' :pitfalls='["用 200 包一切，把自定义 code 塞进 body","把版本号塞进请求体，或悄悄改变已有字段语义","DELETE 成功还返回响应体（204 不该有 body）","把过滤参数直接拼进 SQL，或放任 limit 无限大"]' summary="REST 的核心是让 HTTP 语义替你承担契约：状态码、幂等性、文档三者都不是可选项。" />

## 小结与下一讲

- 资源 + 方法 + 状态码构成接口骨架，201/204/400/422 各有明确分工。
- 幂等性不是理论洁癖，而是对「重试必然发生」的工程回应，靠服务端唯一键实现。
- 契约要机器可读：OpenAPI 由单一元数据生成，版本演进只加不删并给出弃用信号。

到目前为止我们处理的都是「一问一答」的短请求。可如果要服务端主动把消息推给浏览器呢？下一讲进入长连接世界。

[下一讲：WebSocket / SSE 与实时推送](/server/websocket)
