package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Event 是一条解析后的访问日志。TS 是事件时间而非处理时间，
// 窗口归属必须用它，否则处理延迟会把事件算进错误的窗口。
type Event struct {
	TS     int64
	Level  string
	Status int
	Bytes  int
	Raw    string
}

// Envelope 给事件加上来源与位点，支撑 at-least-once 重放与幂等键计算。
type Envelope struct {
	Event
	Source string
	Offset int64
}

// IdempotencyKey 由来源、位点、窗口起点组成：重放同一位点必然命中同一键。
func (e Envelope) IdempotencyKey(windowStart int64) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%d", e.Source, e.Offset, windowStart)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// WindowResult 是一个窗口的聚合结果，落库以窗口起点加级别为唯一键。
type WindowResult struct {
	WindowStart int64
	Level       string
	Count       int64
	Bytes       int64
	Errors      int64
}

// Sink 是落库抽象；BatchWrite 必须幂等，返回 err 时上游重放该批。
type Sink interface {
	BatchWrite(results []WindowResult, keys []string) error
	Close() error
}

func main() {
	e := Envelope{Event: Event{TS: 1700000000, Status: 200}, Source: "app.log", Offset: 42}
	fmt.Println("幂等键:", e.IdempotencyKey(1700000000))
	fmt.Println("重放一致:", e.IdempotencyKey(1700000000) == e.IdempotencyKey(1700000000))
}
