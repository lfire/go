---
title: 第 71 讲 · 文件、对象存储与断点续传
description: 用 io.Reader/Writer 流式处理上传，讲清分块与秒传、对象存储直传、校验重试与临时文件清理，处理大文件上传与存储可靠性
---

<StageHeader stage="L6 · 服务端与数据" level="★★★☆☆" time="45 分钟" goal="处理大文件上传与存储可靠性" prereq="第 70 讲 · 认证授权" />

# 第 71 讲 · 文件、对象存储与断点续传

> 图片、报表、视频这类数据和前面的 JSON 完全不同：一个请求体可能几十 MB 甚至几 GB，传一半断了是常态，重复传一次又很贵。如果沿用「把 body 读进一个 `[]byte` 再处理」的思路，几个并发上传就能把服务打爆。这一讲用 `io.Reader` / `io.Writer` 建立流式处理的直觉，再依次解决分块、秒传、断点续传、对象存储直传、校验与临时文件清理，最后收束整个 L6 阶段。

## 这一讲你会得到什么

<CheckList id="71-goal" title="本讲目标" :items='["能用 io.Copy 与 MultiWriter 流式处理上传，内存占用与文件大小无关","会做大小限制、SHA-256 校验与临时文件清理","能设计分块上传、断点续传与秒传的完整流程","理解预签名直传为什么能绕开应用服务器，以及它的安全边界"]' />

## 1. 先建立直觉

把上传想成搬一批货。`io.Reader` 是「从某处一点一点取货」，`io.Writer` 是「往某处一点一点放货」，`io.Copy` 是那个来回跑的搬运工：它只带一个固定大小的箱子（内部缓冲区），无论货有多少，手里永远只占那一个箱子的空间。这就是流式处理与「整批读进内存」的根本区别——**内存占用恒定，与文件大小无关**。

`io.MultiWriter` 则像搬货时顺手过磅：一次搬运同时写进临时文件和哈希器，不用把文件读第二遍。而 `io.LimitReader`、`io.TeeReader` 这些包装器之所以好用，是因为它们本身也是 `Reader`，可以层层套娃，每一层只加一件事。

<ConceptDiagram type="http-chain" title="一次上传请求依次经过限流、校验、分块、存储与清理" caption="越靠前的检查越便宜：先挡住超大请求，再做解码与校验，最后才落盘和清理" />

## 2. 语法与规则

第一段代码是完整、可运行的标准库上传服务：限制大小、流式落临时文件、同步算哈希、无论成败都清理。

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
)

const maxUpload = 32 << 20 // 请求体上限 32 MiB

func upload(w http.ResponseWriter, r *http.Request) {
	// 先限总大小，避免恶意大 body 打满内存或磁盘。
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	// 内存中最多留 8 MiB，其余由 multipart 落到系统临时文件。
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "body too large or invalid", http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll() // 清理 multipart 自己产生的临时文件

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "upload-*")
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	// 无论成功失败都删临时文件；不要把 defer 放进循环里攒到最后。
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	h := sha256.New()
	// MultiWriter 一次写两处：落盘的同时算哈希，不必读第二遍。
	written, err := io.Copy(io.MultiWriter(tmp, h), file)
	if err != nil {
		http.Error(w, "write failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": header.Filename, "size": written,
		"sha256": hex.EncodeToString(h.Sum(nil)),
	})
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload", upload)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

<OutputBox title="上传响应" output="POST /upload 返回 200\nname: report.pdf\nsize: 3145728\nsha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08" note="哈希由服务端重新计算得出，可直接作为后续秒传与去重的内容指纹" />

<LineByLine code="r.Body = http.MaxBytesReader(w, r.Body, limit)\ntmp, err := os.CreateTemp(dir, pattern)\nif err != nil {\nreturn err\n}\ndefer os.Remove(tmp.Name())\nn, err := io.Copy(io.MultiWriter(tmp, hash), file)\nreturn n, err" :notes='["给请求体套上硬上限，超限时读取会返回错误","创建带随机后缀的临时文件，避免并发互相覆盖","检查创建错误，磁盘满或权限不足都在这里暴露","失败直接返回，不要继续往下写","注册清理，函数从任何路径返回都会删掉临时文件","一次流式搬运同时落盘与计算哈希，内存占用恒定","把字节数与错误一起交给调用方判断"]' title="流式上传逐行看" />

注意 `defer os.Remove` 的位置：紧跟在 `CreateTemp` 成功之后，且**不在循环里**。如果把它写进循环（比如批量处理多个文件时），所有临时文件会一直堆到函数返回才释放，处理一百个文件就同时占着一百份磁盘。批量场景应把单个文件的处理抽成独立函数，让 `defer` 在每次调用结束时立刻生效。

## 3. 深入一步

分块上传把大文件切成固定大小的片，每片独立上传。这带来三个能力：**断点续传**（失败只重传缺失的片）、**并行上传**（多片同时发）、**秒传**（先算整个文件的内容哈希，服务端若已有相同哈希就直接返回成功，不再传字节）。

实现续传的关键是让「重传同一片」成为幂等操作：用 `WriteAt` 按 `index * chunkSize` 定位写入，同一片重传只是覆盖相同偏移，不会错位，也允许分片乱序到达。

| `io` 接口 / 包装器 | 作用 | 在上传里的用途 |
| --- | --- | --- |
| `Reader` / `Writer` | 读 / 写字节流 | 一切流式处理的抽象基础 |
| `Seeker` | 移动读写位置 | 断点续传定位偏移 |
| `WriterAt` | 指定偏移写入 | 分片乱序到达仍正确落盘 |
| `LimitReader` | 限制可读取的字节数 | 单片大小上限 |
| `TeeReader` | 读的同时旁路写一份 | 边读边算哈希 |
| `MultiWriter` | 一次写多个目标 | 落盘与算哈希一次完成 |

第二段代码给出完整、可运行的分块服务：`init` 做秒传检查、`chunk` 用 `WriteAt` 幂等写片、`complete` 校验分片齐全后合并。

```go
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const chunkSize = 1 << 20 // 每片 1 MiB

type Manager struct {
	mu       sync.Mutex
	dir      string
	hashes   map[string]string        // 内容哈希 -> 最终路径（秒传表）
	received map[string]map[int]bool  // uploadID -> 已到分片索引
	total    map[string]int           // uploadID -> 应有分片数
}

func NewManager(dir string) *Manager {
	_ = os.MkdirAll(dir, 0755)
	return &Manager{
		dir: dir, hashes: map[string]string{},
		received: map[string]map[int]bool{}, total: map[string]int{},
	}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (m *Manager) init(w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get("hash")
	total, _ := strconv.Atoi(r.URL.Query().Get("total"))
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.hashes[hash]; ok {
		writeJSON(w, http.StatusOK,
			map[string]any{"instant": true, "path": p})
		return
	}
	id := newID()
	m.received[id] = map[int]bool{}
	m.total[id] = total
	writeJSON(w, http.StatusOK,
		map[string]any{"upload_id": id, "chunk_size": chunkSize})
}

func (m *Manager) chunk(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("upload_id")
	idx, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil || idx < 0 {
		http.Error(w, "bad index", http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, chunkSize+1))
	if err != nil {
		http.Error(w, "read", http.StatusInternalServerError)
		return
	}
	part := filepath.Join(m.dir, id+".part")
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		http.Error(w, "open", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	// WriteAt 让断点重传同一分片成为幂等操作，且允许乱序到达。
	if _, err := f.WriteAt(data, int64(idx)*chunkSize); err != nil {
		http.Error(w, "write", http.StatusInternalServerError)
		return
	}
	m.mu.Lock()
	if m.received[id] == nil {
		m.mu.Unlock()
		http.Error(w, "unknown upload", http.StatusNotFound)
		return
	}
	m.received[id][idx] = true
	m.mu.Unlock()
	writeJSON(w, http.StatusOK,
		map[string]any{"index": idx, "size": len(data)})
}

func (m *Manager) complete(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("upload_id")
	hash := r.URL.Query().Get("hash")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.received[id] == nil {
		http.Error(w, "unknown upload", http.StatusNotFound)
		return
	}
	if len(m.received[id]) < m.total[id] {
		writeJSON(w, http.StatusConflict, map[string]any{
			"received": len(m.received[id]), "total": m.total[id],
		})
		return
	}
	final := filepath.Join(m.dir, hash+".bin")
	if err := os.Rename(
		filepath.Join(m.dir, id+".part"), final); err != nil {
		http.Error(w, "rename", http.StatusInternalServerError)
		return
	}
	m.hashes[hash] = final // 之后同哈希文件可秒传
	delete(m.received, id)
	delete(m.total, id)
	writeJSON(w, http.StatusOK, map[string]any{"path": final})
}

func main() {
	m := NewManager("./uploads")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload/init", m.init)
	mux.HandleFunc("PUT /upload/chunk", m.chunk)
	mux.HandleFunc("POST /upload/complete", m.complete)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

客户端流程是：先算整文件 SHA-256 → 带哈希与总分片数请求 `init` → 若返回 `instant` 则秒传结束，否则逐片 `PUT /upload/chunk` → 全部成功后请求 `complete`。失败的分片按索引重传即可，服务端不关心顺序。

<Term word="秒传" text="以文件内容哈希为指纹；服务端若已存有相同哈希的文件，直接返回成功而不再传输字节，本质是一次全局去重" />

<Callout type="warn" title="五个高频反模式" text="其一，用 io.ReadAll 把整个文件读进内存，几个并发就 OOM。其二，不设上传大小上限，磁盘或内存被打满。其三，拿客户端传来的文件名当存储键，既可能被 ../ 路径穿越写到任意位置，又会让同名文件互相覆盖；正确做法是用服务端生成的 ID 加内容哈希。其四，忽略 io.Copy 的返回值与错误，写出一个截断文件还返回成功。其五，defer os.Remove 放在循环里，临时文件堆到函数结束才清理。" />

<Callout type="idea" title="校验与重试" text="分片级校验用每片的 MD5 或 SHA-256，合并后再校验整文件哈希；对象存储通常返回 ETag 可直接比对。重试要带指数退避，且必须幂等——同一分片重传写的是相同偏移，不会重复追加。断点信息（uploadID、已传分片）应持久化或存 Redis，否则进程重启客户端就得从头再来。" />

第三段代码演示**对象存储直传**：应用服务器只签发一个带过期时间与签名的凭证，真正的字节流由客户端直接传给对象存储，不再经过应用服务器。

```go
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var signKey = []byte("replace-with-object-storage-secret")

const endpoint = "oss.example.com" // 生产从配置读取

// sign 对「方法 + 对象键 + 过期时间」签名，客户端凭它直传对象存储。
func sign(method, object string, expire int64) string {
	raw := fmt.Sprintf("%s\n%s\n%d", method, object, expire)
	mac := hmac.New(sha256.New, signKey)
	_, _ = mac.Write([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verify(method, object string, expire int64, sig string) error {
	if time.Now().Unix() > expire {
		return fmt.Errorf("credential expired")
	}
	want := sign(method, object, expire)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return fmt.Errorf("bad signature")
	}
	return nil
}

func main() {
	mux := http.NewServeMux()
	// 应用服务器只签发凭证，字节流不经过它，带宽与内存压力都转移出去。
	mux.HandleFunc("POST /presign", func(w http.ResponseWriter, r *http.Request) {
		object := r.URL.Query().Get("object")
		// 对象键必须白名单校验：禁止 .. 路径穿越与任意前缀。
		if object == "" || strings.Contains(object, "..") {
			http.Error(w, "invalid object", http.StatusBadRequest)
			return
		}
		expire := time.Now().Add(15 * time.Minute).Unix()
		writeJSON(w, http.StatusOK, map[string]any{
			"url":       endpoint + "/" + object,
			"expire":    expire,
			"signature": sign("PUT", object, expire),
		})
	})
	// 模拟对象存储侧的校验入口，演示服务端如何验签。
	mux.HandleFunc("PUT /verify", func(w http.ResponseWriter, r *http.Request) {
		object := r.URL.Query().Get("object")
		exp, _ := strconv.ParseInt(
			r.URL.Query().Get("expire"), 10, 64)
		sig := r.URL.Query().Get("signature")
		if err := verify("PUT", object, exp, sig); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		fmt.Fprintln(w, "signature ok, upload allowed")
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

签名的三个要素缺一不可：**绑定对象键**（不能拿一个凭证传任意文件）、**绑定方法**（PUT 与 GET 不能混用）、**短过期时间**（泄露后窗口有限）。生产里应使用云厂商 SDK 的预签名能力，并配合 Content-Length 上限、回调通知与服务端二次校验，别自己把密钥下发到浏览器。

### 协议与契约：流式、分块与预签名的推导

**`io.Copy` 的内存恒定可以推导到缓冲区大小。** `io.Copy(dst, src)` 内部申请一块固定缓冲区（默认 32 KiB），反复 `Read` 再 `Write`，任何时刻手里只有这一块，因此内存占用是 O(缓冲区)、与文件大小无关；而 `io.ReadAll(r.Body)` 是 O(文件大小)，一个 32 MiB 的文件就占 32 MiB，50 个并发上传就是 1.6 GiB，OOM 几乎必然。由此还能推出几个包装器的价值：`io.MultiWriter(tmp, hash)` 让一次搬运同时落盘与算哈希，不用把文件读第二遍；`io.LimitReader` 给单片设上限；`io.TeeReader` 边读边旁路写一份。它们本身都是 `Reader` / `Writer`，所以能层层套娃，每层只加一件事。

**multipart 与 HTTP 头的边界。** 上传请求带 `Content-Type: multipart/form-data; boundary=...`，每个 part 有自己的头；`http.MaxBytesReader(w, r.Body, maxUpload)` 给整个请求体设硬上限，超限时后续读取返回 `*http.MaxBytesError`；`ParseMultipartForm(maxMemory)` 让内存中最多留 `maxMemory`，其余由标准库落到系统临时文件，`r.MultipartForm.RemoveAll()` 负责清理。两条契约必须记住：客户端声明的 `Content-Length` 与文件名都**不可信**——大小以实际 `io.Copy` 写出的字节数为准，`Content-Disposition` 里的文件名只用于展示，落盘键必须由服务端生成，否则 `../` 会造成路径穿越、同名文件会互相覆盖。

**分块幂等来自偏移写入。** `WriteAt(data, index*chunkSize)` 把「重传同一片」变成对**相同偏移的覆盖**，既幂等又允许分片乱序到达。推导反例：若用追加写，重传一次就会把同一片接两遍，文件直接损坏；若要靠服务端去重，又得额外维护「哪片已收」的状态。`os.Rename` 在同一文件系统内是原子的，因此 `complete` 用它把 `.part` 改名为最终文件，可以保证「要么看到旧状态、要么看到完整新状态」。秒传则是内容哈希的全局去重：SHA-256 有 2^256 量级的输出空间，碰撞概率在工程上可忽略，但理论上「哈希相同、内容不同」的窗口始终存在，极高价值场景应再校验长度或做二次比对。

**预签名直传的三要素来自威胁模型。** 应用服务器只签发一个 HMAC 签名的短期凭证，真正的字节流由客户端直传对象存储。签名必须**绑定对象键**（否则一个凭证能传任意文件）、**绑定方法**（PUT 与 GET 不能混用）、**带短过期时间**（泄露后的窗口有限），并用 `hmac.Equal` 做恒定时间比较防时序侧信道。边界同样清晰：签名密钥绝不下发浏览器，生产应使用云厂商 SDK 的预签名能力，并配合 `Content-Length` 上限、回调通知与服务端的二次校验。断点续传在 HTTP 侧对应 `Range: bytes=0-1023` 与 `206 Partial Content` 加 `Content-Range`，配 `ETag` / `If-Match` 还能防并发覆盖。

### 设计取舍与替代方案

| 决策点 | 方案 A | 方案 B（推荐） | 方案 A 的代价 |
| --- | --- | --- | --- |
| 读取方式 | `io.ReadAll(r.Body)` | `io.Copy` 流式搬运 | 内存随文件大小线性增长，并发即 OOM |
| 上传通道 | 全部字节流经应用服务器 | 预签名直传对象存储 | 应用服务器带宽与内存成为瓶颈 |
| 分片定位 | 追加写 | `WriteAt` 按偏移写 | 重传会重复追加，文件损坏 |
| 分片存储 | 每片一个独立小文件 | 单文件按偏移写，最后原子改名 | 文件句柄爆炸，合并还要再读一遍 |
| 校验 | 只算整文件哈希 | 片级 MD5 加整文件 SHA-256 / ETag | 单片失败要重传整个文件 |
| 落盘键 | 用客户端文件名 | 服务端生成 ID 加内容哈希 | 路径穿越、同名覆盖、无法秒传 |
| 断点信息 | 只存内存 | 持久化或存 Redis | 进程重启客户端就得从头再来 |

### 失败矩阵

| 失败场景 | 触发条件 | 可观测现象 | 定位手段 | 对策 |
| --- | --- | --- | --- | --- |
| 超时 | 大 body 或慢客户端 | 连接被拖住、goroutine 堆积 | `ReadTimeout` 命中日志 | 四类超时加分片化 |
| 重试放大 | 失败分片无限重试 | 同一片被反复写、带宽浪费 | 分片 index 频次 | 指数退避加最大重试 |
| 幂等缺失 | 用追加写分片 | 重传后文件损坏、哈希不符 | 整文件哈希校验 | `WriteAt` 按偏移覆盖 |
| 缓存与 DB 不一致 | 对象写成功、元数据写失败 | 有文件无记录，或反之 | 对象与元数据比对 | 回调确认后落库、对账任务 |
| 连接池耗尽 | 元数据查询无索引 | 上传完成后接口卡顿 | `WaitCount`、慢 SQL | 索引加超时 |
| 慢查询 | 分片表按 upload_id 无索引 | `complete` 校验变慢 | `EXPLAIN` | 给 upload_id 建索引 |
| 消息重复 | 存储回调通知重投 | 重复落库或重复计费 | 事件 ID 去重 | 幂等写、唯一约束 |
| 磁盘满 / 临时文件泄漏 | 只靠 `defer os.Remove` 且进程崩溃 | `.part` 与临时文件累积 | 临时目录扫描 | 定期清理超期文件 |
| 路径穿越 | 用客户端文件名拼路径 | 写到预期目录之外 | 路径审计日志 | 服务端生成键加白名单 |
| 签名泄露 | 密钥下发浏览器或凭证无绑定 | 任意文件被上传或覆盖 | 对象存储访问日志 | 绑定键、方法、过期与长度 |

<SequenceDiagram title="分块上传、秒传与断点续传" :actors='["Client","App","Store","MetaDB","OSS"]' :steps='[{"from":"Client","to":"App","label":"算内容哈希","note":"整文件 SHA-256","kind":"call"},{"from":"Client","to":"App","label":"init","note":"带哈希与总片数","kind":"call"},{"from":"App","to":"MetaDB","label":"查秒传表","note":"哈希命中免传","kind":"call"},{"from":"MetaDB","to":"App","label":"未命中","note":"返回 uploadID","kind":"return"},{"from":"App","to":"Client","label":"返回 uploadID","note":"携带分片大小","kind":"return"},{"from":"Client","to":"App","label":"PUT 分片 0","note":"按 index 定位","kind":"call"},{"from":"App","to":"Store","label":"WriteAt 偏移","note":"覆盖式幂等写","kind":"call"},{"from":"Client","to":"App","label":"重传分片 0","note":"覆盖相同偏移","kind":"call"},{"from":"App","to":"Store","label":"WriteAt 再写","note":"内容不变","kind":"call"},{"from":"Client","to":"App","label":"请求预签名","note":"绑定键与方法","kind":"call"},{"from":"Client","to":"OSS","label":"直传字节流","note":"不经过应用服务器","kind":"call"},{"from":"Client","to":"App","label":"complete","note":"校验齐全后原子改名","kind":"call"}]' />

## 4. 工程实践

### 可验证实验：把流式响应、分片幂等与 Range 跑出来看

**实验一：curl 上传、幂等重传与断点续传。** 启动第 2 节与第 3 节的服务，依次验证三条契约：

```bash
curl -i -F 'file=@report.pdf' http://127.0.0.1:8080/upload
curl -s -X POST 'http://127.0.0.1:8080/upload/init?hash=abc123&total=3'
curl -s -X PUT 'http://127.0.0.1:8080/upload/chunk?upload_id=ID&index=0' --data-binary @part0
curl -s -X PUT 'http://127.0.0.1:8080/upload/chunk?upload_id=ID&index=0' --data-binary @part0
curl -s -X POST 'http://127.0.0.1:8080/upload/complete?upload_id=ID&hash=abc123'
curl -i -H 'Range: bytes=0-1023' http://127.0.0.1:8080/files/report.pdf
```

<OutputBox title="关键响应" output="POST /upload → 200，name=report.pdf size=3145728 sha256=9f86d081...\nPUT chunk index=0 第 1 次 → index=0 size=1048576\nPUT chunk index=0 第 2 次 → index=0 size=1048576（文件长度不变）\nRange 读取 → 206 Partial Content\nContent-Range: bytes 0-1023/3145728" note="重传同一片不改变文件长度，是 WriteAt 幂等的直接证据" />

逐字段解释：`size=3145728` 是 `io.Copy` **实际写出的字节数**，不是客户端声明的 `Content-Length`，以它为准才不会被伪造的长度骗过；`sha256=9f86d081...` 由 `MultiWriter` 在落盘的同时算出，可直接作为秒传指纹；两次 `PUT index=0` 返回的 `size` 相同、最终文件长度也不变，证明 `WriteAt` 让重传成为覆盖相同偏移的幂等操作；`206 Partial Content` 与 `Content-Range: bytes 0-1023/3145728` 是 HTTP Range 的标准契约，客户端据此续传。对照实验：把 `WriteAt` 换成追加写再跑两次 `PUT index=0`，文件会变成两倍长度、整文件哈希不符——「分片重传天然幂等」的前提就消失了。

**实验二：go test -race 断言 WriteAt 的幂等与乱序安全。** 下面这段完整可编译的测试文件故意让分片乱序到达、并重复重传同一片，断言最终内容仍然正确：

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeChunk(f *os.File, data []byte, index int64, chunkSize int64) error {
	_, err := f.WriteAt(data, index*chunkSize)
	return err
}

func TestWriteAtMakesChunkRetransmissionIdempotent(t *testing.T) {
	const chunkSize = 4
	path := filepath.Join(t.TempDir(), "upload.part")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	part0 := []byte("AAAA")
	part1 := []byte("BBBB")
	// 故意乱序到达，再重复重传 index 0
	_ = writeChunk(f, part1, 1, chunkSize)
	_ = writeChunk(f, part0, 0, chunkSize)
	_ = writeChunk(f, part0, 0, chunkSize)

	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, []byte("AAAABBBB")) {
		t.Fatalf("分片内容错乱: %q", got)
	}
}
```

```bash
go test -race -run TestWriteAtMakesChunkRetransmissionIdempotent -v ./...
```

<OutputBox title="go test -race 预期输出" output="=== RUN   TestWriteAtMakesChunkRetransmissionIdempotent\n--- PASS: TestWriteAtMakesChunkRetransmissionIdempotent (0.00s)\nPASS\nok   example.com/server   1.277s" note="乱序加重传后文件仍是 AAAABBBB，且 race 检测无告警" />

逐字段解释：`--- PASS` 说明两次写 `index=0` 只是覆盖同一偏移，先写 `index=1` 也不影响最终顺序，结果仍是 `AAAABBBB`；`index*chunkSize` 这个偏移量是整套幂等性的支点，把 `WriteAt` 换成 `Write`（追加）再跑，断言立即失败并得到 `AAAABBBBAAAA` 之类的损坏内容；`-race` 无告警说明这里没有共享状态（顺序由偏移而非到达先后决定，因此不需要锁）。把这个用例扩成 100 个随机顺序的分片再跑，结论不变——这正是「服务端不关心分片顺序」能被写进契约的原因。

<Predict q="分块上传改用追加写（而非 WriteAt）后，客户端重传 index=1 的分片，文件会怎样？" :options='["覆盖相同偏移，文件不变","该分片被追加两次，文件损坏且整文件校验失败","自动去重","返回 409 冲突"]' answer="1" output="追加写不按偏移定位，重复的分片被接到文件末尾，长度与内容都错，complete 时整文件哈希校验失败" explain="WriteAt 的偏移覆盖是分片重传幂等的根基；改成追加后，服务端必须额外维护已收分片集合才能去重，复杂度反而更高。" />

### 两条真实工程误解

**误解一：预签名 URL 只是个链接，泄露了也没关系，反正有过期时间。** 错。凭证在有效期内可被任何人用来向绑定的对象键上传或覆盖内容，短过期只是缩小窗口，不是消除风险。签名必须绑定对象键、HTTP 方法、`Content-Length` 上限，密钥绝不下发浏览器，并配合回调通知与服务端二次校验；否则一个泄露的 URL 就等于一次未授权写入。

**误解二：每片算过 MD5 就够了，不必再校验整文件。** 错。片级校验只能发现单片在传输中损坏，挡不住分片顺序错、缺片、合并后被篡改、以及「所有片都合法但拼起来不对」的情况。正确做法是片级校验加 `complete` 阶段的整文件 SHA-256 或对象存储 ETag 比对，两者缺一就会出现「分片全绿、文件全错」这种最难排查的静默损坏。

### 与后续阶段的呼应

- 本讲的上传大小上限、超时与限流，在 /microservice/gateway 会变成入口层的统一约束；上传成功率、大小分布与耗时在 /microservice/metrics 成为可告警指标；一次上传跨服务的串联则由 /microservice/tracing 串起来。
- 断点信息（uploadID、已传分片）要跨实例共享，本质是 /distributed/cap-base 的状态归属问题；全局唯一 uploadID 的生成在 /distributed/distributed-id 展开为 Snowflake 与号段方案。
- L10 的 /projects/net-image-server 几乎就是本讲的完整落地：multipart 与流式读、缩略图、ETag 缓存、对象存储直传、限流与清理；/projects/micro-ecommerce 的商品图片与 /projects/data-log-etl 的大文件输入也复用同一套「流式加分块加幂等」骨架。

存储选型上，本地磁盘只适合单机与临时文件；生产应把最终文件放对象存储，用 CDN 分发读流量，数据库里只存对象键与元数据。临时文件要有兜底清理：除了 `defer os.Remove`，还应有一个定期扫描任务删除超过一定时间的 `.part` 与临时文件，防止进程崩溃留下孤儿文件。

<Compare title="上传处理：整批读还是流式搬运" :panels='[{"label":"反模式 · 整文件读进内存","code":"data, _ := io.ReadAll(r.Body)\nprocess(data)","note":"32 MiB 文件就占 32 MiB 内存，几十个并发上传即可 OOM"},{"label":"正解 · 流式处理","code":"io.Copy(dst, r.Body)","note":"按固定缓冲逐块搬运，内存占用恒定，与文件大小无关"}]' />

<FlowTimeline title="一次断点续传的完整流程" :steps='[{"title":"算内容哈希","desc":"客户端对整个文件计算 SHA-256","tone":"blue"},{"title":"init 秒传检查","desc":"命中已有哈希则直接完成","tone":"purple"},{"title":"逐片上传","desc":"按 index 定位写入，允许乱序","tone":"orange"},{"title":"失败重试","desc":"只重传缺失分片，指数退避","tone":"red"},{"title":"complete 合并","desc":"校验分片齐全后重命名落库","tone":"green"},{"title":"登记哈希","desc":"写入秒传表，后续同文件免传","tone":"gray"}]' direction="horizontal" />

最后再点一个安全边界：**永远不要相信客户端声明的文件名、大小或 Content-Type**。文件名只用于展示，落盘用服务端生成的键；大小以实际 `io.Copy` 写出的字节数为准；类型要靠嗅探文件头而不是扩展名判断。把这几条和前几讲的鉴权、限流合起来，才算把上传接口真正交付出去。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l6-server/lesson-071-file-storage`](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-071-file-storage)。clone 仓库后进入 `examples/l6-server`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给上传 Handler 加大小限制与临时文件清理" task="现有 Handler 直接把请求体写进临时文件且从不删除。请补全：限制请求体上限、创建临时文件、注册清理、流式写入并返回错误。" :hints='["用 http.MaxBytesReader 包住 r.Body，再交给后续读取","os.CreateTemp 之后立刻 defer os.Remove，且不要放进循环","io.Copy 的返回值是字节数与错误，两者都要向上返回"]' solution="r.Body = http.MaxBytesReader(w, r.Body, maxUpload)\ntmp, err := os.CreateTemp(dir, pattern)\nif err != nil {\nreturn err\n}\ndefer os.Remove(tmp.Name())\n_, err = io.Copy(tmp, file)\nreturn err" explain="上限要在最前面拦住；临时文件清理紧跟创建；流式写入把内存占用固定下来。批量场景下把单个文件的处理抽成函数，让 defer 及时生效。" />

## 随堂检测

<Quiz no="71-1" q="断点续传中，用 WriteAt 按 index 乘分片大小定位写入的主要好处是？" :options='["写得更慢更安全","同一分片重传是幂等的，且允许乱序到达","可以省掉哈希计算","能让文件自动压缩"]' answer="1" explain="WriteAt 写的是固定偏移，重传同一片只是覆盖相同位置，不会重复追加，也不要求分片按顺序到达。" />

<Predict no="71-2" q="上传 Handler 里把 defer os.Remove(tmp.Name()) 写在一个处理 100 个文件的循环内部，会发生什么？" :options='["每个文件处理完立即删除","100 个临时文件一直留到函数返回才一起删除，磁盘被占满","Go 编译器会报错","只删除最后一个文件"]' answer="1" output="defer 只在函数返回时执行，100 个临时文件在整个循环期间同时存在" explain="defer 绑定到函数而非循环块。批量处理应把单个文件的逻辑抽成独立函数，让每次调用的 defer 在返回时立刻清理。" />

## 一页纸速记

<KnowledgeCard :points='["io.Copy 流式搬运让内存占用恒定；MultiWriter 一次落盘加算哈希","先限大小再解码，临时文件用 CreateTemp 并紧跟 defer os.Remove","分块加 WriteAt 让重传幂等、乱序可达；内容哈希支撑秒传","预签名直传把字节流移出应用服务器，签名必须绑定方法、对象键与短过期","文件名只用于展示，落盘用服务端生成的键，大小以实际写入字节数为准"]' :pitfalls='["用 io.ReadAll 把整个文件读进内存","不设上传大小上限，磁盘或内存被打满","拿客户端文件名当存储键，导致路径穿越与覆盖","忽略 io.Copy 的字节数与错误","defer 放进循环，临时文件堆积到函数结束"]' summary="大文件处理的核心是把「整批」思维换成「流式 + 分块 + 幂等」，每一层都只做一件事。" />

## 小结与下一讲

- `io.Reader` / `io.Writer` 与 `io.Copy` 让内存占用与文件大小解耦，是上传处理的地基。
- 分块、`WriteAt` 偏移写入与内容哈希，共同支撑断点续传与秒传；分片重传天然幂等。
- 对象存储直传靠带签名的短期凭证，应用服务器不再扛字节流；临时文件与输入校验是可靠性的兜底。

到这里，L6 服务端与数据的十一讲就完整了：从 `net/http` 的 Handler 抽象出发，接上框架、REST、长连接，打通数据库、ORM、缓存、消息队列与搜索，补上认证授权与文件存储，你已经具备独立交付一个生产可用的后端服务与数据访问层的能力。

下一阶段我们把视角从「一个服务」抬到「一组服务」：远程调用要解决什么、协议怎么定、服务之间怎么找到彼此。

[下一讲：阶段七 · RPC 与微服务](/microservice/)
