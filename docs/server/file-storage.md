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

## 4. 工程实践

存储选型上，本地磁盘只适合单机与临时文件；生产应把最终文件放对象存储，用 CDN 分发读流量，数据库里只存对象键与元数据。临时文件要有兜底清理：除了 `defer os.Remove`，还应有一个定期扫描任务删除超过一定时间的 `.part` 与临时文件，防止进程崩溃留下孤儿文件。

<Compare title="上传处理：整批读还是流式搬运" :panels='[{"label":"反模式 · 整文件读进内存","code":"data, _ := io.ReadAll(r.Body)\nprocess(data)","note":"32 MiB 文件就占 32 MiB 内存，几十个并发上传即可 OOM"},{"label":"正解 · 流式处理","code":"io.Copy(dst, r.Body)","note":"按固定缓冲逐块搬运，内存占用恒定，与文件大小无关"}]' />

<FlowTimeline title="一次断点续传的完整流程" :steps='[{"title":"算内容哈希","desc":"客户端对整个文件计算 SHA-256","tone":"blue"},{"title":"init 秒传检查","desc":"命中已有哈希则直接完成","tone":"purple"},{"title":"逐片上传","desc":"按 index 定位写入，允许乱序","tone":"orange"},{"title":"失败重试","desc":"只重传缺失分片，指数退避","tone":"red"},{"title":"complete 合并","desc":"校验分片齐全后重命名落库","tone":"green"},{"title":"登记哈希","desc":"写入秒传表，后续同文件免传","tone":"gray"}]' direction="horizontal" />

最后再点一个安全边界：**永远不要相信客户端声明的文件名、大小或 Content-Type**。文件名只用于展示，落盘用服务端生成的键；大小以实际 `io.Copy` 写出的字节数为准；类型要靠嗅探文件头而不是扩展名判断。把这几条和前几讲的鉴权、限流合起来，才算把上传接口真正交付出去。

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
