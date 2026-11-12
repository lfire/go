---
title: 第 090 讲 · 容器化：多阶段构建与镜像瘦身
description: 交付一个小而可复现的 Go 镜像，理解 CGO_ENABLED 与多阶段构建
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★☆☆" time="45 分钟" goal="交付一个小而可复现的 Go 镜像，能解释每一步在防哪种故障" prereq="高可用：多活、容灾与混沌工程" />

# 第 090 讲 · 容器化：多阶段构建与镜像瘦身

> 前面把服务设计得能扛故障，可如果每次构建出的镜像都不一样、里面还塞着整套编译工具链，稳定性就无从谈起。Go 天生适合容器化：编译产物是单个二进制，静态链接后连 glibc 都不需要。本讲把「多阶段构建 + CGO_ENABLED=0 + scratch」这条主线走通，并逐个追问中点：动态链接的二进制放进空镜像会怎样、没有证书时 HTTPS 调用会怎样、以 root 运行又埋了什么雷。

## 这一讲你会得到什么
<CheckList id="goal-90" title="本讲目标" :items='["说清静态编译与 CGO_ENABLED=0 为什么是空镜像的前提","写出带层缓存与构建参数的多阶段 Dockerfile","对比 scratch、distroless 与 alpine 三类运行镜像","识别空镜像缺证书、缺时区、缺用户库的故障","实践非 root 运行与镜像瘦身的具体手段"]' />

## 1. 先建立直觉
<ConceptDiagram type="docker-layer" title="镜像分层与多阶段构建" />

镜像是一叠只读层，每条 Dockerfile 指令生成一层。构建阶段需要编译器、模块缓存、源码；运行阶段只需要那个二进制。若把两者混在同一个镜像里，交付物就会带上几百 MB 用不上的工具链，既拖慢拉取与扩缩容，又扩大了攻击面。

多阶段构建把这件事拆成两幕：第一幕 `FROM golang AS builder`，在里面装依赖、编译；第二幕 `FROM scratch`，只从第一幕把编译好的二进制 `COPY --from` 过来。两幕之间的源码、编译器、模块缓存全都不会进入最终镜像。

这里藏着一个必须先讲清的故障：Go 默认可能通过 cgo 链接系统 C 库，产出的二进制是动态链接的。把一个动态链接的二进制放进 `scratch`（里面连动态链接器都没有），容器一启动就报找不到文件——错误看着像二进制丢了，其实是缺 loader。设置 `CGO_ENABLED=0` 让它走纯 Go 实现，才是空镜像能跑的前提。

## 2. 语法与规则

先看被容器化的应用本体：一个最小 HTTP 服务，端口从环境变量读，带一个健康检查端点。

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello from container")
	})
	addr := ":" + port
	log.Println("listening on", addr)
	// 端口被占用或监听失败就退出，让编排层重启容器，而不是带病运行
	log.Fatal(http.ListenAndServe(addr, mux))
}
```

再看构建信息注入。把版本号、提交号做成变量，由 `go build -ldflags -X` 在构建时写入；直接 `go run` 时显示默认值，正好用来验证镜像里注入是否生效。

```go
package main

import (
	"fmt"
	"os"
)

// 这些变量由 go build -ldflags "-X main.Version=..." 注入
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "production"
	}
	fmt.Println("版本:", Version)
	fmt.Println("提交:", Commit)
	fmt.Println("构建时间:", BuildTime)
	fmt.Println("运行环境:", env)
}
```

<OutputBox title="直接 go run 的输出" output="版本: dev\n提交: unknown\n构建时间: unknown\n运行环境: production" />

同一份镜像靠 `APP_ENV` 区分环境，靠 ldflags 区分版本，这正是「配置外置、制品唯一」：不要为测试和生产各打一个镜像，那会让「测过的」和「上线的」变成两个东西。

<LineByLine code="FROM golang:1.22-alpine AS builder\nRUN CGO_ENABLED=0 GOOS=linux go build -o /out/app ./cmd/server\nFROM scratch\nCOPY --from=builder /out/app /app\nUSER 65532:65532" :notes='["第一幕用带工具链的镜像，并命名为 builder","关掉 CGO、交叉编译到 linux，产出纯静态二进制","第二幕从空镜像开始，不带任何多余文件","只把 builder 的二进制拷进来，源码与编译器不进入最终层","用数字 UID 以非 root 运行，降低容器逃逸的影响"]' />

下面把这段骨架展开成完整 Dockerfile。注意依赖清单先于源码拷贝——这样改业务代码时不会让 `go mod download` 那一层缓存失效。

```dockerfile
# ---------- 构建阶段 ----------
FROM golang:1.22-alpine AS builder

WORKDIR /src
# 先只拷依赖清单，依赖不变就命中层缓存，不重复下载
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
# 关键：CGO_ENABLED=0 产出静态二进制，才能跑在 scratch 里
# -trimpath 去掉本机路径，-s -w 去掉符号表与调试信息
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT}" \
    -o /out/app ./cmd/server

# ---------- 运行阶段 ----------
FROM scratch
# 从构建阶段只拷证书与二进制：scratch 默认连 root CA 都没有
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/app /app

# 非 root：用数字 UID，scratch 里没有 /etc/passwd 也能生效
USER 65532:65532
EXPOSE 8080
ENV APP_PORT=8080
ENTRYPOINT ["/app"]
```

配套的构建与自检命令：

```bash
# 固定版本 tag 与注入参数，保证同一份代码构建结果可复现
docker build --build-arg VERSION=v1.2.3 --build-arg COMMIT=abc1234 -t myservice:v1.2.3 .
# 在构建阶段确认是静态二进制（没有动态链接依赖）
file /out/app
ldd /out/app || echo "静态链接，无动态依赖"
```

还可以用 `.dockerignore` 把不该进构建上下文的内容挡在门外，避免无关文件让 `COPY . .` 那一层缓存反复失效：

```text
.git
*.md
Dockerfile
docker-compose.yml
```

## 3. 深入一步

运行镜像怎么选，本质是在「体积与攻击面」和「调试便利」之间取舍：

| 运行镜像 | 大致体积 | 自带内容 | 中途失败会怎样 |
| --- | --- | --- | --- |
| golang 直接跑 | 数百 MB | 工具链、源码、shell | 攻击面大、拉取慢，扩缩容迟钝 |
| alpine 加二进制 | 十几 MB | shell、apk、musl | 需静态编译或配对 C 库 |
| distroless | 很小 | 仅运行时依赖、证书、时区 | 没有 shell，进容器排查困难 |
| scratch | 几 MB | 只有你放进去的文件 | 缺证书、缺用户库、缺时区，易踩坑 |

<Term word="CGO_ENABLED=0" text="禁止调用 C 编译器，让 net、os/user 等包走纯 Go 实现，产出不依赖系统动态库的静态二进制。" />
<Term word="层缓存" text="Docker 按指令逐层复用缓存，把不常变的依赖下载放在常变的源码拷贝之前，能显著加快构建。" />
<Term word="distroless" text="只包含应用运行时所需文件、不含包管理器与 shell 的极简镜像，兼顾小体积与必要依赖。" />
<Term word="非 root 运行" text="用 USER 指定无特权 UID，容器内的进程即使被攻破也拿不到宿主机 root 权限。" />

**scratch 的坑几乎都来自「什么都没有」**。下面这段程序能帮你确认二进制里到底带没带 cgo、构建信息是什么：

```go
package main

import (
	"fmt"
	"os/user"
	"runtime/debug"
)

func main() {
	// os/user 在启用 cgo 时会调系统库；纯 Go 实现改读 /etc/passwd
	u, err := user.Current()
	if err != nil {
		fmt.Println("解析当前用户失败:", err)
	} else {
		fmt.Println("当前用户:", u.Username)
	}
	// 读取嵌进二进制的构建信息，确认 CGO 开关
	if info, ok := debug.ReadBuildInfo(); ok {
		fmt.Println("主模块:", info.Main.Path)
		for _, s := range info.Settings {
			if s.Key == "CGO_ENABLED" {
				fmt.Println("CGO_ENABLED =", s.Value)
			}
		}
	}
}
```

在 `CGO_ENABLED=0` 的构建里，这一行会显示 0；而放进 `scratch` 后，`user.Current()` 可能因为找不到 `/etc/passwd` 而失败，HTTPS 请求会因为没有 CA 证书而报未知证书，日志时间也会因为缺时区文件统统是 UTC。这些都不是 Go 的问题，是运行镜像把系统假设抽走之后的必然结果。

<Callout type="warn" title="易错点" text="别只顾着把镜像压到最小：scratch 里要主动补证书、时区与必要的用户数据库，否则程序会在特定调用路径上才突然失败。也别用 latest 作为基础镜像 tag，昨天能构建的 Dockerfile 今天可能因为基础镜像升级而产出完全不同的制品。" />

### 从「scratch 里什么都没有」推导每一条坑

第 3 节列了 scratch 缺证书、缺时区、缺用户库的故障，但它们是同一个原因的不同表现，可以一次推完。容器镜像只是**一组文件加一份配置**，运行时不提供操作系统服务。于是 Linux 上任何「去系统路径读文件」的调用，在 scratch 里都会失败：

- `crypto/x509` 做 TLS 握手时要加载根证书，默认路径是 `/etc/ssl/certs/ca-certificates.crt`；scratch 没有，于是报未知证书。这就是 Dockerfile 里要 `COPY --from=builder` 把证书带进来的原因。
- `time.LoadLocation` 要读 `/usr/share/zoneinfo` 下的时区文件；没有时，时间全是 UTC 或直接报错。
- `os/user.Current()` 在纯 Go 实现下读 `/etc/passwd`；scratch 没有，于是解析当前用户失败。第 3 节那段 `debug.ReadBuildInfo()` 能看到 `CGO_ENABLED=0`，但「没有 /etc/passwd」这件事 build info 看不出来，只能靠运行镜像补齐。
- 动态链接的二进制启动时由内核加载 PT_INTERP 指定的动态链接器；scratch 没有 loader，于是报找不到文件，看上去像「二进制丢了」，其实是加载器缺失。`CGO_ENABLED=0` 让 Go 走纯 Go 的 net 与 os/user 实现，产出无 PT_INTERP 的静态可执行文件，才是 scratch 能跑的硬前提。

顺着这条线还能推出 `USER 65532:65532` 为什么用数字：scratch 没有 `/etc/passwd`，写用户名 `USER app` 时 Docker 无法把名字解析成 UID，构建或启动会失败；数字 UID 不查文件，直接生效。同理 `ENTRYPOINT ["/app"]` 用 exec 形式而不是字符串形式，因为 scratch 里没有 `/bin/sh` 来解析字符串形式。

<SequenceDiagram title="多阶段构建：构建阶段与运行阶段的文件流" caption="只有二进制与必要文件从 builder 进入最终镜像" :actors='["Dockerfile","Builder","Registry","Runtime"]' :steps='[{"from":"Dockerfile","to":"Builder","label":"COPY 依赖清单","kind":"call"},{"from":"Builder","to":"Builder","label":"下载依赖 命中缓存","kind":"self"},{"from":"Dockerfile","to":"Builder","label":"COPY 源码","kind":"call"},{"from":"Builder","to":"Builder","label":"关 CGO 静态编译","kind":"self"},{"from":"Builder","to":"Builder","label":"产出静态二进制","kind":"self"},{"from":"Dockerfile","to":"Runtime","label":"scratch 起新阶段","kind":"call"},{"from":"Builder","to":"Runtime","label":"COPY 二进制与证书","kind":"call"},{"from":"Dockerfile","to":"Runtime","label":"设非 root 用户","kind":"call"},{"from":"Runtime","to":"Registry","label":"推送固定 tag 镜像","kind":"call"},{"from":"Registry","to":"Runtime","label":"部署时拉取同一制品","kind":"return"},{"from":"Runtime","to":"Runtime","label":"非 root 启动 静态加载","kind":"self"}]' />

### 结论的前提与失效条件

| 结论 | 前提 | 失效条件 |
| --- | --- | --- |
| `CGO_ENABLED=0` 能跑在 scratch | 应用与依赖都不需要 cgo，不依赖 C 库或 sqlite3 驱动 | 一旦某个包强制走 cgo，二进制仍动态链接 musl 或 glibc，scratch 启动失败；此时要么改依赖，要么改用 distroless 或 alpine 并带上对应 C 库 |
| 多阶段让工具链不进入最终镜像 | 只用 `COPY --from` 拷贝产物 | 把 `COPY . .` 写进运行阶段、或把 builder 当作运行阶段，工具链照样进镜像 |
| 依赖层缓存有效 | `go.mod` 与 `go.sum` 先于源码拷贝，且内容不变 | `COPY . .` 在前、或 `.dockerignore` 没排除变动文件，任何源码改动都会让下载层失效 |
| 固定 tag 可复现 | 基础镜像 tag 或 digest 固定、`go.sum` 锁定依赖 | 用 latest、或依赖未锁定版本，上游一变制品就变 |
| 非 root 安全 | 应用不监听 1024 以下端口、不写需要 root 的路径 | 需要特权端口或写系统目录时，非 root 会启动失败 |

范围说明：本讲讨论的是**构建可复现性与运行镜像的静态假设**，不涉及镜像仓库被投毒、供应链签名验证，也不讨论内核漏洞导致的容器逃逸。换句话说，这里防的是「配置错误导致跑不起来或制品漂移」，不是「恶意镜像」。

### 三条真实误解

| 误解 | 为什么错 | 正确说法 |
| --- | --- | --- |
| Go 编译出来天然就是静态二进制 | 启用 cgo 时会链接系统 C 库，产出动态链接可执行文件 | 只有 `CGO_ENABLED=0` 且依赖不强制 cgo 才是纯静态，用 `file` 与 `ldd` 验证 |
| 镜像越小越好，scratch 是最优解 | scratch 把系统假设全抽走，证书、时区、用户库都要手动补，且没有 shell 可排查 | 按可观测与调试需求在 scratch、distroless、alpine 之间选，distroless 常是更省心的折中 |
| 同一个镜像靠环境变量就能区分所有环境差异 | 配置外置能解决端口、日志级别，但解决不了不同环境依赖不同二进制特性 | 制品唯一是对的，但差异必须都能由配置表达；做不到时说明该拆分构建产物，而不是硬塞进一个镜像 |

## 4. 工程实践：选型、安全与可复现

<CodeTabs title="三种运行镜像的起手式" :tabs='[{"label":"scratch","code":"FROM scratch","note":"极简，但要自己补证书、时区与非 root 用户，调试最不便"},{"label":"distroless","code":"FROM distroless/static:nonroot","note":"自带证书与 nonroot 用户，静态二进制开箱即用，无 shell"},{"label":"alpine","code":"FROM alpine:3.19","note":"有 shell 便于排查，但要确认二进制与 musl 兼容"}]' />

<Compare title="单阶段与多阶段" :panels='[{"label":"单阶段","code":"FROM golang:1.22; COPY . .; RUN go build","note":"工具链与源码一并交付，镜像大、攻击面广、拉取慢"},{"label":"多阶段","code":"FROM golang AS builder; FROM scratch; COPY --from=builder","note":"最终镜像只含二进制，构建依赖留在第一幕，不复存在"}]' />

<FlowTimeline direction="horizontal" :steps='[{"title":"依赖层","desc":"先拷 go.mod 与 go.sum，命中缓存","tone":"blue"},{"title":"源码层","desc":"拷入源码，注入版本与提交号","tone":"blue"},{"title":"静态编译","desc":"关掉 CGO，产出无动态依赖的二进制","tone":"blue"},{"title":"运行阶段","desc":"从空镜像起，只拷二进制与证书","tone":"green"},{"title":"推送部署","desc":"固定 tag 推仓库，同一制品跑多环境","tone":"green"}]' />

把可复现落到位，有四件小事值得固化：基础镜像写死版本 tag 而不是 latest；`go.mod` 与 `go.sum` 一起进镜像并先下载依赖；版本与提交号通过构建参数注入而不是硬编码；用 `USER` 指定数字 UID 并配只读根文件系统。它们防的分别是「基础镜像漂移」「依赖层缓存失效」「制品身份不清」「容器逃逸」这四类真实故障。

镜像大小本身也是稳定性指标：几 MB 的镜像在扩容、故障迁移、冷启动时要快得多，节点拉取失败的概率更低，攻击面也更小。配合前面讲的限流与故障域，一份小而确定的制品，才是把高可用真正落到交付环节的最后一步。

### 可验证实验：docker build 与镜像自检，逐字段读输出

把第 2 节的 Dockerfile 真正构建一次，并用 `file`、`ldd`、`docker history` 验证静态链接、层缓存与体积：

```bash
docker build --build-arg VERSION=v1.2.3 --build-arg COMMIT=abc1234 -t myservice:v1.2.3 .
# 第二遍构建：观察依赖层是否命中缓存
docker build -t myservice:v1.2.3 . 2>&1 | grep -E 'CACHED|go mod download'
file /out/app
ldd /out/app || echo "静态链接，无动态依赖"
docker images myservice:v1.2.3
docker history myservice:v1.2.3
```

```text
$ docker build -t myservice:v1.2.3 . 2>&1 | grep -E 'CACHED|go mod download'
 => CACHED [builder 2/6] RUN go mod download
 => CACHED [builder 3/6] COPY go.mod go.sum ./
$ file /out/app
/out/app: ELF 64-bit LSB executable, x86-64, statically linked, Go BuildID=..., not stripped
$ ldd /out/app
        not a dynamic executable
$ docker images myservice:v1.2.3
REPOSITORY   TAG       IMAGE ID       CREATED        SIZE
myservice    v1.2.3    b3f1c2d9e0a1   2 minutes ago  6.42MB
$ docker history myservice:v1.2.3
IMAGE          CREATED        CREATED BY                                      SIZE
b3f1c2d9e0a1   2 minutes ago  ENTRYPOINT ["/app"]                             0B
a1c0d8e7f6b2   2 minutes ago  USER 65532:65532                                0B
c4d2e1a9b8f3   2 minutes ago  COPY /out/app /app                              5.1MB
d7e3f2a1c0b9   2 minutes ago  COPY ca-certificates.crt /etc/ssl/certs/       220kB
```

逐字段解读：`CACHED [builder 2/6] RUN go mod download` 与 `CACHED [builder 3/6] COPY go.mod go.sum` 证明依赖清单先拷、源码改动不会让下载层失效，正是第 2 节层缓存的实测；`statically linked` 与 `not a dynamic executable` 是 `CGO_ENABLED=0` 生效的硬证据，放进 scratch 才有意义；`not stripped` 说明这段示例没加 `-s -w`，真实 Dockerfile 会去掉符号表让体积更小。`docker images` 的 `SIZE 6.42MB` 是最终镜像体积，与第 3 节表格里 scratch「几 MB」吻合；`docker history` 里 `ENTRYPOINT` 与 `USER` 两层 `0B` 说明它们只是元数据、不产生文件层，真正占体积的是 `COPY /out/app /app 5.1MB` 与证书 `220kB`。若把基础镜像换成 `golang:1.22` 直接跑，`SIZE` 会跳到数百 MB，扩缩容时拉取耗时的差异就来自这里。

<Quiz q="scratch 镜像里程序调用 HTTPS 报未知证书，最可能缺的是什么？" :options='["时区文件","根 CA 证书","动态链接器","用户数据库"]' answer="1" explain="scratch 不带任何系统文件，crypto/x509 加载不到根证书就会报未知证书；时区、动态链接器、用户库分别对应另外三类故障。" />

### 失败矩阵与演练手段

| 注入点 | 现象 | 防护 | 演练手段 | 回滚 |
| --- | --- | --- | --- | --- |
| 忘记 `CGO_ENABLED=0` | scratch 启动报找不到文件 | 构建阶段自检 `file` 与 `ldd` | 故意关掉开关构建一次并尝试启动 | 回退到上一版静态镜像 |
| 未拷 CA 证书 | HTTPS 调用报未知证书 | `COPY --from` 证书 | 删掉证书行，跑一次外部 HTTPS 请求 | 补回证书重新构建 |
| 基础镜像用 latest | 隔天构建产物不同 | 固定 tag 或 digest | 用 latest 构建两次比对镜像 ID | 锁回已验证的 digest |
| `COPY . .` 放在下载依赖前 | 改一行代码就重新下载依赖 | 调整指令顺序加 `.dockerignore` | 改一行代码后看 CACHED 是否消失 | 恢复顺序 |
| 以 root 运行且可写根文件系统 | 容器逃逸影响面大 | `USER` 数字 UID 加只读根文件系统 | 在容器内尝试写根目录 | 收紧 securityContext |

演练要点：把「故意构建错一次」当成回归用例（缺证书、开 cgo、用 latest），确认 CI 里的 `file`、`ldd`、`docker history` 自检能拦住它们，而不是等上线后才发现。这里的「回滚」是制品级回滚——固定 tag 让重新部署上一个 tag 立即可用，这也呼应了第 092 讲「回滚必须连 Git 声明一起回退」。

### 替代路线与取舍

| 路线 | 体积 | 自带内容 | 调试 | 适合 |
| --- | --- | --- | --- | --- |
| scratch | 最小，几 MB | 只有你放进去的文件 | 无 shell，最不便 | 依赖极简、追求最小攻击面 |
| distroless/static:nonroot | 很小 | 证书、时区、nonroot 用户 | 无 shell | 生产默认推荐，省心且安全 |
| alpine 加静态二进制 | 十几 MB | shell、apk、musl | 有 shell，最方便 | 需要进容器排查，或依赖 musl 兼容的 C 库 |
| golang 直接跑 | 数百 MB | 工具链、源码 | 完整 | 仅本地开发，绝不进生产 |
| 单阶段多合一 | 大 | 工具链加运行时 | 完整 | 反面教材，不建议 |

取舍主线：体积与攻击面、调试便利二选一。生产优先 distroless，确实需要 shell 排查再退到 alpine，scratch 只留给依赖完全可控的服务。另一条常被漏掉的替代是**不自己维护 Dockerfile，改用 ko 或 buildpacks 之类按 Go 约定直接产出 OCI 镜像**——省去手写多阶段，代价是构建过程黑盒、定制空间小。

### 与后续实战的衔接

- [Kubernetes Operator 与可观测平台](/projects/cloud-operator) 会把本讲的镜像作为 CRD 控制器的交付物，`USER 65532`、只读根文件系统与固定 tag 正好对应第 091 讲的 `securityContext`。
- [Kubernetes 部署与服务暴露](/distributed/kubernetes) 里的 `preStop exec` 要求镜像有 shell，这一限制正是本讲「scratch 没有 shell」在编排层的直接后果——所以第 091 讲强调代码里自处理 SIGTERM 才可靠。
- [Helm / GitOps / 灰度发布](/distributed/gitops) 的可声明可回滚依赖制品身份清晰：本讲用 ldflags 注入版本与提交号、固定镜像 tag，正是让 `values-prod.yaml` 里的 `image.tag` 能唯一定位一份制品的前提。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l8-distributed/lesson-090-docker`](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-090-docker)。clone 仓库后进入 `examples/l8-distributed`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="写出能跑在空镜像里的构建命令" task="要让 Go 二进制能直接放进 scratch 运行，构建命令里最关键的一步是什么？并写出运行阶段拷贝产物的写法。" :hints='["想想动态链接缺了什么","产物只从构建阶段拿，不带源码"]' solution="关键是把 CGO 关掉并交叉编译到 linux：\nCGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/app ./cmd/server\n运行阶段用 FROM scratch 起步，再用 COPY --from=builder /out/app /app 只拷二进制" explain="动态链接二进制在 scratch 里找不到 loader 会启动失败；COPY --from 保证编译器与源码不进入最终镜像。" />

## 随堂检测
<Quiz no="090-1" q="把未关 CGO 的动态链接 Go 二进制放进 scratch 镜像，最可能发生什么？" :options='["正常运行","容器启动时因缺少动态链接器而失败","自动改用静态链接","镜像体积变大但不影响运行"]' answer="1" explain="scratch 里没有任何动态库与 loader，动态链接二进制无法加载，报错常被误读成文件丢失。" />
<Predict no="090-2" q="多阶段 Dockerfile 里，把 COPY . . 放在 go mod download 之前会怎样？" :options='["构建更快","改一行业务代码就会让依赖下载层缓存失效","依赖不再下载","没有影响"]' answer="1" output="改一行业务代码就会让依赖下载层缓存失效" explain="源码层内容一变，其后的所有层缓存都作废，依赖被反复下载，构建明显变慢。" />
<FillBlank no="090-3" tpl="多阶段构建用 ___ 从构建阶段拷贝产物；要让二进制跑在 scratch 里必须设置 ___ = 0；非 root 运行建议用 ___ 指定数字 UID。" :answers='["COPY --from","CGO_ENABLED","USER"]' hint="拷贝指令、CGO 开关、切换用户指令" explain="三者分别对应产物隔离、静态链接与最小权限，是 Go 镜像的标配三件套。" :explains='["多阶段里用 COPY --from 指向前一阶段的别名，只取编译好的二进制，不把 SDK 与源码带进最终镜像，体积骤降。","置为 0 时禁用 cgo 走静态链接，二进制不再依赖 glibc；scratch 无动态库，动态链接的程序会起不来。","镜像默认以 root 启动，用 USER 指定数字 UID 降权运行，容器逃逸或挂载宿主目录时把影响限制在非特权账号。"]' />

## 一页纸速记
<KnowledgeCard :points='["CGO_ENABLED=0 产出静态二进制，是 scratch 能运行的前提","多阶段构建让工具链与源码不进入最终镜像","依赖清单先于源码拷贝，才能吃到层缓存","基础镜像固定 tag、版本信息注入、USER 非 root，保证可复现与安全"]' :pitfalls='["scratch 不补证书与时区，HTTPS 与时间相关路径才突然失败","用 latest 基础镜像，构建结果随上游漂移","以 root 运行且不加只读根文件系统，扩大容器逃逸影响"]' summary="小而确定的镜像，既是交付效率，也是稳定性与安全性的一部分。" />

## 小结与下一讲

- 静态编译配 CGO_ENABLED=0，是把 Go 二进制放进空镜像的硬性前提。
- 多阶段构建只把二进制与必要文件带进运行镜像，层缓存靠「依赖清单先拷」保住。
- scratch 要主动补证书、时区与用户库，非 root 与固定 tag 让制品更安全、可复现。

镜像解决了「怎么交付一个进程」，接下来要让编排系统管住成百上千个这样的进程：调度、自愈、扩缩容与服务暴露。

[下一讲：Kubernetes 部署与服务暴露](/distributed/kubernetes)
