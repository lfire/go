package main

import "fmt"

var sink int

func step() { sink++ }

//go:noinline
func eight() {
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	step()
}

//go:noinline
func nine() {
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step()
	defer step() // 第 9 条：超出 8 位 deferBits
	step()
}

func main() {
	eight()
	nine()
	fmt.Println(sink)
}
