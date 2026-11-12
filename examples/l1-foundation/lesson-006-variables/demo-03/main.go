package main

import "fmt"

// iota 从 0 起每行加 1；省略表达式的行会重复上一行的写法
const (
	Monday = iota + 1 // 1
	Tuesday           // 2
	Wednesday         // 3
	_ = iota + 1      // 用空标识符跳过编号 4
	Friday            // 5
)

// 表达式也能复用：这里的 1 << (10 * iota) 会被下面两行沿用
const (
	KB = 1 << (10 * iota) // 1
	MB                    // 1024
	GB                    // 1048576
)

func main() {
	fmt.Println(Monday, Tuesday, Wednesday, Friday)
	fmt.Println(KB, MB, GB)
}
