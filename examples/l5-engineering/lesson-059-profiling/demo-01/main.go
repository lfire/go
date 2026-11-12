// 手动采集 CPU profile：适合离线复现一段热点代码
package main

import (
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
)

// hot 故意制造一段 CPU 热点，便于在 profile 里看到
func hot(n int) int {
	s := ""
	for i := 0; i < n; i++ {
		s += "x"
	}
	return len(s) + strings.Count(s, "x")
}

func main() {
	f, err := os.Create("cpu.pprof")
	if err != nil {
		fmt.Println("创建 profile 文件失败:", err)
		return
	}
	defer f.Close()
	if err := pprof.StartCPUProfile(f); err != nil {
		fmt.Println("启动 CPU profile 失败:", err)
		return
	}
	defer pprof.StopCPUProfile()

	fmt.Println("热点结果:", hot(20000))
	fmt.Println("已写出 cpu.pprof，可用 go tool pprof 分析")
}
