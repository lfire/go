package main

// 本文件集中声明爬虫各层的接口契约，是依赖倒置的锚点：
// 高层模块只依赖接口，不依赖 Redis 或 net/http 的具体实现。

import "context"

// Frontier 是待抓 URL 的有界优先队列。
type Frontier interface {
	Push(url string, priority, depth int) bool
	Pop() (string, int, bool)
	Len() int
}

// Deduplicator 判定并标记 URL，Visit 必须是原子操作。
type Deduplicator interface {
	Visit(url string) bool
}

// Fetcher 下载单个 URL，必须尊重 ctx 取消与超时。
type Fetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

// Store 持久化结果，Save 必须幂等：同一 url 重复写不产生重复行。
type Store interface {
	Save(ctx context.Context, url string, title string) error
}

// Queue 是 V3 分布式队列：领取带租约，确认后移除，超时可重投。
type Queue interface {
	Push(t TaskMsg) error
	Lease(ctx context.Context) (LeaseMsg, bool, error)
	Ack(id int64) (bool, error)
}

// TaskMsg 与 LeaseMsg 是跨进程帧，字段保持可序列化且有零值语义。
type TaskMsg struct {
	URL     string
	Attempt int
	TraceID string
	Epoch   int64
}

type LeaseMsg struct {
	ID      int64
	Task    TaskMsg
	Expires int64 // UnixNano，消除节点本地时钟歧义
}

func main() {}
