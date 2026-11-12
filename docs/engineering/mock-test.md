---
title: 第 53 讲 · Mock：gomock / sqlmock / httptest
description: 用依赖倒置留出可替换的缝隙，掌握手写 fake、gomock 生成、sqlmock 与 httptest，并认清 mock 过度与 gomonkey 的边界
---

<StageHeader stage="L5 · 工程化与质量" level="★★★★☆" time="45 分钟" goal="隔离外部依赖，测真正的逻辑" prereq="第 52 讲 · 单元测试与表驱动测试" />

# 第 53 讲 · Mock：gomock / sqlmock / httptest

> 单测一旦连上真实数据库或真实第三方接口，就不再是单测了：它慢、偶发失败、还得准备一堆外部状态。Mock 不是「把依赖替换掉」这么一句口号，而是先问清楚——**哪些是系统边界，哪些是自己的逻辑**。这一讲把边界划出来，再比较几种替换手段，最后重点讲什么时候不该 mock。

## 这一讲你会得到什么
<CheckList id="goal-53" title="本讲目标" :items='["用依赖倒置把外部依赖变成可替换的接口缝","按场景在手写 fake、gomock、sqlmock、httptest 之间做选择","识别 mock 过度，理解 gomonkey 为何只适合遗留代码兜底"]' />

## 1. 先建立直觉

把业务服务想成一台机器：规则、计算、状态流转是机器内部的齿轮，数据库、HTTP 接口、当前时间是接在机器外面的水管。测试内部齿轮时，你完全可以让齿轮真实咬合，只把外面的水管换成一根可控的短管。所谓依赖倒置，就是让机器按「我需要什么形状的接口」来设计接头，而不是焊死在某根具体水管上。

<ConceptDiagram type="architecture-layer" title="依赖倒置：接口是替换缝隙" />

关键判断在于**接口由谁定义**：应该由使用方（业务层）声明「我需要 Get 方法」，而不是由数据库驱动反过来规定业务层长什么样。这样测试替身和真实实现是平级的两个插头，换谁都不动机器。

## 2. 语法与规则

第一块代码是最小也最被低估的方案：手写 fake。逻辑简单时，几行结构体比生成一整个 mock 文件更清楚。

```go
// user_test.go —— 单独一个文件即可 go test 通过
package user

import (
	"fmt"
	"testing"
)

// Repo 是业务层声明的接口：替换缝隙由使用方定义。
type Repo interface {
	NameByID(id int) (string, error)
}

// Service 只依赖接口，真实实现与测试替身都从构造函数进来。
type Service struct{ repo Repo }

func New(r Repo) Service { return Service{repo: r} }

func (s Service) Greet(id int) (string, error) {
	name, err := s.repo.NameByID(id)
	if err != nil {
		return "", fmt.Errorf("问候失败: %w", err)
	}
	return "你好, " + name, nil
}

// fakeRepo 是手写替身：只需固定返回值时，它最直观。
type fakeRepo struct {
	data map[int]string
	err  error
}

func (f fakeRepo) NameByID(id int) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.data[id], nil
}

func TestGreet(t *testing.T) {
	cases := []struct {
		name string
		repo Repo
		want string
	}{
		{"正常", fakeRepo{data: map[int]string{1: "ada"}}, "你好, ada"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := New(c.repo).Greet(1)
			if err != nil {
				t.Fatalf("不应出错: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
```

<OutputBox title="go test -v 运行结果" output="=== RUN   TestGreet\n=== RUN   TestGreet/正常\n--- PASS: TestGreet (0.00s)\n    --- PASS: TestGreet/正常 (0.00s)\nPASS\nok      user    0.001s" note="接口在哪一层定义，决定了替换成本：定义在使用方，业务代码一行都不用改。" />

第二块用标准库 `httptest.NewRecorder` 测 Handler，不需要监听任何真实端口。

```go
// handler_test.go —— 用标准库 httptest 测 Handler，无需真实端口
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Ping 是被测处理器（示例起见与测试同文件）
func Ping(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func TestPing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	Ping(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 got %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type got %q", ct)
	}
	if got := rec.Body.String(); got != `{"ok":true}` {
		t.Errorf("响应体 got %q", got)
	}
}
```

第三块反过来测「调用外部 API 的客户端」：`httptest.NewServer` 起一个临时服务，把它的 `Client()` 注入进去，连超时都由 context 控制。

```go
// client_test.go —— 外部 HTTP 依赖用 httptest.NewServer 顶替
package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// FetchName 是被测函数（与测试同文件，保证单文件可 go test）
func FetchName(ctx context.Context, c *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func TestFetchName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ada"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := FetchName(ctx, srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("不应出错: %v", err)
	}
	if got != "ada" {
		t.Errorf("got %q, want ada", got)
	}
}
```

<LineByLine title="逐行看替换缝隙" code="type Repo interface {\n    Get(id int) (string, error)\n}\ntype Service struct{ repo Repo }\nfunc New(r Repo) Service {\n    return Service{repo: r}\n}" :notes='["接口由使用方定义，这是可替换的缝隙","方法签名用业务语言，不暴露驱动细节","接口闭合","服务只持有接口字段","构造函数是唯一注入点","按值返回即可","依赖方向不再往下延伸"]' />

## 3. 深入一步

生成式与专用 mock 有自己的位置。`gomock` 适合校验「被调用了几次、参数是什么」，`sqlmock` 适合只验证 SQL 与扫描逻辑而不起真库：

```bash
go install go.uber.org/mock/mockgen@latest
# 接口文件里写一行：//go:generate mockgen -source=repo.go -destination=mock_repo.go -package=user
go generate ./...
```

<CodeTabs title="三种替换手段的写法对照" :tabs='[{"label":"gomock","code":"ctrl := gomock.NewController(t)\ndefer ctrl.Finish()\nm := NewMockRepo(ctrl)\nm.EXPECT().Get(1).Return(name, nil)","note":"校验调用参数与次数，需先用 mockgen 生成 Mock 类型"},{"label":"sqlmock","code":"db, mock, err := sqlmock.New()\nmock.ExpectQuery(...).WillReturnRows(...)\nrepo := New(db)","note":"只验证 SQL 文本与扫描，不起真实数据库"},{"label":"gomonkey","code":"patches := gomonkey.ApplyFunc(time.Now, ...)\ndefer patches.Reset()\n// 必须 go test -gcflags=-l","note":"运行时改写函数指令，新项目不推荐"}]' />

| 方案 | 隔离对象 | 成本 | 适用与边界 |
| --- | --- | --- | --- |
| 手写 fake | 自己定义的接口 | 最低，几行结构体 | 只需固定返回值或错误 |
| gomock | 接口，代码生成 | 需 mockgen 与 go generate | 需要校验调用次数与参数 |
| sqlmock | database/sql 驱动 | 需引第三方包 | 只想验证 SQL 与扫描 |
| httptest | HTTP 边界 | 标准库，零依赖 | Handler 与外部 API 客户端 |
| gomonkey | 任意函数 | 需关内联、非并发安全 | 无法改动的遗留代码兜底 |

从成本看，接口缝一旦开好，手写 fake 与生成 mock 可以混用：大多数用例用几行 fake 固定返回值，只有少数需要验证「是否调用、调用几次」的用例才动用 gomock。把生成代码当成默认方案，反而会让简单用例背上不必要的模板负担。

<Term word="测试替身" text="在测试中顶替真实依赖的对象统称，包含 fake、stub、mock、spy；mock 特指能校验交互行为的那一类。" />

<Callout type="warn" title="易错点：gomonkey 的边界" text="gomonkey 靠运行时改写函数指令打桩，必须关闭内联（go test -gcflags=-l），对 Go 版本与编译器优化敏感，且并发下不安全。它只该用于「代码不能改」的遗留场景；新代码优先用接口加构造注入。" />

## 4. 工程实践

最常见的反模式是 **mock 过度**：把内部纯函数、工具方法、甚至被测对象自己的方法全打上桩，结果测试变成「验证 mock 的配置是否正确」，一重构就大面积报红，却从没抓到过真实 bug。判断标准很简单——只在系统边界替换：网络、数据库、时间、随机数、第三方 SDK；内部逻辑一律真跑。

接口的粒度也值得推敲：一个 `Repo` 若塞了二十个方法，替身就被迫实现一堆用不到的空方法，测试用例反而被接口污染拖累。按用例需要声明一两个方法的小接口，让替身保持几行代码，这是 Go 小接口原则在测试里的直接收益。

<Compare title="mock 过度与克制" :panels='[{"label":"写法 A · 全打桩","code":"// 反例：连纯函数都打桩\nctrl.EXPECT().Add(1, 2).Return(3)\n// 测试只验证了 mock 配置本身","note":"重构内部实现时测试全红，却测不到任何真实行为"},{"label":"写法 B · 只换边界","code":"// 正例：只替换真正的外部边界\nsrv := httptest.NewServer(...)\ndefer srv.Close()\n// 内部逻辑照常真实执行","note":"内部齿轮真实咬合，测试才真正保护业务逻辑"}]' />

另一个取舍是：`sqlmock` 能测出「SQL 写错了」，但测不出「SQL 在真实库上跑不通」，所以它不能替代集成测试；`httptest` 同理，它保证的是 Handler 的输入输出契约，不是真实网络下的行为。把这些工具摆在测试金字塔的合适层位，比多装几个库重要得多。

<Callout type="tip" title="一条可执行的检查" text="写测试前先问：这个依赖在 CI 里能不能稳定、免费、毫秒级地跑？答案是否，才考虑替换；答案是是，就让它真跑。" />

## 动手练一练

<Exercise title="把真实调用改成接口注入" task="下面函数直接调用 http.Get，导致测试必须联网。请把它改成接收一个接口，使测试可以传入固定值替身。\nfunc Load(url string) (string, error) {\n    resp, err := http.Get(url)\n    ...\n}" :hints='["先声明一个只有 Get 方法的接口","让函数或结构体从参数接收该接口","测试里传一个返回固定字符串的替身"]' solution="type Fetcher interface {\n    Get(url string) (string, error)\n}\nfunc Use(f Fetcher, url string) (string, error) {\n    return f.Get(url)\n}" explain="把外部 HTTP 收敛成接口后，测试替身与真实实现平级；既不需要联网，也不需要 gomonkey 打桩。" />

## 随堂检测

<Quiz no="53-1" q="关于依赖倒置中「接口该由谁定义」，哪项最符合 Go 的实践？" :options='["由数据库驱动定义，业务层去实现","由使用方（业务层）按自己需要声明小接口","放在一个公共 types 包里统一定义","用空接口 any 最灵活"]' answer="1" explain="接口由使用方声明，才能做到替换实现不动业务代码；小接口也更容易用 fake 顶替。" />

<Predict no="53-2" q="用 httptest.NewRecorder() 承接一个先 WriteHeader(http.StatusOK) 的 Handler，调用结束后 rec.Code 是多少？" :options='["0","200","500","-1"]' answer="1" output="200" explain="NewRecorder 默认 Code 为 200，显式 WriteHeader 之后保持该值；它只是内存里的 ResponseWriter 实现。" />

## 一页纸速记

<KnowledgeCard :points='["接口由使用方定义、实现由构造函数注入，是 mock 的前提","逻辑简单用手写 fake，需校验交互用 gomock","sqlmock 测 SQL 文本，httptest 测 HTTP 边界，都是标准边界替换","只在网络、数据库、时间、第三方这些边界替换"]' :pitfalls='["mock 过度：把内部纯函数也打桩，测试只验证 mock 配置","用 gomonkey 打桩新代码，忘了关内联或与 -race 冲突","把 sqlmock / httptest 当成集成测试的替代品"]' summary="Mock 的价值是隔离边界、保护真实逻辑，而不是把整台机器都换成假的。" />

## 小结与下一讲

- 依赖倒置留出接口缝，替换实现不动业务代码，这是可测试性的根。
- 手写 fake 最轻，gomock 校验交互，sqlmock 与 httptest 各守一类边界。
- 反模式是 mock 过度；gomonkey 只是遗留代码的兜底，不作为常规手段。

[下一讲：基准测试、模糊测试与覆盖率](/engineering/bench-fuzz)
