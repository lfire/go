package main

import "fmt"

var sink int

func step() { sink++ }

//go:noinline
func openCoded(flag bool) {
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	if flag { // 返回点一
		return
	}
	sink++ // 返回点二
}

//go:noinline
func loopDefer(n int) {
	for i := 0; i < n; i++ {
		defer step() // 循环破坏开放编码，退化为逐次注册
	}
	sink++
}

func main() {
	openCoded(true)
	loopDefer(6)
	fmt.Println(sink)
}
