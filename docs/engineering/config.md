---
title: 第 56 讲 · 配置：flag / env / viper / 配置中心
description: 按十二要素把配置外置，掌握 flag 与环境变量优先级、配置校验、viper 与热加载灰度，避免配置硬编码
---

<StageHeader stage="L5 · 工程化与质量" level="★★★☆☆" time="40 分钟" goal="做到同一份镜像跑多套环境" prereq="第 55 讲 · 日志：slog / zap 与结构化采样" />

# 第 56 讲 · 配置：flag / env / viper / 配置中心

> 同一个二进制，在开发机连本地库、在预发连预发库、在生产连生产库，靠的应该是启动时注入的配置，而不是三份代码。配置硬编码最直接的代价是「改一个端口也要重新构建镜像」；更隐蔽的代价是密钥进了代码库、灰度靠发版。这一讲把配置从代码里请出去，并让它在启动那一刻就被校验清楚。

## 这一讲你会得到什么
<CheckList id="goal-56" title="本讲目标" :items='["理解十二要素的配置外置，说清各来源的优先级","用 flag 与环境变量加载配置，并在启动时完成校验","了解 viper 与配置中心在热加载、灰度里的位置，识别硬编码反模式"]' />

## 1. 先建立直觉

把程序想成一台咖啡机：豆子、水量、浓度是「配置」，机器本身是「代码」。换一种豆子不该把机器拆开重焊，只要从面板上换一项设置。十二要素应用讲的就是这件事——**代码与配置彻底分离**，同一份制品靠环境注入跑在不同环境。多个来源同时存在时，还要有明确的优先级，否则「到底听谁的」会变成线上谜案。

<ConceptDiagram type="pipeline" title="配置来源的优先级链" labels="默认值::代码内建兜底|配置文件::viper 读取|环境变量::部署时注入|启动参数::flag 最终覆盖" caption="优先级从低到高，最终只留一份经过 Validate 的运行时配置" />

一个实用的优先级约定是：代码内默认值最低，其次配置文件，再被环境变量覆盖，最后由启动参数兜底覆盖。这样本地调试可以用 flag 临时改一个值，而部署平台用环境变量统一注入，两者不冲突。

## 2. 语法与规则

第一块代码只用标准库实现「默认值 → flag → 环境变量」，并在返回前统一校验。

```go
// 配置外置：同一份二进制，靠环境变量适配多套环境
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
)

// Config 是唯一的运行时配置结构
type Config struct {
	Addr    string
	Workers int
	Debug   bool
}

// Load 按「默认值 → flag → 环境变量」的顺序覆盖
func Load() (Config, error) {
	addr := flag.String("addr", ":8080", "监听地址")
	workers := flag.Int("workers", 4, "工作协程数")
	debug := flag.Bool("debug", false, "调试模式")
	flag.Parse()

	c := Config{Addr: *addr, Workers: *workers, Debug: *debug}
	if v, ok := os.LookupEnv("APP_ADDR"); ok {
		c.Addr = v
	}
	if v, ok := os.LookupEnv("APP_WORKERS"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("APP_WORKERS 不是整数: %w", err)
		}
		c.Workers = n
	}
	return c, c.Validate()
}

// Validate 启动时就把非法配置挡在门外
func (c Config) Validate() error {
	if c.Workers == 0 {
		return fmt.Errorf("workers 不能为 0")
	}
	return nil
}

func main() {
	c, err := Load()
	if err != nil {
		fmt.Println("配置加载失败:", err)
		return
	}
	fmt.Println("监听:", c.Addr, "协程:", c.Workers, "调试:", c.Debug)
}
```

<OutputBox title="运行结果（未设置环境变量时）" output="监听: :8080 协程: 4 调试: false" note="设置 APP_ADDR=:9090 后，同一份二进制无需重新构建就换了监听地址。" />

第二块代码讲校验：配置文件读进来先解析、再逐项检查，非法值在启动阶段就失败，而不是等到某个请求打进来才崩。

```go
// 从配置文件读取并校验：加载阶段就把问题暴露出来
package main

import (
	"encoding/json"
	"fmt"
)

// Config 用 json tag 声明文件字段
type Config struct {
	Addr    string `json:"addr"`
	Timeout int    `json:"timeout_ms"`
	Mode    string `json:"mode"`
}

func Parse(raw string) (Config, error) {
	var c Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Config{}, fmt.Errorf("解析配置: %w", err)
	}
	if c.Addr == "" {
		return Config{}, fmt.Errorf("addr 不能为空")
	}
	if c.Timeout == 0 {
		return Config{}, fmt.Errorf("timeout_ms 不能为 0")
	}
	switch c.Mode {
	case "dev", "staging", "prod":
	default:
		return Config{}, fmt.Errorf("mode 只能是 dev/staging/prod，当前 %s", c.Mode)
	}
	return c, nil
}

func main() {
	for _, raw := range []string{
		`{"addr":":8080","timeout_ms":500,"mode":"prod"}`,
		`{"addr":":8080","timeout_ms":0,"mode":"prod"}`,
	} {
		c, err := Parse(raw)
		if err != nil {
			fmt.Println("失败:", err)
			continue
		}
		fmt.Println("成功:", c.Addr, c.Mode, c.Timeout)
	}
}
```

```text
成功: :8080 prod 500
失败: timeout_ms 不能为 0
```

第三块讲热加载：用 `atomic.Value` 整份替换，读侧永远看到完整的一份配置，不会读到改到一半的状态。

```go
// 热加载：用 atomic.Value 原子替换整份配置，读侧无锁
package main

import (
	"fmt"
	"sync/atomic"
)

type Config struct {
	RateLimit int
	GrayPct   int
}

// holder 保存当前生效的一份配置，写入是整体替换
var holder atomic.Value

func init() {
	holder.Store(Config{RateLimit: 100, GrayPct: 0})
}

// Reload 从远端拉到新值后整体替换，读侧永远看到完整一份
func Reload(c Config) {
	holder.Store(c)
}

func Current() Config {
	return holder.Load().(Config)
}

func main() {
	fmt.Println("初始:", Current())
	// 模拟一次远端推送：灰度比例从 0 放到 10
	Reload(Config{RateLimit: 200, GrayPct: 10})
	fmt.Println("热更新后:", Current())
}
```

```text
初始: {100 0}
热更新后: {200 10}
```

<LineByLine title="逐行看配置加载顺序" code="c := Config{Addr: defAddr}\nif v, ok := os.LookupEnv(APP_ADDR); ok {\n    c.Addr = v\n}\nreturn c, c.Validate()" :notes='["先用代码内默认值兜底","查询环境变量，ok 表示确实被设置过","存在才覆盖，避免空值把默认值冲掉","闭合条件","返回前统一校验，非法值启动即失败"]' />

## 3. 深入一步

不同来源该放什么，边界要清楚：

| 来源 | 适合放什么 | 变更是否需重新构建 |
| --- | --- | --- |
| 代码默认值 | 端口、超时等兜底值 | 是 |
| flag | 本地调试一次性覆盖 | 否 |
| 环境变量 | 环境差异、密钥引用 | 否 |
| 配置文件 / 配置中心 | 灰度比例、功能开关 | 否，可热加载 |

配置结构本身也该保持稳定：字段名一旦被多个服务引用，重命名就是一次跨仓库改动。因此把 `Config` 当作对外契约来维护，新增字段给默认值，废弃字段先标记再删，别因为一次重构就顺手改名字。校验逻辑跟着结构走，配置演进才不会变成线上事故。

<Term word="十二要素应用" text="一套构建可移植服务的方法论，其中「配置外置」要求把随环境变化的部分存于环境，而不是写进代码。" />

<Callout type="warn" title="易错点：配置硬编码" text="把 DSN、密钥、环境判断写死在代码里，或者让 if env == prod 散落几十处，是最常见的反模式。后果是一份镜像跑不了多套环境、密钥进代码库、灰度只能靠重新发版，且改配置必须走一遍完整构建。" />

## 4. 工程实践

标准库方案适合小项目；来源一多，就该让 `viper` 统一读取 YAML、TOML、环境变量并提供 `WatchConfig` 监听变化。但工具只是外壳，真正的工程判断有三条：**只在入口解析一次**、**解析完立刻 Validate**、**热加载整份替换而不是逐字段改**。配置中心则把「推送变更」这件事从本机文件升级成远程下发，灰度比例、功能开关这类值才值得做成可热更新的。

<Compare title="硬编码与外置校验" :panels='[{"label":"写法 A · 配置硬编码","code":"// 反例：环境差异写死在代码里\nconst dsn = root:secret@tcp(db.prod:3306)/app\nif env == prod { ... }\n// 换环境要改代码、重新构建镜像","note":"密钥进代码库，一份制品无法复用，灰度等于发版"},{"label":"写法 B · 外置加校验","code":"// 正例：入口解析一次\nc, err := Load()\nif err != nil {\n    return err\n}\n// 同一份镜像靠 env 适配","note":"启动时即拦截非法值，环境差异由注入解决"}]' />

还有两个细节容易出错：热加载若逐字段赋值，读侧可能看到新旧值混杂的状态，所以必须整份替换；`Validate` 只检查非空往往不够，端口范围、超时正数、枚举取值都要判，否则错误会延迟到运行期才暴露。另外，密钥本身不要进配置文件明文，交给环境变量或密钥管理，配置里只放引用。

<Callout type="tip" title="一条可执行的检查" text="在 CI 里加一个「空环境启动」用例：不带任何环境变量直接跑 Load，必须能被默认值兜住；再带一组非法值启动，必须立刻报错退出。" />

## 动手练一练

<Exercise title="给 Config 补上启动校验" task="为 Port 与 Timeout 两个字段写一个 Validate 方法，任一为 0 就返回错误，避免非法配置流到运行期。" :hints='["方法签名用值接收者即可，返回 error","用 fmt.Errorf 给出可读的错误信息","两个字段都要检查"]' solution="func (c Config) Validate() error {\n    if c.Port == 0 {\n        return fmt.Errorf(port_invalid)\n    }\n    if c.Timeout == 0 {\n        return fmt.Errorf(timeout_invalid)\n    }\n    return nil\n}" explain="校验集中在 Load 末尾调用，非法配置在进程启动阶段就失败，而不是等请求打进来才崩。" />

## 随堂检测

<Quiz no="56-1" q="按本讲约定的优先级，flag 与环境变量同时设置同一项时，谁生效？" :options='["flag，因为它是最终覆盖层","环境变量，因为它来自部署平台","代码默认值","随机，取决于设置顺序"]' answer="0" explain="约定中启动参数处于最高优先级，便于本地调试临时覆盖，而又不影响部署平台用环境变量统一注入。" />

<Predict no="56-2" q="设置 APP_ADDR=:9090 且传入 -addr=:8080，按本讲 Load 的实现，最终监听地址是？" :options='[":8080",":9090","空地址","报错退出"]' answer="1" output=":9090" explain="Load 中环境变量的覆盖发生在 flag.Parse 之后，因此 APP_ADDR 最终生效。" />

## 一页纸速记

<KnowledgeCard :points='["代码与配置分离，同一份镜像靠环境注入跑多套环境","优先级明确：默认值、配置文件、环境变量、启动参数","只在入口解析一次，解析完立刻 Validate","热加载整份替换，读侧永远看到完整配置"]' :pitfalls='["配置硬编码：DSN、密钥、环境判断写死在代码里","逐字段热更新，导致读侧看到新旧混杂","密钥明文进配置文件，Validate 只判非空"]' summary="配置的工程目标是可移植：换环境不改代码、不重新构建，且在启动那一刻就知道配置是否合法。" />

## 小结与下一讲

- 十二要素要求配置外置；多来源必须有明确的优先级约定。
- 入口解析一次并立刻校验，非法值在启动阶段失败。
- viper 与配置中心服务于热加载与灰度；整份替换、密钥外置是底线。

[下一讲：错误码、错误栈与统一响应](/engineering/error-design)
