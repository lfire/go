// d_open.go
package main

//go:noinline
func f() {
	defer func() { println("cleanup") }()
	println("body")
}

func main() { f() }
