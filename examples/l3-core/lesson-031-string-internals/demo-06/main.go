package main

//go:noinline
func cat3(a, b, c string) string { return a + b + c }

//go:noinline
func cat6(a, b, c, d, e, f string) string { return a + b + c + d + e + f }

//go:noinline
func eqConst(b []byte) bool { return string(b) == "abc" }

//go:noinline
func eqVar(a, b string) bool { return a == b }

func main() {
	println(cat3("a", "b", "c"), cat6("a", "b", "c", "d", "e", "f"),
		eqConst([]byte("abc")), eqVar("a", "a"))
}
