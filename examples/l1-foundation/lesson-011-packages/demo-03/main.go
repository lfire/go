package main

import (
	"fmt"
	_ "net/http/pprof" // 只为触发该包 init 里的注册动作
)

func main() {
	fmt.Println("副作用导入已生效")
}
