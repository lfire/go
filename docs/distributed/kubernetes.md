---
title: 第 91 讲 · Kubernetes 部署与服务暴露
description: 把 Go 服务稳定跑在 K8s 上，给出探针与优雅退出的完整清单
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★★☆" time="55 分钟" goal="把 Go 服务稳定跑在 K8s 上，能解释每个探针与退出步骤在防哪种故障" prereq="容器化：多阶段构建与镜像瘦身" />

# 第 91 讲 · Kubernetes 部署与服务暴露

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

## 动手练一练
<Exercise title="排对优雅退出的顺序" task="Pod 收到 SIGTERM 后，正确的退出顺序是什么？为什么不能一收到信号就立刻关闭监听？" :hints='["想想 Service 端点更新需要时间","存量请求还在处理中"]' solution="正确顺序是：收到 SIGTERM 后先把就绪探针置为失败，让 Service 摘除端点；\n等待 Endpoint 收敛，期间不再接新请求；\n再调用 Shutdown 等待存量请求完成，最后退出进程；\n顺序反了会在摘流前关连接，存量请求被切断" explain="先摘流再停服，才能把滚动更新期间的失败请求数压到零；宽限期要大于整个收尾过程。" />

## 随堂检测
<Quiz no="91-1" q="把存活探针写成检查数据库连通性，数据库短暂抖动时会怎样？" :options='["只影响 readiness，Pod 继续服务","所有 Pod 同时被重启，故障被放大","自动扩容更多副本","没有任何影响"]' answer="1" explain="存活探针失败触发重启，所有副本一起重启会让局部下游故障变成全站不可用。" />
<Predict no="91-2" q="terminationGracePeriodSeconds 设得比 preStop 等待加 Shutdown 超时之和还短，会发生什么？" :options='["优雅退出照常完成","宽限期到点被 SIGKILL 强杀，存量请求失败","K8s 自动延长宽限期","Pod 转为 Completed"]' answer="1" output="宽限期到点被 SIGKILL 强杀，存量请求失败" explain="到点未退出就被强制杀掉，所以宽限秒数必须覆盖整个收尾窗口。" />
<FillBlank no="91-3" tpl="失败会把 Pod 从 Service 端点摘除的是 ___；失败会重启容器的是 ___；定义 SIGTERM 到 SIGKILL 窗口的字段是 ___。" :answers='["readinessProbe~就绪探针","livenessProbe~存活探针","terminationGracePeriodSeconds"]' hint="摘流、重启、宽限期三个不同职责" explain="三者失败后果与职责完全不同，混用是滚动更新掉请求与故障放大的常见原因。" />

## 一页纸速记
<KnowledgeCard :points='["Pod 会重建，Service 提供稳定入口，Deployment 管副本与滚动更新","startup 宽限慢启动，liveness 卡死才重启，readiness 未就绪就摘流","优雅退出顺序是先摘流、等收敛、再 Shutdown，宽限期要覆盖全过程","存活探针只查本地，资源配额留足余量，HPA 与 Ingress 管伸缩与暴露"]' :pitfalls='["存活探针查下游，一次抖动触发全量重启","preStop 用 exec 却跑在 scratch 镜像里，无 shell 直接失败","limits.memory 设太小，进程被 OOMKilled 还找不到原因"]' summary="K8s 的稳定性不来自默认值，而来自你把每个探针与退出步骤都对着故障配过一遍。" />

## 小结与下一讲

- Pod / Deployment / Service 三层分工：运行单元、副本与更新、稳定入口与负载均衡。
- 三类探针失败后果不同，存活探针只查本地，就绪探针负责摘流，启动探针护住慢启动。
- 优雅退出必须先摘流再停服，宽限期要覆盖 preStop 与 Shutdown，配合资源配额与 HPA 才完整。

清单写对了，接下来要解决的是「这些清单怎么被声明、评审、回滚」——把发布变成可追踪、可回退的工程流程。

[下一讲：Helm / GitOps / 灰度发布](/distributed/gitops)
