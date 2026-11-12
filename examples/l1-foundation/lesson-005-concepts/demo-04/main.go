package main

import "fmt"

func main() {
	a := 42       // a 这个盒子当前装着值 42
	b := a        // 把 42 复制一份，装进新盒子 b
	b = 9         // 只换掉 b 盒子里的东西
	fmt.Println(a, b) // a 仍是 42，b 变成 9
}
