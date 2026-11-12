---
title: 关于本站 · 编排思路、交互设计与参与方式
description: 说明 Go 零基础通关的课程编排、参考的公开知识体系类型、交互组件设计意图、内容原创说明与参与修正方式。
---

# 关于本站

> 这是一套面向完全零基础学习者的交互式 Go 中文教程：十一个递进阶段、118 讲正文、十类工程场景，以及一条贯穿多讲的实战主线。这一页讲清楚「为什么这样排」「交互是怎么设计的」「内容从哪里来」，以及你发现问题后怎么参与修正。

<Callout type="idea" title="一句话定位" text="先建直觉，再拆原理，最后在真实场景里反复用熟。目标不是让读者背下语法，而是能独立交付一个生产可用的 Go 服务，并说清它为什么这样设计。" />

## 课程是怎么编排的

全站按 11 个阶段组织，共 118 讲，难度从一级逐级升到五级。编排遵循一条主线：先让人跑起来、再让人写对、然后让人知道为什么，最后把知识放进真实系统里用。

<Callout type="tip" title="三个数字" text="11 个阶段：从导学到工程实战；118 讲：每讲聚焦一组不重复的知识点；10 类场景：命令行、网络、数据、并发、存储、微服务、云原生、跨语言、AI 与安全治理。" />

<FlowTimeline title="从零基础到资深的编排主线" direction="vertical" :steps='[{"title":"先建直觉","desc":"导学阶段回答 Go 是什么、怎么跑起来、这套教程怎么用","tone":"blue"},{"title":"打地基","desc":"不假设编程基础，补齐值、类型、内存、函数与包","tone":"green"},{"title":"学语法","desc":"用切片、map、接口、错误与泛型写出结构完整的小程序","tone":"green"},{"title":"拆底层","desc":"从会用走到知道为什么：逃逸分析、GC、切片与接口底层","tone":"purple"},{"title":"学并发","desc":"建立 CSP 与 GMP 世界观，写出正确且可诊断的并发程序","tone":"orange"},{"title":"做工程","desc":"项目结构、测试、日志、配置、pprof 与 CI，把代码变成工程","tone":"orange"},{"title":"上规模","desc":"服务端、RPC 微服务、分布式与云原生，再进到 runtime 源码","tone":"red"},{"title":"进场景","desc":"十类真实场景与贯穿式实战，把前十个阶段反复用熟","tone":"red"}]' />

| 阶段 | 讲次范围 | 主题 | 入口讲次 |
| --- | --- | --- | --- |
| L0 导学 | 01–04 | 环境、工具链与全局地图 | [开篇：Go 是什么](/guide/opening) |
| L1 编程地基 | 05–12 | 值、类型、内存、函数与包 | [程序、数据、值与类型](/foundation/concepts) |
| L2 语法核心 | 13–25 | 切片、map、接口、错误、泛型与序列化 | [数组与切片](/basics/arrays-slices) |
| L3 核心机制 | 26–37 | 逃逸分析、分配器、GC、编译链接与 unsafe | [值语义深潜](/core/value-semantics) |
| L4 并发编程 | 38–49 | GMP、channel、sync、context 与 race | [并发与并行：CSP 思想](/concurrency/concurrency-intro) |
| L5 工程化 | 50–60 | 结构、规范、测试、日志、配置与 CI | [项目结构与分层](/engineering/project-layout) |
| L6 服务端与数据 | 61–71 | HTTP、框架、SQL、缓存、MQ、搜索与认证 | [net/http 深入](/server/net-http) |
| L7 RPC 与微服务 | 72–82 | gRPC、注册发现、网关、熔断、追踪与自研框架 | [RPC 原理与自研最小 RPC](/microservice/rpc-basics) |
| L8 分布式云原生 | 83–93 | CAP、事务、Raft、Docker、K8s、GitOps 与 SRE | [CAP 与一致性模型](/distributed/cap-base) |
| L9 资深进阶 | 94–102 | runtime 源码、GC 源码、内存模型与架构 | [runtime 启动源码](/advanced/runtime-startup) |
| L10 工程实战 | 103–118 | 十二个实战与 KV 贯穿线 | [待办清单 CLI](/projects/cli-todo) |

### 十类工程场景

课程不只按知识点推进，还把知识映射到十类真实场景，让每个阶段都有可落地的落点。

| 场景类别 | 代表内容 | 对应讲次 |
| --- | --- | --- |
| 命令行与工具链 | CLI、参数解析、代码生成器 | [待办清单 CLI](/projects/cli-todo) · [工具链全景](/guide/toolchain) |
| 网络服务 | 短链、图片服务与实时推送 | [短链服务](/projects/net-shortlink) · [图片服务](/projects/net-image-server) |
| 数据处理 | 日志 ETL 与多数据源查询 | [日志分析 ETL](/projects/data-log-etl) · [CSV / SQL 工具](/projects/data-sql-tool) |
| 并发系统 | 爬虫与分布式任务调度 | [并发爬虫](/projects/concurrent-crawler) · [任务调度系统](/projects/concurrent-scheduler) |
| 存储与中间件 | 内存版 KV 到分布式存储 | [KV ① 内存版](/projects/kv-1-memory) · [KV ④ 分布式](/projects/kv-4-distributed) |
| 微服务架构 | 电商系统与秒杀库存 | [电商微服务](/projects/micro-ecommerce) · [秒杀与高并发库存](/projects/micro-seckill) |
| 云原生与 SRE | Operator 与稳定性治理 | [Operator 与可观测平台](/projects/cloud-operator) · [SRE](/distributed/sre) |
| 跨语言与底层 | unsafe、cgo 与代码生成 | [unsafe 与 cgo](/core/unsafe-cgo) · [代码生成器](/projects/cli-codegen) |
| 数据与 AI 工程 | 搜索与 LLM 网关 | [LLM 网关](/projects/ai-gateway) · [Elasticsearch](/server/search) |
| 安全与治理 | 认证授权与网关限流 | [认证授权](/server/auth) · [API 网关](/microservice/gateway) |

<Callout type="note" title="贯穿式实战" text="KV 存储是本教程的一条主线：从内存版与接口抽象起步，逐步加上网络层、WAL 持久化、并发控制，最终演进成带分片、复制与 Raft 的分布式存储。同一个业务反复升级，比每讲换一个玩具例子更能体现架构取舍。详见第 111 讲到第 114 讲。" />

## 参考了哪些公开知识体系类型

这套教程的编排借鉴了几类常见的公开教学组织方式，但不对应、也不复制任何一门具体的课程：

| 参考的体系类型 | 在本站里的体现 |
| --- | --- |
| 计算机导论式的零基础铺垫 | L1 阶段不假设任何编程基础，先讲值、类型与内存直觉 |
| 官方文档与语言规范式的梳理 | 语法、 Effective Go 式的命名与规范，强调可编译、可验证的写法 |
| 底层原理教材式的拆解 | L3 与 L9 阶段深入逃逸、GC、编译器与 runtime 源码 |
| 分布式系统教材式的理论框架 | CAP、共识、事务与 SRE，先给判断框架再谈组件 |
| 开源项目贯穿式实战 | 十类场景与 KV 主线，用同一业务反复做架构演进 |
| 能力图谱与自评式的复习 | 随堂检测、速查表、坑清单与面试自评清单 |

<Callout type="warn" title="关于来源与授权" text="本站不隶属于任何培训或商业课程，不转载、不收录任何特定课程的内容，也不主张任何机构授权或认证。文中出现的库与工具名称仅用于客观介绍 Go 生态，示例与讲解均围绕公开的 Go 语言知识重新组织。" />

## 交互设计意图

长教程最大的敌人是「读过了但没学会」。因此每一讲都把交互组件当作学习动作，而不是装饰。

| 组件类型 | 设计意图 |
| --- | --- |
| StageHeader 与 ProgressTracker | 每讲可打卡，进度汇总到首页，让一百多讲的行程有可见推进感 |
| Quiz、Predict、FillBlank | 先作答再揭晓解析，用主动回忆代替被动阅读 |
| CheckList 与 KnowledgeCard | 把目标、要点与易错点清单化，便于自评与考前复习 |
| ConceptDiagram 与 FlowTimeline | 抽象机制先给一张图，降低零基础读者的认知负担 |
| Compare、CodeTabs、LineByLine | 用对照与逐行讲解回答「为什么这样写而不是那样写」 |
| Exercise 与动手练一练 | 折叠式练习与状态演示，鼓励边读边改、边改边跑 |
| Callout、Term、OutputBox | 把易错点、术语与运行结果放在最需要的位置，减少来回翻找 |

<CheckList id="about-how-to-use" title="建议的读法" :items='["按阶段顺序学习，前四个阶段不随意跳讲", "每讲动手跑一遍示例代码，再对照输出理解", "随堂检测先自己作答，再看解析", "学完一个阶段回到练习与速查栏目做一次自测", "发现错字、坏链或不能编译的示例时去仓库反馈"]' />

<Callout type="idea" title="进度只存在本地" text="打卡与勾选状态用浏览器的 localStorage 保存，不会上传服务器，也不会要求登录。换设备或清理浏览器数据后进度会重置，这是刻意的隐私取舍。" />

## 内容原创说明

<Callout type="note" title="原创与验证" text="全部讲次正文、讲解、题目与练习均为围绕 Go 公开知识体系原创撰写；示例代码为讲解专门编写，遵循 import 完整、可编译、带中文注释的要求。站内引用只指向本站讲次，不嵌入第三方课程内容。若你发现示例无法编译或描述与当前 Go 版本不符，欢迎按下面的方式反馈修正。" />

## 如何参与修正

站点源码托管在 GitHub，仓库地址是 [github.com/lfire/go](https://github.com/lfire/go)。内容全部是 `docs/` 下的 Markdown 文件，修正不需要搭建后端环境：

1. 在仓库开一个 issue，说明出问题的页面、具体段落或报错信息；
2. 或者直接 fork 仓库，改动对应的 `.md` 文件后提交 Pull Request；
3. 最受欢迎的修正包括：错别字、示例代码不能编译、站内链接失效、与新版 Go 行为不符的描述；
4. 若想补讲次或加练习，建议先开 issue 讨论，避免与现有 118 讲的知识点重复。

<Callout type="tip" title="改哪一页" text="正文在 docs/ 下按阶段目录组织，附录在 docs/practice/ 与 docs/about/；导航与侧栏在 docs/.vitepress/config.mts，学习进度的页面清单在 docs/.vitepress/utils.ts。改完 Markdown 后重新构建即可看到效果。" />

## 本地运行与部署

本站是标准的 VitePress 静态站点，不需要数据库与服务端。完整步骤见 [本地运行与 GitHub Pages 部署](/about/deploy)。

```bash
npm install          # 安装依赖
npm run dev          # 本地预览，默认在 localhost:5173
npm run build        # 构建静态产物到 docs/.vitepress/dist/
npm run preview      # 预览构建产物
```

## 站点信息

| 项目 | 说明 |
| --- | --- |
| 站点类型 | VitePress 静态站点，简体中文 |
| 课程规模 | 11 阶段 · 118 讲 · 10 类工程场景 |
| 难度分级 | 一级到五级，逐级递进 |
| 进度存储 | 浏览器本地，不上传、不要求登录 |
| 源码仓库 | [github.com/lfire/go](https://github.com/lfire/go) |
| 发布方式 | 推送到 main 后由 GitHub Actions 自动构建并部署 Pages |

<KnowledgeCard :points='["11 阶段由浅入深，118 讲覆盖语法到源码与架构", "10 类场景加贯穿式实战把知识真正用起来", "交互组件服务于主动回忆与即时自评", "内容原创撰写，欢迎通过仓库提 issue 或 PR 修正"]' :pitfalls='["把附录页当成正文的替代品", "跳阶段导致底层与并发的概念断层", "误以为打卡进度会上传到服务器"]' summary="先建直觉，再拆原理，最后在真实场景里反复用熟。" />

## 从这里开始

[课程总目录 · 118 讲](/guide/curriculum) · [学习路线](/guide/) · [练习与速查](/practice/) · [语法速查表](/practice/cheatsheet) · [综合自测](/practice/) · [本地运行与部署](/about/deploy)
