package main

import "fmt"

type Option struct {
	Name   string
	Scores map[string]float64
}

// 加权评分：先明确约束与权重，再算总分，避免凭熟悉度选型。
func main() {
	weights := map[string]float64{
		"性能":    0.3,
		"可维护性": 0.3,
		"生态":    0.2,
		"团队熟悉度": 0.2,
	}
	candidates := []Option{
		{Name: "方案 A", Scores: map[string]float64{"性能": 5, "可维护性": 3, "生态": 4, "团队熟悉度": 5}},
		{Name: "方案 B", Scores: map[string]float64{"性能": 3, "可维护性": 5, "生态": 5, "团队熟悉度": 3}},
	}
	for _, c := range candidates {
		total := 0.0
		for k, w := range weights {
			total += c.Scores[k] * w
		}
		fmt.Printf("%s 加权得分 = %.2f\n", c.Name, total)
	}
}
