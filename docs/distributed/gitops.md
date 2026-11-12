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

### 从「声明式收敛」推导为什么回滚必须改 Git

第 3 节用 `Sync(desired, actual)` 返回 desired 表达了声明式系统的脾气，现在把它推成一条可验证的因果链。设 Git 里的声明为 `D`，集群实际状态为 `A`，控制器每轮执行「把 A 收敛到 D」。这个更新是**单向**的：现实向声明收敛，声明不向现实收敛。

新版本出问题时，若只做 `kubectl set image` 把 `A` 改回旧版，而 `D` 仍是新版本，下一轮同步必然把现实重新收敛到新声明，故障复现。故回滚的正确不动点条件是：**`D` 与 `A` 同时回到旧声明**。用 `git revert` 让 `D` 回到旧值，控制器自动把 `A` 同步成旧值，两者重新一致。这就是为什么「回滚必须连 Git 一起回退」不是流程洁癖，而是单向更新函数的必然结论。

同理可以推出 `selfHeal` 的另一面：它把「A 与 D 不一致」一律视为异常，因此紧急止血时的手工修改会被覆盖。正确动作是先暂停自动同步，或把声明临时改成你想要的状态，而不是跟控制器抢着改集群。

金丝雀的权重步骤也能从「失败影响面」推出来：设放量权重 `w`，新版本故障率为 `p`，则受影响请求占比约 `w · p`。把 `w` 从 5% 逐步抬到 100%，等价于把「最坏情况下受影响的用户比例」从 5% 逐步放开；每一步 pause 期间观测错误率、P99 与饱和度，就是为了在 `w` 还小的时候估计出 `p`。若没有指标分析，pause 只是把 `w` 慢慢放大，期望损失并不会变小——这正是第 3 节提示「闭眼放量」的原因。

<SequenceDiagram title="GitOps 回滚：只改集群会被同步回来" caption="revert 提交让声明与现实一起回到旧版本" :actors='["Engineer","Git","ArgoCD","Cluster","Metrics"]' :steps='[{"from":"Engineer","to":"Git","label":"提交 v1.3.0","kind":"call"},{"from":"ArgoCD","to":"Git","label":"检测到声明变化","kind":"call"},{"from":"ArgoCD","to":"Cluster","label":"同步 v1.3.0 清单","kind":"call"},{"from":"Metrics","to":"ArgoCD","label":"金丝雀错误率越界","kind":"call"},{"from":"Engineer","to":"Cluster","label":"手工把镜像改回旧版","note":"只改现实","kind":"call"},{"from":"ArgoCD","to":"Cluster","label":"自愈同步回新版","note":"故障复现","kind":"call"},{"from":"Engineer","to":"Git","label":"revert 提交","kind":"call"},{"from":"Git","to":"ArgoCD","label":"声明回到 v1.2.3","kind":"call"},{"from":"ArgoCD","to":"Cluster","label":"同步旧版清单","kind":"call"},{"from":"Cluster","to":"Metrics","label":"错误率回落","kind":"return"},{"from":"ArgoCD","to":"Engineer","label":"报告同步健康","kind":"return"}]' />

### 结论的前提与失效条件

| 结论 | 前提 | 失效条件 |
| --- | --- | --- |
| selfHeal 能纠正漂移 | 控制器持续运行、有权限写回、Git 可达 | 控制器停摆或 Git 仓库不可达时，漂移不会被发现，此时唯一事实源暂时失效 |
| 回滚等于 revert 提交 | 所有变更都经 Git，没有带外 `kubectl edit` | 有人绕过 Git 直改集群，声明与现实的对应关系被破坏，revert 也未必能收敛 |
| Helm 一份模板多环境 | 环境差异都能由 values 表达 | 差异需要改模板结构（不同资源类型、不同 CRD）时，values 不够用，会出现大量条件分支，反而更难维护 |
| 金丝雀降低失败影响面 | 每步有明确指标与暂停点，且流量能按权重精确切分 | 无指标分析，或流量无法按用户与请求维度切分（长连接、有状态会话），权重切分失真 |
| Secret 不进 Git | 用 Sealed Secrets、SOPS 或 External Secrets | 把加密后的密文误当安全直接提交，或密钥仍在 CI 日志里打印 |

范围说明：本讲讨论的是**声明式发布流程与配置漂移**，不涉及 Git 仓库被篡改、CI 供应链投毒等安全对抗场景；密钥管理只要求「不进 Git」，签名与审计属于另一层。另外，声明式收敛默认是最终一致的——从提交到集群生效有几秒到几十秒延迟，把「已 commit」当成「已上线」是常见的事故来源。

### 三条真实误解

| 误解 | 为什么错 | 正确说法 |
| --- | --- | --- |
| GitOps 就是把 YAML 放进 Git | 关键不是存储位置，而是有一个控制器持续把现实收敛到声明 | 写 YAML 是声明式描述，GitOps 的核心是 reconcile 循环、漂移检测与自愈 |
| 金丝雀配了 pause 就安全了 | pause 只是放慢节奏，没有指标分析就等于闭眼放量 | 每步必须绑定错误率、P99、饱和度等分析指标，越界自动权重归零 |
| 把 Secret 加密后提交进 Git 就安全了 | 密文仍随仓库历史长期留存，且解密密钥一旦泄露全部暴露；CI 日志也可能打印明文 | 用 Sealed Secrets、SOPS 或 External Secrets，让密文只在集群内解密 |

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

### 可验证实验：helm template 渲染与 argocd 漂移检测

用 `helm template` 把同一份 Chart 用两套 values 渲染出来，验证「模板不变、入参变」；再用 Argo CD 的状态命令观察漂移与自愈：

```bash
# 本地渲染，不连集群即可验证 values 是否按预期替换
helm template myservice ./charts/myservice -f values-prod.yaml | grep -E 'name:|replicas:|image:'
helm template myservice ./charts/myservice --set image.tag=v1.3.0 | grep 'image:'
# 查看应用与 Git 声明的差异、同步状态
argocd app get myservice --refresh | grep -E 'Sync Status|Health Status'
argocd app diff myservice
# 模拟一次回滚：revert 提交后观察同步
git revert --no-edit HEAD
argocd app sync myservice
argocd app wait myservice --health
```

```text
$ helm template myservice ./charts/myservice -f values-prod.yaml | grep -E 'name:|replicas:|image:'
  name: myservice-prod
  replicas: 5
          image: myservice:v1.2.3
$ helm template myservice ./charts/myservice --set image.tag=v1.3.0 | grep 'image:'
          image: myservice:v1.3.0
$ argocd app diff myservice
===== apps/Deployment myservice =====
  spec.template.spec.containers[0].image:
    - myservice:v1.2.3
    + myservice:v1.3.0-hotfix
$ argocd app get myservice --refresh | grep -E 'Sync Status|Health Status'
Sync Status:      OutOfSync
Health Status:    Healthy
```

逐字段解读：两条 `helm template` 输出对比说明同一份模板、`replicas` 与 `image.tag` 完全由 values 驱动，`-f values-prod.yaml` 得到 `replicas: 5`，`--set` 则覆盖单个字段——这正是第 2 节那段 Go 模板代码模拟的机制。`argocd app diff` 里减号行是 Git 声明、加号行是集群实际状态，`+ myservice:v1.3.0-hotfix` 说明有人手工改过镜像，即配置漂移；`Sync Status: OutOfSync` 是控制器判定「现实与声明不一致」，`Health Status: Healthy` 则提醒我们**漂移不等于故障**——应用可能跑得好好的，但状态已经偏离唯一事实源。开着 `selfHeal` 时下一轮同步会把加号行覆盖回减号行。`git revert` 后 `argocd app sync` 再 `wait --health`，才是「声明与现实一起回到旧版」的完整回滚验证。注意 `helm template` 是纯本地渲染、不连集群，可以安全地进 CI 做冒烟检查，比 `helm install --dry-run` 更轻。

<Quiz q="金丝雀发布每一步 pause 之间必须绑定什么才有意义？" :options='["固定的等待时长","明确的分析指标","更多副本数","手动审批人"]' answer="1" explain="pause 只是放慢放量节奏，必须绑定错误率、P99、饱和度等指标并设定越界权重归零，才能在小流量阶段估计故障率、真正压低影响面。" />

### 失败矩阵与演练手段

| 注入点 | 现象 | 防护 | 演练手段 | 回滚 |
| --- | --- | --- | --- | --- |
| 新版本在 5% 金丝雀错误率越界 | 只影响 5% 用户 | 指标分析加权重归零 | Rollout 里注入一个高错误率镜像 | 权重归零，旧版本一直在 |
| 只改集群不改 Git | selfHeal 把新版本推回，故障复现 | revert 提交 | 手工 `kubectl set image` 后观察同步 | `git revert` 加 `argocd app sync` |
| 有人半夜 `kubectl edit` | 配置漂移，无人知道谁改的 | 收紧 RBAC、开审计 | 手工改一个字段看 `argocd app diff` | revert 或补一次合规提交 |
| Secret 明文进 Git | 密钥随历史泄露 | Sealed Secrets 或 SOPS | 在测试仓库试提交一次并检查扫描 | 轮换密钥、清理历史 |
| 发布到一半控制器或 Git 不可达 | 同步停滞，声明未生效 | 控制器高可用、Git 镜像 | 停掉 Argo CD 控制器观察 | 恢复后自动续同步 |
| 回滚没退干净，残留新 Pod | 新旧版本并存 | `prune: true` 加 `app wait --health` | 回滚后检查 Pod 列表 | 手动清理残留资源 |

演练要求「把回滚当功能练」：定期在预发走一遍「改 values、argocd sync、制造异常、revert、app wait 健康检查、确认无残留」，记下实际耗时。回滚演练还必须检查 `prune` 是否清掉了新版本引入的 ConfigMap 或 CRD，否则「回滚成功」只是镜像退回、资源仍残留——这是比镜像更隐蔽的失败。

### 替代路线与取舍

| 路线 | 事实源 | 回滚方式 | 审计 | 代价 | 适合 |
| --- | --- | --- | --- | --- | --- |
| GitOps（Argo CD 或 Flux） | Git 声明 | revert 提交 | 天然留痕 | 需部署控制器、收敛有延迟 | 多服务、多环境、强审计 |
| 命令式 kubectl 或 CI 直推 | 无单一事实源 | 重新执行旧命令 | 依赖 CI 日志 | 上手快，漂移难发现 | 小团队、单集群 |
| Helm 加手工 release | Chart 加 release 记录 | `helm rollback` | 有 release 历史 | 回滚不自动改 Git，易与 GitOps 冲突 | 已用 Helm、未上 GitOps |
| 纯声明式但无自愈 | Git 声明 | revert 加手动 sync | 有 | 漂移需人工发现 | 对自动写回有顾虑的合规场景 |

取舍主线：**审计与自动化程度，对收敛延迟与运维复杂度**。GitOps 把「谁改了什么」变成 Git 历史，代价是引入控制器与几秒到几十秒的收敛延迟；`helm rollback` 很快，但它改的是 release 而非 Git 声明，在开了 selfHeal 的集群里会立刻被同步回去——两种回滚机制混用，正是「回滚没退干净」的常见根源。

### 与后续实战的衔接

- [Kubernetes Operator 与可观测平台](/projects/cloud-operator) 是 GitOps 思路的延伸：CRD 是声明，Reconcile 循环是控制器，本讲「现实向声明收敛」会成为 Operator 的核心逻辑；它的「告警规则」会消费金丝雀分析里的错误率与 P99，把权重归零变成自动动作。
- [电商微服务系统](/projects/micro-ecommerce) 的多服务、多环境发布正是 Helm 加 GitOps 的主场景：几十个服务共用一套 Chart 模板，靠 values 区分测试与生产，靠 Git 历史回答「谁改了库存服务的副本数」。
- [分布式任务调度系统](/projects/concurrent-scheduler) 的 Master 选主后，任务配置同样应以声明形式存放并留痕，避免「谁改了 cron 表达式」变成无人能答的问题——这是 GitOps 在调度领域的同构应用。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l8-distributed/lesson-092-gitops`](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-092-gitops)。clone 仓库后进入 `examples/l8-distributed`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="解释回滚为什么必须改 Git" task="开着 selfHeal 的 Argo CD 上线了有问题的新版本，若只在集群里把镜像改回旧版，会发生什么？正确做法是什么？" :hints='["想想控制器下一轮同步会做什么","声明式系统以谁为准"]' solution="回滚必须把 Git 里的声明也改回旧版本：\n因为 Argo CD 以 Git 为唯一事实源并开了 selfHeal，\n只在集群里改镜像会被下一轮同步覆盖回有问题的新版本；\n正确做法是 revert 提交，让控制器把旧版本重新同步上线" explain="声明式系统里现实向声明收敛，只改现实不改声明，回滚几秒后就会失效。" />

## 随堂检测
<Quiz no="92-1" q="GitOps 里开了 selfHeal，有人手工 kubectl edit 改了集群镜像，结果会怎样？" :options='["手工改动被保留","Argo CD 检测到漂移并同步回 Git 声明状态","Git 仓库被自动修改","集群进入只读"]' answer="1" explain="selfHeal 会自动纠正漂移，这正是以 Git 为唯一事实源的含义，紧急止血要先暂停同步。" />
<Predict no="92-2" q="新版本有问题，只把集群镜像改回旧版、不改 Git 声明，几分钟后会怎样？" :options='["旧版本稳定运行","Argo CD 把新版本同步回来，故障复现","自动关闭 selfHeal","触发告警但不回滚"]' answer="1" output="Argo CD 把新版本同步回来，故障复现" explain="声明仍是新版本，控制器持续把现实收敛到声明，所以回滚必须连 Git 一起 revert。" />
<FillBlank no="92-3" tpl="Helm 用 ___ 文件承载环境差异；GitOps 把 ___ 当作唯一事实源；___ 发布先放小比例流量再逐步放量。" :answers='["values","Git","金丝雀~canary"]' hint="模板入参、事实源、按权重放量" explain="values 驱动模板渲染，Git 声明由控制器同步，金丝雀用权重把失败影响面压到最小。" :explains='["同一套 chart 模板靠这份参数文件注入镜像版本、副本数等差异，渲染出各环境清单，模板本身不变，换环境只改参数。","期望状态以声明式清单进版本库，控制器持续比对并驱动集群向它收敛；提交历史又顺带给出审计、评审与回滚的依据。","先给新版本分很小比例流量，确认错误率与延迟正常再逐步加权；一旦指标劣化立即回切，让故障只波及极少数用户。"]' />

## 一页纸速记
<KnowledgeCard :points='["Helm 用模板加 values 实现一份 Chart 多环境复用","GitOps 以 Git 为唯一事实源，selfHeal 自动纠正配置漂移","滚动逐批、蓝绿整切、金丝雀按权重放量，回滚代价依次变优","回滚必须 revert 提交，让声明与现实一起回到旧版本"]' :pitfalls='["把 Secret 明文提交进 Git，密钥随仓库历史泄露","只改集群不改 Git，selfHeal 把有问题的版本同步回来","金丝雀定义了 pause 却没定义分析指标，等于闭眼放量"]' summary="可回滚的发布，靠的是声明可追溯、灰度影响可控、退路被真实演练过。" />

## 小结与下一讲

- Helm 用模板加 values 复用清单，GitOps 用 Git 声明与 selfHeal 让集群自动收敛。
- 滚动、蓝绿、金丝雀的差别在中途失败的影响面与回滚速度，金丝雀要配指标与暂停点。
- 回滚必须 revert 提交；密钥不进 Git，回滚路径要定期演练。

发布流程把「变更」管住了，最后一讲要回答的是更上层的治理问题：用什么样的量化指标决定「还能不能继续发布、要不要停下来修稳定性」。

[下一讲：SRE：SLO、告警、容量与稳定性治理](/distributed/sre)
