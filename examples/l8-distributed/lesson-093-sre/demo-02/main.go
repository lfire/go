package main

import "fmt"

// Decide 根据已消耗的错误预算给出发布决策，把 SLO 变成可执行依据
func Decide(burnedPercent float64) string {
	switch {
	case burnedPercent >= 100:
		return "预算耗尽：冻结非必要变更，全力修稳定性"
	case burnedPercent >= 75:
		return "预算紧张：只放行修复与回滚，暂停新功能"
	case burnedPercent >= 50:
		return "预算过半：发布需走灰度并加强观测"
	default:
		return "预算充足：可正常发布新功能"
	}
}

func main() {
	for _, burned := range []float64{20, 55, 80, 100} {
		fmt.Printf("已消耗 %3.0f%% -> %s\n", burned, Decide(burned))
	}
}
