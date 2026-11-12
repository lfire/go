package main

import "fmt"

func tiny(x int) int { return x + 1 }

func withCall(x int) int { return len(fmt.Sprint(x)) }

func twoCalls(x int) int { return len(fmt.Sprint(x)) + len(fmt.Sprint(x)) }

func main() {
	fmt.Println(tiny(1), withCall(2), twoCalls(3))
}
