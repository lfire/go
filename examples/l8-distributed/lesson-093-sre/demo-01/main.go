package main

import "fmt"

// SLO 定义可用性目标与统计周期内的请求情况
type SLO struct {
	Target float64 // 例如 0.999 表示 99.9%
	Total  int     // 周期内总请求数
	Bad    int     // 失败请求数
}

// Availability 返回实际可用性
func (s SLO) Availability() float64 {
	if s.Total == 0 {
		return 1
	}
	return 1 - float64(s.Bad)/float64(s.Total)
}

// Budget 返回错误预算：周期内允许的最大失败数
func (s SLO) Budget() float64 {
	return (1 - s.Target) * float64(s.Total)
}

// Burned 返回已消耗的错误预算比例
func (s SLO) Burned() float64 {
	b := s.Budget()
	if b == 0 {
		return 0
	}
	return float64(s.Bad) / b
}

// Met 判断本周期是否达标
func (s SLO) Met() bool {
	return s.Availability() >= s.Target
}

func main() {
	s := SLO{Target: 0.999, Total: 1_000_000, Bad: 420}
	fmt.Printf("实际可用性: %.4f%%\n", s.Availability()*100)
	fmt.Printf("错误预算允许失败数: %.0f\n", s.Budget())
	fmt.Printf("已消耗预算: %.1f%%\n", s.Burned()*100)
	fmt.Println("本周期达标:", s.Met())
}
