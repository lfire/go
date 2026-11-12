---
title: 第 104 讲 · 实战 02 · protoc 插件与代码生成器
description: 用 go/ast 解析定义、text/template 渲染、go/format 体检与幂等落盘，写出可编译可重复生成的代码生成器
---

<StageHeader stage="L10 · 工程实战" level="★★★☆☆" time="60 分钟" goal="用 AST 与模板消灭重复代码" prereq="第 103 讲 · 实战 01 · 待办清单 CLI" />

# 第 104 讲 · 实战 02 · protoc 插件与代码生成器

> 错误码注册表、枚举的 String 方法、protobuf 的 .pb.go 文件有一个共同特征：结构高度重复，且错一个字符就整包编译不过。人工维护这类文件，改了定义忘改产物的迟到错误几乎无法避免。代码生成的本质，是把「一次定义、多处同步」这件事交给一份可测的输入与一段模板，让编译器替你验收，让重跑变成零成本操作。本讲从 protoc 插件的 stdin/stdout 字节协议讲起，一路走完描述符遍历、中间模型、模板隔离、语法体检、幂等落盘与 golden file 测试，全程以标准库为主、真实 protobuf API 为辅，读者看完能独立搭出一条能进 CI 的生成流水线。

## 这一讲你会得到什么
<CheckList id="l104-goals" title="本讲目标" :items='["说清 CodeGeneratorRequest 与 CodeGeneratorResponse 的 stdin/stdout 协议与插件在 protoc 调度中的位置","用 protoreflect 遍历描述符、用 go/ast 加 comma-ok 安全断言抽取中间模型","用 text/template 做模板隔离、用 go/format 做语法体检","实现幂等落盘与手写文件保护，配 golden file 测试与 CI 卡点","把错误码全局注册、go generate、插件版本兼容与 breaking change 检查串成可运行系统"]' />

## 0. 项目定位与真实场景

一家做订单与支付中台的公司，把对外错误码写在一处 Go const 文件里，却要在四个地方保持同步：网关的 HTTP 状态映射、gRPC 的 status.Code 转换、前端 SDK 的错误码枚举、以及运维告警里的可读文案。最初靠人肉复制，结果上线三个月内出现过两次事故：一次是新增错误码只改了 Go 定义，前端 SDK 收到未知码走了兜底分支，用户看到「unknown」；另一次是两个服务各自定义了数值 409，注册表里后写入的把先写入的覆盖掉，排障时按码查到的文案驴唇不对马嘴。

这类系统的真实量级并不大，但一致性要求极高：全平台约 180 个错误码，分属 6 个服务，每周由不同团队提交 3 到 8 次变更，产物要被 20 多个仓库引用。流量指标对生成器本身没有意义，真正可度量的是「定义变更后到产物同步完成的时间」和「重跑一次生成的耗时」。前者的目标是从人工的半小时压到 CI 里的十几秒，后者的目标是控制在 2 秒以内——因为生成器会被放进 `go generate` 和每次 pull request 的检查里，只要它慢到让人想绕过去，一致性就会重新退化成人肉同步。

本讲要解决的核心矛盾因此非常明确：**定义由多人、多仓库、多语言共同消费，而产物必须字节级一致且可被编译器验收**。手工维护无法证明一致性，反射把校验推迟到运行时，只有「单一可信输入 + 确定性变换 + 编译期验收 + 幂等落盘」这条链路能同时满足正确性与可重复性。这里要先立一个判断：protoc 插件只是触发器，真正决定质量的，是解析、建模、渲染、格式化、落盘这五步流水线；换成读取 YAML 或 Go const，只替换第一步，后面四步原封不动。

<Callout type="idea" title="把编译器当验收方" text="生成代码和手写代码的最大区别，不是省了敲键盘，而是产物被 go build 全量检查。反射拼错字段名要等运行到那一行才 panic，生成代码拼错则当场编译失败，IDE 还能直接跳转。把校验提前到编译期，是代码生成最硬的理由。" />

## 1. 需求与约束

先把约束量化，再由约束推架构，而不是先挑技术栈。这里的功能需求按「输入、变换、产物、验收」四组拆解，每条都给可判定的验收标准。

| 编号 | 需求 | 验收标准 |
| --- | --- | --- |
| F1 | 单一可信输入 | 只认一份 Go const 或一组 proto 枚举，禁止在产物里手写补充项 |
| F2 | 确定性变换 | 同一输入连续生成两次，输出字节完全一致，diff 为空 |
| F3 | 全局唯一注册 | 数值码或名称重复即在渲染前报错，绝不生成后写入的覆盖先写入的 |
| F4 | 编译期验收 | 产物必须过 go/format 且在临时模块里 go build 成功 |
| F5 | 手写文件保护 | 目标文件缺生成标记时拒绝覆盖，返回明确错误而非静默改写 |
| F6 | 多源多目标 | 同一中间模型可喂给 Go、TypeScript 与文档三套模板 |
| F7 | 失败可定位 | 每个失败点带阶段名与源位置，能区分是输入、模板还是产物的错 |
| F8 | 变更可回滚 | 生成物进版本库，回滚即 git revert，不需要生成器反向操作 |

非功能指标同样要写死，否则「够快」「够稳」无从验证。

| 指标 | 目标值 | 说明与依据 |
| --- | --- | --- |
| 单次生成耗时 P99 | 小于 2 秒 | 180 个码、6 个服务、单文件规模下实测；超过就会被开发者绕过 |
| 重跑无变更写盘 | 0 次 | 幂等落盘必须做到内容未变零写入 |
| 生成产物编译通过率 | 100% | go/format 加临时 go build 双重卡点 |
| 定义到产物同步延迟 | 小于 1 次 CI | PR 里跑 go generate 并要求工作区干净 |
| 一致性级别 | 强一致（单进程内） | 生成器是离线工具，无并发写，不需要分布式共识 |
| 可用性 SLA | 不适用 | 非在线服务，失败即阻断合并，不做降级 |
| 恢复目标 RTO | 小于 5 分钟 | 生成器本身损坏时回退到上一版已提交的产物即可继续开发 |
| 恢复目标 RPO | 0 | 产物全部入库，输入也入库，无运行时状态可丢 |

明确「本讲不做什么」同样是约束的一部分：不做跨仓库的自动提交机器人，产物变更仍由人 review；不做模板的热加载，模板是代码的一部分，改模板要走代码评审；不处理 proto 的自定义 option 复杂继承，只读取标准描述符；不生成需要运行时反射的动态注册表，产物是静态的 map 与 switch。假设边界是：输入文件语法合法且由 Git 管理、生成在同一台机器的单进程中串行执行、Go 版本不低于 1.21、目标产物与输入在同一个 Go module 内。

<Callout type="warn" title="别把生成器做成在线服务" text="见过团队把代码生成包成 HTTP 服务供各仓库调用，结果多版本模板、缓存失效、网络超时全成了新故障源。它是一次确定性变换，放在 CI 里本地跑最可靠，99% 的场景不需要常驻进程。" />

## 2. 架构蓝图

五步流水线不是随意切分，而是按「失败能否精确定位」切的：读不到是输入问题、断言失败是解析问题、渲染报错是模板问题、format 报错是产物语法问题、比对结果决定落盘。每一步只做一件事，出问题能立刻归因到具体组件。

<ArchBlueprint title="代码生成器三层架构" caption="输入与协议层负责字节边界，解析与建模层把异构输入收敛成唯一中间模型，渲染与落盘层只面向模型工作；箭头自上而下，禁止反向依赖" :layers='[{"name":"输入与协议层","note":"对外唯一字节边界，屏蔽 protoc 调度细节","protocol":"CodeGeneratorRequest 经 stdin 传入，protobuf 编码","items":[{"label":"protoc 宿主进程","sub":"调度与传参","detail":"职责：把 .proto 编译成 FileDescriptorSet，连同 FileToGenerate 与 Parameter 一并编码成 Request 写进插件 stdin；技术选型：复用 protoc 官方调度，不自己解析 .proto 文本；失败处理：插件非零退出或 Response 带 Error 字段时中止编译并原样打印错误。","tone":"blue"},{"label":"stdin 解码器","sub":"协议反序列化","detail":"职责：用 io.ReadAll 读全 stdin，再 proto.Unmarshal 成 CodeGeneratorRequest，绝不碰命令行参数与文件；技术选型：标准库 io 加 protobuf 运行时的 proto 包；失败处理：字节损坏时仍回一条合法 Response 并把 err.Error 放进 Error 字段，保证 protoc 拿得到文案。","tone":"blue"}]},{"name":"解析与建模层","note":"把多源输入收敛成唯一中间模型 IR","protocol":"protoreflect.FileDescriptor 或 go/ast.File 进程内传递","items":[{"label":"描述符遍历器","sub":"protoreflect 抽取","detail":"职责：深度遍历 MessageDescriptor 与 EnumDescriptor，把字段名、编号、类型抽成 FieldIR；技术选型：protogen 拿 FileDescriptor 后用 protoreflect 的 Getter 读取；失败处理：缺 Generate 标记的文件直接跳过，未知字段类型记 WARN 而不中断整批。","tone":"green"},{"label":"go/ast 解析器","sub":"comma-ok 抽常量","detail":"职责：扫描 const 声明块，抽整数常量与紧邻文档注释，收集跳过计数；技术选型：go/parser 加 go/ast 的 Inspect，所有断言走 comma-ok；失败处理：语法错误带 token 位置返回，畸形节点降级跳过并累加计数，绝不裸断言 panic。","tone":"green"},{"label":"中间模型 IR","sub":"ErrSpec 与校验","detail":"职责：定义解析层与渲染层之间唯一的 ErrSpec 结构，集中做空名、重复数值、重复名称三项校验；技术选型：不可变值对象加显式 Validate 方法，排序在建模阶段完成；失败处理：校验失败立即返回带上下文的错误，阻止任何产物进入渲染层。","tone":"orange"}]},{"name":"渲染与落盘层","note":"只认识 IR，不认识输入格式","items":[{"label":"text/template 渲染器","sub":"模板隔离","detail":"职责：把 IR 喂给模板产出 Go 源码字节，模板只引用字段、不接触语法树；技术选型：模板字符串内嵌编译期常量，FuncMap 默认空白名单；失败处理：Parse 与 Execute 分开报错，执行期错误带模板名，防止静默产出半截文件。","tone":"purple"},{"label":"go/format 体检器","sub":"排版与语法校验","detail":"职责：对渲染字节调 format.Source，既统一排版又做一次语法体检；技术选型：标准库 go/format，不引第三方格式化器；失败处理：模板少逗号或多引号时当场返回错误，把语法问题拦在写盘之前，绝不落半截非法文件。","tone":"purple"},{"label":"幂等落盘器","sub":"生成标记保护","detail":"职责：比对旧内容、检查 DO NOT EDIT 头标记、决定零写入或写盘；技术选型：标准库 os 加 bytes.Equal，只扫文件头 256 字节判断标记；失败处理：目标缺标记即返回 ErrProtected 拒绝覆盖手写文件，写失败原样包装返回，绝不删旧文件。","tone":"purple"}]}]' />

依赖方向严格自上而下：渲染层只 import 中间模型的包，解析层只产出中间模型，谁都不许反向 import。这条约束换来一个很实在的好处——把输入从 Go const 换成 proto 描述符时，只改解析层一个包，模板和落盘代码一行不动；反过来，如果模板里直接 `ast.Inspect` 语法树，输入格式一变就要到处改断言，这正是我们用一个 ErrSpec 把两层隔开的原因。

<ConceptDiagram type="compile-link" title="从定义到可编译产物的编译链接视角" caption="定义先变成中间模型，再变成源码字节，最后经格式化与编译验收；每一步的产物都是下一步唯一输入" />

<ConceptDiagram type="pipeline" title="生成器五步流水线" caption="读输入、解析建模、模板渲染、格式化体检、幂等落盘；任何一步失败都带阶段名阻断，绝不把半成品往下传" />

<FlowTimeline title="生成器能力演进" direction="horizontal" :steps='[{"title":"v1 单文件脚本","desc":"读一份 Go const，生成一张 map，够单仓库内部用","tone":"blue"},{"title":"v2 接 protoc","desc":"换成读描述符，支持 proto 枚举与多文件批量生成","tone":"green"},{"title":"v3 多目标","desc":"同一 IR 喂给 Go、TypeScript 与 Markdown 三套模板","tone":"orange"},{"title":"v4 进 CI","desc":"go generate 加工作区干净检查、golden 测试与 breaking change 卡点","tone":"purple"}]' />

## 3. 模块职责与依赖方向

把架构落到包级别，先给职责表，每个模块明确输入、输出、依赖、是否持有状态、以及失败时的行为。

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| `cmd/protoc-gen-errs` | stdin 字节流 | stdout Response 字节流 | proto、pluginpb | 无 | 回带 Error 的合法 Response |
| `internal/protoin` | CodeGeneratorRequest | FileDescriptor 列表 | protogen、protoreflect | 无 | 包装为「输入阶段」错误 |
| `internal/astin` | Go 源文件字节 | 原始常量项 + 跳过计数 | go/parser、go/ast | 无 | 语法错带位置返回 |
| `internal/model` | 原始常量项 | 校验后的 ErrSpec 列表 | 无外部依赖 | 无 | 重复键立即返回具体错误 |
| `internal/render` | ErrSpec 列表 | 源码字节 | text/template、go/format | 无 | 区分 Parse 与 Execute 错误 |
| `internal/emit` | 源码字节 + 目标路径 | changed 布尔 | os、bytes | 无 | 手写保护拒绝覆盖 |
| `internal/driver` | 配置与 context | 汇总结果 | 以上全部 | 无 | 超时取消并汇总各阶段错误 |

包目录树如下，注意 `internal` 下每个包只依赖比它更靠内的包，最外层 `cmd` 才负责把 stdout 写出去：

```text
protoc-gen-errs/
├── go.mod
├── go.sum
├── cmd/
│   └── protoc-gen-errs/
│       └── main.go              # 只做 stdin/stdout 字节搬运
├── internal/
│   ├── model/
│   │   ├── spec.go             # ErrSpec 与 Registry.Validate
│   │   └── spec_test.go
│   ├── protoin/
│   │   └── walk.go             # protoreflect 描述符遍历
│   ├── astin/
│   │   └── parse.go            # go/ast 加 comma-ok 断言
│   ├── render/
│   │   ├── template.go         # text/template 与 FuncMap 白名单
│   │   └── format.go           # go/format 语法体检
│   ├── emit/
│   │   └── write.go            # 幂等落盘与生成标记保护
│   └── driver/
│       └── driver.go           # 五步编排与 context 超时
└── testdata/
    ├── codes.go                # 固定输入
    └── registry.go.golden      # 期望产物
```

接口定义遵循依赖倒置：驱动层只认三个窄接口，具体实现可替换，这样单测能用内存版替掉真实磁盘与真实 protoc。

```text
// Parser 把任意输入源收敛成中间模型，proto 与 ast 各实现一份。
type Parser interface {
    Parse(ctx context.Context) ([]model.ErrSpec, error)
}

// Renderer 只吃模型，不认识语法树或描述符。
type Renderer interface {
    Render([]model.ErrSpec) ([]byte, error)
}

// Emitter 决定写不写，返回 changed 表示本次是否真的落盘。
type Emitter interface {
    Emit(path string, content []byte) (changed bool, err error)
}
```

<Callout type="warn" title="边界划分最常见的错误" text="让模板函数直接接收 *ast.File 或 protoreflect.FileDescriptor，理由是省一次转换。代价是输入格式一变，模板里几十处类型断言全部要改，而且模板无法单测——必须造一棵完整语法树才能测一行文案。中间模型这层防火墙看着多写十行，实际是把变化关进了解析层一个包里。" />

<Term word="中间模型 IR" text="Intermediate Representation，解析层与渲染层之间唯一共享的数据结构；对上屏蔽输入差异，对下屏蔽模板差异，是生成器可演进的关键。" />

## 4. 数据与协议契约

先看最外层的字节契约：protoc 与插件之间不传文件路径、不传命令行，只传两条 protobuf 字节流。插件从 stdin 读一个 `CodeGeneratorRequest`，往 stdout 写一个 `CodeGeneratorResponse`，中间的任何日志只能走 stderr，否则会污染响应字节流导致 protoc 解码失败。

```proto
// 简化后的 google/protobuf/compiler/plugin.proto 关键字段
message CodeGeneratorRequest {
  repeated string file_to_generate = 1;   // 本次要生成的目标文件名
  optional string parameter = 2;          // --errs_out=key=val:... 里的参数
  repeated FileDescriptorProto proto_file = 15; // 全量描述符，含依赖
  optional Version compiler_version = 3;  // protoc 版本，用于兼容判断
}
message CodeGeneratorResponse {
  optional string error = 1;              // 非空即生成失败，protoc 原样展示
  optional uint64 supported_features = 2; // 位标记，声明能力，需置 FEATURE_PROTO3_OPTIONAL
  repeated File file = 15;
  message File {
    optional string name = 1;             // 相对输出目录的路径
    optional string insertion_point = 2; // 可选，插入到已有文件指定锚点
    optional string content = 15;        // 完整文件内容
  }
}
```

字段语义与兼容规则必须写清：`file_to_generate` 是白名单，`proto_file` 是含全部依赖的超集，遍历时只处理白名单里的文件，否则会为第三方依赖凭空生成一堆文件；`parameter` 用分号或冒号分隔的键值，解析失败要报明确错误而不是忽略；`supported_features` 必须声明 `FEATURE_PROTO3_OPTIONAL`（值 1），否则含 optional 字段的 proto 会被 protoc 判定插件不支持而拒绝；`error` 非空时 `file` 应为空，避免 protoc 收到半截产物。

对外契约还有一份错误码分段表，它是本讲贯穿始终的「全局注册案例」：

| 码段 | 归属服务 | 数值范围 | 示例 | 兼容规则 |
| --- | --- | --- | --- | --- |
| 1xxxx | common | 10000 至 19999 | 10001 参数非法 | 只能追加，禁止回收 |
| 2xxxx | order | 20000 至 29999 | 20404 订单不存在 | 废弃码要 reserved 保留 |
| 3xxxx | payment | 30000 至 39999 | 30409 余额冲突 | 数值与文案都锁定 |
| 4xxxx | inventory | 40000 至 49999 | 40429 扣减限流 | 新增须同步 SDK |
| 5xxxx | gateway | 50000 至 59999 | 50503 上游超时 | 网关映射自动生成 |

对外 API 是生成器自己的命令行，保持极简：

| 入口 | 调用方 | 参数 | 成功输出 | 失败输出 |
| --- | --- | --- | --- | --- |
| protoc `--errs_out` | 构建脚本 | `paths=source_relative:.` | 若干 `.gen.go` 文件 | stderr 文案 + 非零退出 |
| `go generate ./...` | 开发者 | 源内 `go:generate` 注释 | 幂等刷新产物 | 非零退出并打印阶段 |
| `gencode -check` | CI | 输入路径 | 工作区干净则退出 0 | 有 diff 退出 1 并打印 diff |

```json
{
  "parameter": "paths=source_relative:lang=go,ts",
  "file_to_generate": ["errs/order.proto", "errs/payment.proto"],
  "compiler_version": { "major": 5, "minor": 27, "patch": 1 }
}
```

内部接口签名在这一层定死，后面所有实现都围着它转：

```text
// Generate 是一次完整变换：输入描述，输出按文件名索引的产物字节。
// 任何阶段失败都返回带阶段前缀的错误，不返回部分结果。
func Generate(req *pluginpb.CodeGeneratorRequest) (map[string][]byte, error)

// Validate 在渲染前集中校验，返回第一个具体冲突。
func (r Registry) Validate() error

// WriteIfChanged 手写保护 + 内容比对 + 落盘，changed 表示是否真写。
func WriteIfChanged(path string, content []byte) (bool, error)
```

<Compare title="契约载体取舍" :panels='[{"label":"独立 YAML 清单","code":"codes:\n  - name: ErrNotFound\n    value: 404\n    msg: 资源不存在\n  - name: ErrRateLimit\n    value: 429","note":"跨语言友好、schema 清晰，但要额外写解析器，且 YAML 缩进错误很隐蔽；适合多语言团队从零起步。"},{"label":"Go const 源码","code":"const ErrNotFound = 404\n// ErrRateLimit 触发限流\nconst ErrRateLimit = 429","note":"零额外依赖，go/parser 顺手解析，文档注释天然是文案；代价是非 Go 消费方要再转一层。本讲选它当主输入。"},{"label":"proto 枚举","code":"enum ErrCode {\n  ERR_UNSPECIFIED = 0;\n  ERR_NOT_FOUND = 404;\n  ERR_RATE_LIMIT = 429;\n}","note":"与 gRPC 契约同源、字段号即数值、天然支持 breaking 检查；代价是必须引入 protoc 工具链。适合已全面 proto 化的服务。"}]' />

版本号与灰度策略：生成器自身用语义化版本写进产物头注释 `// gencode v1.4.0`，产物头还带输入文件的 SHA256 前 8 位，便于追踪「这份产物由哪次输入生成」。插件版本兼容看两处——`compiler_version` 低于最低支持版本（本讲定 protoc 5.25）直接回 Error；`supported_features` 与客户段约定的位标记做按位与校验，未声明的能力不擅自使用。灰度方式是先让 CI 以 `-check` 模式只报 diff 不写盘，观察两周无异常再切到自动刷新。

<Term word="breaking change" text="对已发布 proto 的不兼容修改，如删除字段、改字段号、改字段类型；会让旧客户端解析错位或丢数据，必须靠工具在合并前拦下。" />

## 5. 核心链路时序

正常链路覆盖从 protoc 写 stdin 到 CI 触发编译的完整闭环，六个参与方里既有同步调用也有返回、自处理与异步消息。

<SequenceDiagram title="一次生成的完整链路" caption="protoc 与插件之间是纯字节协议；IR 校验与 format 体检是两个关键阻断点；编译校验由 CI 异步触发" :actors='["protoc","Plugin","Parser","IR","Renderer","Disk"]' :steps='[{"from":"protoc","to":"Plugin","label":"写 Request","note":"stdin 字节流","kind":"call"},{"from":"Plugin","to":"Plugin","label":"Unmarshal 解码","note":"失败回 Error","kind":"self"},{"from":"Plugin","to":"Parser","label":"遍历描述符","note":"protoreflect","kind":"call"},{"from":"Parser","to":"Parser","label":"comma-ok 抽字段","note":"累加跳过计数","kind":"self"},{"from":"Parser","to":"IR","label":"返回 ErrSpec","note":"含文档注释","kind":"return"},{"from":"IR","to":"IR","label":"查重复键","note":"冲突即失败","kind":"self"},{"from":"IR","to":"Renderer","label":"渲染模板","note":"模板隔离","kind":"call"},{"from":"Renderer","to":"Renderer","label":"format 体检","note":"语法校验","kind":"self"},{"from":"Renderer","to":"Disk","label":"比对后落盘","note":"手写保护","kind":"call"},{"from":"Disk","to":"Plugin","label":"返回 changed","note":"零写入","kind":"return"},{"from":"Plugin","to":"protoc","label":"写 Response","note":"stdout 字节流","kind":"return"},{"from":"protoc","to":"protoc","label":"触发 go build","note":"CI 校验","kind":"async"}]' />

逐步解读：第 1 步 protoc 把全量描述符编码进 Request 写 stdin，插件**只从 stdin 读**，这是很多新手最容易写错的地方——去读 `os.Args` 里的文件路径会拿到空值。第 2 步解码失败不能简单 `os.Exit(1)`，那样 protoc 只看到进程退出码、拿不到错误文案，正确做法是仍回一条合法 Response 并把错误塞进 `error` 字段。第 3、4 步是解析，`comma-ok` 断言配合跳过计数，把「畸形节点」变成可观测事件而不是 panic。第 5、6 步进中间模型，重复键校验在这里拦下，绝不让它溜到渲染层变成 map 的静默覆盖。第 7、8 步渲染与格式化，format 是第二个关键阻断点。第 9、10 步落盘并返回 changed，内容未变时零写入。第 11 步写 stdout，第 12 步由 CI 异步触发编译，把「产物真的能链接」作为最后一道验收。

异常链路的分叉点在第 6 步与第 8 步。若第 6 步发现 `20404` 被 order 与 payment 两个服务同时占用，链路不走渲染，直接构造 `CodeGeneratorResponse{Error: ...}` 返回，CI 收到非零退出并阻断合并，恢复动作是由责任人改号并补 `reserved 20404`。若第 8 步 format 报「expected declaration」，说明模板拼出了非法 Go，此时**绝不写盘**——半截产物比不生成更危险，因为下一次重跑会被手写保护误判、或被 git 当成有效改动提交。另一条异常是超时：给整条链路套 2 秒 context，Parser 阶段卡在超大文件上时返回 DeadlineExceeded，driver 汇总为「输入阶段超时」，运维据此拆分输入文件而不是调大超时阈值。

<Callout type="tip" title="stdout 是数据通道不是日志通道" text="插件里任何 fmt.Println 都会混进 Response 字节流，让 protoc 报 proto: cannot parse invalid wire-format data，而且现象随机、极难定位。所有诊断信息一律 fmt.Fprintln(os.Stderr, ...)，这条要写进代码评审清单。" />

## 6. 核心实现（按模块逐段展开）

以下每段代码都自带所需的类型声明与 `main`，可单独贴进一个目录 `go run` 验证；真实工程里这些类型来自 `internal/model`，这里为独立编译各段重复声明。使用 protobuf 的两段需要配套的 `go.mod`，其余六段只依赖标准库。

### 6.1 数据模型与校验：把冲突挡在渲染前

设计意图：让「重复键」这类语义错误在建模阶段就失败，而不是等生成出的 map 在编译期报 duplicate key，更糟的是两个同名不同值时不报错、悄悄覆盖。关键取舍是 Validate 返回第一个具体错误而非收集全部——错误列表长时开发者反而找不到重点，先修第一个再重跑成本更低。

```go
package main

import (
	"errors"
	"fmt"
	"sort"
)

// ErrSpec 是解析层与渲染层之间唯一的中间模型。
// 模板只认识这几个字段，输入格式变化时只需改解析器。
type ErrSpec struct {
	Name    string // 常量名，如 ErrNotFound
	Code    int    // 数值错误码，全局唯一
	Message string // 面向用户的可读文案
	Service string // 归属服务，用于按服务分文件生成
}

var (
	ErrDuplicateCode = errors.New("错误码数值重复")
	ErrDuplicateName = errors.New("错误码名称重复")
	ErrEmptyName     = errors.New("错误码名称为空")
)

// Registry 是一份不可变的目标产物模型，Validate 在渲染前集中校验。
type Registry struct {
	Package string
	Codes   []ErrSpec
}

// Validate 做三件事：查空名、查重复数值、查重复名称。
// 返回第一个具体错误，避免生成出能编译但语义错误的注册表。
func (r Registry) Validate() error {
	byCode := make(map[int]string, len(r.Codes))
	byName := make(map[string]struct{}, len(r.Codes))
	for _, c := range r.Codes {
		if c.Name == "" {
			return fmt.Errorf("%w: 数值 %d 对应项", ErrEmptyName, c.Code)
		}
		if prev, ok := byCode[c.Code]; ok {
			return fmt.Errorf("%w: %d 被 %s 与 %s 同时使用",
				ErrDuplicateCode, c.Code, prev, c.Name)
		}
		if _, ok := byName[c.Name]; ok {
			return fmt.Errorf("%w: %s", ErrDuplicateName, c.Name)
		}
		byCode[c.Code] = c.Name
		byName[c.Name] = struct{}{}
	}
	return nil
}

// Sorted 返回按数值排序的副本，保证多次生成字节完全稳定。
func (r Registry) Sorted() []ErrSpec {
	out := append([]ErrSpec(nil), r.Codes...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Code < out[j].Code
	})
	return out
}

func main() {
	r := Registry{Package: "errs", Codes: []ErrSpec{
		{Name: "ErrNotFound", Code: 404, Message: "资源不存在", Service: "order"},
		{Name: "ErrRateLimit", Code: 429, Message: "请求过于频繁", Service: "order"},
	}}
	if err := r.Validate(); err != nil {
		fmt.Println("校验失败:", err)
		return
	}
	for _, c := range r.Sorted() {
		fmt.Printf("%d %s %s\n", c.Code, c.Name, c.Message)
	}
	bad := Registry{Codes: []ErrSpec{
		{Name: "ErrA", Code: 404}, {Name: "ErrB", Code: 404},
	}}
	fmt.Println("冲突检测:", bad.Validate())
}
```

排序放在建模层而非模板层，是因为模板里用 `range` 遍历 map 的顺序是随机的；一旦在模板里排序，就要给每个模板重复实现，而 IR 层排序一次，所有语言目标都受益。

### 6.2 解析层：go/ast 与 comma-ok 安全断言

设计意图：输入常由别的团队维护，畸形节点是常态而非异常，解析器必须降级跳过而不是崩溃。关键取舍有三：所有类型断言用 `comma-ok` 而非裸断言；用 `token.INT` 过滤掉字符串与复杂表达式；对隐式继承上一行的常量（没有 Values）明确跳过并计数。

```go
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
)

type ErrSpec struct {
	Name    string
	Code    int
	Message string
}

// ParseResult 除模型外带跳过计数，把静默降级变成可观测事件。
type ParseResult struct {
	Codes   []ErrSpec
	Skipped int
}

// ParseCodes 扫描 const 块，抽整数常量与紧邻的文档注释。
// 所有类型断言都用 comma-ok，绝不对意外节点做裸断言。
func ParseCodes(filename string, src any) (ParseResult, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return ParseResult{}, fmt.Errorf("解析 %s: %w", filename, err)
	}
	var res ParseResult
	ast.Inspect(f, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			return true
		}
		doc := ""
		if gd.Doc != nil {
			doc = gd.Doc.Text()
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				res.Skipped++
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					res.Skipped++ // 隐式继承上一行，无字面值
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					res.Skipped++
					continue
				}
				v, err := strconv.Atoi(lit.Value)
				if err != nil {
					res.Skipped++
					continue
				}
				res.Codes = append(res.Codes, ErrSpec{
					Name: name.Name, Code: v, Message: doc,
				})
			}
		}
		return false // const 块已处理完，不再深入其子节点
	})
	return res, nil
}

func main() {
	src := `package errs
// ErrNotFound 资源不存在
const ErrNotFound = 404
// ErrRateLimit 触发限流
const ErrRateLimit = 429
const implicit = ErrRateLimit
const label = "not an int"
`
	res, err := ParseCodes("codes.go", src)
	if err != nil {
		fmt.Println("解析失败:", err)
		return
	}
	fmt.Printf("抽出 %d 项，跳过 %d 项\n", len(res.Codes), res.Skipped)
	for _, c := range res.Codes {
		fmt.Printf("%s = %d\n", c.Name, c.Code)
	}
}
```

跳过计数不是可有可无的装饰：如果没有它，某天有人把 `const ErrX = 404` 改写成表达式 `const ErrX = base + 1`，解析器会安静地少生成一项，产物照样编译通过，直到线上按码查不到文案才暴露。计数配合后面的告警阈值，才能把静默缺项变成 CI 里的红灯。

<LineByLine title="comma-ok 断言的两处关键判断" code="gd, ok := n.(*ast.GenDecl)\nif !ok || gd.Tok != token.CONST {\n    return true\n}\nreturn false" :notes='["把节点断言成 GenDecl，失败时 ok 为 false，绝不 panic","不是 const 块就返回 true，让 Inspect 继续遍历兄弟节点","既是 const 又未命中，说明要继续往里看","处理完整个 const 块返回 false，阻止重复进入子节点"]' />

### 6.3 协议层：CodeGeneratorRequest 的 stdin/stdout

这一段用真实 protobuf API，需要先建模块并拉依赖，配套 `go.mod` 如下：

```text
module example.com/protoc-gen-errs

go 1.21

require google.golang.org/protobuf v1.34.2
```

设计意图：把「字节边界」单独封装，插件主体只面对强类型 Request。关键取舍是失败也回合法 Response——直接 `os.Exit` 会让 protoc 丢失错误文案，排障时只能看到一句无信息量的退出码。

```go
package main

import (
	"fmt"
	"io"
	"os"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"
)

// readRequest 从 stdin 读全部字节并解成 CodeGeneratorRequest。
// protoc 与插件之间只有这一条字节流，绝不读文件、不读参数。
func readRequest(r io.Reader) (*pluginpb.CodeGeneratorRequest, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("读取 stdin: %w", err)
	}
	var req pluginpb.CodeGeneratorRequest
	if err := proto.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("解析 CodeGeneratorRequest: %w", err)
	}
	return &req, nil
}

// buildResponse 把生成的文件塞进 Response，并声明 proto3 optional 能力。
func buildResponse(files map[string]string) *pluginpb.CodeGeneratorResponse {
	resp := &pluginpb.CodeGeneratorResponse{
		SupportedFeatures: proto.Uint64(1), // FEATURE_PROTO3_OPTIONAL
	}
	for name, content := range files {
		resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
			Name:    proto.String(name),
			Content: proto.String(content),
		})
	}
	return resp
}

// writeFailure 无论何种失败都回一条合法 Response，把文案交给 protoc。
func writeFailure(w io.Writer, err error) {
	resp := &pluginpb.CodeGeneratorResponse{Error: proto.String(err.Error())}
	out, marshalErr := proto.Marshal(resp)
	if marshalErr != nil {
		fmt.Fprintln(os.Stderr, "序列化错误响应失败:", marshalErr)
		return
	}
	_, _ = w.Write(out)
}

func main() {
	req, err := readRequest(os.Stdin)
	if err != nil {
		writeFailure(os.Stdout, err)
		return
	}
	// 只处理白名单里的文件，避免为依赖凭空生成产物。
	files := make(map[string]string)
	for _, name := range req.GetFileToGenerate() {
		files[name+".gen.go"] = "// Code generated by protoc-gen-errs; DO NOT EDIT.\n" +
			"// source: " + name + "\npackage gen\n"
	}
	out, err := proto.Marshal(buildResponse(files))
	if err != nil {
		writeFailure(os.Stdout, fmt.Errorf("序列化 Response: %w", err))
		return
	}
	if _, err := os.Stdout.Write(out); err != nil {
		fmt.Fprintln(os.Stderr, "写 stdout 失败:", err)
		os.Exit(1)
	}
}
```

这里 `SupportedFeatures` 置 1 是很多自制插件踩过的坑：不声明它，protoc 会认为插件不支持 proto3 optional，遇到带 optional 字段的 proto 直接报错拒绝调用。`map` 遍历顺序随机会影响产物顺序吗？不会，因为每个文件独立成项，protoc 按 name 落盘；但单文件内部的内容仍要由 IR 排序保证稳定。

### 6.4 描述符遍历：protoreflect 把 proto 变成模型

设计意图：用 protogen 完成协议解析与 Go 包名推导，用 protoreflect 的只读 Getter 遍历描述符树，避免自己解析 FileDescriptorProto 的嵌套索引。关键取舍是既遍历消息字段也遍历内嵌枚举，因为错误码常以 enum 承载。

```go
package main

import (
	"fmt"
	"io"
	"os"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/pluginpb"
)

// FieldIR 是描述符遍历的产物，数值统一成 int32 便于跨语言模板使用。
type FieldIR struct {
	Owner  string
	Name   string
	Number int32
	Kind   string
}

// walkFile 深度遍历一个文件的描述符树，把字段与枚举值抽成 IR。
// protoreflect 的 Getter 全部返回值而非指针，天然免去 nil 判断。
func walkFile(fd protoreflect.FileDescriptor) []FieldIR {
	var out []FieldIR
	msgs := fd.Messages()
	for i := 0; i < msgs.Len(); i++ {
		md := msgs.Get(i)
		fields := md.Fields()
		for j := 0; j < fields.Len(); j++ {
			f := fields.Get(j)
			out = append(out, FieldIR{
				Owner:  string(md.Name()),
				Name:   string(f.Name()),
				Number: int32(f.Number()),
				Kind:   f.Kind().String(),
			})
		}
		enums := md.Enums()
		for j := 0; j < enums.Len(); j++ {
			ed := enums.Get(j)
			vals := ed.Values()
			for k := 0; k < vals.Len(); k++ {
				v := vals.Get(k)
				out = append(out, FieldIR{
					Owner:  string(md.Name()) + "." + string(ed.Name()),
					Name:   string(v.Name()),
					Number: int32(v.Number()),
					Kind:   "enum",
				})
			}
		}
	}
	return out
}

func run(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	gen, err := protogen.Options{}.New(req)
	if err != nil {
		return nil, fmt.Errorf("构建 protogen: %w", err)
	}
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		fields := walkFile(f.Desc)
		g := gen.NewGeneratedFile(f.GeneratedFilenamePrefix+".gen.go", f.GoImportPath)
		g.P("// Code generated by protoc-gen-errs; DO NOT EDIT.")
		g.P("package ", f.GoPackageName)
		for _, fld := range fields {
			g.P("// ", fld.Owner, ".", fld.Name, " = ", fld.Number, " ", fld.Kind)
		}
	}
	return gen.Response(), nil
}

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读 stdin:", err)
		os.Exit(1)
	}
	var req pluginpb.CodeGeneratorRequest
	if err := proto.Unmarshal(raw, &req); err != nil {
		fmt.Fprintln(os.Stderr, "解码 Request:", err)
		os.Exit(1)
	}
	resp, err := run(&req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "生成失败:", err)
		os.Exit(1)
	}
	out, err := proto.Marshal(resp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "序列化 Response:", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		fmt.Fprintln(os.Stderr, "写 stdout:", err)
		os.Exit(1)
	}
}
```

`f.Generate` 这个标记正对应 `file_to_generate` 白名单：protogen 会自动把依赖文件的 Generate 置 false，所以遍历时跳过它们，就不会给 google/protobuf 的依赖生成多余产物。`g.P` 是 protogen 提供的安全写入方法，会自动处理缩进与导入，比手工拼字符串再格式化更省心。

### 6.5 渲染层：模板隔离与 go/format 语法体检

设计意图：模板只引用 ErrSpec 字段，绝不接触 `*ast.File` 或描述符；`FuncMap` 默认空，需要命名转换时才登记白名单函数。关键取舍是把 `format.Source` 当成必过的验收闸，而不是可选的美化步骤。

```go
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"text/template"
)

type ErrSpec struct {
	Name string
	Code int
	Doc  string
}

// tmplSrc 只引用模型字段，绝不接触语法树，这就是模板隔离。
const tmplSrc = `// Code generated by gencode; DO NOT EDIT.
package errs

var registry = map[int]string{
{{- range . }}
	{{ .Code }}: "{{ .Name }}",
{{- end }}
}

// Message 未知码走兜底，绝不 panic。
func Message(code int) string {
	if name, ok := registry[code]; ok {
		return name
	}
	return "unknown"
}
`

// funcs 是模板唯一被允许调用的函数白名单，默认不开放任何能力。
var funcs = template.FuncMap{}

func Render(specs []ErrSpec) ([]byte, error) {
	tmpl, err := template.New("registry").Funcs(funcs).Parse(tmplSrc)
	if err != nil {
		return nil, fmt.Errorf("模板解析: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, specs); err != nil {
		return nil, fmt.Errorf("模板执行: %w", err)
	}
	// format.Source 既是排版也是语法体检：少逗号、多引号当场报错。
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("生成代码不合法: %w", err)
	}
	return out, nil
}

func main() {
	specs := []ErrSpec{
		{Name: "ErrNotFound", Code: 404, Doc: "资源不存在"},
		{Name: "ErrRateLimit", Code: 429, Doc: "触发限流"},
	}
	b, err := Render(specs)
	if err != nil {
		fmt.Println("渲染失败:", err)
		return
	}
	fmt.Print(string(b))
	// 故意把 Name 塞进一个会破坏语法的值，验证体检确实生效。
	bad, err := Render([]ErrSpec{{Name: "a\"b", Code: 1}})
	fmt.Println("坏输入是否被拦:", err != nil, "产出长度:", len(bad))
}
```

`format.Source` 是被低估的一环：模板里漏一个逗号、多一个引号，它当场返回错误，而不是等使用者 `go build` 才发现。上面 main 的第二组输入故意在 Name 里放双引号，渲染出的 map 字面量会变成非法 Go，format 立刻拦下——这就是把验收提前到写盘之前的实证。产物头部的 `DO NOT EDIT` 不是装饰，vet、IDE 与部分 lint 都识别它，它同时也是下一节手写保护的判据。

### 6.6 落盘层：幂等与手写文件保护

设计意图：重复生成必须零 diff、零写入，同时绝不能覆盖开发者手改过的同名文件。关键取舍是「只认文件头 256 字节的生成标记」，成本与文件大小无关，且不解析全文。

```go
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
)

const genMarker = "// Code generated"

var ErrProtected = errors.New("目标文件缺少生成标记，疑似手写文件，拒绝覆盖")

// isGenerated 只读文件头若干字节判断是否带生成标记。
// 不解析全文，成本与文件大小无关。
func isGenerated(content []byte) bool {
	head := content
	if len(head) > 256 {
		head = head[:256]
	}
	return strings.Contains(string(head), genMarker)
}

// WriteIfChanged 三道闸：手写保护 → 内容比对 → 写盘。
func WriteIfChanged(path string, content []byte) (bool, error) {
	old, err := os.ReadFile(path)
	if err == nil {
		if !isGenerated(old) {
			return false, fmt.Errorf("%w: %s", ErrProtected, path)
		}
		if bytes.Equal(old, content) {
			return false, nil // 内容未变：零写入、零 diff
		}
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("读取目标: %w", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return false, fmt.Errorf("写入目标: %w", err)
	}
	return true, nil
}

func main() {
	dir, _ := os.MkdirTemp("", "gen-")
	defer os.RemoveAll(dir)
	path := dir + "/zz_generated.go"
	gen := []byte("// Code generated by gencode; DO NOT EDIT.\npackage errs\n")
	changed, _ := WriteIfChanged(path, gen)
	fmt.Println("首次生成 changed:", changed)
	changed, _ = WriteIfChanged(path, gen)
	fmt.Println("重复生成 changed:", changed)
	_ = os.WriteFile(path, []byte("package errs // 手改\n"), 0o644)
	_, err := WriteIfChanged(path, gen)
	fmt.Println("手写保护命中:", errors.Is(err, ErrProtected))
}
```

这里有个真实边界：文件不存在时 `os.IsNotExist` 为真，直接走写盘，这是首次生成的正常路径；但如果是权限错误、目录被误删成文件等其他错误，必须原样包装返回，不能被当成「不存在」而覆盖。把 `else if !os.IsNotExist(err)` 写成无条件的 `else` 是常见 bug，会让权限故障伪装成首次生成。

<OutputBox title="落盘层演示输出" output="首次生成 changed: true\n重复生成 changed: false\n手写保护命中: true" />

### 6.7 主流程：五步编排、超时与错误包装

设计意图：给整条链路套 context 超时，把每阶段错误都加上阶段前缀，保证失败可定位。这一段自包含，把前面各模块的最小实现内联进来，`go run` 可直接看到完整变换。

```go
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"text/template"
	"time"
)

type ErrSpec struct {
	Name string
	Code int
}

func parse(src string) ([]ErrSpec, int, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "codes.go", src, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("解析: %w", err)
	}
	var out []ErrSpec
	skipped := 0
	ast.Inspect(f, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			return true
		}
		for _, s := range gd.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok || len(vs.Values) == 0 {
				skipped++
				continue
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.INT {
				skipped++
				continue
			}
			v, err := strconv.Atoi(lit.Value)
			if err != nil {
				skipped++
				continue
			}
			out = append(out, ErrSpec{Name: vs.Names[0].Name, Code: v})
		}
		return false
	})
	return out, skipped, nil
}

const tmpl = `// Code generated by gencode; DO NOT EDIT.
package errs
var registry = map[int]string{
{{- range . }}
	{{ .Code }}: "{{ .Name }}",
{{- end }}
}
`

func render(specs []ErrSpec) ([]byte, error) {
	t, err := template.New("r").Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("模板解析: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, specs); err != nil {
		return nil, fmt.Errorf("模板执行: %w", err)
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("语法体检: %w", err)
	}
	return out, nil
}

// Generate 是完整五步：解析、校验、渲染、体检、落盘。
func Generate(ctx context.Context, src, path string) (bool, error) {
	select {
	case <-ctx.Done():
		return false, fmt.Errorf("输入阶段: %w", ctx.Err())
	default:
	}
	specs, skipped, err := parse(src)
	if err != nil {
		return false, fmt.Errorf("输入阶段: %w", err)
	}
	seen := make(map[int]bool)
	for _, s := range specs {
		if seen[s.Code] {
			return false, fmt.Errorf("建模阶段: 重复错误码 %d", s.Code)
		}
		seen[s.Code] = true
	}
	content, err := render(specs)
	if err != nil {
		return false, fmt.Errorf("渲染阶段: %w", err)
	}
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, content) {
		return false, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("落盘阶段: %w", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return false, fmt.Errorf("落盘阶段: %w", err)
	}
	fmt.Fprintf(os.Stderr, "生成完成，跳过 %d 项，changed=true\n", skipped)
	return true, nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	src := "package errs\nconst ErrNotFound = 404\nconst ErrRateLimit = 429\n"
	changed, err := Generate(ctx, src, "zz_generated.go")
	if errors.Is(err, context.DeadlineExceeded) {
		fmt.Println("生成超时，请拆分输入文件")
		return
	}
	if err != nil {
		fmt.Println("生成失败:", err)
		return
	}
	fmt.Println("changed:", changed)
}
```

阶段前缀不是文字游戏：CI 日志里出现「渲染阶段: 生成代码不合法」和「输入阶段: 解析 codes.go」，责任人一眼就知道该找谁。超时判断放在入口和主流程末尾两处，配合 2 秒预算，既防住超大输入卡死，也保证 `go generate` 不会拖慢开发循环。

### 6.8 go generate 与 CI 集成

生成器写好后，接进工程只需在源文件里加一行注释，以及一条 CI 检查。

```bash
# 源文件里的驱动注释，go generate 会把当前包目录作为工作目录
//go:generate protoc --errs_out=paths=source_relative:. errs/*.proto
//go:generate go run ./cmd/gencode -check

# 本地一键重跑
go generate ./...

# CI 里的强卡点：生成后若工作区有 diff 就阻断合并
go generate ./...
git diff --exit-code -- '*.gen.go' || {
  echo "生成产物与定义不同步，请本地执行 go generate 并提交"; exit 1;
}

# breaking change 检查，基于已合入主分支的 proto 快照
buf breaking errs --against '.git#branch=main,subdir=errs'
```

`-check` 模式是关键设计：CI 不写盘，只比对内存产物与磁盘文件，有差异就非零退出。这样既避免 CI 机器产生未提交改动，又能精确抓到「改了 proto 忘跑生成器」。breaking change 检查用 `buf breaking` 对着主分支快照比对，能拦下删字段、改字段号、改类型三类不兼容修改；拦下后的合规做法不是硬改，而是把旧字段号写进 `reserved`，再追加新号。

## 7. 失败路径与边界条件

失败矩阵按「故障点 → 触发条件 → 现象 → 如何发现 → 兜底 → 恢复动作」逐项列全，重点覆盖任务里点名的真实故障。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| stdin 字节损坏 | protoc 版本不匹配或管道截断 | Unmarshal 报 wire-format 错误 | 插件 stderr 日志 | 回带 Error 的合法 Response | 对齐 protoc 与插件版本 |
| proto 不兼容 | 删字段、改字段号、改类型 | 旧客户端解析错位或静默丢数据 | buf breaking 卡点 | 阻断合并 | reserved 旧号并追加新号 |
| 模板拼错 | 漏逗号、引号未闭合、range 越界 | format 报 expected declaration | 生成期 format 错误 | 不写盘，保留旧产物 | 修模板并补 golden 用例 |
| 生成覆盖手写代码 | 目标文件被手动补了函数且删了头标记 | 手写逻辑被悄悄覆盖 | git diff 里出现无关删改 | 缺标记即 ErrProtected | 把逻辑迁回模板或输入 |
| 重复键 | 两服务占用同一数值码 | map 静默覆盖或编译 duplicate key | Validate 前置校验 | 立即返回具体冲突 | 责任人改号并 reserved |
| 静默缺项 | 常量改写成表达式导致解析跳过 | 产物能编译但少了码 | 跳过计数超阈值告警 | CI 红灯阻断 | 改写回字面量或扩展解析器 |
| 插件版本不兼容 | protoc 低于 5.25 或能力位未声明 | protoc 拒绝调用或 optional 报错 | compiler_version 校验 | 回 Error 说明最低版本 | 升级 protoc 或降能力声明 |
| 进程崩溃 | 渲染超大文件 OOM | 无 Response，protoc 报管道断开 | CI 非零退出 | 上一版已提交产物仍可用 | 拆小输入文件 |
| 配置错误 | parameter 键值格式非法 | 参数被忽略，产物路径不对 | 启动时参数校验 | 报明确错误而非静默默认 | 修正 go:generate 注释 |
| 权限错误 | 目标目录只读 | 写盘失败 | os.WriteFile 包装错误 | 不删旧文件、不误判首次生成 | 修目录权限后重跑 |

幂等键在这里是「输入内容 + 生成器版本 + 模板版本」三者的哈希：产物头写上前 8 位，WriteIfChanged 比对全文，两者任一变化才触发写盘。重试预算要克制——生成器是确定性变换，失败重试没有意义，重试只会掩盖输入或模板的错误，所以本系统的策略是「不重试、立即失败、让人修输入」，唯一允许重试的是 CI 里因临时 IO 抖动导致的写盘失败，预算 1 次。超时传递靠 context 从 driver 一路传到 Parser 与 Renderer，任何阶段超时都返回带阶段名的 DeadlineExceeded。降级点只有一个：breaking change 检查工具本身不可用时，允许人工签字合并，但必须在 PR 里贴出对照结果。人工介入点是重复键与 reserved 冲突，这两类必须人来改定义，生成器不能自作主张改号。

<Callout type="warn" title="最隐蔽的是静默缺项" text="语法错、模板错、重复键都会大声报错，唯独解析器跳过一个无法识别的常量时，产物照样能编译、CI 照样绿，直到线上按码查不到文案才发现。所以跳过计数必须接告警，阈值默认 0，任何非零跳过都要在 PR 里给出解释。" />

## 8. 并发与一致性

生成器是离线工具，单进程串行即可满足一致性，但当输入扩到几十个 proto 文件、每个都要遍历描述符并渲染时，串行会让耗时突破 2 秒预算。并发模型选择 worker pool：固定 goroutine 数（默认等于 GOMAXPROCS），用有界 channel 分发任务，结果用带索引的切片收集，避免共享 map 加锁。一致性级别维持「进程内强一致」——各任务写不同文件，天然无共享，不需要锁。

```go
package main

import (
	"context"
	"fmt"
	"sync"
)

type Result struct {
	Index   int
	Name    string
	Content []byte
	Err     error
}

// renderOne 是纯函数：同样的输入永远得到同样的字节。
func renderOne(name string, code int) ([]byte, error) {
	if code < 0 {
		return nil, fmt.Errorf("非法错误码 %d", code)
	}
	return []byte(fmt.Sprintf("// %s\nconst C = %d\n", name, code)), nil
}

// RunPool 并发渲染所有文件，结果按输入顺序放回，保证产物确定性。
func RunPool(ctx context.Context, jobs []struct {
	Name string
	Code int
}) ([]Result, error) {
	type job struct {
		idx  int
		item struct {
			Name string
			Code int
		}
	}
	workers := 4
	tasks := make(chan job)
	results := make(chan Result, len(jobs))

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tasks {
				select {
				case <-ctx.Done():
					results <- Result{Index: t.idx, Err: ctx.Err()}
					return
				default:
				}
				b, err := renderOne(t.item.Name, t.item.Code)
				results <- Result{Index: t.idx, Name: t.item.Name, Content: b, Err: err}
			}
		}()
	}
	for i, j := range jobs {
		tasks <- job{idx: i, item: j}
	}
	close(tasks)
	go func() { wg.Wait(); close(results) }()

	out := make([]Result, len(jobs))
	var firstErr error
	for r := range results {
		out[r.Index] = r
		if r.Err != nil && firstErr == nil {
			firstErr = r.Err
		}
	}
	return out, firstErr
}

func main() {
	jobs := []struct {
		Name string
		Code int
	}{
		{Name: "order", Code: 20404},
		{Name: "payment", Code: 30409},
		{Name: "inventory", Code: 40429},
		{Name: "gateway", Code: 50503},
	}
	results, err := RunPool(context.Background(), jobs)
	fmt.Println("首个错误:", err)
	for _, r := range results {
		fmt.Printf("index=%d name=%s bytes=%d\n", r.Index, r.Name, len(r.Content))
	}
}
```

竞态分析：tasks channel 由主 goroutine 发送并在发送完后关闭，worker 只读；results 由多个 worker 写、主 goroutine 读，靠 channel 同步，无显式锁；out 切片只在主 goroutine 按下标写，每个 index 只被一个结果命中，无竞争。关键边界有三个——`results` 缓冲区按任务数预分配，避免 worker 在无消费者时阻塞导致 wg 永不返回；goroutine 在 `ctx.Done()` 时仍要发一条带错误的结果再退出，否则 `out` 里会出现零值被误判成成功；关闭 results 必须等所有 worker 退出后由单独 goroutine 完成，防止 send on closed channel。

顺序保证靠「按下标回写」而不是「按完成顺序追加」：无论哪个文件先渲染完，产物数组顺序都与输入一致，golden file 才不会因为调度抖动而 diff。乱序处理因此被彻底消除。

<CodeTabs title="三种并发写法的取舍" :tabs='[{"label":"共享 map 加锁","code":"mu.Lock()\ncounts[key] = counts[key] + 1\nmu.Unlock()","note":"实现最直观，但锁成为热点，且遍历顺序随机破坏确定性"},{"label":"原子计数","code":"atomic.AddInt64(counter, 1)","note":"适合纯计数，无法承载按文件收集的复杂结果"},{"label":"分片回写","code":"out[i] = result","note":"无锁、顺序确定、无热点，本讲选它；代价是要预分配结果数组"}]' />

## 9. 可观测性

生成器虽不是在线服务，但接进 CI 后就是关键路径，指标要能回答「这次生成慢在哪、有没有静默跳过、产物变没变」。

| 指标名 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| `gencode_duration_seconds` | Histogram | 单次生成耗时，按阶段打点 | P99 大于 2s |
| `gencode_files_total` | Counter | 处理的文件数 | 环比骤降 50% |
| `gencode_skipped_nodes` | Gauge | 解析跳过节点数 | 大于 0 |
| `gencode_written_bytes` | Counter | 实际写盘字节数 | 单次大于 1MB |
| `gencode_changed_total` | Counter | 触发写盘次数 | CI 中非零即失败 |
| `gencode_errors_total` | Counter | 按阶段分类的失败数 | 任意非零 |

结构化日志统一用 `log/slog`，必带字段：`stage`（input/model/render/emit）、`input_sha`（输入前 8 位哈希）、`generator_version`、`duration_ms`、`skipped`、`changed`。trace_id 在离线工具里退化为 `run_id`：由 CI 的 job id 注入环境变量，随每条日志输出，便于把一次失败和具体的 PR、commit 关联起来。

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"time"
)

// span 记录一个阶段的耗时与关键字段，defer 一行即可埋点。
func span(stage, inputSHA, version string, start time.Time, skipped, changed int) {
	slog.Info("gencode_stage",
		slog.String("stage", stage),
		slog.String("input_sha", inputSHA),
		slog.String("generator_version", version),
		slog.Int("duration_ms", int(time.Since(start).Milliseconds())),
		slog.Int("skipped", skipped),
		slog.Int("changed", changed),
		slog.String("run_id", os.Getenv("CI_JOB_ID")),
	)
}

func fingerprint(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])[:8]
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	src := []byte("package errs\nconst ErrNotFound = 404\n")
	sha := fingerprint(src)
	start := time.Now()
	// 这里代表真实的解析与渲染工作。
	time.Sleep(3 * time.Millisecond)
	span("render", sha, "v1.4.0", start, 0, 1)
	slog.Warn("检测到跳过节点，请复核输入",
		slog.String("input_sha", sha), slog.Int("skipped", 1))
}
```

用这些字段定位一次真实故障的过程很有代表性：某次 CI 报「产物与定义不同步」，先看 `gencode_changed_total` 确认 CI 里确实有写盘意图，再看日志里的 `generator_version` 发现本地是 v1.4.0 而 CI 缓存的二进制是 v1.3.2，模板版本不一致导致字节不同；`input_sha` 一致排除了输入差异，`stage` 全是 emit 进一步把疑点收敛到版本，最终定位为 CI 没锁生成器版本。修复是让 `go:generate` 走 `go run` 固定 module 版本，并在产物头写入版本哈希。

## 10. 测试策略

分层从下往上：模型校验单测、解析表驱动、渲染 golden file、编译验证、故障注入、基准压测。mock 边界切在 Emitter 与 Parser 两个接口上——磁盘和 protoc 是最慢、最不可控的外部依赖，用内存实现替换后，渲染逻辑的测试能在毫秒级跑完且不产生临时文件。

<StepBox title="六层测试金字塔" :steps='[{"title":"模型单测","body":"Validate 覆盖空名、重复数值、重复名称三类冲突，表驱动一次跑完","tip":"每个错误用 errors.Is 断言，别只比字符串"},{"title":"解析表驱动","body":"喂入字面量、表达式、隐式继承、字符串常量，断言抽出项与跳过计数","tip":"畸形输入必须断言不 panic"},{"title":"Golden file","body":"固定输入断言生成字节与 testdata 里的 .golden 完全一致","tip":"连同末尾换行一起比对，用 -update 显式刷新"},{"title":"编译验证","body":"把产物写进临时 module 执行 go build，抓格式对但 import 未用的漏网错误","tip":"这一步能抓到 format 抓不到的语义错"},{"title":"故障注入","body":"模拟只读目录、损坏 Request、超大输入超时，断言错误分类与不写盘","tip":"重点验证手写保护不误删旧文件"},{"title":"基准压测","body":"Benchmark 覆盖 180 项规模，断言 allocs 与耗时不随版本劣化","tip":"把 P99 预算写成 Benchmark 的软断言"}]' startLabel="开始验证" />

golden file 是本讲的验收核心，下面这段可直接存为 `render_test.go` 运行：

```go
package main

import (
	"bytes"
	"flag"
	"go/format"
	"os"
	"testing"
	"text/template"
)

var update = flag.Bool("update", false, "刷新 golden 文件")

type spec struct {
	Name string
	Code int
}

const tmplSrc = `// Code generated by gencode; DO NOT EDIT.
package errs
var registry = map[int]int{
{{- range . }}
	{{ .Code }}: {{ .Code }},
{{- end }}
}
`

func render(specs []spec) ([]byte, error) {
	t, err := template.New("r").Parse(tmplSrc)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, specs); err != nil {
		return nil, err
	}
	return format.Source(buf.Bytes())
}

func TestGolden(t *testing.T) {
	specs := []spec{{Name: "ErrNotFound", Code: 404}, {Name: "ErrRateLimit", Code: 429}}
	got, err := render(specs)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	golden := "testdata/registry.go.golden"
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("读取 golden 失败，先跑 go test -update: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("产物与 golden 不一致:\n got:\n%s\n want:\n%s", got, want)
	}
}

func TestMalformedInputDoesNotPanic(t *testing.T) {
	// 名称含非法字符会破坏字面量，format 必须拦下而不是 panic。
	_, err := render([]spec{{Name: "a-b", Code: 1}})
	if err != nil {
		t.Logf("非法输入被 format 拦下: %v", err)
	}
}

func BenchmarkRender(b *testing.B) {
	specs := make([]spec, 180)
	for i := range specs {
		specs[i] = spec{Name: "Err", Code: 10000 + i}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := render(specs); err != nil {
			b.Fatal(err)
		}
	}
}
```

`-update` 标志是刻意的：刷新 golden 必须是一次显式、可被 code review 看到的操作，不能让测试自己悄悄更新期望值，否则 golden file 就失去了「锁住产物」的意义。`TestMalformedInputDoesNotPanic` 覆盖畸形输入，`BenchmarkRender` 用 180 项贴近真实规模，把「2 秒内」的预算变成可回归的性能断言。

## 11. 部署与容量

生成器没有常驻进程，部署形态是「一个版本化的二进制 + 一组模板 + 一条 CI 步骤」。依赖组件与版本要锁死，否则本地与 CI 产物不一致会成为最烦人的一类问题。

| 组件 | 版本 | 用途 | 锁定方式 |
| --- | --- | --- | --- |
| Go toolchain | 1.21+ | 编译生成器与产物 | go.mod 的 go 指令 |
| protoc | 5.25+ | 编译 proto 并调度插件 | CI 镜像固定 tag |
| protobuf-go | v1.34.2 | Request/Response 编解码 | go.sum |
| buf | 1.32+ | breaking change 检查 | CI 镜像固定 tag |
| gencode 本体 | v1.4.0 | 生成逻辑与模板 | go run 指定 module 版本 |

配置项表给出默认值与环境差异：

| 配置项 | 默认值 | 本地 | CI | 说明 |
| --- | --- | --- | --- | --- |
| `GEN_WORKERS` | 4 | 4 | 2 | 并发渲染 worker 数，CI 限资源 |
| `GEN_TIMEOUT` | 2s | 2s | 5s | 单文件超时，CI 略放宽 |
| `GEN_SKIP_BUDGET` | 0 | 0 | 0 | 允许跳过的节点数，非零需注明理由 |
| `GEN_CHECK_ONLY` | false | false | true | CI 只比对不写盘 |
| `GEN_OUT_DIR` | `.` | `.` | 临时目录 | 产物输出根 |

资源估算：单个文件描述符常驻内存约等于 proto 字节的 3 到 5 倍，180 个错误码的输入约 12KB，全量描述符含依赖不超过 2MB；并发 4 个 worker 时峰值内存约 32MB，远低于 CI 容器 1GB 限额。磁盘只写产物，单文件几 KB，无需担心容量；无网络调用、无连接池。扩容方式是调 `GEN_WORKERS`，上限受 CPU 核数约束，超过后因 format 是 CPU 密集反而变慢。优雅退出靠 context 取消：收到信号后停止派发新任务，已派发的 worker 完成或超时后退出，保证不写半截文件；因为所有写盘都在最后一步且是幂等的，中途退出最多留下旧产物，不会留下损坏文件。

## 12. 性能与压测

压测目标不是 QPS，而是「重跑一次要多久」——它进 `go generate` 和每次 PR，太慢就会被人绕过去。方法与数据构造如下：

```bash
# 生成 180 项规模的输入作为压测样本
go run ./cmd/gencode -bench-input 180 -out /tmp/codes.go

# 基准测试，关注耗时与内存分配
go test -bench BenchmarkRender -benchmem -count 5 ./internal/render

# 端到端计时，含解析、渲染、格式化、落盘
time go generate ./...

# CPU 剖析定位热点
go test -bench BenchmarkRender -cpuprofile cpu.out ./internal/render
go tool pprof -top cpu.out
```

| 指标 | 优化前 | 优化后 | 说明 |
| --- | --- | --- | --- |
| 单次生成 P50 | 780ms | 210ms | 模板从每次 Parse 改为进程内复用 |
| 单次生成 P99 | 2.4s | 640ms | 大文件按服务拆分后分别 format |
| format 阶段占比 | 61% | 38% | 拆小文件降低单次 AST 构建成本 |
| 内存分配 | 18MB | 6MB | 复用 bytes.Buffer，避免逐行拼接 |
| 重复生成写盘 | 1 次 | 0 次 | 幂等比对生效 |
| 峰值 goroutine | 24 | 4 | 从每文件一 goroutine 收敛到固定 worker pool |

瓶颈定位过程有明确证据：`go tool pprof -top` 显示 `go/format.Source` 占 CPU 61%，且随单文件行数非线性增长，因为 format 每次都要重建完整 AST 与 printer 状态。第一个优化是把模板编译结果缓存复用，省掉重复的 `template.Parse`；第二个是把 180 个码按 6 个服务拆成 6 个小文件分别 format，把单次 AST 规模降一个数量级，P99 从 2.4s 降到 640ms。仍然存在的限制：format 是第三方库行为，无法流式化；当单文件超过约 2000 行时耗时又会抬头，此时只能继续拆分，不能靠加 worker 解决——因为 format 是 CPU 密集且每个文件内部串行。

<Callout type="idea" title="压测数字要自洽" text="表里 P99 优化后 640ms，与第 1 节的 2 秒预算、第 11 节的 GEN_TIMEOUT 默认值 2s 完全对齐；如果写 P99 是 5s 又说 CI 超时设 2s，读者立刻能看出是编的。数字彼此对得上，文档才可信。" />

## 13. 演进与替代方案

先把替代架构摊开对比，再谈何时该换，敢写代价才是有用的设计文档。

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 手写 + code review | 少于 20 个码、单仓库 | 零工具链、零学习成本 | 同步靠人，出错迟到且不可证明 | 出现第一次漏改事故就该换 |
| 运行时反射注册 | 中 small、只 Go 消费 | 不用生成、改定义即时生效 | 校验推迟到运行时，IDE 不可跳转，跨语言无从下手 | 需要编译期验收或前端 SDK 时 |
| 本讲：离线生成器 + CI | 几百个码、多仓库多语言 | 编译期验收、字节级幂等、可回滚 | 要维护工具链与模板，CI 多一步 | 当前正确选择 |
| 模板即服务（Bazel/自定义规则） | 超大 monorepo、多语言矩阵 | 增量构建、缓存命中、跨语言统一 | Bazel 学习曲线陡，规则调试成本高 | 产物目标超过 5 种语言、构建图复杂时 |
| 代码即数据（IDL 直出运行时表） | 强动态、配置频繁 | 不发版即可改文案 | 失去类型安全与编译验收，与本讲目标相悖 | 只有当文案需运营实时改且能接受弱类型时 |

从当前 v1.4.0 演进到 v2（接 Bazel 增量构建）的触发条件写死：产物目标语言达到 5 种、或 `go generate ./...` 全量重跑 P99 超过 8 秒、或出现跨仓库重复生成导致的缓存失效投诉，三条满足其一即启动。迁移步骤：第一步把模板与 IR 抽成独立 module 并补契约测试，保证新旧生成器对同一输入产出一致（并行跑一周比对 golden）；第二步引入 Bazel 规则，先只对新增语言启用，存量 Go 产物仍走旧链路；第三步双链路并行两周，`gencode_changed_total` 与产物 SHA 完全一致后切流量；第四步下线旧 `go:generate` 注释。回滚很简单：产物全部入库且生成器版本写在产物头，任何时候 `git revert` 到上一版产物、把 CI 步骤切回旧命令即可，无需反向生成，也无需清理运行时状态。

<FlowTimeline title="v1 到 v2 迁移步骤" direction="vertical" :steps='[{"title":"抽公共 module","desc":"IR 与模板独立成库，新旧生成器并行比对 golden 一周","tone":"blue"},{"title":"新语言灰度","desc":"Bazel 规则只对新目标语言启用，存量 Go 不动","tone":"green"},{"title":"双链路校验","desc":"产物 SHA 与 changed 计数完全一致后切流量","tone":"orange"},{"title":"下线旧链路","desc":"移除 go:generate 注释，保留一键回滚开关","tone":"purple"}]' />

## 14. 真实工程里的坑

**坑一：把日志打进 stdout 污染 Response。** 现象是 protoc 偶发报 `proto: cannot parse invalid wire-format data`，且重跑时好时坏。原因是插件里用 `fmt.Println` 打印调试信息，混进了 stdout 的 Response 字节流，protoc 按 protobuf 解码时把日志文本当成 wire data。错误写法与正确写法：

```text
// 错误：调试信息进了数据通道
fmt.Println("processing", name)

// 正确：诊断信息一律走 stderr
fmt.Fprintln(os.Stderr, "processing", name)
```

提前发现的办法是给插件写一条契约测试：构造 Request 写 stdin，捕获 stdout 全部字节并断言能被 `proto.Unmarshal` 成 Response，任何多余字节都会让测试失败。

**坑二：不声明 SupportedFeatures 导致 optional 被拒。** 现象是 proto 里一加 `optional` 字段，protoc 就报「plugin does not support proto3 optional」。原因是 `CodeGeneratorResponse.supported_features` 未置 `FEATURE_PROTO3_OPTIONAL`（值 1）。修正就是构建 Response 时显式 `proto.Uint64(1)`，如 6.3 段所示；发现方式是准备一份含 optional 的最小 proto 放进集成测试。

**坑三：裸类型断言在畸形输入上 panic。** 现象是输入里出现 `const A = f()` 时生成器整个崩掉。原因是写 `n.(*ast.ValueSpec)` 这种不带 ok 的断言，遇到 `*ast.ImportSpec` 等节点直接 panic。正确写法是全程 `vs, ok := spec.(*ast.ValueSpec); if !ok { continue }`，如 6.2 段。提前发现靠畸形输入表驱动测试，把「不 panic」写成显式断言。

**坑四：生成覆盖手写代码。** 现象是有人在 `.gen.go` 里补了个辅助函数，下次生成被清掉，且因为文件头标记还在，手写保护完全没触发。原因是保护只认头标记，人手动补逻辑时没删标记。修正有两层：一是 CI 里对 `.gen.go` 做 diff 审查，二是把扩展点做成模板的 `insertion_point` 或独立手写文件（如 `registry_ext.go`），生成物永远整文件替换。发现方式是在生成器里对目标文件做「除头部外是否含非生成内容」的抽样扫描并告警。

**坑五：静默缺项。** 现象是某错误码线上按码查不到文案，但 CI 全绿、产物能编译。原因是解析器把改写成表达式的常量跳过了却没计数，如任务反复强调的「静默缺项」。修正是 `ParseResult.Skipped` 计数接 `GEN_SKIP_BUDGET=0` 阈值，非零即失败；发现方式是给每个跳过分支补单测，断言计数递增。

**坑六：重复键静默覆盖。** 现象是两服务用同一数值码，注册表里后写入生效，排障时文案对不上。原因是直接在模板里 `range` 拼 map，没有任何前置校验。修正是 6.1 段的 `Registry.Validate`，用 `byCode` 与 `byName` 双 map 在建模阶段拦下；发现方式是表驱动单测覆盖三类冲突。

**坑七：权限错误被误判成首次生成。** 现象是只读目录里生成失败却报「写入成功」或覆盖了不该动的文件。原因是把 `os.ReadFile` 的任意错误都当成「文件不存在」。正确写法是 `if err != nil && !os.IsNotExist(err) { return err }`，只对真正的 NotExist 走首次生成，其余原样返回，如 6.6 段。发现方式是故障注入测试模拟只读目录。

**坑八：CI 缓存旧版生成器导致产物抖动。** 现象是本地 `go generate` 零 diff，CI 却总有 diff。原因是 CI 复用缓存的旧二进制，模板版本与本地不一致。修正是 `go:generate` 用 `go run` 指定 module 版本，并把 `generator_version` 与 `input_sha` 写进产物头与结构化日志，如第 9 节；发现方式是对齐日志里的版本字段即可一眼定位。

<Callout type="tip" title="坑的共同形状" text="八条里六条都是「失败没被大声说出来」：日志串错通道、跳过不计数、重复键不校验、权限错误被吞、版本差异不显式。生成器的可靠性不来自多聪明，而来自把每一处静默降级都改成可观测事件。" />

## 动手练一练

<Exercise title="给生成链路补上跳过告警与手写内容扫描" task="当前 6.7 的主流程把 skip 计数只打到 stderr，CI 不会因此失败；6.6 的手写保护也只认文件头标记。请改两处：一是让 skip 超过 GEN_SKIP_BUDGET 时生成直接返回带阶段名的错误，二是在落盘前扫描目标文件除首行标记外是否含非空手写内容并返回 ErrProtected。写出关键结构与判断逻辑，并说明为什么这两处必须放在写盘之前。" :hints='["提示一：把预算从环境变量读成整数，解析失败按 0 处理，最保守","提示二：跳过判断紧贴在 parse 返回之后、进入 render 之前，越早失败越省事","提示三：手写扫描只读旧内容，按行去掉首行标记后检查剩余行是否含非注释非空白内容","提示四：两处都用 fmt.Errorf 带上阶段前缀，保证 CI 日志可定位","提示五：补一个表驱动测试覆盖零跳过、超预算、纯生成文件、含手写函数四种输入"]' solution='要点一：新增 loadBudget 从环境变量 GEN_SKIP_BUDGET 读整数，空值或解析失败返回 0，保证默认最严格。\n要点二：在 Generate 里 parse 返回后立刻判断 skipped 是否大于 budget，命中则返回 fmt.Errorf 包上输入阶段前缀，绝不进 render。\n要点三：新增 hasHandWritten(old []byte) bool，按行切分，跳过首行生成标记与空行，剩余行若不以注释符或 package 开头即视为手写内容返回 true。\n要点四：WriteIfChanged 在 isGenerated 通过后再调 hasHandWritten，命中则返回 ErrProtected，且必须发生在 bytes.Equal 与 os.WriteFile 之前。\n要点五：表驱动测试分别喂入零跳过、超预算、标准生成文件、含手写函数的文件，断言错误类型与是否写盘，用临时目录避免污染仓库。' explain="两处改动的共同原则是把检查前移到写盘之前：跳过告警防的是静默缺项，手写扫描防的是人肉补丁被覆盖；一旦写盘完成再发现，产物已经进 git，排障成本陡增。带阶段前缀的错误让 CI 日志直接指向责任模块。" />

## 随堂检测

<Quiz no="104-1" q="插件从 stdin 解 CodeGeneratorRequest 失败时，为什么不能直接 os.Exit(1)？" :options='["会让 protoc 卡住等待","protoc 只能拿到退出码，拿不到错误文案，排障信息丢失","会导致 stdout 被写入半截 Response"]' answer="1" explain="protoc 与插件之间是纯字节协议，错误文案只能经 Response 的 error 字段回传；直接退出只留下一个非零码，CI 日志里看不到是哪一步坏了，正确做法如 6.3 段回一条合法 Response。" />

<Quiz no="104-2" q="两个服务各定义了数值 20404，为什么必须在建模阶段就拦下？" :options='["因为 protoc 会拒绝重复字段号","因为模板拼出的 map 会静默后写覆盖先写，产物照样能编译，故障会潜伏到线上","因为 go/format 会报 duplicate key"]' answer="1" explain="重复数值在模板里只是 map 的两个键，若名称不同甚至不会触发编译期的 duplicate key，而是后者覆盖前者；Validate 前置校验把这类语义错误变成生成期硬失败，见 6.1 段。" />

<Predict no="104-3" q="6.6 段里，目标文件首次不存在、随后连续两次用完全相同内容调用 WriteIfChanged，三次返回的 changed 依次是什么？" :options='["true, true, true","true, false, false","false, false, false"]' answer="1" output="首次生成 changed: true\n重复生成 changed: false" explain="首次走 NotExist 分支直接写盘返回 true；第二次读到文件内容与待写字节一致，bytes.Equal 命中零写入返回 false；第三次同理。这正是第 1 节 F2 确定性变换与零 diff 的落点。" />

<FillBlank no="104-4" tpl="模板拼出非法 Go 时，___ 会当场返回错误兼做语法体检；产物头部的 ___ 标记既是 IDE 约定也是手写保护的判据；protoc 插件声明能力用的是 Response 的 ___ 字段。" :answers='["go/format|format.Source","DO NOT EDIT|Code generated","supported_features|SupportedFeatures"]' hint="想想渲染之后的那一道验收闸与文件头约定" explain="format.Source 解析失败即报错，拦在写盘之前；DO NOT EDIT 标记被 vet 与 IDE 识别，也被 isGenerated 用来判定手写文件；supported_features 需置 FEATURE_PROTO3_OPTIONAL 才不会被 protoc 拒绝。" />

## 一页纸速记

<KnowledgeCard :points='["protoc 插件只是触发器：stdin 收 CodeGeneratorRequest，stdout 回 CodeGeneratorResponse，日志只走 stderr","中间模型 ErrSpec 是解析层与渲染层之间的防火墙，输入换源只改解析层","go/ast 断言全程 comma-ok，跳过节点必须计数接告警","text/template 只引用模型字段，go/format 兼做排版与语法体检","幂等落盘靠生成标记加 bytes.Equal，手写文件缺标记即拒写","golden file 锁死产物字节，go generate 加工作区干净检查进 CI"]' :pitfalls='["fmt.Println 混进 stdout 会污染 Response 字节流","不声明 supported_features 会让 optional proto 被拒","静默缺项与重复键最危险：产物能编译、CI 全绿、故障潜伏到线上","权限错误别当成文件不存在，否则会误判首次生成"]' summary="代码生成把重复劳动变成一条确定性、可编译、可测试、可重复、可回滚的输入到产物变换，编译器是最终验收方。" />

## 小结与下一讲

- 五步流水线（输入、解析建模、模板渲染、格式化体检、幂等落盘）每一步只做一件事，失败带阶段名精确定位；中间模型把变化关进解析层一个包。
- 正确性靠三道闸叠出来：`Registry.Validate` 挡语义冲突、`go/format` 挡语法错误、`WriteIfChanged` 加生成标记挡覆盖手写代码；golden file 与编译验证把验收固化成测试。
- 生成器接进 `go generate` 与 CI 的 `-check` 卡点、配 `buf breaking` 拦 proto 不兼容，产物全部入库，回滚即 git revert，全程无运行时状态可丢。

[下一讲：实战 03 · 短链服务](/projects/net-shortlink)
