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

### 协议与契约：幂等、状态码与版本从哪推导出来

**幂等性不是风格，是对「三态超时」的回应。** 一次请求超时后，客户端只知道三种可能：成功、失败、**未知**——响应可能在回程丢了，服务端其实已经执行。RFC 9110 第 9.2.2 节对幂等的定义是「同一请求执行一次与执行多次，服务端预期状态相同」，注意它约束的是**服务端状态**，不是响应字节：DELETE 第一次删成功返回 204，第二次资源已不存在返回 404，状态一致，因此仍然幂等。PUT 同理，整体替换 N 次结果由最后一次请求体唯一决定。POST 是「从属于目标资源的新建」，重发就产生第二个从属资源；PATCH 若含 `balance += 1` 这类自增语义，重发会叠加。承认「重试必然发生」（超时、SDK 自动重发、消息重投），再为它设计，比祈祷前端别重复点可靠得多。

**幂等键的难点全在原子性。** 「查重加写入」若写成两条独立语句，两个并发的相同键会同时通过查重、各自写入，幂等形同虚设。推导：设两个请求 A、B 同时到达，都执行 `SELECT` 未命中，再各自 `INSERT`，中间没有任何互斥，结果就是两份订单。所以要么把唯一约束交给数据库（`INSERT ... ON DUPLICATE KEY UPDATE` 或去重表唯一索引），要么用 Redis `SETNX`，且**判重与业务写入必须在同一事务**里，否则仍有「业务写成功、去重记录没写上」的窗口。回放时还要连状态码与响应体一起缓存，只回一个「已处理」字符串会让客户端拿不到首次的结果。

**状态码是分层契约。** 201 依 RFC 9110 第 15.3.2 节必须带 `Location` 指向新资源；204 语义上不允许有响应体（写了就是协议违规）；202 表示「已接收、尚未处理完」，与 201「已建成」是两件事，第 68 讲的 `POST /orders` 返回 202 正源于此。400 来自语法错误，422 来自 RFC 4918 的「语义校验失败」，401 与 403 由 RFC 9110 分成「未认证」与「已认证但无权限」，409 表示资源冲突（唯一键重复），429 来自 RFC 6585 的限流。把这一切塞进 200 加自定义 code 的代价是具体的：CDN 会缓存错误响应、网关无法按状态做重试与熔断、监控无法按 5xx 告警、客户端必须解析 body 才知道成败。

**版本与分页都是可推导的取舍。** 版本放 URL 前缀（`/v1`），网关按路径做灰度与路由零成本；放媒体类型协商（`Accept: application/vnd.x.v2+json`）更「RESTful」但工具链支持弱、缓存键要跟着变；放请求体最差，路由、缓存、日志、灰度全都得先解析 body 才能判断版本。分页 `offset/limit` 支持跳页，但深分页时数据库必须扫描并丢弃前 N 行，代价 O(offset)；游标分页以上一页最后一条的有序键为起点，代价恒定，代价是无法随机跳页。

### 设计取舍与替代方案

| 决策点 | 方案 A | 方案 B（推荐） | 方案 A 的代价 |
| --- | --- | --- | --- |
| 幂等保障 | 前端按钮置灰、靠用户不重复点 | 服务端 `Idempotency-Key` 加唯一索引 | 超时发生在网关与客户端之间时用户毫无感知，重试由 SDK 自动完成，前端去重完全失效 |
| 查重实现 | 先 SELECT 判重再 INSERT | 唯一索引冲突即命中，或 SETNX 加同事务 | 并发下两个请求同时判重通过，产生重复业务数据 |
| 错误契约 | 万物皆 200，body 里塞自定义 code | 用真实 HTTP 状态码加统一错误体 | 缓存、重试、熔断、告警全链路失效 |
| 版本位置 | 请求体或自定义头 | URL 前缀 | 网关灰度、CDN 缓存、日志检索都先要解析 body |
| 分页 | 纯 offset/limit | 深分页改游标，并给 limit 设上限 | 深分页慢查询、单个大 limit 拖垮数据库 |
| 演进方式 | 悄悄改已有字段语义 | 只加可选字段，破坏性变更才升版本 | 老客户端在某次发布后静默崩溃 |

### 失败矩阵

| 失败场景 | 触发条件 | 可观测现象 | 定位手段 | 对策 |
| --- | --- | --- | --- | --- |
| 超时重试放大 | POST 超时被自动重发且无幂等 | 两个内容相同的订单 | 幂等键命中日志 | 幂等键加唯一索引 |
| 幂等键竞争 | 查重与写入不在同一事务 | 并发下仍产生重复 | 去重表唯一键冲突计数 | 唯一索引或 SETNX 加同事务 |
| 缓存与 DB 不一致 | 写后更新缓存且并发写乱序 | 缓存留旧值，读到脏数据 | 缓存与库值比对 | 写后删缓存，必要时延迟双删（见 /server/redis） |
| 深分页慢查询 | offset 极大且无上限 | 单请求扫描百万行 | `EXPLAIN`、慢 SQL 日志 | limit 设上限、改游标分页 |
| 连接池耗尽 | 慢查询叠加高并发 | `WaitCount` 上涨、延迟毛刺 | `db.Stats()` | 索引、超时、并发上限 |
| 版本静默破坏 | 悄悄改字段含义或删字段 | 老客户端发布后突然报错 | 契约测试差异比对 | 只加不删、Deprecation 与 Sunset |
| 限流被误判 | 429 写成 503 | 客户端继续重试，流量越压越高 | 入口 429 与后端 QPS 对比 | 用 429 加 Retry-After（见 /microservice/resilience） |

<SequenceDiagram title="幂等键重放：并发相同请求只落一次" :actors='["Client","Gateway","Server","Dedup","DB"]' :steps='[{"from":"Client","to":"Gateway","label":"POST 带键","note":"携带 Idempotency-Key","kind":"call"},{"from":"Gateway","to":"Server","label":"转发","note":"超时后自动重发同一键","kind":"call"},{"from":"Server","to":"Dedup","label":"原子登记","note":"唯一索引或 SETNX","kind":"call"},{"from":"Dedup","to":"Server","label":"首次登记成功","note":"获得处理权","kind":"return"},{"from":"Server","to":"DB","label":"写业务","note":"与去重记录同事务","kind":"call"},{"from":"DB","to":"Server","label":"提交成功","note":"结果回传","kind":"return"},{"from":"Server","to":"Gateway","label":"201 加结果","note":"缓存状态码与体","kind":"return"},{"from":"Gateway","to":"Client","label":"首次响应","note":"回程丢失","kind":"return"},{"from":"Client","to":"Gateway","label":"超时重发","note":"同一幂等键","kind":"call"},{"from":"Gateway","to":"Server","label":"再次转发","note":"键相同","kind":"call"},{"from":"Server","to":"Dedup","label":"查重命中","note":"唯一键冲突","kind":"call"},{"from":"Server","to":"Gateway","label":"回放结果","note":"不再写业务","kind":"return"}]' />

## 4. 工程实践

### 可验证实验：把状态码契约与幂等原子性跑出来看

**实验一：curl 逐条核对状态码语义。** 启动第 2 节的 users 服务，按顺序发四条请求：

```bash
curl -i -X POST http://127.0.0.1:8080/v1/users -d '{"name":"go"}'
curl -i -X DELETE http://127.0.0.1:8080/v1/users/1
curl -i -X POST http://127.0.0.1:8080/v1/users -d 'not-json'
curl -i 'http://127.0.0.1:8080/v1/users?limit=999999'
```

<OutputBox title="四条请求的状态行与关键头" output="POST    → HTTP/1.1 201 Created\nLocation: /v1/users/1\nDELETE  → HTTP/1.1 204 No Content（无响应体）\nPOST 非法体 → HTTP/1.1 400 Bad Request\nGET 大 limit → HTTP/1.1 200 OK（limit 被夹到上限）" note="状态码本身就是契约，CDN、网关、监控无需读 body 即可判断成败" />

逐字段解释：`201 Created` 配 `Location: /v1/users/1` 兑现 RFC 9110 的创建语义，客户端据此知道新资源地址；`204 No Content` 之后必须没有响应体，若 Handler 多写了字节，严格客户端会解析错乱；`400 Bad Request` 对应「请求体不是合法 JSON」的语法错误，与字段不合规的 422 分属两层；最后一条 `200 OK` 说明 `limit=999999` 被服务端夹到上限（如 20）而不是照单全收——`limit` 必须设上限，否则一个请求就能把库拖垮。把 429 写成 503 的对照可用 `curl -i` 看 `Retry-After` 头是否缺失，直接验证「限流被误判」那一行。

**实验二：go test -race 证明幂等键在并发下必须原子。** 下面这段完整可编译的测试文件让 20 个相同 `Idempotency-Key` 的请求并发打到同一个 Handler，断言业务只创建一次：

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

type idempotentStore struct {
	mu      sync.Mutex
	seen    map[string]int
	creates int32
}

func (s *idempotentStore) handle(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	s.mu.Lock()
	if code, ok := s.seen[key]; ok {
		s.mu.Unlock()
		w.WriteHeader(code)
		return
	}
	s.seen[key] = http.StatusCreated
	s.mu.Unlock()
	atomic.AddInt32(&s.creates, 1)
	w.WriteHeader(http.StatusCreated)
}

func TestIdempotencyKeyUnderConcurrency(t *testing.T) {
	s := &idempotentStore{seen: make(map[string]int)}
	srv := httptest.NewServer(http.HandlerFunc(s.handle))
	defer srv.Close()
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
			req.Header.Set("Idempotency-Key", "order-1001")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			_ = resp.Body.Close()
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&s.creates); got != 1 {
		t.Fatalf("相同幂等键创建了 %d 次，应为 1", got)
	}
}
```

```bash
go test -race -run TestIdempotencyKeyUnderConcurrency -v ./...
```

<OutputBox title="go test -race 预期输出" output="=== RUN   TestIdempotencyKeyUnderConcurrency\n--- PASS: TestIdempotencyKeyUnderConcurrency (0.00s)\nPASS\nok   example.com/server   1.412s" note="20 个并发相同键，creates 计数仍为 1，且无数据竞争" />

逐字段解释：`=== RUN` 是用例开始；`--- PASS` 说明 `creates` 最终为 1，即 19 个重复请求都在 `seen` 命中被回放、没有再写业务；`-race` 未报竞争，说明「查重加登记」被 `sync.Mutex` 包成原子操作。做对照实验：把 `s.mu.Lock()` / `Unlock()` 两行删掉再跑，要么 `creates` 大于 1，要么 race detector 直接报 `WARNING: DATA RACE` 甚至 `concurrent map writes` 崩溃——这就把「查重加写入必须原子」从结论变成可证伪的断言。生产里 `seen` 换成带唯一索引的去重表，同一事务同时写业务数据与去重记录，原理完全一致。

<Predict q="把 429 限流错写成 503，客户端重试逻辑会怎样？" :options='["立刻停止重试","当成临时故障继续带退避重试，流量被放大","自动降级到 GET","返回 404"]' answer="1" output="客户端把 503 视为服务端暂时不可用并继续重试，后端 QPS 反而上升" explain="429 才是「请求方太快来慢点」的信号，配 Retry-After 才能让客户端退避；503 表示服务端故障，重试逻辑只会加剧压力，这是状态码混用引发重试放大的典型路径。" />

### 两条真实工程误解

**误解一：幂等键用 Redis SETNX 就够了，业务写库不用管事务。** 错。SETNX 只保证「只有一个请求拿到处理权」，但业务写入与去重标记若分属两个系统，中间进程崩溃就会出现「标记写了、业务没写」或反过来「业务写了、标记没写」的窗口；后者在重放时会被当成首次处理，重复执行业务。正确做法是去重表唯一索引加本地事务，或用本地消息表把两者绑进同一个原子单元（见 /server/mq）。

**误解二：把版本号放进请求体最灵活，网关不用改。** 错。请求体版本让路由、CDN 缓存键、访问日志、灰度分流全都依赖先解析 body 才能判断版本，CDN 无法按 URL 缓存，灰度必须在应用层做。URL 前缀把版本暴露在路径上，网关按前缀路由与灰度零成本，监控按路径聚合即可看到各版本流量；代价只是 URL 变长，以及破坏性变更需要新增一组路由。两相比较，后者在绝大多数工程里更划算。

### 与后续阶段的呼应

- 本讲的 `Idempotency-Key` 与「查重加写入同事务」会在 /server/redis 变成缓存双写一致、在 /server/mq 变成幂等消费、在 /distributed/distributed-lock 变成去重表与状态机；状态码到错误码的映射则在 /microservice/grpc-advanced 里被翻译成 gRPC `status`。
- L7 的 /microservice/gateway 按 URL 前缀做版本灰度与统一错误响应，/microservice/resilience 用 429 加 Retry-After 与重试预算把「重试放大」关进笼子。
- L10 的 /projects/micro-ecommerce 里订单创建必须服务端幂等，/projects/micro-seckill 的预扣减更是把幂等键当作正确性底线；/projects/ai-gateway 的令牌计费接口同样靠幂等键避免重试重复扣费。

接口设计里最贵的错误往往发生在版本演进：把版本号塞进请求体、给同一个字段悄悄换含义、删除字段不通知，都会让老客户端在某次发布后突然崩掉。稳定的做法是「只加不删、新增字段可选、废弃字段先标记再下线」，并用契约测试在 CI 里比对新旧版本差异。

<Compare title="错误契约：要不要用 HTTP 语义" :panels='[{"label":"写法 A · 万物皆 200","code":"w.WriteHeader(200)\njson.NewEncoder(w).Encode(resp)","note":"客户端必须解析 body 里的自定义 code 才能判断成败，缓存、重试、告警全部失效"},{"label":"写法 B · 用 HTTP 语义","code":"w.WriteHeader(status)\njson.NewEncoder(w).Encode(payload)","note":"状态码被 CDN、网关、监控天然理解，客户端可直接按类别处理"}]' />

<FlowTimeline title="一次写请求的处理链" :steps='[{"title":"版本与路由","desc":"按 URL 前缀分到对应版本的 Handler","tone":"blue"},{"title":"解码","desc":"语法错误返回 400","tone":"purple"},{"title":"字段校验","desc":"语义错误返回 422 并带明细","tone":"orange"},{"title":"幂等检查","desc":"查 Idempotency-Key，命中直接回放结果","tone":"green"},{"title":"业务与持久化","desc":"成功返回 201 与 Location","tone":"blue"},{"title":"统一序列化","desc":"错误体与成功体由同一层写出","tone":"gray"}]' direction="horizontal" />

另一个高频反模式是**把幂等性寄托于前端**：靠按钮置灰、靠「用户不会点两次」。网络超时发生在客户端与网关之间时，用户往往什么都没看到，重试是 SDK 与浏览器自动做的。只有服务端用唯一键兜底，才是可靠的。同理，分页的 `limit` 必须设上限（如 100），否则一个 `limit=1000000` 就能把数据库拖垮。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l6-server/lesson-063-rest-api`](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-063-rest-api)。clone 仓库后进入 `examples/l6-server`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给下单接口加幂等键" task="实现中间件：读取请求头里的 Idempotency-Key，若该键已处理过则直接回放上次响应，否则放行并在业务完成后记录结果。请写出中间件骨架并说明并发下要注意什么。" :hints='["命中缓存时不要再调用 next，直接写回上次的 status 与 body","查重与记录必须原子；内存 map 要加锁，生产用 Redis SETNX 或数据库唯一索引","键要有过期时间，否则去重表会无限膨胀"]' solution="func idempotent(next http.HandlerFunc) http.HandlerFunc {\nreturn func(w http.ResponseWriter, r *http.Request) {\nkey := r.Header.Get(idempotencyHeader)\nif key == empty {\nnext(w, r)\nreturn\n}\nif cached, ok := lookup(key); ok {\nreplay(w, cached)\nreturn\n}\nrec := new(recorder)\nrec.ResponseWriter = w\nnext(rec, r)\nstoreResult(key, rec.status, rec.body)\n}\n}" explain="并发下必须用 Redis SETNX 或唯一索引保证「查重 + 标记」原子，否则两个同时到达的相同键都会放行；回放时要连状态码一起缓存。" />

## 随堂检测

<Quiz no="63-1" q="下列哪一组方法全部是幂等的？" :options='["GET、PUT、DELETE","GET、POST、PATCH","POST、PUT、DELETE","GET、POST、PUT"]' answer="0" explain="GET 只读、PUT 整体替换、DELETE 删除后重复执行结果一致；POST 与 PATCH 通常不幂等。" />

<Predict no="63-2" q="客户端对 POST /v1/orders 请求超时后自动重发一次，服务端没有做幂等处理，最可能发生什么？" :options='["只创建一单，HTTP 层自动去重","创建了两个内容相同的订单","第二次返回 404","第二次返回 409"]' answer="1" output="服务端收到两次合法的创建请求，创建了两个内容相同的订单" explain="超时只说明客户端没收到响应，不代表服务端没执行。POST 不幂等，重试会放大副作用，这正是幂等键存在的原因。" />

<FillBlank no="63-3" tpl="PUT 与 DELETE 是 ___ 的，POST 通常 ___；为了让超时重试安全，客户端应携带 ___ 请求头。" :answers='["幂等~idempotent","非幂等","Idempotency-Key~幂等键"]' hint="想想重复执行对服务端状态的影响" explain="幂等性描述的是「重复执行的影响」，不是「返回内容是否相同」；DELETE 第二次可能返回 404，但资源状态仍然一致，因此依旧幂等。" :explains='["PUT 整体替换、DELETE 删除同一资源，重复多次对状态的净效果与一次相同，因此幂等，与返回码是否一致无关。","POST 多用于新建资源，每重放一次就多产生一条记录，状态不断累加，所以超时后盲目重试可能造成重复下单。","客户端为每次业务操作配一个唯一键，服务端据此识别重放并返回首次结果，从而让非幂等的请求也能安全重试。"]' />

## 一页纸速记

<KnowledgeCard :points='["用名词命名资源，用 HTTP 方法表达动作，用状态码表达结果","POST 不幂等，超时重试要靠服务端 Idempotency-Key 兜底","400 管语法、422 管语义校验，错误体要带字段级明细","分页 limit 必须设上限，深分页改游标；文档由单一元数据生成"]' :pitfalls='["用 200 包一切，把自定义 code 塞进 body","把版本号塞进请求体，或悄悄改变已有字段语义","DELETE 成功还返回响应体（204 不该有 body）","把过滤参数直接拼进 SQL，或放任 limit 无限大"]' summary="REST 的核心是让 HTTP 语义替你承担契约：状态码、幂等性、文档三者都不是可选项。" />

## 小结与下一讲

- 资源 + 方法 + 状态码构成接口骨架，201/204/400/422 各有明确分工。
- 幂等性不是理论洁癖，而是对「重试必然发生」的工程回应，靠服务端唯一键实现。
- 契约要机器可读：OpenAPI 由单一元数据生成，版本演进只加不删并给出弃用信号。

到目前为止我们处理的都是「一问一答」的短请求。可如果要服务端主动把消息推给浏览器呢？下一讲进入长连接世界。

[下一讲：WebSocket / SSE 与实时推送](/server/websocket)
