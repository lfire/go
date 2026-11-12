---
title: 第 103 讲 · 实战 01 · 待办清单 CLI
description: 用命令树、JSON 持久化、原子写与退出码契约交付第一个完整命令行工具
---

<StageHeader stage="L10 · 工程实战" level="★☆☆☆☆" time="50 分钟" goal="从零交付第一个完整命令行工具" prereq="第 102 讲 · 资深能力图谱、技术选型与持续精进" />

# 第 103 讲 · 实战 01 · 待办清单 CLI

> 真正的工具要能反复调用：命令怎么组织、数据落在哪里、崩溃会不会写坏文件、脚本靠什么判断成败。本讲只用标准库把这些问题走完。

## 这一讲你会得到什么
<CheckList id="l103-goals" title="本讲目标" :items='["把命令组织成命令树，理解 cobra 的本质","用原子替换避免半截文件","用退出码区分用户输错与系统故障"]' />

## 1. 项目目标与约束

| 维度 | 目标 | 约束 |
| --- | --- | --- |
| 功能 | 增、列、完成、删除 | 不做协作 |
| 存储 | 单文件 JSON | 全量序列化，上万条后变慢 |
| 依赖 | 零第三方 | 命令树自己写 |
| 可靠性 | 崩溃不留半截文件 | 临时文件加重命名 |
| 可集成 | 退出码可判定 | 错误必须分类 |

零依赖这条最容易被跳过。生产直接用 cobra 即可，但先手写命令树才明白它只提供按名字分发的树、每层一组 flag 与统一的帮助输出。

## 2. 架构蓝图

<ConceptDiagram type="layout" title="待办 CLI 的分层" caption="命令层解析分发，领域层定义 Task，存储层独占文件读写，依赖指向稳定的领域层" />

分三层是为隔离变化：命令层因加子命令频繁改，存储层因换 SQLite 整体重写，领域层最稳定。若命令处理里直接写文件，换存储时每个子命令都得改。校验放在进入存储层之前，空标题在命令层就拒绝。

## 3. 关键模块实现

### 3.1 领域模型与存储：原子写是核心

```go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Task 是领域模型；ID 由 Store 分配，Done 表示是否已完成。
type Task struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// Store 独占文件与内存态；mu 让多个并发命令不会互相覆盖。
type Store struct {
	mu   sync.Mutex
	path string
	data []Task
}

// OpenStore 把「文件不存在」当成合法的首次运行，而不是错误。
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取存储文件: %w", err)
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("存储文件损坏: %w", err)
	}
	return s, nil
}

// Add 追加一条任务，返回分配到的 ID。
func (s *Store) Add(title string) (Task, error) {
	if title == "" {
		return Task{}, errors.New("标题不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	maxID := 0
	for _, t := range s.data {
		if t.ID > maxID {
			maxID = t.ID
		}
	}
	t := Task{ID: maxID + 1, Title: title}
	s.data = append(s.data, t)
	if err := s.flush(); err != nil {
		return Task{}, err
	}
	return t, nil
}

// flush 先写临时文件再原子重命名，避免崩溃时留下半截 JSON。
func (s *Store) flush() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("写临时文件: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("原子替换: %w", err)
	}
	return nil
}

// List 返回副本，调用方改动不会穿透到内部切片。
func (s *Store) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, len(s.data))
	copy(out, s.data)
	return out
}

func main() {
	dir, _ := os.MkdirTemp("", "todo-")
	defer os.RemoveAll(dir)
	s, err := OpenStore(filepath.Join(dir, "todo.json"))
	if err != nil {
		fmt.Println("打开失败:", err)
		return
	}
	s.Add("写第 103 讲")
	s.Add("核对示例代码")
	for _, t := range s.List() {
		fmt.Printf("%d\t%s\t%v\n", t.ID, t.Title, t.Done)
	}
}
```

用 errors.Is 把首次运行建模成正常状态；List 返回副本，否则调用方一句赋值就能绕过锁改内部状态。

### 3.2 命令树

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// Command 是命令树节点；每个子命令有独立的 FlagSet 与 Run。
type Command struct {
	Name  string
	Usage string
	Run   func(args []string) error
	Sub   map[string]*Command
}

func (c *Command) Execute(argv []string) error {
	if len(argv) == 0 {
		return c.help()
	}
	if sub, ok := c.Sub[argv[0]]; ok {
		return sub.Execute(argv[1:])
	}
	if argv[0] == "-h" || argv[0] == "help" {
		return c.help()
	}
	if c.Run == nil {
		return fmt.Errorf("未知命令 %q", argv[0])
	}
	return c.Run(argv)
}

func (c *Command) help() error {
	fmt.Printf("用法: %s 子命令\n", c.Name)
	for name, sub := range c.Sub {
		fmt.Printf("  %-8s %s\n", name, sub.Usage)
	}
	return nil
}

func main() {
	add := &Command{Name: "add", Usage: "add -title 文字 新增任务"}
	add.Run = func(args []string) error {
		fs := flag.NewFlagSet("add", flag.ContinueOnError)
		title := fs.String("title", "", "任务标题")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if strings.TrimSpace(*title) == "" {
			return fmt.Errorf("缺少 -title")
		}
		fmt.Println("已新增:", *title)
		return nil
	}
	list := &Command{Name: "list", Usage: "列出全部任务", Run: func([]string) error {
		fmt.Println("(此处调用 Store.List)")
		return nil
	}}
	root := &Command{Name: "todo", Sub: map[string]*Command{"add": add, "list": list}}
	if err := root.Execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(2)
	}
}
```

Execute 先按首个参数分发，余下参数交给该子命令自己的 FlagSet，各子命令 flag 互不干扰。cobra 补的是自动帮助与补全，骨架没变。

### 3.3 退出码契约

```go
package main

import (
	"errors"
	"fmt"
	"os"
)

// 用哨兵错误把「用户输错」和「系统坏掉」分开。
var (
	ErrUsage    = errors.New("参数错误")
	ErrNotFound = errors.New("任务不存在")
)

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrUsage):
		return 2
	case errors.Is(err, ErrNotFound):
		return 3
	default:
		return 1 // 存储损坏、IO 失败等系统级错误
	}
}

func complete(id int) error {
	if id <= 0 {
		return fmt.Errorf("%w: id 必须为正整数", ErrUsage)
	}
	if id > 100 {
		return ErrNotFound
	}
	return nil
}

func main() {
	for _, id := range []int{0, 5, 999} {
		err := complete(id)
		fmt.Printf("id=%d 退出码=%d 错误=%v\n", id, exitCode(err), err)
	}
	os.Exit(0)
}
```

分三类是因为动作不同：2 表示改参数再来、3 表示目标没了、1 表示系统故障值得告警。一律 exit 1 会让脚本盲目重试。

## 4. 失败路径与边界条件

文件损坏包装成可读错误并退出 1；并发 Add 由互斥锁串行化；空标题先 TrimSpace；ID 越界在命令层拦截；写满时 flush 失败但不 rename，旧文件仍在。最易漏的是删后增：按 len 分配 ID 会与历史撞号，所以实现取最大 ID 加一。

<Callout type="warn" title="易错点" text="List 返回内部切片会让调用方绕过锁；rename 失败要清理临时文件。" />

## 5. 测试与压测思路

用 t.TempDir 做表驱动测试，覆盖首次创建、损坏文件、删后增；起百个 goroutine 同时 Add 跑 go test -race；Benchmark 塞万条任务观察单次 Add 全量 Marshal 的线性增长，那就是换存储的明确信号。

## 6. 可继续扩展的方向

把 Store 抽成接口，JSON 与 SQLite 各写一份，是第 111 讲 KV 贯穿线的雏形；用 cobra 重写命令层；加 due、tag 靠 omitempty 兼容；用 go install 与交叉编译发布。

## 随堂检测

<Quiz no="103-1" q="为什么落盘要先写临时文件再 rename？" :options='["读得更快","避免崩溃时留下写了一半的文件","JSON 只能写进 .tmp"]' answer="1" explain="同目录 rename 是原子操作，读者只看到旧版或新版；直接覆盖在写入中途被杀会留下无法解析的半截内容。" />

## 一页纸速记

<KnowledgeCard :points='["三层单向依赖，变化被隔离","文件不存在是正常状态，损坏才是错误","临时文件加 rename 是持久化可靠性底线"]' :pitfalls='["按 len 分配 ID 会撞号","List 返回内部切片会绕过锁"]' summary="小项目的价值是把失败路径想全，而不是把功能堆多。" />

## 小结与下一讲

- 分层让换命令与换存储互不牵连。
- 原子替换、锁、退出码分别解决崩溃、并发、可集成三类失败。
- 用表驱动测试、-race 与 Benchmark 量出全量序列化的天花板。

[下一讲：实战 02 · protoc 插件与代码生成器](/projects/cli-codegen)
