---
title: 第 091 讲 · Kubernetes 部署与服务暴露
description: 把 Go 服务稳定跑在 K8s 上，给出探针与优雅退出的完整清单
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★★☆" time="55 分钟" goal="把 Go 服务稳定跑在 K8s 上，能解释每个探针与退出步骤在防哪种故障" prereq="容器化：多阶段构建与镜像瘦身" />

# 第 091 讲 · Kubernetes 部署与服务暴露

> 镜像解决的是「交付一个进程」，K8s 解决的是「管住成百上千个这样的进程」：谁坏了重启、谁没就绪就别接流量、扩容时多起几份、外部请求怎么找到它们。可这些能力全靠你写对声明式配置。一个配错的存活探针，能在下游数据库抖动时把整个服务的所有副本一起重启；一个缺失的优雅退出，会让每次发布都掉一批请求。本讲给出探针与优雅退出的完整 YAML，并逐个追问中途失败会怎样。

## 这一讲你会得到什么
<CheckList id="goal-91" title="本讲目标" :items='["说清 Pod、Deployment 与 Service 各自负责什么","区分 startup、liveness、readiness 三类探针的失败后果","写出带探针、优雅退出、资源配额与非 root 的完整 Deployment","用 ConfigMap 与 Secret 做到同一镜像跑多套环境","解释 HPA 与 Ingress 如何配合扩缩容和服务暴露"]' />

## 1. 先建立直觉
<ConceptDiagram type="k8s" title="Kubernetes：Deployment 管副本，Service 管入口" />

三句话建立地图：**Pod** 是最小的运行单元，里面装着你的容器；**Deployment** 声明「我要 3 个一样的 Pod」，并负责滚动更新与自愈；**Service** 给这一组 Pod 一个稳定的虚拟 IP，把请求负载均衡到健康的端点上。Pod 会随时被重建、IP 会变，调用方只认 Service，这就是解耦。

探针是 K8s 与应用的健康契约。**存活探针**失败就重启容器，用来处理进程还活着但已经卡死的场景；**就绪探针**失败就把它从 Service 端点里摘掉，不重启、不接新流量；**启动探针**给慢启动的应用一段宽限期，期间不触发存活检查，避免刚起来就被误杀。三者失败后果完全不同，混用是生产事故的高发区。

至于优雅退出，本质是「面向故障设计」在发布环节的体现：Pod 被删除不是瞬间消失，K8s 先发 SIGTERM、给一段宽限期、到点才 SIGKILL。应用必须利用这段窗口先摘流、再等存量请求跑完，否则每次滚动更新都会切掉一批正在处理的请求。

## 2. 语法与规则

先看应用侧该长什么样：两个探针端点，加一段收到 SIGTERM 后先摘流再停服的逻辑。

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// ready 控制就绪探针，退出前先置 false 主动摘流
var ready atomic.Bool

func main() {
	ready.Store(true)
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// 存活探针只检查进程本身，绝不依赖数据库等下游
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// 就绪探针失败时返回 503，Service 会把本 Pod 摘掉
		if !ready.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ready")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello from k8s")
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux}
	go func() {
		log.Println("listening on", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	// K8s 删除 Pod 前发 SIGTERM，并给 terminationGracePeriodSeconds 窗口
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// 关键顺序：先摘流，留出时间让 Endpoint 更新，最后才停服
	ready.Store(false)
	log.Println("收到 SIGTERM，先置未就绪，等待流量摘除")
	time.Sleep(3 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Println("优雅退出超时:", err)
	}
	log.Println("已优雅退出")
}
```

<LineByLine code="ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)\ndefer cancel()\nsrv.Shutdown(ctx)" :notes='["创建带 10 秒超时的上下文，给存量请求留出收尾时间","函数退出时释放上下文资源","Shutdown 停止接收新连接并等待存量请求完成"]' />

把退出顺序单独抽成状态机，能更清楚地看到「先摘流、再停服」为什么不能反：

```go
package main

import "fmt"

type shutdownState string

const (
	stateServing  shutdownState = "Serving"
	stateDraining shutdownState = "Draining"
	stateStopping shutdownState = "Stopping"
	stateExited   shutdownState = "Exited"
)

// Transition 按固定顺序推进状态，返回经过的状态序列
func Transition(sigterm bool) []shutdownState {
	seq := []shutdownState{stateServing}
	if !sigterm {
		return seq
	}
	seq = append(seq, stateDraining) // 先置未就绪，摘流
	seq = append(seq, stateStopping) // 再关连接，等存量请求
	seq = append(seq, stateExited)
	return seq
}

func main() {
	for _, s := range Transition(true) {
		fmt.Println("状态:", s)
	}
	fmt.Println("正确顺序: 先摘流再停服，避免存量请求被切断")
}
```

<OutputBox title="运行结果" output="状态: Serving\n状态: Draining\n状态: Stopping\n状态: Exited\n正确顺序: 先摘流再停服，避免存量请求被切断" />

`Draining` 一定要排在 `Stopping` 前面：先让就绪探针失败、等 Service 端点收敛，此时新请求不再进来，才关闭监听并等存量请求完成。反过来做，等于一边收新请求一边关连接，必然有请求被切断。

## 3. 深入一步

三类探针的失败后果必须分清，配错的代价各不相同：

| 探针 | 失败后果 | 防住什么 | 中途失败会怎样 |
| --- | --- | --- | --- |
| startupProbe | 先等启动，不重启 | 慢启动被存活探针误杀 | 启动期不触发存活检查 |
| livenessProbe | 重启容器 | 进程死锁但端口还开着 | 依赖下游会引发全量重启 |
| readinessProbe | 从 Service 端点摘除 | 流量打到未就绪或正在退出的 Pod | 不摘流会让退出期请求失败 |

<Term word="Pod" text="K8s 最小调度单元，内含一个或多个共享网络与存储的容器，随时可能被重建。" />
<Term word="Deployment" text="声明副本数与更新方式的控制器，负责自愈、滚动更新与版本回退。" />
<Term word="Service" text="为一组 Pod 提供稳定入口与负载均衡，端点由就绪探针动态维护。" />
<Term word="terminationGracePeriodSeconds" text="从发出 SIGTERM 到强制 SIGKILL 的宽限秒数，必须大于摘流与收尾所需时间。" />

<Callout type="warn" title="探针与退出的真实坑" text="存活探针绝不要查数据库或下游：下游一抖，所有 Pod 同时重启，故障被成倍放大。terminationGracePeriodSeconds 要大于 preStop 等待加 Shutdown 超时之和，否则窗口没走完就被 SIGKILL。preStop 里的 exec 还要求镜像有 shell，scratch 与 distroless 没有，这一行会直接失败——所以代码里那段 SIGTERM 自处理才是真正可靠的兜底。" />

资源配额同样要回答「中途失败会怎样」：`requests` 决定 Pod 被调度到哪、也决定 HPA 的利用率分母；`limits.memory` 设得过小，进程稍一涨就被 OOMKilled，表现为毫无征兆的重启；`limits.cpu` 只会限流不会杀进程。滚动更新用 `maxUnavailable: 0` 配合就绪探针，保证新 Pod 真正就绪后才替换旧的，这才是「发布不掉请求」的配置依据。

### 从「Pod 删除不是瞬间」推导优雅退出顺序

第 2 节把退出顺序写成状态机，现在把「为什么必须这个顺序」从 K8s 的控制回路推出来。删除一个 Pod 时，控制面并行发生两件事：**kubelet 开始 terminationGracePeriodSeconds 倒计时并发 SIGTERM**，以及 **Endpoint 控制器把该 Pod 从 Service 后端摘除**。这两件事是异步的，中间存在一个窗口：应用已经收到 SIGTERM，但 kube-proxy、Ingress 或调用方客户端仍可能把新请求发过来。

于是顺序被唯一确定：

1. `ready.Store(false)`：让 `/readyz` 返回 503，主动向控制面声明「别再给我新流量」。**这一步必须最先做**，因为端点摘除需要时间（控制器同步、kube-proxy 更新规则、客户端连接池），先停服等于在摘流生效前就关掉监听。
2. 等待一段（代码里 `time.Sleep(3 * time.Second)`，清单里 `preStop sleep 5`）：给端点收敛留出窗口。
3. `srv.Shutdown(ctx)`：停止接受新连接、等待存量请求完成，超时由 `context.WithTimeout` 兜底。
4. 进程退出；若在宽限期内没走完，kubelet 发 SIGKILL 强杀。

把不等式写出来就是：**宽限期必须大于 preStop 等待、摘流收敛时间与 Shutdown 超时三者之和**。清单里 30 大于 5 加 3 加 10，留了余量。任何一项配小了，结论都是「存量请求被切断」——这正是随堂检测第 2 题的答案。

<SequenceDiagram title="Pod 删除与优雅退出的异步窗口" caption="先置未就绪、等端点收敛，再 Shutdown，最后在宽限期内退出" :actors='["APIServer","Kubelet","App","Service","Client"]' :steps='[{"from":"APIServer","to":"Kubelet","label":"删除 Pod 指令","kind":"call"},{"from":"Kubelet","to":"App","label":"发送 SIGTERM","kind":"call"},{"from":"Kubelet","to":"Kubelet","label":"启动 30 秒倒计时","kind":"self"},{"from":"App","to":"App","label":"ready 置 false","note":"先主动摘流","kind":"self"},{"from":"App","to":"Service","label":"readyz 返回 503","kind":"call"},{"from":"Service","to":"Service","label":"摘除本 Pod 端点","kind":"self"},{"from":"Client","to":"Service","label":"新请求只到健康 Pod","kind":"call"},{"from":"App","to":"App","label":"等待端点收敛","kind":"self"},{"from":"App","to":"App","label":"Shutdown 等存量请求","kind":"self"},{"from":"App","to":"Kubelet","label":"进程正常退出","kind":"call"},{"from":"Kubelet","to":"APIServer","label":"上报已终止","kind":"return"}]' />

### 结论的前提与失效条件

| 结论 | 前提 | 失效条件 |
| --- | --- | --- |
| 就绪探针失败就不接流量 | Service 的端点由就绪探针动态维护 | 直接按 Pod IP 调用、或客户端长连接与连接池缓存了旧端点，摘流对它们无效 |
| 存活探针只查本地 | 应用能区分「本进程卡死」与「下游不可用」 | 存活探针里查数据库、缓存或下游，一次抖动触发全量重启 |
| `maxUnavailable: 0` 发布不掉请求 | 新 Pod 真正就绪后才替换旧的 | 就绪探针口径太松，只查端口不查依赖就绪，新 Pod 假就绪就接流量 |
| `requests` 决定调度与 HPA 分母 | HPA 按用量与 requests 之比算利用率 | requests 设得远高于真实用量，利用率永远很低，HPA 不扩容；设太低则 Pod 被挤到超售节点 |
| `limits.cpu` 只限流不杀 | CPU 是可压缩资源 | `limits.memory` 超限触发 OOMKilled，表现为无征兆重启 |

范围说明：本讲讨论的是**Pod 生命周期、探针配置与资源配额的正常语义**，不涉及节点内核故障、kubelet 与 apiserver 之间的拜占庭行为，也不覆盖有状态服务（StatefulSet）的逐序号启停与存储重连。另外，`preStop exec` 依赖镜像里有 shell，这一约束来自第 090 讲——scratch 与 distroless 镜像里这一行会直接失败，所以真正可靠的兜底是代码里自处理 SIGTERM。

### 三条真实误解

| 误解 | 为什么错 | 正确说法 |
| --- | --- | --- |
| 存活探针查得越全越好，把数据库也查上 | 存活探针失败会重启容器，所有副本一起重启会把局部下游故障放大成全站不可用 | 存活只查本进程是否卡死；下游健康交给就绪探针、熔断与降级 |
| Pod 收到 SIGTERM 就可以立刻 Shutdown | 端点摘除是异步的，关监听时新请求仍可能被路由进来 | 先置未就绪、等端点收敛、再 Shutdown，宽限期覆盖全过程 |
| limits.memory 只是限制，超了会变慢 | 内存超限触发 OOMKilled，容器被直接杀掉 | CPU 超限才限流；内存限额要按峰值留足，否则表现为莫名重启 |

## 4. 工程实践：完整清单与配置外置

先看应用如何读配置：ConfigMap 常以环境变量注入，Secret 常以文件挂载，且绝不把口令打进日志。

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// 优先读挂载的 Secret 文件，其次读环境变量，最后用默认值
func readSecret(name, envKey, mountDir, fallback string) string {
	p := filepath.Join(mountDir, name)
	if b, err := os.ReadFile(p); err == nil {
		return string(b)
	}
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return fallback
}

func main() {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	_ = readSecret("db-password", "DB_PASSWORD", "/etc/secret", "not-set")
	fmt.Println("日志级别:", logLevel)
	fmt.Println("口令读取顺序: 挂载文件 -> 环境变量 -> 默认值")
	fmt.Println("Secret 不以明文打印，避免泄露到日志")
}
```

下面是带探针、优雅退出、资源配额与非 root 的完整 Deployment 与 Service 清单。`startupProbe` 给慢启动留 60 秒宽限，`livenessProbe` 只查本地 `/healthz`，`readinessProbe` 查 `/readyz`，`preStop` 与 `terminationGracePeriodSeconds` 一起撑出摘流窗口。

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myservice
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
  selector:
    matchLabels:
      app: myservice
  template:
    metadata:
      labels:
        app: myservice
    spec:
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
      containers:
        - name: app
          image: myservice:v1.2.3
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: 8080
          envFrom:
            - configMapRef:
                name: myservice-config
          env:
            - name: DB_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: myservice-secret
                  key: db-password
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 256Mi
          startupProbe:
            httpGet:
              path: /healthz
              port: 8080
            failureThreshold: 30
            periodSeconds: 2
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8080
            periodSeconds: 10
            failureThreshold: 3
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8080
            periodSeconds: 5
            failureThreshold: 2
          lifecycle:
            preStop:
              exec:
                command: ["sh", "-c", "sleep 5"]
          volumeMounts:
            - name: secret-vol
              mountPath: /etc/secret
              readOnly: true
      volumes:
        - name: secret-vol
          secret:
            secretName: myservice-secret
---
apiVersion: v1
kind: Service
metadata:
  name: myservice
spec:
  selector:
    app: myservice
  ports:
    - port: 80
      targetPort: 8080
  type: ClusterIP
```

配套的配置与外部暴露、弹性伸缩：

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: myservice-config
data:
  LOG_LEVEL: "info"
  APP_PORT: "8080"
---
apiVersion: v1
kind: Secret
metadata:
  name: myservice-secret
type: Opaque
stringData:
  db-password: "change-me"
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myservice
spec:
  rules:
    - host: myservice.internal
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: myservice
                port:
                  number: 80
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: myservice
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: myservice
  minReplicas: 3
  maxReplicas: 20
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
```

<CodeTabs title="三种探针各管一段" :tabs='[{"label":"startup","code":"httpGet /healthz failureThreshold 30","note":"启动期宽限 60 秒，不触发存活重启"},{"label":"liveness","code":"httpGet /healthz periodSeconds 10","note":"只查本地，失败才重启容器"},{"label":"readiness","code":"httpGet /readyz periodSeconds 5","note":"失败即摘端点，退出前主动置失败"}]' />

<Compare title="存活探针查谁" :panels='[{"label":"依赖下游","code":"liveness = checkDatabase()","note":"数据库一抖，所有 Pod 同时被重启，把局部故障放大成全站不可用"},{"label":"只查本地","code":"liveness = checkLocalLoop()","note":"仅判断本进程是否卡死，下游故障交给熔断与降级处理"}]' />

<FlowTimeline direction="horizontal" :steps='[{"title":"删除 Pod","desc":"控制面发 SIGTERM 并执行 preStop","tone":"blue"},{"title":"主动摘流","desc":"readyz 置失败，Service 摘除端点","tone":"red"},{"title":"等待收敛","desc":"留出时间让 Endpoint 与 kube-proxy 更新","tone":"red"},{"title":"关闭监听","desc":"Shutdown 停止接新连接，等存量请求完成","tone":"red"},{"title":"退出进程","desc":"宽限期内正常退出，否则被 SIGKILL","tone":"green"}]' />

把整条链合起来看：Deployment 保证副本数，就绪探针保证流量只打给能服务的 Pod，优雅退出保证滚动更新不切请求，资源配额保证单 Pod 不会拖垮节点，HPA 按 CPU 利用率在 3 到 20 之间伸缩，Ingress 把集群内 Service 暴露成外部可访问的入口。每一环都在回答同一个问题——这一刻若发生故障，用户请求会怎样。

### 可验证实验：kubectl 逐字段读探针与滚动更新

把第 4 节的清单部署到集群，用 kubectl 观察探针失败、端点摘除与滚动更新：

```bash
kubectl apply -f deployment.yaml
kubectl rollout status deployment/myservice --timeout=120s
kubectl get pods -l app=myservice -o wide
kubectl get endpoints myservice -o yaml | grep -A6 subsets
kubectl describe pod myservice-xxxx | grep -A12 'Liveness\|Readiness\|Startup'
# 观察一次滚动更新期间的端点变化
kubectl set image deployment/myservice app=myservice:v1.2.4
kubectl rollout status deployment/myservice --timeout=120s
```

```text
$ kubectl rollout status deployment/myservice --timeout=120s
Waiting for deployment "myservice" rollout to finish: 1 of 3 updated replicas are available...
Waiting for deployment "myservice" rollout to finish: 2 of 3 updated replicas are available...
deployment "myservice" successfully rolled out
$ kubectl get pods -l app=myservice -o wide
NAME                         READY   STATUS    RESTARTS   AGE   IP           NODE
myservice-7d9c8b5f4-2xk9p    1/1     Running   0          3m   10.42.0.11   node-a
myservice-7d9c8b5f4-8m4qt    1/1     Running   0          3m   10.42.1.12   node-b
myservice-7d9c8b5f4-wp7rs    1/1     Running   0          3m   10.42.2.13   node-c
$ kubectl get endpoints myservice -o yaml | grep -A6 subsets
subsets:
- addresses:
  - ip: 10.42.0.11
  - ip: 10.42.1.12
  - ip: 10.42.2.13
  ports:
  - port: 8080
```

逐字段解读：`1 of 3 updated replicas are available` 逐行递增，是 `maxSurge: 1` 与 `maxUnavailable: 0` 的直接证据——新 Pod 就绪一个才替换一个，全程可用副本数不低于 3；`READY 1/1` 表示容器内 1 个容器就绪，分母来自 `containers` 数量，分子由就绪探针决定；`RESTARTS 0` 说明存活探针没有误杀，配合 `kubectl describe` 里 `Liveness: http-get /healthz` 与 `Readiness: http-get /readyz` 可确认两者查的是不同端点；`subsets.addresses` 列出的三个 IP 正是 `READY 1/1` 的 Pod，就绪探针失败的那个 IP 会从这里消失，这正是「摘流」的落点。演练时手动让某个 Pod 的 `/readyz` 返回 503，会看到 `READY 0/1` 且该 IP 从 addresses 移除但 `STATUS` 仍是 `Running`——**不重启、只摘流**，这一对照把三类探针的职责彻底分清。

<Quiz q="Pod 收到 SIGTERM 后，正确的第一步动作是什么？" :options='["立刻调用 Shutdown","先把就绪探针置为失败","直接退出进程","重启容器"]' answer="1" explain="端点摘除是异步的，必须先置未就绪停止接新流量、等端点收敛，再 Shutdown 等存量请求，否则会切断正在处理的请求。" />

### 失败矩阵与演练手段

| 注入点 | 现象 | 防护 | 演练手段 | 回滚 |
| --- | --- | --- | --- | --- |
| 下游数据库抖动 | 若存活探针查库，全量 Pod 重启 | 存活只查本地、下游交给熔断 | `tc netem` 注入数据库延迟 | `kubectl rollout undo` |
| 新版本启动慢 | 无 startupProbe 时被存活探针误杀 | startupProbe 给 60 秒宽限 | 故意让应用 sleep 后启动 | 回退镜像 tag |
| 宽限期小于收尾时间 | 被 SIGKILL 强杀，存量请求失败 | 宽限期覆盖全过程 | 调小宽限期观察失败请求数 | 调回 30 秒 |
| `limits.memory` 太小 | OOMKilled，无征兆重启 | 按峰值压测留余量 | 内存压测触发 OOMKilled | 调大 limits |
| `preStop exec` 跑在 scratch 镜像 | 无 shell，preStop 失败 | 代码自处理 SIGTERM | 用 scratch 镜像部署一次 | 换回自处理信号 |
| 滚动更新时新 Pod 假就绪 | 请求打到未初始化完的实例 | 就绪探针查真实依赖就绪 | 让 `/readyz` 过早返回 200 | `kubectl rollout undo` |

演练顺序：先在预发把 `terminationGracePeriodSeconds` 故意调小，观察 `vegeta` 压测下的失败请求数，确认宽限期不等式；再注入下游延迟验证存活探针不误杀；最后走一次完整的 `set image`、`rollout status`、`rollout undo`，记录回滚耗时。数据迁移或有状态场景还要额外验证 PVC 重连与逐序号启停，那超出了本讲 Deployment 的范围。

### 替代路线与取舍

| 路线 | 更新方式 | 掉请求风险 | 资源 | 适合 |
| --- | --- | --- | --- | --- |
| Deployment 滚动更新 | 逐批替换 | 低，配好探针与优雅退出 | 低 | 无状态服务，默认选择 |
| 蓝绿（双 Deployment 切 Service） | 整切 selector | 切换瞬间全量 | 双倍 | 要求回滚最快、可接受双倍成本 |
| 金丝雀（Rollout 或多版本） | 按权重放量 | 只影响小比例 | 两套并存 | 需要按指标灰度，见第 092 讲 |
| Recreate（先删后起） | 全停再全起 | 有明显停机 | 最低 | 离线任务、无法并存的有状态服务 |
| StatefulSet | 按序号逐个 | 低但慢 | 带独立存储 | 数据库、KV 等有状态服务 |

取舍主线：无状态服务用 Deployment 滚动更新即可，把预算花在写好探针与优雅退出上；只有「回滚必须秒级」或「要按指标放量」时才升级到蓝绿或金丝雀。别为了用高级发布方案而把简单服务复杂化。

### 与后续实战的衔接

- [Kubernetes Operator 与可观测平台](/projects/cloud-operator) 的 Reconcile 循环本质上就是「把现实收敛到声明」，与本讲 Deployment 控制器、就绪探针维护端点的思路同源；它的「告警规则」会直接消费这里的探针与重启指标。
- [Helm / GitOps / 灰度发布](/distributed/gitops) 把本讲的 `deployment.yaml` 抽成 Chart 模板，`replicas`、`image.tag`、探针参数都进 `values.yaml`，实现一份清单多环境复用。
- [实战 12 · KV 贯穿线 ④ 分片、复制与 Raft](/projects/kv-4-distributed) 若要以 StatefulSet 部署，本讲的「Pod 会重建、IP 会变、Service 提供稳定入口」会成为副本间寻址的基础，而成员变更（第 088 讲）必须与优雅退出配合，避免滚动更新时误判节点下线。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l8-distributed/lesson-091-kubernetes`](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-091-kubernetes)。clone 仓库后进入 `examples/l8-distributed`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="排对优雅退出的顺序" task="Pod 收到 SIGTERM 后，正确的退出顺序是什么？为什么不能一收到信号就立刻关闭监听？" :hints='["想想 Service 端点更新需要时间","存量请求还在处理中"]' solution="正确顺序是：收到 SIGTERM 后先把就绪探针置为失败，让 Service 摘除端点；\n等待 Endpoint 收敛，期间不再接新请求；\n再调用 Shutdown 等待存量请求完成，最后退出进程；\n顺序反了会在摘流前关连接，存量请求被切断" explain="先摘流再停服，才能把滚动更新期间的失败请求数压到零；宽限期要大于整个收尾过程。" />

## 随堂检测
<Quiz no="091-1" q="把存活探针写成检查数据库连通性，数据库短暂抖动时会怎样？" :options='["只影响 readiness，Pod 继续服务","所有 Pod 同时被重启，故障被放大","自动扩容更多副本","没有任何影响"]' answer="1" explain="存活探针失败触发重启，所有副本一起重启会让局部下游故障变成全站不可用。" />
<Predict no="091-2" q="terminationGracePeriodSeconds 设得比 preStop 等待加 Shutdown 超时之和还短，会发生什么？" :options='["优雅退出照常完成","宽限期到点被 SIGKILL 强杀，存量请求失败","K8s 自动延长宽限期","Pod 转为 Completed"]' answer="1" explain="到点未退出就被强制杀掉，所以宽限秒数必须覆盖整个收尾窗口。" />
<FillBlank no="091-3" tpl="失败会把 Pod 从 Service 端点摘除的是 ___；失败会重启容器的是 ___；定义 SIGTERM 到 SIGKILL 窗口的字段是 ___。" :answers='["readinessProbe~就绪探针","livenessProbe~存活探针","terminationGracePeriodSeconds"]' hint="摘流、重启、宽限期三个不同职责" explain="三者失败后果与职责完全不同，混用是滚动更新掉请求与故障放大的常见原因。" :explains='["它回答的是能否对外服务，失败只把 Pod 从 Service 端点列表摘除而不重启，正好用于滚动更新期间隔离尚未预热的新实例。","它判定进程是否已死锁到无法自愈，失败由 kubelet 按重启策略重建容器，用于把卡死但仍在跑的实例强制拉回健康。","它规定发出 SIGTERM 后等待多少秒才补 SIGKILL，给应用留出关连接、处理完在途请求的时间，字段名里的 Seconds 就点明单位是秒。"]' />

## 一页纸速记
<KnowledgeCard :points='["Pod 会重建，Service 提供稳定入口，Deployment 管副本与滚动更新","startup 宽限慢启动，liveness 卡死才重启，readiness 未就绪就摘流","优雅退出顺序是先摘流、等收敛、再 Shutdown，宽限期要覆盖全过程","存活探针只查本地，资源配额留足余量，HPA 与 Ingress 管伸缩与暴露"]' :pitfalls='["存活探针查下游，一次抖动触发全量重启","preStop 用 exec 却跑在 scratch 镜像里，无 shell 直接失败","limits.memory 设太小，进程被 OOMKilled 还找不到原因"]' summary="K8s 的稳定性不来自默认值，而来自你把每个探针与退出步骤都对着故障配过一遍。" />

## 小结与下一讲

- Pod / Deployment / Service 三层分工：运行单元、副本与更新、稳定入口与负载均衡。
- 三类探针失败后果不同，存活探针只查本地，就绪探针负责摘流，启动探针护住慢启动。
- 优雅退出必须先摘流再停服，宽限期要覆盖 preStop 与 Shutdown，配合资源配额与 HPA 才完整。

清单写对了，接下来要解决的是「这些清单怎么被声明、评审、回滚」——把发布变成可追踪、可回退的工程流程。

[下一讲：Helm / GitOps / 灰度发布](/distributed/gitops)
