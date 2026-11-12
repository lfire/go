// d_loop.go
package main

//go:noinline
func g(n int) {
	defer func() { println("outer") }()
	for i := 0; i < n; i++ {
		defer func() { println("loop") }()
	}
}

func main() { g(3) }
