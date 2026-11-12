package main

var sink int

func add(a, b int) int { return a + b }

func unused(n int) { _ = add(n, 1) }

func used(n int) { sink = add(n, 1) }

func main() {
	unused(10)
	used(10)
}
