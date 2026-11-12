package main

//go:noinline
func f(a int, b float64, c any) float64 {
	_ = c
	return float64(a) + b
}

func main() { println(f(1, 2, 3)) }
