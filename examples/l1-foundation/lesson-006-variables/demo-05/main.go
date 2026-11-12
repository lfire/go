package main

import "fmt"

const Exported = 1 // 大写：可被其他包访问，但仍是常量
var mutable = 2    // 小写：只在本包可见，却可以被重新赋值

func main() {
	mutable = 3
	fmt.Println(Exported, mutable)
}
