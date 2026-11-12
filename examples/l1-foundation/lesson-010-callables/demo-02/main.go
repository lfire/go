package main

import "fmt"

func main() {
	n := 1
	f := func() { fmt.Println("闭包读到：", n) }
	n = 9
	f()
}
