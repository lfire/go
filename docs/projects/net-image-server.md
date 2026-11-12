---
title: 第 106 讲 · 实战 04 · 图片服务与对象存储
description: 用流式 multipart、魔数与 DecodeConfig 防像素炸弹、EXIF 方向校正、标准库缩略图与格式转换、ETag 内容寻址、对象存储直传与签名 URL，处理上传缓存与安全边界
---

<StageHeader stage="L10 · 工程实战" level="★★★☆☆" time="60 分钟" goal="处理上传、缩略图、缓存与直传" prereq="第 105 讲 · 实战 03 · 短链服务" />

# 第 106 讲 · 实战 04 · 图片服务与对象存储

> 图片服务流的是几 MB 的二进制，整请求读进内存会吃穿内存，只信客户端声明的类型会把伪装文件缓存出去，忽略 EXIF 方向会让手机拍的照片全躺倒，漏算像素上限则会被一张声明几万乘几万的图打爆。难点集中在四件事：不把大对象装进内存、不信任何元数据、让重复下载几乎零成本、把临时文件与签名 URL 的边界守干净。本讲先把约束量化，再按「接收、校验、派生、存储、缓存」这条链逐层落地，全程标准库可编译、可压测、可评审。

<CheckList id="l106-goals" title="本讲交付清单" :items='["用 MultipartReader 与 MaxBytesReader 做到常数内存上传","用魔数、DecodeConfig 与解码重编码挡住伪装文件和像素炸弹","解析 EXIF 方向并做等比缩略图与格式转换","用内容哈希做 ETag 与 304 缓存","用 HMAC 预签名 URL 直传对象存储并保证临时文件必被清理"]' />

## 0. 项目定位与真实场景

图片服务活在用户头像、商品主图、社区帖子配图、内容审核与富文本编辑器背后。它和上一讲的短链服务看起来都是「key 查 value」，本质却相反：短链是小 key 查小 value，瓶颈在查得多快；图片是小 key 搬大 value，瓶颈在搬不搬得动、验不验得真、缓不缓存得下。一个短链跳转响应只有几百字节，一张原图却常有 3 到 8 MB，缩略图、Web 适配图、审核图还会派生出多份副本。

使用者同样分三类：上传方（App、运营后台、编辑器）关心别超时、别失败，浏览方（页面、CDN、客户端）关心秒开且不重复下载，运维方关心磁盘不被临时文件撑满、对象存储账单不失控、外链不被当成盗图或恶意文件分发。三者对「同一张图」的诉求也不同：上传方要能覆盖、浏览方要能永久缓存、运维方要能按内容去重。

先给可讨论的量级基线：日新增图片 50 万张，平均原图 2 MB、缩略图 40 KB，存量对象约 20 TB；上传 QPS 峰值 300，下载 QPS 峰值 2 万，读写比约七十比一；峰值出带宽按 2 万 QPS 乘平均 60 KB 估算约 9.6 Gbps，其中九成以上应由 CDN 与 304 消化，源站只承担回源流量。这些数字决定了：上传必须流式、下载必须条件缓存、原图必须走对象存储直传而不是穿过应用服务器。

现有方案最典型的痛点有四类。第一类是 `r.ParseMultipartForm` 或 `r.FormFile` 默认把最多 32 MiB 分片缓进内存，几十个并发上传就把进程内存打满。第二类是只检查扩展名或 `Content-Type`，一个改名为 `png` 的 HTML 被原样存进对象存储并被 CDN 分发出去，成为存储型攻击的载体。第三类是直接 `image.Decode` 一张声明 30000 乘 30000 像素的 PNG，解码器按宽高分配像素缓冲，单请求就能申请数 GB 内存，这就是像素炸弹。第四类是忽略 EXIF 的 Orientation 标签，手机竖拍的照片在页面上横躺，运营只能手动重新上传。本讲的核心矛盾可以概括成：在内存有硬上限、输入完全不可信、带宽成本敏感、对象最终放远端的四重约束下，把上传链做成一条流式、可校验、可清理、可缓存的管道。

<Callout type="idea" title="和短链服务最本质的区别" text="短链瓶颈在查得多快，图片瓶颈在搬不搬得动；短链失败最多是跳错页，图片失败可能是内存被打爆或恶意文件被分发出去，因此校验必须前移到读满 body 之前。" />

## 1. 需求与约束

功能需求按编号管理，每条带可验收标准，测试与压测都从这张表派生。特别注意「真实性」与「内存」两条不是性能偏好而是硬性安全约束。

| 编号 | 功能需求 | 验收标准 |
| --- | --- | --- |
| F1 | 流式上传 | 单请求内存占用与文件大小无关，并发 20 个 8 MiB 文件时堆内存平稳 |
| F2 | 体积上限 | 单文件超过 8 MiB 在读阶段即拒绝，返回 413 且不落临时文件 |
| F3 | 格式真实性 | 只接受 JPEG 与 PNG，魔数校验加解码重编码双重通过 |
| F4 | 像素炸弹防护 | 宽高乘积超过四千万像素在 DecodeConfig 阶段拒绝，不分配像素缓冲 |
| F5 | EXIF 方向 | 按 Orientation 1 到 8 校正后再生成缩略图，手机竖拍图不躺倒 |
| F6 | 派生图 | 上传即生成等比缩略图并做格式转换，链路失败不影响原图落库 |
| F7 | 条件缓存 | 同内容 URL 返回带引号 ETag 与长 Cache-Control，命中 If-None-Match 回 304 无正文 |
| F8 | 直传签名 | 预签名 URL 绑定方法、对象键与过期时间，过期或篡改即校验失败 |
| F9 | 资源清理 | 请求结束临时文件必被删除，异常分支也不例外 |

非功能指标写进口径，所有数字与第 0 节量级自洽。

| 指标 | 目标值 | 说明与代价 |
| --- | --- | --- |
| 上传 QPS 峰值 | 300 | 四台实例各约 75，令牌桶与并发信号量双限 |
| 下载 QPS 峰值 | 20000 | 九成以上由 CDN 与 304 消化，源站回源约 2000 |
| 单请求内存峰值 | 小于 1 MB | 流式读写，禁止整图 io.ReadAll |
| 上传 P99 | 小于 800 毫秒 | 含落临时文件、校验与一次对象存储 PUT |
| 首次下载 P99 | 小于 50 毫秒 | 命中元数据即回，正文由对象存储或 CDN 承担 |
| 304 占比 | 大于 85% | 内容寻址让同一 URL 可永久缓存 |
| 缩略图失败率 | 小于 0.1% | 派生失败降级为仅原图可用，不阻塞上传 |
| 可用性 SLA | 99.95% | 对象存储不可用则上传失败、下载走 CDN 缓存 |
| RTO / RPO | 30 秒 / 0 | 应用无状态，对象与元数据持久化，RPO 为 0 |

明确「本讲不做什么」：不做断点续传与分片上传（属于第 071 讲）、不做 CDN 回源策略与鉴权刷新、不做 AVIF/WebP 编码（标准库不支持，需第三方编解码器）、不做内容审核与鉴黄、不做防盗链的 Referer 校验、不接真实对象存储 SDK（用等价接口与本地临时目录替代，接口保持可切换）。假设单文件上限 8 MiB、像素上限四千万、只处理基线 JPEG 与 PNG、系统时钟不发生大幅回拨（回拨会让签名 URL 提前失效，见第 14 节）。

<Callout type="warn" title="安全检查必须在读满 body 之前" text="若把解码放在体积限制之后、或先 io.ReadAll 再校验，几个超大或畸形文件就能打满 CPU 与内存；正确的顺序是方法、限额、分片、魔数、DecodeConfig、最后才解码。" />

## 2. 架构蓝图

约束确定后，架构按「检查成本从低到高」纵向切分：越靠前的层越便宜，也越应该早拒绝；越靠后的层越贵，越要确认前面的门都过了才做。

<ArchBlueprint title="图片服务四层架构" caption="上传按检查成本逐层收窄，下载按缓存语义逐层前移；对象存储直传把大流量挡在应用之外" :layers='[{"name":"接入层","note":"公网统一入口，承担最便宜的拒绝","protocol":"HTTPS 与 multipart 流","items":[{"label":"上传网关","sub":"限流与体积校验","detail":"职责：校验方法、限制请求体、用并发信号量挡住洪峰；技术选型：net/http 中间件链加 MaxBytesReader 与有界 channel，不引第三方框架；失败处理：超限返回 413 或 503 并携带 Retry-After，规则加载失败默认拒绝上传。","tone":"red"},{"label":"下载网关","sub":"ETag 与条件请求","detail":"职责：解析对象键、比对 If-None-Match、决定 304 还是 200；技术选型：标准库条件缓存加内容寻址元数据；失败处理：元数据不可用回源对象存储，仍失败返回 503 不返回脏正文。","tone":"blue"}]},{"name":"处理层","note":"无状态 CPU 密集，可独立扩缩","protocol":"io.Reader 流式接口","items":[{"label":"图片校验","sub":"魔数与像素炸弹","detail":"职责：魔数判真实格式、DecodeConfig 读头部判像素上限、解析 EXIF 方向；技术选型：标准库 image.DecodeConfig 加手写 APP1 与 TIFF 解析，不引第三方 EXIF 库；失败处理：任一不通过即拒绝并清理临时文件，绝不继续解码。","tone":"green"},{"label":"派生与转码","sub":"缩略图与格式转换","detail":"职责：按 EXIF 方向校正、等比缩放、透明底转 JPEG 前合成白底；技术选型：标准库 image 与 image/draw 手写最近邻，接口预留可换双线性；失败处理：派生失败仅记告警降级为原图可用，不回滚上传。","tone":"green"}]},{"name":"缓存层","note":"让重复下载几乎零成本","protocol":"HTTP 缓存语义","items":[{"label":"浏览器与 CDN 缓存","sub":"ETag 与 immutable","detail":"职责：用带引号 ETag 与长 Cache-Control 让同一内容永久缓存；技术选型：内容哈希即键即 ETag，免额外计算；失败处理：条件头缺失或过期按 200 正常回源，缓存故障不影响正确性。","tone":"orange"},{"label":"热点本地缓存","sub":"元数据与缩略图","detail":"职责：缓存热点对象的元数据与缩略图，减少回源；技术选型：读写锁保护的映射加短 TTL 与容量上限；失败处理：整体不可用只降延迟，回源对象存储仍可服务。","tone":"orange"}]},{"name":"存储层","note":"唯一可信副本与临时候车区","items":[{"label":"对象存储","sub":"原图与派生图","detail":"职责：保存原图与各规格派生图的唯一权威副本，按内容哈希去重；技术选型：预签名 URL 直传，应用不经手大对象；失败处理：PUT 失败重试两次并退避，仍失败返回 503 且清理临时文件。","tone":"purple"},{"label":"元数据与签名服务","sub":"记录与临时文件","detail":"职责：保存对象键、内容类型、尺寸与 ETag，签发直传 URL，回收临时文件；技术选型：HMAC 预签名加临时目录与 defer 清理；失败处理：签名密钥缺失拒绝启动，临时目录清理失败计入告警并定时兜底扫描。","tone":"purple"}]}]' />

逐层看为什么这样切。接入层把最便宜的拒绝放最前：方法、体积、并发额度都在读满请求体之前完成，一个畸形上传在这里就被挡掉。处理层是唯一的 CPU 密集段，做成无状态后可按 CPU 独立扩缩；其中「校验」与「派生」分开，是因为校验失败必须立刻清理并拒绝，派生失败却可以降级——两者失败语义不同，放一起会互相拖累。缓存层专门服务下载，内容寻址让 ETag 就是键本身，省掉一次计算、也让 URL 天然可永久缓存。存储层只保留唯一可信副本与一个「临时候车区」：临时文件是上传过程中不可避免的中间态，必须有一层明确负责它的生与死。

<ConceptDiagram type="http-chain" title="上传请求的中间件链" caption="越靠前的检查越便宜，每层都可能在读满 body 之前就拒绝请求" />

<ConceptDiagram type="pipeline" title="上传处理流水线" caption="流式落盘、双重校验、解码重编码、派生、落对象存储、投递事件，各阶段只通过 io.Reader 与元数据衔接" />

依赖方向严格自上而下：接入层依赖处理层与缓存层的接口，处理层依赖存储层抽象，存储层绝不反向调用上层。原图直传是这条规则的直接体现——大对象从客户端直接进对象存储，应用只见元数据与校验结果，既省带宽也避免应用服务器成为瓶颈。一旦允许对象存储回调应用做同步派生，任一抖动都会沿着调用环放大成上传全链路阻塞，所以派生走异步队列而非同步回调。

<Callout type="note" title="为什么写后不预热缓存" text="预热会在缓存里留下尚未提交成功的幽灵对象，一旦 PUT 失败就出现「缓存有、存储没有」；正确做法是只写对象存储、读时按需回填，缓存整体挂掉只影响延迟。" />

## 3. 模块职责与依赖方向

把第 2 节的层落到具体模块，明确输入、输出、依赖、状态与失败行为，这张表也是后面代码与测试的分工依据。

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| 上传网关 | 原始 HTTP 请求 | 放行或 413/429/503 | 体积限额、并发信号量 | 每连接令牌 | 规则加载失败默认拒绝 |
| 下载网关 | 对象键与条件头 | 200/304/404 | 元数据、对象存储 | 无状态 | 元数据不可用回源对象存储 |
| 图片校验 | 临时文件头与流 | 格式、尺寸、方向 | DecodeConfig、EXIF 解析 | 无状态 | 不通过即拒绝并清理临时文件 |
| 派生与转码 | 已校正的图像 | 缩略图与派生格式 | image、image/draw | 无状态 | 失败降级为仅原图可用 |
| 内容寻址存储 | 字节流 | 对象键与 ETag | sha256、对象存储 | 唯一可信副本 | PUT 失败重试两次后 503 |
| 元数据与签名 | 对象键与方法 | 预签名 URL、记录 | HMAC、密钥配置 | 元数据记录 | 密钥缺失拒绝启动 |
| 缓存层 | 对象键 | 命中值或 304 | 元数据、热点映射 | 可重建派生 | 整体不可用只降延迟 |
| 临时文件清理 | 临时目录路径 | 删除回执 | 文件系统 | 中间态文件 | 清理失败告警并定时兜底 |

包目录按「领域内聚、依赖单向」组织，cmd 只做装配，internal 里每一层只暴露接口给上一层：

```text
image-server/
├── cmd/
│   └── server/
│       └── main.go            # 装配配置、构造依赖、启动与优雅退出
├── internal/
│   ├── api/
│   │   ├── upload.go         # 上传 handler 与状态码决策
│   │   ├── download.go       # ETag、304 与条件请求
│   │   └── middleware.go     # trace_id、限流、体积与并发控制
│   ├── image/
│   │   ├── sniff.go          # 魔数校验
│   │   ├── guard.go          # DecodeConfig 像素上限
│   │   ├── exif.go           # APP1 与 TIFF 方向解析
│   │   ├── transform.go      # 方向校正、等比缩放、格式转换
│   │   └── resize.go         # 最近邻缩放（可替换实现）
│   ├── storage/
│   │   ├── object.go         # ObjectStore 接口（依赖倒置点）
│   │   ├── local.go          # 本讲本地临时目录实现
│   │   └── oss.go            # 预留：对象存储 SDK 实现
│   ├── cache/
│   │   ├── etag.go           # 内容寻址 ETag 与 304
│   │   └── hot.go            # 热点元数据缓存
│   ├── sign/
│   │   └── presign.go        # HMAC 预签名 URL 与校验
│   └── meta/
│       └── store.go          # 元数据记录与幂等
└── go.mod
```

依赖倒置的关键在 storage 包：上传与下载只依赖 `ObjectStore` 接口，本地临时目录实现与真实 OSS 实现都实现同一组方法，替换时上层一行不改。接口要刻意做小，`Put/Get/Delete` 三个方法足够；不要把限流、埋点、缩略图硬塞进同一个大接口，否则任何一处改动都会牵动全链路。

<Callout type="warn" title="边界划分最常见的错误" text="把流式读取、临时文件、校验、派生、落对象存储全写进一个 upload handler 函数，会让任何一步的异常分支都漏掉资源清理；正确做法是每步一个明确所有权的函数，临时文件的创建者负责用 defer 删除。" />

## 4. 数据与协议契约

元数据用一张表承载对象的权威描述，对象键由内容哈希生成、做主键，天然去重；尺寸、格式、方向都落库，供下载时直接回 ETag 与 Content-Type 而不必回源读图。

```sql
CREATE TABLE image_objects (
    obj_key      CHAR(64)     NOT NULL COMMENT 'sha256 内容哈希，主键即 ETag 源',
    content_type VARCHAR(32)  NOT NULL COMMENT '规范化后的 MIME，来自魔数而非请求头',
    width        INT          NOT NULL COMMENT '校正方向后的宽',
    height       INT          NOT NULL COMMENT '校正方向后的高',
    orientation  TINYINT      NOT NULL DEFAULT 1 COMMENT 'EXIF 方向 1 到 8',
    bytes        BIGINT       NOT NULL COMMENT '原图字节数',
    thumb_key    CHAR(64)     NOT NULL DEFAULT '' COMMENT '派生缩略图对象键',
    created_at   BIGINT       NOT NULL COMMENT '创建毫秒时间戳',
    PRIMARY KEY (obj_key),
    KEY idx_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

字段语义与零值必须写清：`obj_key` 存十六进制小写哈希，长度固定 64，不带引号、不带前缀；`content_type` 只可能是 `image/jpeg` 或 `image/png`，来自魔数校验而非客户端 `Content-Type`；`orientation` 零值不可用，默认 1 表示无需校正；`thumb_key` 为空字符串表示派生失败或未生成，此时下载缩略图应回退原图而不是 404；`width/height` 记录的是**校正方向之后**的尺寸，避免前端再次旋转。兼容规则是只增列不删列、枚举值只追加不复用，新增字段必须给默认值，老代码读到新行不报错。

对外 API 用 multipart 与 JSON over HTTPS，下载只走路径参数：

| 方法 | 路径 | 请求 | 成功响应 | 主要错误 |
| --- | --- | --- | --- | --- |
| POST | /api/v1/upload | multipart 字段 image | 201 与 obj_key、ETag | 400 格式错误、413 超限、415 不支持、429 超频 |
| GET | /api/v1/presign | object_key、method | 200 与预签名 URL、过期时间 | 400 缺参、403 非法方法 |
| GET | /img/{obj_key} | 路径参数，可带 If-None-Match | 200 正文或 304 无正文 | 404 不存在、503 存储不可用 |
| HEAD | /img/{obj_key} | 同 GET | 200 只回头 | 404、503 |
| GET | /healthz | 无 | 200 | 503 依赖不健康 |

```json
{
  "obj_key": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
  "url": "/img/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
  "content_type": "image/png",
  "width": 1080,
  "height": 1920,
  "etag": "\"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08\""
}
```

版本号与灰度策略：URL 不带版本号，因为内容寻址下「内容变了键就变」，天然没有覆盖问题；`PATCH` 语义不存在，改图就是新对象。灰度时响应字段只增不删，服务端先能读新字段、再开始写新字段、最后才删旧字段，三步分开发布；ETag 必须带双引号，这是 HTTP 对 entity-tag 的格式要求，少了引号客户端不认、304 永远命中不了。

内部接口签名把依赖倒置固化成代码，下面是可独立编译的契约定义与最小实现：

```go
package main

import (
	"bytes"
	"fmt"
	"io"
	"time"
)

// ObjectMeta 是对象的权威描述，ETag 由键派生，不额外计算。
type ObjectMeta struct {
	Key         string
	ContentType string
	Size        int64
	ETag        string
	CreatedAt   time.Time
}

// ObjectStore 定义唯一可信副本，上传与下载只依赖这三个方法。
type ObjectStore interface {
	Put(key string, r io.Reader, ct string) (ObjectMeta, error)
	Get(key string) (io.ReadCloser, ObjectMeta, error)
	Delete(key string) error
}

// ImageProcessor 抽象派生能力，缩略图失败可降级不影响主链路。
type ImageProcessor interface {
	Thumbnail(r io.Reader, width int) (io.Reader, string, error)
}

type memStore struct {
	data map[string][]byte
	meta map[string]ObjectMeta
}

func newMemStore() *memStore {
	return &memStore{
		data: make(map[string][]byte),
		meta: make(map[string]ObjectMeta),
	}
}

func (m *memStore) Put(key string, r io.Reader, ct string) (ObjectMeta, error) {
	b, err := io.ReadAll(io.LimitReader(r, 8<<20))
	if err != nil {
		return ObjectMeta{}, err
	}
	m.data[key] = b
	meta := ObjectMeta{
		Key:         key,
		ContentType: ct,
		Size:        int64(len(b)),
		ETag:        fmt.Sprintf("%q", key),
		CreatedAt:   time.Now(),
	}
	m.meta[key] = meta
	return meta, nil
}

func (m *memStore) Get(key string) (io.ReadCloser, ObjectMeta, error) {
	b, ok := m.data[key]
	if !ok {
		return nil, ObjectMeta{}, fmt.Errorf("对象不存在: %s", key)
	}
	return io.NopCloser(bytes.NewReader(b)), m.meta[key], nil
}

func (m *memStore) Delete(key string) error {
	delete(m.data, key)
	delete(m.meta, key)
	return nil
}

func main() {
	var s ObjectStore = newMemStore()
	meta, err := s.Put("abc", bytes.NewReader([]byte("image-bytes")), "image/png")
	if err != nil {
		fmt.Println("写入失败:", err)
		return
	}
	rc, got, err := s.Get("abc")
	if err != nil {
		fmt.Println("读取失败:", err)
		return
	}
	defer rc.Close()
	fmt.Println("契约往返:", meta.Key, got.Size, got.ContentType, got.ETag)
}
```

<Compare title="图片对象命名的两种契约设计" :panels='[{"label":"内容哈希寻址","code":"sha256(bytes) 作为 key","note":"天然去重、URL 可永久缓存、ETag 免算；代价是相同内容无法区分不同上传者，且改图必产生新键"},{"label":"自增 ID 加版本","code":"img_1024_v2 作为 key","note":"便于按业务归组与原位覆盖；代价是需额外去重、ETag 单独计算、缓存失效要显式管理"}]' note="本讲选内容寻址：把存储、去重、缓存三件事合并成一个键，下载链路上不必再维护版本与失效逻辑" />

## 5. 核心链路时序

先看上传链路。写路径的重心不是快，而是「任何一步失败都不留脏对象、不留临时文件」。

<SequenceDiagram title="图片上传写链路" caption="流式落临时文件后先校验再落对象存储，派生任务异步投递，不阻塞上传响应" :actors='["Client","Gateway","Uploader","Decoder","ObjectStore","Worker"]' :steps='[{"from":"Client","to":"Gateway","label":"POST /upload","note":"multipart 流","kind":"call"},{"from":"Gateway","to":"Uploader","label":"限流并拆分片","note":"体积已限额","kind":"call"},{"from":"Uploader","to":"Uploader","label":"流式落临时文件","note":"边读边算哈希","kind":"self"},{"from":"Uploader","to":"Decoder","label":"魔数与像素校验","note":"含 EXIF 方向","kind":"call"},{"from":"Decoder","to":"Uploader","label":"返回尺寸方向","note":"拒绝即清理","kind":"return"},{"from":"Uploader","to":"ObjectStore","label":"流式 PUT 原图","note":"键即内容哈希","kind":"call"},{"from":"ObjectStore","to":"Uploader","label":"返回 ETag","note":"写入成功","kind":"return"},{"from":"Uploader","to":"Worker","label":"异步派生缩略图","note":"失败只告警","kind":"async"},{"from":"Uploader","to":"Gateway","label":"返回 201","note":"清理临时文件","kind":"return"},{"from":"Gateway","to":"Client","label":"返回 obj_key","note":"含访问 URL","kind":"return"}]' />

逐步骤看分叉点。第一跳网关先过体积限额与并发信号量，超限在读满 body 之前就返回 413 或 503；第二跳把 multipart 分片暴露成 `io.Reader`；第三跳是自处理，边流式写临时文件边算内容哈希，内存只与缓冲区有关；第四跳魔数、`DecodeConfig` 像素上限、EXIF 方向三道校验，任一不过立即 `defer` 清理临时文件并拒绝；第五跳返回尺寸与方向；第六跳把临时文件作为上传流直接 PUT 到对象存储，应用不经手整图；第七跳拿到 ETag；第八跳把缩略图派生标成异步，Worker 抖动不影响上传接口；最后两跳返回结果，并在返回前完成临时文件清理。

再看下载链路，它是高频路径，重点在条件缓存与「零正文」响应。

<SequenceDiagram title="图片下载读链路" caption="内容寻址让 ETag 就是键，条件命中直接 304 无正文；异步统计不阻塞响应" :actors='["Client","CDN","Gateway","Meta","ObjectStore"]' :steps='[{"from":"Client","to":"CDN","label":"GET /img/key","note":"带 If-None-Match","kind":"call"},{"from":"CDN","to":"Gateway","label":"条件回源","note":"边缘未命中","kind":"call"},{"from":"Gateway","to":"Meta","label":"查元数据与 ETag","note":"不读原图","kind":"call"},{"from":"Meta","to":"Gateway","label":"返回 ETag","note":"键即哈希","kind":"return"},{"from":"Gateway","to":"Gateway","label":"比对条件头","note":"相等即 304","kind":"self"},{"from":"Gateway","to":"CDN","label":"返回 304 无正文","note":"省整图带宽","kind":"return"},{"from":"CDN","to":"Client","label":"返回 304","note":"复用本地缓存","kind":"return"},{"from":"Gateway","to":"ObjectStore","label":"异步预热热点","note":"不阻塞响应","kind":"async"}]' />

异常链路与正常链路有三处分叉。其一是校验失败：魔数不符、`DecodeConfig` 判像素炸弹、或 EXIF 段损坏，都在第五步之前终止，返回 415 或 400，且临时文件由 `defer` 删除，绝不留下半截对象。其二是对象存储 PUT 超时：`context` 带 3 秒截止，重试预算两次、指数退避 50 与 100 毫秒，仍失败返回 503 并清理临时文件，因为「让用户重试」比「留下一个没有元数据的孤儿对象」安全。其三是派生失败：Worker 生成缩略图失败仅记告警、把 `thumb_key` 留空，下载缩略图时回退原图，上传接口照样返回 201——派生是增强而非前提。三处分叉共同保证：校验失败不落地、存储抖动不残留、派生失败不拖垮主链路。

<StepBox title="上传延迟异常的排查顺序" :steps='[{"title":"先看内存","body":"堆内存随并发线性增长说明又用了 io.ReadAll","tip":"对比并发前后的 ReadMemStats 与 pprof heap"},{"title":"再看拒绝率","body":"415 与 413 突增先怀疑输入而非服务","tip":"区分魔数拒绝、像素炸弹拒绝与体积超限"},{"title":"最后看清理","body":"临时目录文件数持续增长是清理漏了分支","tip":"检查每个 return 前是否有 defer 兜底"}]' startLabel="开始排查" />

## 6. 核心实现

下面六段代码覆盖数据模型与接口、流式 I/O、核心算法、并发处理、错误与清理、以及一个可运行的主流程，每段都带完整包声明与导入、可独立编译，且刻意保留超时、nil、部分失败与资源释放的判断。

### 6.1 流式 multipart 上传：常数内存与临时文件必清理

设计意图：把分片暴露成 `io.Reader`，只读头部判魔数，其余字节边流式写临时文件边算哈希，最后用 `defer` 无条件清理。关键取舍是用 `MultipartReader` 而非 `r.FormFile`：后者走 `ParseMultipartForm`，默认最多把 32 MiB 缓进内存，正好违背常数内存目标。

```go
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
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

// streamUpload 把分片流式落到临时文件，边写边算哈希，内存与文件大小无关。
func streamUpload(part *multipart.Part, dir string) (string, string, string, error) {
	head := make([]byte, 512)
	n, readErr := io.ReadFull(part, head)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return "", "", "", readErr
	}
	head = head[:n]
	kind := sniff(head)
	if kind == "" {
		return "", "", "", errors.New("不是支持的图片格式")
	}
	f, err := os.CreateTemp(dir, "up-*")
	if err != nil {
		return "", "", "", err
	}
	path := f.Name()
	h := sha256.New()
	if _, err := f.Write(head); err != nil {
		f.Close()
		return path, "", "", err
	}
	_, _ = h.Write(head)
	if _, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(part, maxUpload)); err != nil {
		f.Close()
		return path, "", "", err
	}
	if err := f.Close(); err != nil {
		return path, "", "", err
	}
	return path, kind, hex.EncodeToString(h.Sum(nil)), nil
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只接受 POST", http.StatusMethodNotAllowed)
		return
	}
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

	dir, err := os.MkdirTemp("", "img-")
	if err != nil {
		http.Error(w, "建临时目录失败", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(dir) // 请求结束一定清理，杜绝临时文件泄漏
	path, kind, sum, err := streamUpload(part, dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		return
	}
	defer os.Remove(path)
	info, err := os.Stat(path)
	if err != nil {
		http.Error(w, "读取临时文件失败", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "type=%s bytes=%d sha256=%s", kind, info.Size(), sum[:12])
}

func main() {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("image", "a.png")
	_, _ = fw.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4})
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	uploadHandler(rec, req)
	fmt.Println("合法上传:", rec.Code, rec.Body.String())

	var bad bytes.Buffer
	mw2 := multipart.NewWriter(&bad)
	fw2, _ := mw2.CreateFormFile("image", "evil.png")
	_, _ = fw2.Write([]byte("<html>not an image</html>"))
	_ = mw2.Close()
	req2 := httptest.NewRequest(http.MethodPost, "/upload", &bad)
	req2.Header.Set("Content-Type", mw2.FormDataContentType())
	rec2 := httptest.NewRecorder()
	uploadHandler(rec2, req2)
	fmt.Println("伪装文件:", rec2.Code, "临时目录已由 defer 清理")
}
```

`streamUpload` 的关键点：头部读完，剩余字节继续以流的方式消费；`io.MultiWriter(f, h)` 让落盘与哈希一次遍历完成；任何写错误都在关闭文件后返回，路径交给调用方的 `defer os.Remove` 兜底。`uploadHandler` 里 `defer part.Close()`、`defer os.RemoveAll(dir)`、`defer os.Remove(path)` 三层清理叠加，保证哪怕中途 `return`，临时文件也不会残留。生产里把 `path` 对应的文件直接作为对象存储 PUT 的 body，读完即删，应用服务器始终不持有整图。

<OutputBox title="流式上传演示输出" output="合法上传: 200 type=png bytes=12 sha256=...\n伪装文件: 415 临时目录已由 defer 清理" />

### 6.2 DecodeConfig 防像素炸弹与 EXIF 方向解析

设计意图：`image.DecodeConfig` 只读头部就能拿到宽高，**不分配像素缓冲**，因此能在真正解码前判像素上限；EXIF 方向藏在 JPEG 的 APP1 段里，标准库没有解析器，手写一段只依赖 `encoding/binary` 的最小实现，避免引入无法独立编译的第三方库。关键取舍是接受「只解析 IFD0 的 Orientation 标签」这一种最小需求，代价是不支持压缩缩略图、GPS 等其它 EXIF 字段。

```go
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	_ "image/jpeg"
	"image/png"
)

const maxPixels = 40_000_000 // 四千万像素上限

// checkPixels 只用 DecodeConfig 读头部，不分配像素缓冲，挡住像素炸弹。
func checkPixels(head []byte) (int, int, string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil {
		return 0, 0, "", fmt.Errorf("解码配置失败: %w", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return 0, 0, "", fmt.Errorf("像素炸弹: %dx%d 超过上限", cfg.Width, cfg.Height)
	}
	return cfg.Width, cfg.Height, format, nil
}

func tiffOrientation(t []byte) (int, error) {
	if len(t) < 8 {
		return 1, errors.New("TIFF 头过短")
	}
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1, errors.New("TIFF 字节序非法")
	}
	if order.Uint16(t[2:4]) != 42 {
		return 1, errors.New("TIFF 魔数错误")
	}
	ifd := int(order.Uint32(t[4:8]))
	if ifd+2 > len(t) {
		return 1, errors.New("IFD 越界")
	}
	n := int(order.Uint16(t[ifd : ifd+2]))
	for i := 0; i < n; i++ {
		e := ifd + 2 + i*12
		if e+12 > len(t) {
			break
		}
		if order.Uint16(t[e:e+2]) == 0x0112 && order.Uint16(t[e+2:e+4]) == 3 {
			v := int(order.Uint16(t[e+8 : e+10]))
			if v >= 1 && v <= 8 {
				return v, nil
			}
		}
	}
	return 1, nil
}

// jpegOrientation 扫描 APP1 段，读取 Exif 里的 Orientation 标签。
func jpegOrientation(data []byte) (int, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1, errors.New("不是 JPEG")
	}
	for pos := 2; pos+4 <= len(data); {
		if data[pos] != 0xFF {
			pos++
			continue
		}
		marker := data[pos+1]
		if marker == 0xD9 || marker == 0xDA {
			break
		}
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) {
			pos += 2
			continue
		}
		segLen := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		if segLen < 2 || pos+2+segLen > len(data) {
			return 1, errors.New("段长度非法")
		}
		payload := data[pos+4 : pos+2+segLen]
		if marker == 0xE1 && len(payload) >= 6 && string(payload[:6]) == "Exif\x00\x00" {
			return tiffOrientation(payload[6:])
		}
		pos += 2 + segLen
	}
	return 1, nil
}

// buildExifJPEG 构造一段最小 JPEG：SOI 加 APP1 加 EOI，仅用于验证解析器。
func buildExifJPEG(o int) []byte {
	tiff := []byte{
		'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00,
		0x01, 0x00,
		0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, byte(o), 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segLen := len(payload) + 2
	seg := append([]byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen & 0xFF)}, payload...)
	out := append([]byte{0xFF, 0xD8}, seg...)
	return append(out, 0xFF, 0xD9)
}

// pngBomb 构造 CRC 合法的 IHDR，声明 30000 乘 30000 像素，不携带像素数据。
func pngBomb() []byte {
	ihdr := []byte{0, 0, 0x75, 0x30, 0, 0, 0x75, 0x30, 8, 2, 0, 0, 0}
	body := append([]byte("IHDR"), ihdr...)
	crc := crc32.ChecksumIEEE(body)
	chunk := []byte{0, 0, 0, 13}
	chunk = append(chunk, body...)
	chunk = append(chunk, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
	sig := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	return append(sig, chunk...)
}

func main() {
	var src bytes.Buffer
	if err := png.Encode(&src, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		fmt.Println("造图失败:", err)
		return
	}
	w, h, format, err := checkPixels(src.Bytes())
	fmt.Println("合法小图:", w, h, format, err)
	_, _, _, err = checkPixels(pngBomb())
	fmt.Println("像素炸弹:", err)
	o, err := jpegOrientation(buildExifJPEG(6))
	fmt.Println("EXIF 方向:", o, err)
}
```

<OutputBox title="像素与 EXIF 校验演示输出" output="合法小图: 2 2 png err=&lt;nil&gt;\n像素炸弹: 像素炸弹: 30000x30000 超过上限\nEXIF 方向: 6 err=&lt;nil&gt;" />

`pngBomb` 特意用 `crc32.ChecksumIEEE` 算出合法校验和，让 `DecodeConfig` 能顺利读出 IHDR 里声明的 30000 乘 30000，从而证明拦截发生在「读到尺寸之后、分配像素之前」；若 CRC 错误只会在解析阶段失败，反而掩盖了尺寸判断。EXIF 解析按「JPEG SOI → APP1 段 → Exif 前缀 → TIFF 字节序与 IFD0 → Orientation 标签」逐层读取，每个切片前都做长度检查，避免畸形文件触发越界 panic。方向值 6 表示顺时针旋转 90 度，交给第 6.3 节校正。

<Callout type="tip" title="DecodeConfig 与 Decode 是两道不同的门" text="DecodeConfig 只读头、不分配像素，用来做准入；Decode 才真正分配宽乘高乘四字节的缓冲，必须在准入通过之后调用，这个顺序反了就是像素炸弹。" />

### 6.3 EXIF 校正、等比缩略图与格式转换

设计意图：把「方向校正」与「缩放」拆成两个纯函数，输入 `image.Image`、输出 `image.Image`，互不依赖、各自可测。关键取舍是用标准库手写最近邻缩放：画质一般但零依赖、内存只与输出尺寸有关，接口不变即可替换为双线性或 `golang.org/x/image/draw`。透明 PNG 转 JPEG 前必须合成白底，因为 JPEG 不支持 alpha。

```go
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
)

// applyOrientation 按 EXIF 方向 1 到 8 做镜像与旋转，返回校正后的图。
func applyOrientation(src image.Image, o int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	switch o {
	case 5, 6, 7, 8:
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.At(b.Min.X+x, b.Min.Y+y)
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			default:
				dx, dy = x, y
			}
			dst.Set(dx, dy, c)
		}
	}
	return dst
}

// resizeNN 用最近邻等比缩到指定宽度，高度按原比例取整。
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

// flatten 把带 alpha 的图合成到白底，PNG 转 JPEG 前必须调用。
func flatten(src image.Image) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, b, src, b.Min, draw.Over)
	return dst
}

func main() {
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x * 60), G: 120, B: uint8(y * 100), A: 255})
		}
	}
	rotated := applyOrientation(src, 6) // 模拟手机竖拍，方向 6
	fmt.Println("方向校正:", src.Bounds(), "到", rotated.Bounds())
	thumb := resizeNN(rotated, 3)
	fmt.Println("缩略图:", thumb.Bounds())

	// 解码后重编码既是产物，也是一道安全清洗，把输入归一化成安全格式。
	var jbuf bytes.Buffer
	if err := jpeg.Encode(&jbuf, flatten(thumb), &jpeg.Options{Quality: 80}); err != nil {
		fmt.Println("JPEG 编码失败:", err)
		return
	}
	decoded, err := jpeg.Decode(bytes.NewReader(jbuf.Bytes()))
	if err != nil {
		fmt.Println("JPEG 回读失败:", err)
		return
	}
	fmt.Println("JPEG 回读:", decoded.Bounds(), "字节=", jbuf.Len())

	var pbuf bytes.Buffer
	if err := png.Encode(&pbuf, thumb); err != nil {
		fmt.Println("PNG 编码失败:", err)
		return
	}
	pdec, err := png.Decode(bytes.NewReader(pbuf.Bytes()))
	if err != nil {
		fmt.Println("PNG 回读失败:", err)
		return
	}
	fmt.Println("PNG 回读:", pdec.Bounds(), "字节=", pbuf.Len())
}
```

`applyOrientation` 按输出像素反算输入坐标，方向 6 到 8 会交换宽高；`resizeNN` 的内存只与 `width × height` 有关，与输入尺寸无关，这正是大图缩略不会放大内存的原因。`flatten` 用 `draw.Draw` 先铺白底再 `Over` 叠加，避免透明区域编码成黑块。解码后重编码是第二道安全闸：只验魔数仍可在合法头后塞畸形数据，重新编码把输入归一化成标准库自己产出的安全产物。生产把 `resizeNN` 换成更高质量的缩放器时，只需替换循环体，调用方与 `ImageProcessor` 接口不变。

<OutputBox title="派生与转码演示输出" output="方向校正: (0,0)-(4,2) 到 (0,0)-(2,4)\n缩略图: (0,0)-(3,6)\nJPEG 回读: (0,0)-(3,6) 字节= ...\nPNG 回读: (0,0)-(3,6) 字节= ..." />

### 6.4 ETag 与内容寻址：让重复下载零成本

设计意图：用内容哈希同时做对象键、URL 与 ETag，三件事合成一件。关键取舍是 ETag 必须带双引号、且用 `immutable` 长效缓存：内容寻址下键随内容变化，永远不会出现「URL 不变内容变了」的脏缓存，因此可以放心设一年。

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

type contentStore struct {
	mu   sync.RWMutex
	data map[string][]byte
	typ  map[string]string
}

func newContentStore() *contentStore {
	return &contentStore{
		data: make(map[string][]byte),
		typ:  make(map[string]string),
	}
}

func (c *contentStore) Put(b []byte, ct string) string {
	sum := sha256.Sum256(b)
	key := hex.EncodeToString(sum[:])
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.data[key]; !ok {
		c.data[key] = append([]byte(nil), b...)
		c.typ[key] = ct
	}
	return key
}

func (c *contentStore) Get(key string) ([]byte, string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	b, ok := c.data[key]
	return b, c.typ[key], ok
}

func etagOf(key string) string {
	return "\"" + key + "\"" // entity-tag 必须带引号
}

// matchETag 兼容多值、弱校验与通配符，命中即回 304。
func matchETag(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		p := strings.TrimSpace(part)
		if p == etag || p == "W/"+etag || p == "*" {
			return true
		}
	}
	return false
}

func serveImage(c *contentStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/img/")
		b, ct, ok := c.Get(key)
		if !ok {
			http.NotFound(w, r)
			return
		}
		etag := etagOf(key)
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("Content-Type", ct)
		if matchETag(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(b)
	})
}

func main() {
	c := newContentStore()
	key := c.Put([]byte("fake-image-bytes"), "image/png")
	srv := httptest.NewServer(serveImage(c))
	defer srv.Close()

	r1, _ := http.Get(srv.URL + "/img/" + key)
	body, _ := io.ReadAll(r1.Body)
	_ = r1.Body.Close()
	fmt.Println("首次:", r1.StatusCode, "bytes=", len(body), "ETag 有引号=",
		strings.HasPrefix(r1.Header.Get("ETag"), "\""))

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/img/"+key, nil)
	req.Header.Set("If-None-Match", r1.Header.Get("ETag"))
	r2, _ := http.DefaultClient.Do(req)
	b2, _ := io.ReadAll(r2.Body)
	_ = r2.Body.Close()
	fmt.Println("带缓存:", r2.StatusCode, "bytes=", len(b2))

	weak, _ := http.NewRequest(http.MethodGet, srv.URL+"/img/"+key, nil)
	weak.Header.Set("If-None-Match", "W/"+r1.Header.Get("ETag"))
	r3, _ := http.DefaultClient.Do(weak)
	_ = r3.Body.Close()
	fmt.Println("弱校验:", r3.StatusCode)
}
```

<OutputBox title="条件缓存演示输出" output="首次: 200 bytes= 16 ETag 有引号= true\n带缓存: 304 bytes= 0\n弱校验: 304" />

内容哈希带来三个好处：相同图片只存一份、URL 天然可永久缓存、ETag 不需要额外计算。`matchETag` 处理 `If-None-Match` 的多值、`W/` 弱校验与 `*` 通配，避免某些客户端的合法条件头被漏判而白白传输整图。304 分支只写状态码、不写正文，这是省带宽的关键；HEAD 请求同样只回头部，供 CDN 探活与取元数据。

<Callout type="warn" title="ETag 忘加引号等于没有缓存" text="HTTP 要求 entity-tag 用双引号包裹，少了引号客户端不会把它当作合法 ETag，If-None-Match 永远匹配不上，304 命中率直接归零。" />

### 6.5 对象存储直传与 HMAC 预签名 URL

设计意图：让客户端把大对象直接 PUT 到对象存储，应用只签发一个短时效、绑定方法与对象键的 URL，既省应用带宽也缩小攻击面。关键取舍是签名串同时绑定 method、object_key、expires 三项，并用 `hmac.Equal` 做常量时间比较——普通字符串比较会泄漏时序信息，给暴力伪造签名留下缝隙。

```go
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

type Signer struct {
	secret []byte
	ttl    time.Duration
}

func (s *Signer) message(method, key string, exp int64) string {
	return method + "\n" + key + "\n" + strconv.FormatInt(exp, 10)
}

func (s *Signer) sign(method, key string, exp int64) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(s.message(method, key, exp)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Presign 生成带过期时间与签名的直传 URL，方法被写进签名串不可篡改。
func (s *Signer) Presign(base, method, key string) (string, int64) {
	exp := time.Now().Add(s.ttl).Unix()
	q := url.Values{}
	q.Set("expires", strconv.FormatInt(exp, 10))
	q.Set("method", method)
	q.Set("signature", s.sign(method, key, exp))
	return base + "/" + key + "?" + q.Encode(), exp
}

// Verify 先判过期，再用常量时间比较签名，避免时序侧信道。
func (s *Signer) Verify(method, key string, exp int64, sig string) error {
	if time.Now().Unix() > exp {
		return fmt.Errorf("签名已过期")
	}
	want := s.sign(method, key, exp)
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return fmt.Errorf("签名不匹配")
	}
	return nil
}

func main() {
	s := &Signer{secret: []byte("test-secret-key"), ttl: 5 * time.Minute}
	method, key := "PUT", "sha256-abcdef"
	raw, exp := s.Presign("https://oss.example.com/upload", method, key)
	fmt.Println("预签名 URL 已生成, 过期时间戳=", exp)

	u, err := url.Parse(raw)
	if err != nil {
		fmt.Println("解析 URL 失败:", err)
		return
	}
	q := u.Query()
	gotExp, _ := strconv.ParseInt(q.Get("expires"), 10, 64)
	sig := q.Get("signature")
	fmt.Println("合法校验:", s.Verify(q.Get("method"), key, gotExp, sig))
	fmt.Println("篡改对象键:", s.Verify(q.Get("method"), key+"-evil", gotExp, sig))
	fmt.Println("过期签名:", s.Verify(q.Get("method"), key, time.Now().Add(-time.Hour).Unix(), sig))
	fmt.Println("篡改方法:", s.Verify("DELETE", key, gotExp, sig))
}
```

<OutputBox title="签名 URL 校验演示输出" output="预签名 URL 已生成, 过期时间戳= ...\n合法校验: err=&lt;nil&gt;\n篡改对象键: 签名不匹配\n过期签名: 签名已过期\n篡改方法: 签名不匹配" />

`message` 把方法、对象键、过期时间用换行拼接，三项任一被改都会让签名失配；`Verify` 先判过期再比签名，避免对已过期 URL 做无意义计算；`hmac.Equal` 是常量时间比较，不能用 `==` 替代。生产还要补三件事：密钥从配置中心或 KMS 注入、缺失则拒绝启动；URL 走 HTTPS 且不写进日志；签名有效期按对象大小收紧，8 MiB 的直传给 5 分钟足够，过长会放大泄露风险。

<Term word="像素炸弹" text="在图片头里声明极大宽高、诱使解码器按宽乘高分配数 GB 像素缓冲的畸形文件；用 image.DecodeConfig 只读头部判尺寸上限即可挡住。" />

### 6.6 HTTP 主流程：上传、下载、并发限流与优雅退出

设计意图：把前面所有模块装配成一条可运行的服务，集中展示流式上传、条件缓存、并发信号量与优雅退出。关键取舍是用有缓冲 channel 做并发上限而非令牌桶：上传是重 I/O 任务，超限时立刻 503 比排队更保护内存。

```go
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const maxUpload = 8 << 20

type contentStore struct {
	mu   sync.RWMutex
	data map[string][]byte
	ct   map[string]string
}

func newContentStore() *contentStore {
	return &contentStore{data: make(map[string][]byte), ct: make(map[string]string)}
}

func (c *contentStore) PutKey(key string, b []byte, ct string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.data[key]; !ok {
		c.data[key] = append([]byte(nil), b...)
		c.ct[key] = ct
	}
}

func (c *contentStore) Get(key string) ([]byte, string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	b, ok := c.data[key]
	return b, c.ct[key], ok
}

func sniff(head []byte) string {
	switch {
	case len(head) >= 3 && bytes.Equal(head[:3], []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case len(head) >= 8 && bytes.Equal(head[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return "image/png"
	default:
		return ""
	}
}

// semaphore 用有缓冲 channel 限并发，超限立刻 503，不让请求堆积。
func semaphore(n int) func(http.Handler) http.Handler {
	sem := make(chan struct{}, n)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				next.ServeHTTP(w, r)
			default:
				http.Error(w, "服务繁忙", http.StatusServiceUnavailable)
			}
		})
	}
}

func uploadHandler(store *contentStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "只接受 POST", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, "不是合法 multipart", http.StatusBadRequest)
			return
		}
		var part *multipart.Part
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, "读分片失败", http.StatusBadRequest)
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

		dir, err := os.MkdirTemp("", "img-")
		if err != nil {
			http.Error(w, "建临时目录失败", http.StatusInternalServerError)
			return
		}
		defer os.RemoveAll(dir) // 请求结束一定清理，杜绝临时文件泄漏

		tmp, err := os.CreateTemp(dir, "up-*")
		if err != nil {
			http.Error(w, "建临时文件失败", http.StatusInternalServerError)
			return
		}
		head := make([]byte, 512)
		n, _ := io.ReadFull(part, head)
		head = head[:n]
		ct := sniff(head)
		if ct == "" {
			tmp.Close()
			http.Error(w, "不是支持格式", http.StatusUnsupportedMediaType)
			return
		}
		h := sha256.New()
		_, _ = tmp.Write(head)
		_, _ = h.Write(head)
		if _, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(part, maxUpload)); err != nil {
			tmp.Close()
			http.Error(w, "流式读取失败", http.StatusBadRequest)
			return
		}
		if err := tmp.Close(); err != nil {
			http.Error(w, "关闭临时文件失败", http.StatusInternalServerError)
			return
		}
		data, err := os.ReadFile(tmp.Name())
		if err != nil {
			http.Error(w, "回读失败", http.StatusInternalServerError)
			return
		}
		key := hex.EncodeToString(h.Sum(nil))
		store.PutKey(key, data, ct)
		slog.Info("upload", "key", key[:12], "bytes", len(data), "ct", ct)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"key": key})
	})
}

func downloadHandler(store *contentStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/img/")
		b, ct, ok := store.Get(key)
		if !ok {
			http.NotFound(w, r)
			return
		}
		etag := "\"" + key + "\""
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("Content-Type", ct)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(b)
	})
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	store := newContentStore()
	mux := http.NewServeMux()
	mux.Handle("/upload", semaphore(4)(uploadHandler(store)))
	mux.Handle("/img/", downloadHandler(store))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("监听失败:", err)
		return
	}
	go func() { _ = srv.Serve(ln) }()
	base := "http://" + ln.Addr().String()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("image", "a.png")
	_, _ = fw.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3})
	_ = mw.Close()
	resp, err := http.Post(base+"/upload", mw.FormDataContentType(), &buf)
	if err != nil {
		fmt.Println("上传请求失败:", err)
		return
	}
	var out struct {
		Key string `json:"key"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()
	fmt.Println("上传状态:", resp.StatusCode)

	r2, _ := http.Get(base + "/img/" + out.Key)
	_, _ = io.Copy(io.Discard, r2.Body)
	_ = r2.Body.Close()
	fmt.Println("首次下载:", r2.StatusCode, "ETag 存在=", r2.Header.Get("ETag") != "")

	req, _ := http.NewRequest(http.MethodGet, base+"/img/"+out.Key, nil)
	req.Header.Set("If-None-Match", r2.Header.Get("ETag"))
	r3, _ := http.DefaultClient.Do(req)
	_, _ = io.Copy(io.Discard, r3.Body)
	_ = r3.Body.Close()
	fmt.Println("带缓存下载:", r3.StatusCode)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		fmt.Println("关闭超时:", err)
	}
	fmt.Println("服务已优雅退出")
}
```

`semaphore(4)` 把同时处理的上传限制在 4 个，超限立刻 503，避免几十个 8 MiB 上传同时打满内存；`http.MaxBytesReader` 在读阶段截断超大请求；`defer os.RemoveAll(dir)` 保证每个 return 分支都清理临时目录；`slog` 打一条带键前缀与字节数的结构化日志，便于串联一次上传。`http.Server` 设 `ReadHeaderTimeout` 防慢头攻击，每个 `resp.Body` 都 `Close` 释放连接，最后 `Shutdown` 带超时优雅退出。生产应把 `signal.NotifyContext` 接进来，收到 SIGTERM 后先摘流量、等在途上传收尾、再关闭。

<OutputBox title="HTTP 主流程演示输出" output="上传状态: 201\n首次下载: 200 ETag 存在= true\n带缓存下载: 304\n服务已优雅退出" />

## 7. 失败路径与边界条件

评审时最该被追问的是「哪个组件挂了会怎样」。下表把故障点、触发条件、现象、发现手段、兜底与恢复动作逐条对齐，所有兜底都要能对应到前面某段代码。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| 超大请求体 | 单文件超过 8 MiB | 上传 413 | 413 计数与请求体大小分布 | MaxBytesReader 在读阶段截断 | 客户端压缩或改用分片上传 |
| 对象存储 PUT 超时 | 网络抖动超过 3 秒 | 上传 503 | PUT P99 与 timeout 计数 | 重试两次并退避，仍失败返回 503 且清理临时文件 | 排查对象存储、扩容带宽、从孤儿扫描清理残留 |
| 像素炸弹 | IHDR 声明超大尺寸 | 内存暴涨或进程 OOM | DecodeConfig 拒绝计数突增 | 校验阶段拒绝，不分配像素缓冲 | 属攻击流量无需恢复，补 WAF 规则 |
| 伪装文件 | 扩展名 png 实为 HTML | 恶意文件差点入库 | 415 计数与魔数拒绝日志 | 魔数校验加解码重编码双重拒绝 | 清理已误存对象并追溯上传方 |
| EXIF 段损坏 | APP1 长度非法 | 方向解析失败 | 解析错误日志与告警 | 方向默认 1，不中断上传 | 记录样本、后续换成熟 EXIF 库 |
| 派生失败 | Worker 解码异常 | 缩略图缺失 | thumb_key 为空的比例告警 | 降级为仅原图可用，不回滚上传 | 异步重放派生任务、补生成缩略图 |
| 临时文件残留 | 清理分支漏 defer | 磁盘持续增长 | 临时目录文件数与磁盘水位 | 请求级 defer 加定时兜底扫描 | 扫描删除超过阈值的孤儿文件 |
| 签名密钥缺失 | 配置未注入 | 启动失败 | 启动配置校验 | 拒绝启动，不降级为不签名 | 注入密钥后重新发布 |
| 时钟回拨 | NTP 校时倒退 | 合法签名被判过期 | 时间偏移监控 | 校验方允许小范围时钟偏差 | 等待时钟追平或人工刷新签名 |
| 进程崩溃 | OOM 或 panic | 在途上传丢失 | 存活探针与崩溃计数 | 无状态实例重启，临时文件由定时扫描兜底清理 | 自动拉起、清理孤儿临时文件 |
| 缓存条件头缺失 | 老客户端不带 If-None-Match | 304 占比下降 | 304 命中率曲线 | 按 200 正常回源，正确性不受影响 | 推动客户端升级或前置 CDN 兜底 |

幂等键在图片服务里就是内容哈希本身：同一张图重复上传得到同一个 `obj_key`，`Put` 里用「键已存在则不覆盖」收敛，不产生重复对象，也天然免去去重表。重试预算写死：对象存储 PUT 最多两次、退避 50 与 100 毫秒；派生任务最多三次且进死信；下载回源不重试，超时直接 503。超时通过 `context.WithTimeout` 沿链路逐层传递，不允许某一层私自延长 deadline。降级有明确人工介入点：当临时目录文件数持续增长或孤儿对象占比超过 1% 时，值班需手动触发一次全量清理扫描；签名密钥缺失、时钟大幅回拨则必须人工确认后才能恢复，禁止自动带病启动。

<Callout type="idea" title="派生失败不该拖垮上传" text="缩略图是增强而非前提：把它放异步队列，失败只记告警并把 thumb_key 留空，下载缩略图时回退原图，上传接口照样返回 201，这条边界能挡住大半线上事故。" />

## 8. 并发与一致性

系统的并发模型分三类 goroutine：每个请求一个短生命周期 handler goroutine，由 `net/http` 管理；对象存储与元数据用 `sync.RWMutex` 保护共享状态；派生 Worker 与定时清理是长生命周期后台 goroutine，由 `context` 统一叫停。channel 用在两处：并发信号量限流、以及有界的派生任务队列，不用于请求间传递大对象——大对象一律走 `io.Reader` 流式接口，避免把几 MB 数据塞进 channel 造成拷贝与阻塞。

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

// worker 从 jobs 取任务、把结果写入 results；jobs 关闭且排空后退出。
func worker(id int, jobs <-chan int, results chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		time.Sleep(time.Millisecond) // 模拟缩略图处理
		results <- fmt.Sprintf("worker=%d job=%d", id, job)
	}
}

func main() {
	const total, workers = 10, 4
	jobs := make(chan int)              // 无缓冲，天然背压
	results := make(chan string, total) // 有缓冲，避免 worker 被消费者阻塞
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker(i, jobs, results, &wg)
	}
	go func() {
		for i := 0; i < total; i++ {
			jobs <- i
		}
		close(jobs) // 必须关闭，否则 worker 永远阻塞
		wg.Wait()
		close(results) // 所有生产者退出后才关结果通道
	}()
	count := 0
	for range results {
		count++
	}
	fmt.Println("处理结果数:", count, "worker 数:", workers)
}
```

竞态分析的重点有三处：`contentStore` 的 `data` 与 `ct` 两个 map 必须在同一把写锁内更新，否则并发上传会读到「有字节没类型」的中间态；`signer` 的密钥只读、无需加锁，但配置热加载时要用 `atomic.Value` 整体替换而非逐字段改；worker pool 的 `close(jobs)` 必须由唯一生产者执行，`close(results)` 必须等 `wg.Wait()` 之后，否则会触发向已关闭 channel 发送的 panic。锁粒度上，元数据与对象映射分两把锁，信号量是独立 channel，避免一把大锁把上传与下载串行化。

一致性级别选择「读己之写最终一致」：上传成功后允许极短时间元数据未命中而回源对象存储，但通过「PUT 成功才写元数据、失败即清理」保证不会返回指向不存在对象的键。顺序保证上，图片服务只要求「同一 obj_key 的重复上传收敛为一次写入」，不要求全局有序；派生任务允许乱序与重试，因为缩略图由内容哈希寻址，重算结果必然一致。对乱序的处理是 `Put` 里先查键再写，键已存在直接返回，把重复挡在写入口。

<Callout type="warn" title="别用无界 channel 做派生队列" text="派生任务用无界 channel 会在上传洪峰时无限堆积、吃穿内存；必须用有界 channel 加背压，队列满时丢弃低价值派生并计数告警，主链路不受影响。" />

## 9. 可观测性

上传链的内存与拒绝率、下载链的 304 占比，是这类系统的生命线，指标必须能直接回答「现在是输入问题、存储问题还是缓存问题」。

| 指标名 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| img_upload_total | Counter | 上传总次数，按状态码分标签 | 5xx 占比大于 0.5% |
| img_upload_bytes | Histogram | 单文件大小分布 | P99 接近 8 MiB 上限 |
| img_upload_duration_ms | Histogram | 上传耗时 | P99 大于 800 毫秒 |
| img_reject_reason | Counter | 拒绝原因（魔数/像素/体积/限流） | 像素炸弹大于 0 |
| img_decode_reject | Counter | DecodeConfig 拒绝数 | 突增视为攻击 |
| img_download_304_ratio | Gauge | 304 占比 | 低于 85% |
| img_thumb_fail | Counter | 派生失败数 | 大于上传量 0.1% |
| img_temp_files | Gauge | 临时目录当前文件数 | 持续增长不回落 |
| img_presign_verify_fail | Counter | 签名校验失败数 | 突增视为伪造尝试 |

结构化日志统一带 `trace_id`、`obj_key`、`status`、`bytes`、`cost_ms`、`reason` 六个字段：`trace_id` 从入口中间件注入并透传给派生 Worker 与对象存储客户端，一次失败上传就能用一条 grep 串起流式读取、校验、PUT、清理全过程。采样上，成功上传按 5% 采样，4xx/5xx 与像素炸弹拒绝全量保留，避免高频上传把日志盘打爆；日志里绝不打印完整签名 URL 与请求体。

```go
package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
)

type Metrics struct {
	UploadBytes atomic.Int64
	BombReject  atomic.Int64
	NotModified atomic.Int64
	ThumbFail   atomic.Int64
}

func (m *Metrics) ObserveUpload(bytes int64, status int) {
	m.UploadBytes.Add(bytes)
	slog.Info("upload", "bytes", bytes, "status", status)
}

func (m *Metrics) ObserveReject(reason string) {
	if reason == "pixel-bomb" {
		m.BombReject.Add(1)
	}
	slog.Warn("reject", "reason", reason)
}

func (m *Metrics) Ratio304(total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(m.NotModified.Load()) / float64(total)
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	m := &Metrics{}
	m.ObserveUpload(2<<20, 201)
	m.ObserveReject("pixel-bomb")
	m.ObserveReject("bad-magic")
	m.NotModified.Add(85)
	fmt.Println("累计上传字节:", m.UploadBytes.Load(),
		"像素炸弹拒绝:", m.BombReject.Load(),
		"304 占比:", m.Ratio304(100))
}
```

用这套埋点定位一次真实故障：告警「上传 P99 升到 3 秒、`img_temp_files` 持续上涨、5xx 抬头」→ 看 `img_reject_reason` 无异常，排除输入问题 → 看对象存储 PUT P99 同步抬升，确认下游变慢 → 看 `img_temp_files` 不回落，说明某个超时分支漏了 `defer` 清理 → 结论是 PUT 超时后临时文件残留累积占满磁盘、反过来拖慢后续上传 → 按第 7 节补清理并触发全量扫描。整条推理链只依赖上面这张指标表，不靠猜。

## 10. 测试策略

测试按五层切分，mock 边界画在 `ObjectStore`、`ImageProcessor`、`Signer` 三个接口上：领域逻辑用内存 fake 做纯单测，不碰网络与磁盘；HTTP 层用 `httptest` 做集成测试，验证状态码、ETag 与清理；契约测试锁定 JSON 字段与错误码；故障注入用可控 fake 让 `Put` 返回超时、让临时目录删除失败，验证兜底分支；基准与压测单独跑。这样切的理由是：把外部依赖挡在接口外，能让 90% 的校验与派生逻辑在毫秒级单测里覆盖，把真正需要 I/O 的部分收敛到少量集成测试。

```go
package main

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

const maxPixels = 40_000_000

func pngBomb() []byte {
	ihdr := []byte{0, 0, 0x75, 0x30, 0, 0, 0x75, 0x30, 8, 2, 0, 0, 0}
	body := append([]byte("IHDR"), ihdr...)
	crc := crc32.ChecksumIEEE(body)
	chunk := []byte{0, 0, 0, 13}
	chunk = append(chunk, body...)
	chunk = append(chunk, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
	sig := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	return append(sig, chunk...)
}

func checkPixels(head []byte) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil {
		return err
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return fmt.Errorf("像素超限: %dx%d", cfg.Width, cfg.Height)
	}
	return nil
}

func TestCheckPixels(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{"合法小图", buf.Bytes(), false},
		{"像素炸弹", pngBomb(), true},
		{"伪造后缀", []byte("<html>not image</html>"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkPixels(c.data)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func BenchmarkCheckPixels(b *testing.B) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		b.Fatal(err)
	}
	data := buf.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = checkPixels(data)
	}
}
```

<OutputBox title="测试与基准执行命令" output="go test -race -cover ./...\ngo test -run TestCheckPixels -v\ngo test -bench BenchmarkCheckPixels -benchmem" />

`TestCheckPixels` 用表驱动覆盖合法小图、像素炸弹、伪造后缀三类输入，断言 `DecodeConfig` 准入判断的边界；`pngBomb` 构造 CRC 合法的 IHDR，确保测的是尺寸判断而非解析失败；集成层另用 `httptest` 断言 201 与 ETag 带引号、304 无正文、415 拒绝伪装文件，并在 handler 返回后检查临时目录文件数归零；`-race` 验证 `contentStore`、信号量与 worker pool 的并发正确性；Benchmark 报告每次准入检查的分配数，防止后续优化把「只读头部」退化成整图解码。

## 11. 部署与容量

部署拓扑是「四台无状态应用实例 + 对象存储 + 元数据主从 + 派生 Worker 池 + CDN」，实例前面挂负载均衡做健康检查，只有 `/healthz` 全绿才接流量；大对象由客户端凭预签名 URL 直传对象存储，不穿过应用。

```text
                    ┌──────────────┐
   上传客户端 ─────▶│  负载均衡 LB  │◀──── 浏览客户端
                    └──────┬───────┘          │
        ┌──────────────┬───┴────┐             │ 命中 CDN 直接返回
        ▼              ▼        ▼             ▼
   ┌────────┐     ┌────────┐ ┌────────┐   ┌────────┐
   │应用实例│×4   │   ...   │ │ Worker │   │  CDN   │
   └───┬────┘     └────────┘ └───┬────┘   └───┬────┘
       │ 元数据        预签名直传 │ 派生      │ 回源
       ▼                         ▼           ▼
   ┌────────────┐          ┌──────────────────────┐
   │ 元数据主从 │          │ 对象存储（原图+派生图）│
   └────────────┘          └──────────────────────┘
```

依赖组件与版本、配置项、资源估算分别如下，配置项默认值与环境差异必须写清，否则「同一份镜像跑多套环境」就是空话。

| 组件 | 版本 | 用途 | 单实例规格 |
| --- | --- | --- | --- |
| Go 运行时 | 1.21+ | 服务本体 | 2 核 2 GB |
| 对象存储 | S3 兼容协议 | 原图与派生图 | 托管，按量 |
| MySQL | 8.0 | 元数据与对象键 | 4 核 16 GB SSD |
| CDN | 托管 | 边缘缓存与 304 | 托管 |
| 消息队列 | Kafka 3.x | 派生任务与死信 | 3 节点 |

| 配置项 | 默认值 | 生产值 | 说明 |
| --- | --- | --- | --- |
| MAX_UPLOAD | 8 MiB | 8 MiB | 单文件上限，与 MaxBytesReader 一致 |
| MAX_PIXELS | 4000 万 | 4000 万 | 像素上限，防炸弹 |
| THUMB_WIDTH | 320 | 320 | 缩略图宽度，高度按比例 |
| UPLOAD_CONCURRENCY | 4 | 16 | 并发上传信号量容量 |
| PUT_TIMEOUT | 3s | 3s | 对象存储 PUT 超时，超时重试两次 |
| PRESIGN_TTL | 5min | 5min | 直传签名有效期 |
| CACHE_MAX_AGE | 31536000 | 31536000 | immutable 缓存秒数 |
| TEMP_DIR | 系统临时目录 | /data/tmp | 必须落在可监控的独立分区 |
| SIGN_SECRET | 无 | KMS 注入 | 缺失则拒绝启动 |

资源估算：上传并发上限 16、单请求内存约 512 KB 缓冲，四实例合计不到 32 MB 用于上传缓冲，与文件大小无关；元数据按五千万对象、单行约 200 字节估算约 10 GB，可放内存热点缓存约 2 GB；临时目录按峰值 16 个上传乘 8 MiB 估算最多 128 MB，独立分区给 2 GB 足够并设水位告警；带宽按下载 2 万 QPS、304 占比 85%、其余平均 60 KB 估算源站出带宽约 1.4 Gbps，CDN 承担大头。扩容方式是水平加应用实例：无状态、对象与元数据在远端，加机器不改代码；优雅退出用 `signal.NotifyContext` 接 SIGTERM，先让 `/healthz` 返回 503 摘流量、等待在途上传收尾（包括临时文件清理完成）、再 `Shutdown` 关闭连接并 `wg.Wait` 收回派生 Worker 与定时清理 goroutine，最后才退出进程。

## 12. 性能与压测

压测要模拟真实读写比与大对象，只打小图或只打上传都会得出误导性结论。先用 `go test -bench` 建立准入检查与缩略图基线，再用 `hey` 按 70 比 1 混合压上传与下载，下载侧务必带 `If-None-Match` 以还原 304 占比。

```bash
# 1) 准入检查与派生基准
go test -bench . -benchmem -count 5 | tee bench.txt
# 2) 构造 2 MiB 真实 PNG 作为压测样本
go run ./cmd/gen-sample -size 2MiB -out sample.png
# 3) 上传侧混合压测，观察内存是否随并发线性增长
hey -z 5m -c 40 -m POST \
  -H 'Content-Type: multipart/form-data; boundary=bench' \
  -D sample.png 'https://img.example.com/api/v1/upload' > upload.txt
# 4) 下载侧带条件头压测，统计 304 占比
hey -z 5m -c 200 -H 'If-None-Match: "基准ETag"' \
  'https://img.example.com/img/基准键' > download.txt
# 5) 采集内存与 CPU 证据
go tool pprof -http=:0 http://127.0.0.1:6060/debug/pprof/heap?seconds=30
```

| 指标 | 优化前 | 优化后 | 说明 |
| --- | --- | --- | --- |
| 并发 20 上传峰值内存 | 1.9 GB | 96 MB | 换 MultipartReader 流式读，去掉 io.ReadAll |
| 上传 P99 | 2.4 s | 620 ms | 落临时文件与哈希一次遍历完成 |
| 首次下载 P99 | 210 ms | 38 ms | 元数据命中即回，正文交 CDN |
| 304 占比 | 41% | 89% | ETag 带引号并正确处理弱校验 |
| 源站出带宽 | 4.1 Gbps | 1.4 Gbps | immutable 长缓存加条件请求 |
| 像素炸弹拦截 | 无 | 100% | DecodeConfig 准入，零像素分配 |
| 临时文件残留 | 日均数千 | 0 | 每个 return 分支都有 defer 清理 |

瓶颈定位过程有指标证据而不是拍脑袋：首轮压测并发 20 上传时内存冲到 1.9 GB，pprof heap 显示 `io.ReadAll` 与 `multipart.File` 缓冲占了大头，改用 `MultipartReader` 加 `io.MultiWriter` 后降到 96 MB 且与文件大小解耦。第二轮发现 304 占比只有 41%，抓包确认 ETag 忘了加引号导致条件头永不匹配，补上引号与 `matchETag` 后升到 89%，源站出带宽随之从 4.1 Gbps 降到 1.4 Gbps。仍然存在的限制要如实写出：最近邻缩略图在大幅缩小时锯齿明显，换高质量缩放器会增加 CPU 与依赖；标准库不支持 WebP/AVIF 编码，无法进一步压体积；手写 EXIF 解析只覆盖 IFD0 的 Orientation，遇到压缩缩略图或异常字节序会退回方向 1；`immutable` 长缓存意味着一旦误存错误内容，只能靠换新键或 CDN 刷新纠正，没有「改了就生效」的捷径。

## 13. 演进与替代方案

没有一套架构能吃到所有规模，评审必须写清「什么时候该换」。下表给出两个真实替代方案与当前方案的取舍对比。

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 当前：本地临时文件 + 应用代理 PUT + 单机元数据 | 日新增十万级、单机房 | 结构简单、无外部队列、RPO 为 0 | 应用仍经手一次上传流、单机元数据是扩展瓶颈 | 上传量持续增长或需要跨机房容灾 |
| 替代一：预签名 URL 全量直传 + 独立派生服务 + 对象存储事件通知 | 日新增百万级、多机房 | 应用完全不经手大对象、派生可独立扩缩 | 依赖对象存储事件通知、最终一致带来短暂缩略图缺失、链路可观测更复杂 | 应用带宽成为瓶颈且能接受派生延迟 |
| 替代二：边缘上传 + 边缘实时转码 + 多规格自适应 | 日新增千万级、全球加速 | 上传与转码都在边缘完成、源站压力极低 | 成本高、供应商锁定、格式与质量策略难统一 | 流量全球化且对首图延迟极敏感 |

从当前版本演进到替代一的触发条件写死成三条：应用出带宽持续一周超过预算的 70%、上传 P99 因代理转发长期不达标、或需要跨机房双活。迁移步骤分四步且每步可回滚：第一步上线预签名接口但保留代理上传作为影子路径，双写比对同一内容哈希是否一致；第二步按客户端版本灰度切到直传，元数据写入改由对象存储事件通知触发，代理路径冻结；第三步观察一周，校验 `img_thumb_fail` 与孤儿对象比例无异常后下掉代理上传代码；第四步删除临时目录相关配置与清理 goroutine。回滚方案是保留代理上传二进制与临时目录分区至少一个发布周期，一旦事件通知大面积延迟或签名校验异常，立即把客户端切回代理路径，并用内容哈希做去重避免重复对象。

<FlowTimeline title="图片服务演进路线" direction="horizontal" :steps='[{"title":"v1 单机代理","desc":"MultipartReader 流式落临时文件再代理 PUT，够用于内部系统与低流量验证","tone":"blue"},{"title":"v2 缓存与直传","desc":"内容寻址 ETag 加 CDN 长缓存，预签名 URL 让大对象绕过应用","tone":"green"},{"title":"v3 派生解耦","desc":"缩略图进队列由独立 Worker 处理，失败降级且可重放","tone":"orange"},{"title":"v4 边缘化","desc":"上传与转码下沉到边缘节点，接 ETL 做成本与命中分析","tone":"purple"}]' />

v3 的派生任务队列与失败重放正是第 110 讲分布式调度与第 107 讲日志 ETL 的输入；断点续传、分块与秒传属于第 071 讲文件存储；缓存穿透、击穿、雪崩的系统化治理在第 067 讲。本讲只把「流式、验真、可缓存、可清理」这条主线打穿。

## 14. 真实工程里的坑

以下六条都来自真实生产事故，逐条给出现象、原因、错误写法与正确写法，以及能提前发现的手段。

**坑一：`r.FormFile` 把分片缓进内存打满堆。** 现象是并发二十个上传后进程 OOM。原因是 `ParseMultipartForm` 默认最多把 32 MiB 缓进内存。错误写法是 `f, _, _ := r.FormFile("image")` 后 `io.ReadAll(f)`；正确写法用 `r.MultipartReader()` 把分片当 `io.Reader`，只读头部判魔数、其余 `io.Copy` 到临时文件（见第 6.1 节）。提前发现：并发上传压测采样 `runtime.ReadMemStats`，断言内存不随文件大小与并发线性增长。

**坑二：只信扩展名导致恶意文件被分发。** 现象是对象存储里出现改名为 png 的 HTML，被 CDN 缓存后成为存储型攻击载体。错误写法是 `if strings.HasSuffix(name, ".png") { 放行 }`；正确写法用魔数校验真实类型，再解码重编码把输入归一化成安全产物（见第 6.1 与 6.3 节）。提前发现：把伪装 HTML、错魔数的 JPEG 列入安全单测，CI 强制 415 通过。

**坑三：直接 `image.Decode` 中像素炸弹。** 现象是一张声明 30000 乘 30000 的 PNG 让单请求申请数 GB 内存。原因是解码器按宽高分配像素缓冲。错误写法是拿到流就 `image.Decode(r)`；正确写法先 `image.DecodeConfig` 读头部判 `宽乘高` 上限，通过后才 `Decode`（见第 6.2 节）。提前发现：故障注入一个 CRC 合法的超大 IHDR，断言在分配像素前就被拒绝且 `img_decode_reject` 计数加一。

**坑四：忽略 EXIF 方向让手机照片躺倒。** 现象是竖拍人像在页面上横躺，运营反复重传。原因是 JPEG 像素本身是横向存储，方向只写在 EXIF Orientation 里。错误写法是解码后直接缩放；正确写法先读 APP1 段的 Orientation，按 1 到 8 校正再派生（见第 6.2 与 6.3 节）。提前发现：为八个方向各造一张带标记的测试图，断言校正后的宽高与像素位置符合预期。

**坑五：临时文件在某个 early return 分支残留。** 现象是磁盘水位缓慢上涨直至写满，且难以复现。原因是只在成功路径 `os.Remove`，异常 `return` 漏了清理。错误写法是函数末尾 `os.Remove(path)`；正确写法是创建后立刻 `defer os.RemoveAll(dir)` 与 `defer os.Remove(path)`，让每个分支都兜底（见第 6.1 与 6.6 节）。提前发现：对每个错误分支做故障注入，断言 handler 返回后临时目录文件数归零，并对 `img_temp_files` 设持续增长告警。

**坑六：ETag 忘加引号导致 304 永不命中。** 现象是带宽账单居高不下，抓包发现客户端从不发 `If-None-Match`。原因是 entity-tag 规范要求双引号包裹，裸哈希不被识别。错误写法是 `w.Header().Set("ETag", key)`；正确写法是 `"\"" + key + "\""` 并用 `matchETag` 兼容弱校验（见第 6.4 节）。提前发现：契约测试断言响应头 ETag 以引号开头，且带 `If-None-Match` 的请求返回 304 且正文长度为零。

<Callout type="idea" title="坑要写进回归用例" text="每一条生产坑都应至少变成一个自动化测试或一条告警规则，否则复盘完还会再犯；本讲六条坑分别对应内存基准、安全用例、故障注入、方向用例、清理断言与契约测试。" />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l10-projects/lesson-106-net-image-server`](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-106-net-image-server)。clone 仓库后进入 `examples/l10-projects`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给上传链补上像素上限与 EXIF 校正" task="在本讲流式上传的基础上，让畸形大图不再打爆内存、让手机竖拍图不再躺倒：用 DecodeConfig 判像素上限，解析 EXIF 方向并校正后再生成缩略图，且保证任何拒绝分支都清理临时文件。" :hints='["提示一：image.DecodeConfig 只读头部、不分配像素缓冲，必须在 image.Decode 之前调用","提示二：像素上限用 int64 计算宽乘高，避免 int 溢出导致判断失效","提示三：EXIF 方向藏在 JPEG 的 APP1 段，先判 Exif 前缀再按 TIFF 字节序读 IFD0 的 0x0112 标签","提示四：缩略图前先按方向校正，方向 6 到 8 会交换宽高","提示五：每个拒绝分支都要有 defer 清理临时文件，断言目录最终为空"]' solution='要点一：流式落临时文件后，先用 image.DecodeConfig 读头部拿到宽高，用 int64 计算乘积，超过四千万像素立即返回 415 并清理。\n要点二：准入通过后扫描 JPEG 的 APP1 段，确认 Exif 前缀，按 TIFF 字节序定位 IFD0 读取 Orientation 标签，取值限定在 1 到 8。\n要点三：完整解码后先调用 applyOrientation 校正方向，再调用 resizeNN 等比缩放，方向 6 到 8 会交换宽高。\n要点四：透明 PNG 转 JPEG 前用白底合成，编码后回读一次作为安全清洗。\n要点五：临时目录创建后立刻 defer os.RemoveAll，任何 return 分支都不残留文件，并用故障注入断言目录为空。' explain="五处改动分别对应准入、方向解析、校正派生、格式清洗与资源释放；顺序不能颠倒，Decode 必须在 DecodeConfig 之后，清理必须覆盖所有分支。" />

参考实现可直接复用第 6.2 节的 `checkPixels`、`jpegOrientation` 与第 6.3 节的 `applyOrientation`、`resizeNN`：把 `DecodeConfig` 作为解码前的第一道门，把方向值传给 `applyOrientation`，并在所有 `http.Error` 之前确认 `defer` 已就位。改完用第 10 节的 `-race` 与 `BenchmarkCheckPixels` 验证并发与准入开销没有回退。

## 随堂检测

<Quiz no="106-1" q="为什么上传用 MultipartReader 而不是 r.FormFile？" :options='["FormFile 不支持 PNG","FormFile 走 ParseMultipartForm 默认最多缓存 32 MiB 到内存，破坏常数内存目标","MultipartReader 编码更快","FormFile 无法读取字段名"]' answer="1" explain="FormFile 背后是 ParseMultipartForm，会把分片缓存进内存；MultipartReader 把分片暴露成 io.Reader，可边读边写临时文件，内存与文件大小解耦。" />

<Quiz no="106-2" q="一张声明 30000 乘 30000 像素的 PNG 为什么必须用 DecodeConfig 先挡？" :options='["DecodeConfig 更快","image.Decode 会按宽高分配数 GB 像素缓冲，DecodeConfig 只读头部不分配","DecodeConfig 能自动压缩图片","PNG 不支持大尺寸"]' answer="1" explain="解码器按宽乘高乘四字节分配像素缓冲，直接 Decode 会申请数 GB 内存；DecodeConfig 只读 IHDR 拿尺寸，可在分配前判上限拒绝，这是像素炸弹的唯一正确防线。" />

<Predict no="106-3" q="某客户端带正确的 If-None-Match 再次请求同一张图，服务端会返回什么？" :options='["200 并带完整正文","404 Not Found","304 Not Modified 且无正文","500 错误"]' answer="2" output="HTTP/1.1 304 Not Modified" explain="内容寻址下 ETag 就是对象键，条件头相等说明客户端已有最新副本，服务端只回 304 不写正文，这是重复下载几乎零成本的关键。" />

<FillBlank no="106-4" tpl="防像素炸弹要用 ___ 只读头部判尺寸，ETag 必须用 ___ 包裹才合法，直传签名要用 ___ 做常量时间比较。" :answers='["DecodeConfig","双引号","hmac.Equal"]' hint="想想准入、缓存契约与签名校验" explain="image.DecodeConfig 不分配像素缓冲即可读宽高；HTTP 要求 entity-tag 带双引号；hmac.Equal 避免普通比较的时序侧信道。" :explains='["它只读头部取宽高，不分配像素缓冲；声明超大尺寸的伪造图若直接解码会瞬间吃光内存，先判尺寸即可在准入阶段拒掉。","HTTP 规定 entity-tag 必须是带引号的不透明串，弱校验加 W/ 前缀；缺引号就不合规，浏览器与 CDN 的条件请求可能直接忽略。","普通比较遇到首个不同字节就返回，耗时随匹配前缀长度变化，可被按时序逐位猜出签名；Equal 恒定走完全程，不泄漏匹配进度。"]' />

## 一页纸速记

<KnowledgeCard :points='["流式读让内存与文件大小解耦，MultipartReader 加 MaxBytesReader 是上传底座","魔数、DecodeConfig 准入、解码重编码三层叠加才把上传变成可暴露接口","EXIF 方向先校正再缩放，透明图转 JPEG 前要合成白底","内容哈希同时解决去重、URL、ETag 与永久缓存，304 让重复下载零成本","预签名 URL 绑定方法、键、过期时间，临时文件必由 defer 清理"]' :pitfalls='["只验魔数不验尺寸会中像素炸弹","ETag 忘加引号 304 永不命中","FormFile 或 io.ReadAll 会把内存打满","异常 return 分支漏删临时文件会撑满磁盘","派生失败同步阻塞上传会把增强变成单点"]' summary="别把大对象装进内存，也别信请求里的元数据，让每个决策的代价都写得出来。" />

## 小结与下一讲

- 中间件链按检查成本排序，便宜的拒绝放在昂贵的解码之前；流式读、双重校验、解码重编码三层叠加才敢把上传暴露到公网。
- 内容哈希把存储、去重、URL 与 ETag 合成一件事，带引号的 ETag 加 304 让重复下载几乎零成本。
- 预签名直传把大对象挡在应用之外，临时文件由 defer 无条件清理，派生失败只降级不阻塞主链路。

[下一讲：实战 05 · 日志分析 ETL 管道](/projects/data-log-etl)
