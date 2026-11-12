package main

import "fmt"

// BurnRate 是某窗口内错误率相对 SLO 允许错误率的倍数
func BurnRate(allowed, observed float64) float64 {
	if allowed == 0 {
		return 0
	}
	return observed / allowed
}

// Alert 用长短两个窗口的燃烧率联合判定，抑制瞬时抖动造成的误报
func Alert(shortBurn, longBurn float64) string {
	// 快窗口高倍且慢窗口也高，才升级为呼叫；否则只记工单
	if shortBurn >= 14 && longBurn >= 6 {
		return "page：立即呼叫 On-call"
	}
	if shortBurn >= 6 || longBurn >= 3 {
		return "ticket：记录工单，工作时间内处理"
	}
	return "ok：不告警"
}

func main() {
	allowed := 0.001 // 99.9% SLO 允许的错误率
	cases := []struct {
		name        string
		shortWindow float64
		longWindow  float64
	}{
		{"瞬时抖动", 0.008, 0.001},
		{"持续劣化", 0.015, 0.008},
		{"正常", 0.0004, 0.0003},
	}
	for _, c := range cases {
		fmt.Printf("%s -> %s\n", c.name, Alert(BurnRate(allowed, c.shortWindow), BurnRate(allowed, c.longWindow)))
	}
}
