package main

import (
	"fmt"
	"time"
)

// Sample 是某一时刻抓到的一组累计计数快照。
type Sample struct {
	At       time.Time
	Requests uint64
	Errors   uint64
	Duration float64 // 累计耗时秒数
}

// RED 是面向服务最核心的三指标：速率、错误占比、平均耗时。
type RED struct {
	Rate     float64
	Errors   float64
	Duration float64
}

func ComputeRED(prev, cur Sample) RED {
	dt := cur.At.Sub(prev.At).Seconds()
	if dt <= 0 {
		return RED{}
	}
	dReq := float64(cur.Requests - prev.Requests)
	dErr := float64(cur.Errors - prev.Errors)
	dDur := cur.Duration - prev.Duration
	r := RED{Rate: dReq / dt, Duration: dDur / dReq}
	if dReq > 0 {
		r.Errors = dErr / dReq
	}
	return r
}

func main() {
	prev := Sample{At: time.Unix(0, 0), Requests: 1000, Errors: 10, Duration: 120}
	cur := Sample{At: time.Unix(60, 0), Requests: 4600, Errors: 58, Duration: 552}
	r := ComputeRED(prev, cur)
	fmt.Printf("Rate=%.1f/s Errors=%.2f Duration=%.3fs\n", r.Rate, r.Errors, r.Duration)
	// 简单告警判定：错误率超过 1% 就报警
	fmt.Println("触发错误率告警:", r.Errors > 0.01)
}
