---
title: 第 08 讲 · 字符串、rune 与高效拼接
description: 理解 UTF-8 与不可变，写出不乱码也不慢的字符串代码
---

<StageHeader stage="L1 · 编程地基" level="★★☆☆☆" time="30 分钟" goal="理解 UTF-8 并写出不乱码、不慢的字符串代码" prereq="第 07 讲" />

# 第 08 讲 · 字符串、rune 与高效拼接

> 对中文开发者来说，字符串往往是第一道真正的坎：`len("中文")` 为什么不是 2？用下标取出来的为什么是乱码？循环里用加号拼一段日志为什么会把 CPU 吃满？三个问题背后其实是同一件事——Go 的 `string` 是一段只读的 UTF-8 字节序列。这一讲把不可变、字节与字符、遍历方式、以及高效拼接一次讲透，之后写字符串既不乱码也不慢。

## 这一讲你会得到什么

<CheckList id="L1-08" title="本讲目标" :items='["能解释 len 返回字节数而不是字符数，知道怎么数字符","能说清 string 不可变意味着什么，以及为什么不能直接改下标","能用 range 正确遍历含中文的字符串，看懂索引是字节偏移","知道循环拼接为什么慢，会用 strings.Builder 与 strings.Join"]' />

## 1. 先建立直觉

把字符串想成一串**封死的珠子**：每颗珠子是一个字节，串好后就不能替换其中任何一颗，想要变样只能另串一串。这里的麻烦在于，"一个字"和"一颗珠子"并不一一对应：英文字符一颗珠子就够了，一个汉字在 UTF-8 里却要三颗。于是 `len` 数的是珠子（字节），而我们脑子里数的是字（字符）。`byte` 关心单颗珠子，`rune` 关心"一个完整的字"，`range` 则是懂得把三颗珠子合起来读成一个字的读法。

<ConceptDiagram type="slice-header" title="string 是一个描述符加一段只读的连续字节" />

借这张切片图看结构：变量本身只是个描述符，含一个指向底层数据的指针和一个长度，真正的字节在别处连续存放。string 的头部与之类似，区别是那段字节**只读**。这正好解释了两个现象：长度字段记的是字节数，以及任何"改动"都得另开一块内存。底层结构的精确定义留到第 31 讲，这里先借用这幅图建立直觉。

## 2. 语法与规则

先看最容易算错的一个数：

```go
package main

import (
	"fmt"
	"unicode/utf8"
)

func main() {
	s := "Go中文"
	fmt.Println("字节数 len：", len(s))
	fmt.Println("字符数：", utf8.RuneCountInString(s))
	fmt.Println("下标第 0 个字节：", s[0])
	for i, r := range s {
		fmt.Printf("字节偏移 %d 处是字符 %c\n", i, r)
	}
}
```

<LineByLine code="for i, r := range s {\n    fmt.Println(i, r)\n}" :notes='["range 会按 UTF-8 解码，每次给出一个字节偏移 i 和一个 rune r","i 是字节偏移而不是第几个字符，遇到中文会跳着前进"]' />

<OutputBox title="运行结果" output="字节数 len： 8\n字符数： 4\n下标第 0 个字节： 71\n字节偏移 0 处是字符 G\n字节偏移 1 处是字符 o\n字节偏移 2 处是字符 中\n字节偏移 5 处是字符 文" guess="true" />

`Go中文` 里两个英文字母各占 1 字节、两个汉字各占 3 字节，合计 8 字节、4 个字符。`s[0]` 拿到的是第一个**字节**（`G` 的编码 71），不是第一个字符；想数字符请用 `utf8.RuneCountInString`。`range` 的索引 `i` 是字节偏移，所以从"中"的 2 直接跳到"文"的 5——这不是 bug，正是 UTF-8 变长编码的证据。

第二段看不可变。字符串不能按下标改写，转换和拼接都会产生新数据：

```go
package main

import "fmt"

func main() {
	s := "Go"
	bs := []byte(s) // 转换会拷贝出一份可修改的字节切片
	bs[0] = 'H'     // 改的是副本，不是原串
	fmt.Println("原串：", s)
	fmt.Println("副本：", string(bs))
	t := s + "lang" // 加号拼接得到全新的字符串
	fmt.Println("拼接：", t)
}
```

<OutputBox title="运行结果" output="原串： Go\n副本： Ho\n拼接： Golang" />

直接写 `s[0] = 'H'` 会**编译报错**：string 是只读的。想改内容，只能先转成 `[]byte`（这一步会拷贝）改完再转回来；而 `s + "lang"` 也不会原地追加，而是分配一块新内存、把两边都拷进去。<Term word="不可变" text="string 一旦创建，其字节内容不能被修改；任何改动都会生成一份新字符串。" />

第三段看拼接工具。循环里反复用加号，代价来自上一句结论：

```go
package main

import (
	"fmt"
	"strings"
)

func main() {
	var b strings.Builder
	for i := 0; i < 3; i++ {
		b.WriteString("Go") // 只往缓冲区追加，不产生临时字符串
	}
	fmt.Println("拼接结果：", b.String())
	fmt.Println("缓冲长度：", b.Len())
}
```

<OutputBox title="运行结果" output="拼接结果： GoGoGo\n缓冲长度： 6" />

`strings.Builder` 内部是一块可增长的缓冲，`WriteString` 只往里追加，最后 `String()` 才生成一次结果。用加号循环拼接时，每轮都要分配新串并拷贝已有内容，总工作量近似随长度平方增长；Builder 把它压成线性。

## 3. 深入一步

| 场景 | 推荐写法 | 原因 |
| --- | --- | --- |
| 两三个固定片段 | 加号直接拼 | 简单直观，编译器会优化 |
| 已有字符串切片 | `strings.Join` | 一次算出总长、一次分配 |
| 循环中逐步追加 | `strings.Builder` | 可增长缓冲，避免反复拷贝 |
| 带格式的拼装 | `fmt.Sprintf` | 可读性优先，但热路径偏慢 |
| 混入大量非字符串 | `fmt.Fprintf` 写 Builder | 少一次中间字符串 |

<Term word="UTF-8" text="变长编码：英文 1 字节，常用汉字 3 字节；Go 源码与 string 默认都用它。" />

<Callout type="warn" title="四个易错点" text="第一，len(s) 是字节数，别拿来当字符个数或做截断，切中文切一半就是乱码。第二，s[i] 取到的是字节，试图用 s[i] 赋值会编译失败。第三，strings.Builder 复制后再使用会 panic，它内部持有状态，别按值传参。第四，for range 的循环变量在 Go 1.22 起改为每次迭代独立声明，老代码里把它取地址存起来的写法结果会变，第 15 讲专门拆解这个坑。" />

<Compare :panels='[{"label":"循环里用加号","code":"for i := 0; i != n; i++ {\n    s += 片段\n}","note":"每轮分配新串并全量拷贝，长度大时开销近似平方级"},{"label":"用 Builder","code":"var b strings.Builder\nfor i := 0; i != n; i++ {\n    b.WriteString(片段)\n}","note":"往同一块缓冲追加，最后只生成一次字符串"}]' title="同一个循环，两种拼接代价" />

<CodeTabs :tabs='[{"label":"Join","code":"strings.Join(parts, sep)","note":"已有切片时最省事，一次分配"},{"label":"Builder","code":"b.WriteString(part)","note":"逐步追加，最后 b.String()"},{"label":"Sprintf","code":"fmt.Sprintf(format, args)","note":"带格式时可读，但不要放进高频循环"}]' title="按场景挑拼接方式" />

<FlowTimeline :steps='[{"title":"读一个字节","desc":"看高位判断这个字符占几字节","tone":"blue"},{"title":"凑齐一整个字符","desc":"按 UTF-8 规则解码成一个 rune","tone":"green"},{"title":"给出字节偏移","desc":"索引是起始位置，会跳着前进","tone":"orange"},{"title":"交给循环体","desc":"拿到的是字符，不是残缺字节","tone":"purple"}]' direction="horizontal" />

## 4. 工程实践

真实服务里，字符串开销经常藏在日志与协议拼装这类"看起来不值一提"的地方。经验是：拼接次数少就直接用加号，别过早优化；一旦出现在循环或高频路径，就换 `strings.Builder`，并且先 `b.Grow(预估长度)` 让缓冲一次到位，能再省掉几次扩容。已有 `[]string` 时 `strings.Join` 最省事，它会先算总长度再分配一次。

格式化方面，`fmt.Sprintf` 可读性好但会反射解析格式串、产生中间分配，热路径上应换成结构化的日志接口（阶段五会讲 `slog`）或直接写 Builder。还有一个容易忽略的边界：`string` 与 `[]byte` 互转会拷贝整段数据，处理大 body 时这份拷贝并不便宜；第 31 讲会讲 `unsafe` 零拷贝转换的适用条件与风险，业务代码默认不要碰。

<Callout type="tip" title="一条判断准则" text="先用正确的写法（range 遍历、RuneCountInString 数字数），再谈优化。乱码是正确性问题，慢是性能问题，顺序不能反；等真的测出瓶颈再换 Builder，往往比一上来就到处套优化更省心。" />

## 动手练一练

<Exercise title="用 Builder 代替加号" task="已有两个字符串变量 a 和 b，请用 strings.Builder 把它们依次写入并打印结果，不要用加号拼接。" :hints='["先声明一个 strings.Builder 变量，注意别和变量 b 重名","逐次调用 WriteString，最后用 String 取结果"]' solution="var sb strings.Builder\nsb.WriteString(a)\nsb.WriteString(b)\nfmt.Println(sb.String())" explain="Builder 把多次追加收进同一块缓冲，最后只生成一次字符串；按值复制 Builder 再使用会 panic，所以要用同一个变量。" />

## 随堂检测

<Quiz no="08-1" q="关于 Go 的 string，下面哪一项说法是错误的？" :options='["len 返回的是字节数而不是字符数","string 不可变，s[0] 赋值会编译报错","for range 遍历字符串时，索引一定是连续的 0、1、2","把 string 转成 []byte 会拷贝出一份新数据"]' answer="2" explain="range 的索引是字节偏移，UTF-8 变长编码下遇到汉字会跳着走，并不连续。" />

<Predict no="08-2" q="表达式 len(Go中文) 的结果是多少？" :options='["4","6","8","2"]' answer="2" explain="两个英文字母各 1 字节，两个汉字在 UTF-8 里各 3 字节，合计 8 字节；字符数才是 4。" />

<FillBlank no="08-3" tpl="len 返回的是字符串的 ___ 数\n按字符遍历应该用 for ___ s\n循环拼接推荐用 strings.___" :answers='["字节~byte","range","Builder"]' hint="第一空数的是字节而不是字符，第二空是遍历关键字，第三空是拼接用的类型。" explain="len 数字节，range 按 rune 解码遍历，Builder 用可增长缓冲承担循环追加。" />

## 一页纸速记

<KnowledgeCard :points='["string 是只读的 UTF-8 字节序列，len 数字节，RuneCountInString 数字符","s[i] 取到的是字节；range 按 rune 解码，索引是字节偏移会跳着走","不可变意味着任何改动都另开内存，转换 []byte 也会拷贝","循环拼接用 strings.Builder，已有切片用 Join，固定少量用加号"]' :pitfalls='["别用 len 或下标截断中文，切一半就是乱码","别按值复制 strings.Builder 后再使用，会 panic","别在热路径用 fmt.Sprintf 反复拼字符串"]' summary="先分清字节与字符，再选对拼接方式，正确性和性能就都稳了。" />

## 小结与下一讲

- `string` 是只读 UTF-8 字节序列，`len` 是字节数，字符数要用 `utf8.RuneCountInString`。
- `range` 按 rune 解码、索引是字节偏移；不可变让拼接与转换都要另分配内存。
- 拼接按场景选：少量用加号、切片用 `Join`、循环用 `Builder`、格式化用 `Sprintf`。

值为什么会复制、变量又住在哪里——下一讲把内存模型这张底图补上。

[下一讲：栈、堆、值语义与指针](/foundation/memory)
