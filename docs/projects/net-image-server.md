---
title: 第 106 讲 · 实战 04 · 图片服务与对象存储
description: 用流式 multipart、魔数校验、解码重编码、标准库缩略图与 ETag 内容寻址，处理上传缓存与安全边界
---

<StageHeader stage="L10 · 工程实战" level="★★★☆☆" time="60 分钟" goal="处理上传、缩略图、缓存与直传" prereq="第 105 讲 · 实战 03 · 短链服务" />

# 第 106 讲 · 实战 04 · 图片服务与对象存储

> 图片服务流的是几 MB 二进制：整请求读进内存会吃穿内存，只信客户端类型会把伪装文件缓存出去。难点在不装大对象、不信元数据、让重复下载零成本。

## 这一讲你会得到什么
<CheckList id="l106-goals" title="本讲目标" :items='["用 MultipartReader 与 MaxBytesReader 做到常数内存上传","用魔数与解码重编码挡住伪装文件与图片炸弹","用标准库 image 包生成等比缩略图","用内容哈希做 ETag 与 304 缓存"]' />

## 1. 项目目标与约束

| 维度 | 目标 | 约束与代价 |
| --- | --- | --- |
| 内存 | 与文件大小无关 | 必须流式读，不能 io.ReadAll |
| 体积 | 单文件上限 8 MiB | 超限在读阶段就拒绝 |
| 真实性 | 只接受 JPEG 与 PNG | 魔数加重编码双重校验 |
| 派生图 | 上传即生成缩略图 | 标准库最近邻画质一般 |
| 缓存 | 同一内容只传一次 | 内容寻址 |

<Callout type="tip" title="和短链服务最本质的区别" text="短链是小 key 查小 value，瓶颈在查得多快；图片服务是小 key 搬大 value，瓶颈在搬不搬得动。所有设计都围绕一个字：流。" />

本讲不做断点续传、分片与 CDN 回源，那些属于第 71 讲，只把单机内接收、校验、派生、存储、缓存这条链做扎实。

## 2. 架构蓝图

<ConceptDiagram type="http-chain" title="上传请求的中间件链" caption="越靠前的检查越便宜，也越应该早做；每层都可能在读满 body 之前就拒绝请求" />

顺序按检查成本排：先看方法、再限体积、再拆 multipart，最后才做解码编码。若把解码放在大小限制之前，几个超大文件就能打满 CPU 与内存。解码后重编码是关键清洗：只验魔数仍可在合法头后塞畸形数据，重新编码把输入归一化成安全产物。

## 3. 关键模块实现

### 3.1 流式上传：常数内存与魔数校验

```go
package main

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
)

const maxUpload = 8 << 20 // 8 MiB

// sniff 用魔数判断真实图片类型，不信客户端声明的 Content-Type。
func sniff(head []byte) string {
	switch {
	case len(head) >= 3 && bytes.Equal(head[:3], []byte{0xFF, 0xD8, 0xFF}):
		return "jpeg"
	case len(head) >= 8 && bytes.Equal(head[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return "png"
	default:
		return ""
	}
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只接受 POST", http.StatusMethodNotAllowed)
		return
	}
	// MaxBytesReader 让超大请求在读的阶段就被截断，而不是先吃进内存。
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "不是合法的 multipart 请求", http.StatusBadRequest)
		return
	}
	var part *multipart.Part
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "读取分片失败", http.StatusBadRequest)
			return
		}
		if p.FormName() == "image" {
			part = p
			break
		}
		p.Close()
	}
	if part == nil {
		http.Error(w, "缺少 image 字段", http.StatusBadRequest)
		return
	}
	defer part.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(part, head)
	head = head[:n]
	kind := sniff(head)
	if kind == "" {
		http.Error(w, "不是支持的图片格式", http.StatusUnsupportedMediaType)
		return
	}
	// 关键：头部读完，剩余字节继续以流的方式消费，不整体读进内存。
	rest, _ := io.Copy(io.Discard, part)
	fmt.Fprintf(w, "type=%s bytes=%d", kind, int64(n)+rest)
}

func main() {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("image", "a.png")
	fw.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3})
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	uploadHandler(rec, req)
	fmt.Println(rec.Code, rec.Body.String())

	// 改了后缀的 HTML 会被魔数校验挡下。
	var bad bytes.Buffer
	mw2 := multipart.NewWriter(&bad)
	fw2, _ := mw2.CreateFormFile("image", "evil.png")
	fw2.Write([]byte("<html>not an image</html>"))
	mw2.Close()
	req2 := httptest.NewRequest(http.MethodPost, "/upload", &bad)
	req2.Header.Set("Content-Type", mw2.FormDataContentType())
	rec2 := httptest.NewRecorder()
	uploadHandler(rec2, req2)
	fmt.Println("伪装文件:", rec2.Code)
}
```

MultipartReader 把分片暴露成 io.Reader，只读 512 字节判断、其余流式消费；FormFile 走 ParseMultipartForm 默认缓存最多 32 MiB，正好违背常数内存目标。defer part.Close 也不能省。

### 3.2 缩略图：标准库能做到哪一步

```go
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
)

// resizeNN 用最近邻把图缩到指定宽度，高度按原比例取整。
// 标准库没有高质量缩放器，手写一遍是为了说明原理与内存行为。
func resizeNN(src image.Image, width int) image.Image {
	b := src.Bounds()
	if width <= 0 || b.Dx() == 0 {
		return src
	}
	height := b.Dy() * width / b.Dx()
	if height <= 0 {
		height = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		sy := b.Min.Y + y*b.Dy()/height
		for x := 0; x < width; x++ {
			sx := b.Min.X + x*b.Dx()/width
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func main() {
	// 造一张 4x2 的纯色图，避免依赖外部文件。
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x * 60), G: 120, B: uint8(y * 100), A: 255})
		}
	}
	thumb := resizeNN(src, 2)
	fmt.Println("原图:", src.Bounds(), "缩略图:", thumb.Bounds())

	// 重新编码一次：既是产物，也是「解码后重编码」这道安全清洗。
	var jbuf bytes.Buffer
	if err := jpeg.Encode(&jbuf, thumb, &jpeg.Options{Quality: 80}); err != nil {
		fmt.Println("JPEG 编码失败:", err)
		return
	}
	decoded, err := jpeg.Decode(bytes.NewReader(jbuf.Bytes()))
	if err != nil {
		fmt.Println("JPEG 解码失败:", err)
		return
	}
	fmt.Println("JPEG 回读成功:", decoded.Bounds(), "字节数=", jbuf.Len())

	var pbuf bytes.Buffer
	if err := png.Encode(&pbuf, thumb); err != nil {
		fmt.Println("PNG 编码失败:", err)
		return
	}
	pdec, err := png.Decode(bytes.NewReader(pbuf.Bytes()))
	if err != nil {
		fmt.Println("PNG 解码失败:", err)
		return
	}
	fmt.Println("PNG 回读成功:", pdec.Bounds(), "字节数=", pbuf.Len())
}
```

标准库 image/jpeg 与 image/png 只提供编解码，没有插值缩放器，高质量缩放需 golang.org/x/image/draw。手写最近邻把代价摊开：按输出像素反算输入坐标，内存只与输出尺寸有关。生产换缩放器时接口不变，只替换循环体。

### 3.3 ETag 与内容寻址：让重复下载零成本

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// contentStore 用内容哈希做 key，天然去重，ETag 就是 key 本身。
type contentStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func (c *contentStore) Put(b []byte) string {
	sum := sha256.Sum256(b)
	key := hex.EncodeToString(sum[:])
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = append([]byte(nil), b...)
	return key
}

func (c *contentStore) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	b, ok := c.data[key]
	return b, ok
}

func serveImage(c *contentStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/img/")
		b, ok := c.Get(key)
		if !ok {
			http.NotFound(w, r)
			return
		}
		etag := "\"" + key + "\""
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		// If-None-Match 命中只回 304，不传正文，省掉整张图的带宽。
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(b)
	})
}

func main() {
	c := &contentStore{data: make(map[string][]byte)}
	key := c.Put([]byte("fake-image-bytes"))
	srv := httptest.NewServer(serveImage(c))
	defer srv.Close()

	r1, _ := http.Get(srv.URL + "/img/" + key)
	body, _ := io.ReadAll(r1.Body)
	r1.Body.Close()
	fmt.Println("首次:", r1.StatusCode, "bytes=", len(body), "ETag=", r1.Header.Get("ETag"))

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/img/"+key, nil)
	req.Header.Set("If-None-Match", r1.Header.Get("ETag"))
	r2, _ := http.DefaultClient.Do(req)
	b2, _ := io.ReadAll(r2.Body)
	r2.Body.Close()
	fmt.Println("带缓存:", r2.StatusCode, "bytes=", len(b2))
}
```

内容哈希带来三个好处：相同图片只存一份、URL 天然可永久缓存、ETag 不需要额外计算。etag 必须带引号，这是 HTTP 对 entity-tag 的格式要求；少了引号客户端可能不认，304 就永远命中不了。

## 4. 失败路径与边界条件

超大体由 MaxBytesReader 截断；伪装文件被魔数挡下；像素炸弹要先用 DecodeConfig 设上限；临时文件靠 defer 删除；ETag 必须带引号。换对象存储时把 part 直接作为上传流。

## 5. 测试与压测思路

用 httptest 构造合法 PNG 头、伪装 HTML、超大 body 三类输入断言状态码；并发上传 20 个 8 MiB 文件采样 ReadMemStats 确认内存平稳；再压 GET 统计 304 占比。

压测指标是峰值内存与出带宽，不是 QPS；若内存随并发线性增长，说明某处又用了 io.ReadAll。

## 6. 可继续扩展的方向

把 resizeNN 换成双线性缩放器；用预签名 URL 直传对象存储；缩略图进队列由第 110 讲调度；定期回收无引用哈希。

## 随堂检测

<Quiz no="106-1" q="为什么上传用 MultipartReader 而不是 r.FormFile？" :options='["FormFile 不支持 PNG","FormFile 会把分片缓存进内存，破坏常数内存目标","MultipartReader 更快"]' answer="1" explain="FormFile 走 ParseMultipartForm 默认最多缓存 32 MiB 到内存；MultipartReader 把分片暴露成 io.Reader 可边读边处理。" />

## 一页纸速记

<KnowledgeCard :points='["流式读让内存与文件大小解耦","魔数与解码重编码是两道安全闸","内容哈希同时解决去重、ETag 与缓存"]' :pitfalls='["只验魔数不验尺寸会中像素炸弹","ETag 忘加引号 304 不命中"]' summary="别把大对象装进内存，也别信请求里的元数据。" />

## 小结与下一讲

- 中间件链按检查成本排序，便宜的拒绝放在昂贵的解码之前。
- 流式读、双重校验、解码重编码三层叠加，才把上传变成可对外暴露的接口。
- 内容哈希把存储、去重与缓存合成一件事，304 让重复下载几乎零成本。

[下一讲：实战 05 · 日志分析 ETL 管道](/projects/data-log-etl)
