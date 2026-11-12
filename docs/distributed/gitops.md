---
title: 第 92 讲 · Helm / GitOps / 灰度发布
description: 做到可声明、可回滚的发布，把配置漂移与灰度失败管进流程
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★★☆" time="50 分钟" goal="做到可声明可回滚的发布，能解释漂移、灰度与回退各自的故障边界" prereq="Kubernetes 部署与服务暴露" />

# 第 92 讲 · Helm / GitOps / 灰度发布

> 上一讲把一份 Deployment 写对了，可真实生产里有几十个服务、多套环境、每天多次发布：清单谁来改、改完谁批准、上线出问题怎么在几分钟内退回去、有人半夜 `kubectl edit` 手工改了镜像又怎么发现？这些不是 YAML 语法问题，而是发布流程问题。本讲把 Helm、GitOps 与灰度串成一条链，并持续追问：发布到一半失败、配置和 Git 不一致、回滚没退干净，系统会怎样。

## 这一讲你会得到什么
<CheckList id="goal-92" title="本讲目标" :items='["说出 Helm Chart 的结构与 values 如何驱动多环境渲染","解释 GitOps 以 Git 为唯一事实源与 selfHeal 自愈的含义","对比滚动、蓝绿、金丝雀三种发布的中途失败与回滚代价","说明为什么回滚必须连 Git 声明一起回退","识别密钥明文进 Git 与配置漂移两类真实风险"]' />

## 1. 先建立直觉
<ConceptDiagram type="k8s" title="GitOps：Git 声明经 Argo CD 同步到 K8s 集群" />

Helm 解决的是「模板 + 参数」：把一堆 YAML 抽成 Chart，环境差异收进 `values.yaml`，测试环境 1 副本、生产环境 5 副本，用的是同一份模板。它像函数，Chart 是函数体，values 是入参。

GitOps 解决的是「谁说了算」：Git 仓库里的声明是唯一事实源，集群里跑什么，必须由 Argo CD 这类控制器持续比对并同步。**声明式**的含义不是「写 YAML」，而是「描述期望状态，由控制器负责把现实收敛过去」。于是有人手工改了集群，控制器会发现漂移并自动改回来——这通常被叫作自愈，但如果你正紧急止血，它会把你的临时修改覆盖掉，这一点必须提前知道。

灰度发布解决的是「失败的影响面」。滚动是一批批替换，蓝绿是两套整切，金丝雀先放一小撮流量看指标。三者真正的区别，全在「新版本有问题时，多少用户会受影响、多久能退回去」。

## 2. 语法与规则

先看 Helm 的核心机制：用 Go 的 `text/template` 把 values 渲染成最终清单。下面这段模拟 Chart 的渲染过程，同一份模板喂不同 values，产出不同环境的配置。

```go
package main

import (
	"bytes"
	"fmt"
	"text/template"
)

// Values 模拟 Helm 的 values.yaml，环境差异都收在这里
type Values struct {
	Env      string
	Replicas int
	ImageTag string
}

const deploymentTpl = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myservice-{{ .Env }}
spec:
  replicas: {{ .Replicas }}
  template:
    spec:
      containers:
        - name: app
          image: myservice:{{ .ImageTag }}
`

func main() {
	chart := template.Must(template.New("deployment").Parse(deploymentTpl))
	for _, v := range []Values{
		{Env: "staging", Replicas: 1, ImageTag: "v1.3.0-rc1"},
		{Env: "prod", Replicas: 5, ImageTag: "v1.2.3"},
	} {
		var buf bytes.Buffer
		if err := chart.Execute(&buf, v); err != nil {
			fmt.Println("渲染失败:", err)
			continue
		}
		fmt.Printf("--- %s 环境 ---\n%s", v.Env, buf.String())
	}
}
```

<LineByLine code="type Values struct {\nEnv string\nReplicas int\nImageTag string\n}" :notes='["Values 是所有环境差异的唯一入口，模板本身不含环境判断","Env 用于生成资源名，区分 staging 与 prod","Replicas 控制副本数，生产通常远大于测试","ImageTag 决定部署哪个版本，也是发布与回滚改动的字段","结构体结束，Chart 靠它实现一份模板多份产物"]' />

再看金丝雀的流量切分。真实系统按用户 ID 哈希保证同一用户始终落同一版本，这里用序号配额把放量比例演示清楚：

```go
package main

import "fmt"

// Route 按固定配额把一定比例的请求分给 canary
func Route(seq, canaryPercent int) string {
	if seq%100 < canaryPercent {
		return "canary"
	}
	return "stable"
}

func main() {
	for _, percent := range []int{5, 20, 50, 100} {
		canary := 0
		for i := 0; i < 1000; i++ {
			if Route(i, percent) == "canary" {
				canary++
			}
		}
		fmt.Printf("金丝雀权重 %3d%% : 命中 canary %d / 1000\n", percent, canary)
	}
}
```

<OutputBox title="运行结果" output="金丝雀权重   5% : 命中 canary 50 / 1000\n金丝雀权重  20% : 命中 canary 200 / 1000\n金丝雀权重  50% : 命中 canary 500 / 1000\n金丝雀权重 100% : 命中 canary 1000 / 1000" />

从 5% 到 20%、50% 再到 100%，每一步之间都要停一停看指标：错误率、P99 延迟、饱和度。只要任何一项越界，就把权重归零，流量瞬间回到旧版本，故障只影响那一小撮用户。没有「可观测后放量」这一步，金丝雀就只是慢一点的滚动发布。

## 3. 深入一步

三种发布方式的差别，用「中途失败会怎样」一问就清楚：

| 方式 | 中途失败会怎样 | 回滚方式 | 资源代价 |
| --- | --- | --- | --- |
| 滚动 | 新版本有问题会逐批污染部分实例 | 重新 rollout 旧版本，较慢 | 低，逐批替换 |
| 蓝绿 | 切换瞬间全量生效，问题影响所有用户 | 切回旧环境 selector，最快 | 双倍资源 |
| 金丝雀 | 只影响小比例流量，异常即权重归零 | 旧版本一直保留，风险最小 | 两套并存，中等 |

<Term word="Helm Chart" text="模板加默认值的目录包，渲染时叠加 values 生成最终清单，实现一份模板多环境复用。" />
<Term word="配置漂移" text="集群实际状态与 Git 声明不一致，常由手工修改或带外操作造成，GitOps 靠持续比对发现它。" />
<Term word="selfHeal" text="Argo CD 在检测到漂移时自动把集群同步回 Git 声明状态，也会覆盖紧急手工改动。" />
<Term word="金丝雀" text="先把少量流量导向新版本，观测核心指标无异常后再逐步放量，把失败影响面压到最小。" />

**回滚最容易踩的坑是只改集群、不改 Git**。设想新版本有问题，你 `kubectl` 把镜像改回旧版止血；可 Argo CD 开着 `selfHeal`，下一轮同步发现集群与 Git 声明不符，立刻又把新版本推回去——回滚看起来成功了，几秒后故障复现。正确做法是 `git revert` 那次提交，让控制器把旧版本重新同步上线，这样声明与现实重新一致。

<Callout type="warn" title="真实风险" text="绝不要把 Secret 明文提交进 Git：仓库有阅读权限的人、历史记录、CI 日志都会留下它，要用 Sealed Secrets、SOPS 或 External Secrets 管理。金丝雀的暂停步骤也必须预先定义分析指标，否则每步 pause 只是白等十分钟，错误版本照样被放到全量。" />

把漂移检测与自愈写成代码，能更直观地看到「以 Git 为准」到底意味着什么：

```go
package main

import "fmt"

// State 是镜像与副本数构成的最小状态
type State struct {
	ImageTag string
	Replicas int
}

// Drift 比较 Git 声明与集群实际状态
func Drift(desired, actual State) bool {
	return desired.ImageTag != actual.ImageTag || desired.Replicas != actual.Replicas
}

// Sync 把实际状态对齐回声明状态，模拟 Argo CD 的自愈
func Sync(desired, actual State) State {
	fmt.Println("检测到漂移，同步回声明状态:", desired.ImageTag)
	return desired
}

func main() {
	declared := State{ImageTag: "v1.2.3", Replicas: 5}
	running := State{ImageTag: "v1.2.3", Replicas: 5}
	fmt.Println("一致时存在漂移:", Drift(declared, running))

	// 模拟有人手工 kubectl edit 改了镜像
	running.ImageTag = "v1.3.0-hotfix"
	fmt.Println("手工改动后存在漂移:", Drift(declared, running))

	// 回滚的正确做法：把 Git 声明改回旧版本，控制器自动同步
	rolled := State{ImageTag: "v1.2.2", Replicas: 5}
	running = Sync(rolled, running)
	fmt.Println("回滚后镜像:", running.ImageTag, "，与声明一致:", !Drift(rolled, running))
}
```

`Sync` 返回的是 desired 而不是 actual，这正是声明式系统的脾气：现实必须向声明收敛，反之则被视为异常。理解了这一点，就能明白为什么紧急止血时要先暂停自动同步，而不是跟控制器抢着改。

## 4. 工程实践：Chart、同步与灰度清单

一个最小 Chart 的目录结构长这样，模板只放骨架，环境差异全在 values 文件里：

```text
myservice/
├── Chart.yaml
├── values.yaml
├── values-prod.yaml
└── templates/
    ├── deployment.yaml
    ├── service.yaml
    └── ingress.yaml
```

```yaml
# Chart.yaml
apiVersion: v2
name: myservice
version: 1.0.0
appVersion: "1.2.3"
---
# values-prod.yaml
replicaCount: 5
image:
  tag: "v1.2.3"
resources:
  limits:
    memory: 256Mi
```

Argo CD 用一份 Application 声明把 Git 与集群连起来，`automated.selfHeal` 保证漂移被自动纠正：

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myservice
spec:
  project: default
  source:
    repoURL: git@git.internal:platform.git
    targetRevision: main
    path: charts/myservice
    helm:
      valueFiles:
        - values-prod.yaml
  destination:
    server: https://kubernetes.default.svc
    namespace: production
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

金丝雀则用带权重步骤的 Rollout，每一步之间留观测窗口：

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: myservice
spec:
  replicas: 5
  strategy:
    canary:
      steps:
        - setWeight: 5
        - pause: {duration: 10m}
        - setWeight: 20
        - pause: {duration: 10m}
        - setWeight: 50
        - pause: {duration: 10m}
        - setWeight: 100
  template:
    spec:
      containers:
        - name: app
          image: myservice:v1.3.0
```

<CodeTabs title="三种发布的形态" :tabs='[{"label":"滚动","code":"maxSurge 1 maxUnavailable 0","note":"逐批替换，新版本有问题会污染部分实例"},{"label":"蓝绿","code":"switch service selector to green","note":"两套并存整切，回滚最快但占双倍资源"},{"label":"金丝雀","code":"setWeight 5 then 20 then 100","note":"按权重放量，每步看指标，异常即归零"}]' />

<Compare title="发布与回滚的两种做法" :panels='[{"label":"命令式","code":"kubectl set image deploy/app app=v1.3.0","note":"改完集群了事，无评审无记录，漂移后无人知道谁改的"},{"label":"GitOps","code":"git commit values-prod.yaml","note":"改动可评审可追溯，控制器同步，回滚就是 revert 提交"}]' />

<FlowTimeline direction="horizontal" :steps='[{"title":"改 values","desc":"提交新镜像 tag 到 Git 并发起评审","tone":"blue"},{"title":"自动同步","desc":"Argo CD 检测差异，渲染并应用清单","tone":"blue"},{"title":"金丝雀放量","desc":"先放 5% 流量，观测错误率与延迟","tone":"red"},{"title":"指标异常","desc":"任一指标越界即权重归零","tone":"red"},{"title":"Revert 回滚","desc":"回退提交，控制器同步旧版本","tone":"green"}]' />

把回滚当功能来演练，而不是等事故时第一次试：定期在预发环境走一遍「改 values → 同步 → 制造异常 → revert → 确认旧版本恢复」，记下实际耗时。每次发布都应满足三个可验证条件：改动在 Git 里留痕、灰度有明确指标与暂停点、回滚路径被真实走过。做到这三点，发布才从「祈祷别出事」变成「出事也只影响一小撮、几分钟能退」。

## 动手练一练
<Exercise title="解释回滚为什么必须改 Git" task="开着 selfHeal 的 Argo CD 上线了有问题的新版本，若只在集群里把镜像改回旧版，会发生什么？正确做法是什么？" :hints='["想想控制器下一轮同步会做什么","声明式系统以谁为准"]' solution="回滚必须把 Git 里的声明也改回旧版本：\n因为 Argo CD 以 Git 为唯一事实源并开了 selfHeal，\n只在集群里改镜像会被下一轮同步覆盖回有问题的新版本；\n正确做法是 revert 提交，让控制器把旧版本重新同步上线" explain="声明式系统里现实向声明收敛，只改现实不改声明，回滚几秒后就会失效。" />

## 随堂检测
<Quiz no="92-1" q="GitOps 里开了 selfHeal，有人手工 kubectl edit 改了集群镜像，结果会怎样？" :options='["手工改动被保留","Argo CD 检测到漂移并同步回 Git 声明状态","Git 仓库被自动修改","集群进入只读"]' answer="1" explain="selfHeal 会自动纠正漂移，这正是以 Git 为唯一事实源的含义，紧急止血要先暂停同步。" />
<Predict no="92-2" q="新版本有问题，只把集群镜像改回旧版、不改 Git 声明，几分钟后会怎样？" :options='["旧版本稳定运行","Argo CD 把新版本同步回来，故障复现","自动关闭 selfHeal","触发告警但不回滚"]' answer="1" output="Argo CD 把新版本同步回来，故障复现" explain="声明仍是新版本，控制器持续把现实收敛到声明，所以回滚必须连 Git 一起 revert。" />
<FillBlank no="92-3" tpl="Helm 用 ___ 文件承载环境差异；GitOps 把 ___ 当作唯一事实源；___ 发布先放小比例流量再逐步放量。" :answers='["values","Git","金丝雀~canary"]' hint="模板入参、事实源、按权重放量" explain="values 驱动模板渲染，Git 声明由控制器同步，金丝雀用权重把失败影响面压到最小。" />

## 一页纸速记
<KnowledgeCard :points='["Helm 用模板加 values 实现一份 Chart 多环境复用","GitOps 以 Git 为唯一事实源，selfHeal 自动纠正配置漂移","滚动逐批、蓝绿整切、金丝雀按权重放量，回滚代价依次变优","回滚必须 revert 提交，让声明与现实一起回到旧版本"]' :pitfalls='["把 Secret 明文提交进 Git，密钥随仓库历史泄露","只改集群不改 Git，selfHeal 把有问题的版本同步回来","金丝雀定义了 pause 却没定义分析指标，等于闭眼放量"]' summary="可回滚的发布，靠的是声明可追溯、灰度影响可控、退路被真实演练过。" />

## 小结与下一讲

- Helm 用模板加 values 复用清单，GitOps 用 Git 声明与 selfHeal 让集群自动收敛。
- 滚动、蓝绿、金丝雀的差别在中途失败的影响面与回滚速度，金丝雀要配指标与暂停点。
- 回滚必须 revert 提交；密钥不进 Git，回滚路径要定期演练。

发布流程把「变更」管住了，最后一讲要回答的是更上层的治理问题：用什么样的量化指标决定「还能不能继续发布、要不要停下来修稳定性」。

[下一讲：SRE：SLO、告警、容量与稳定性治理](/distributed/sre)
