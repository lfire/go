package main

import "fmt"

//go:noinline
func add(a, b int) int { return a + b } // 强制不内联，作为对照

func double(x int) int { return x * 2 } // 成本极低，通常被内联

func main() {
	fmt.Println(add(1, 2))
	fmt.Println(double(21))
}
