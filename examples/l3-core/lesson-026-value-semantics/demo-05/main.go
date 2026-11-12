package main

type S3 struct{ A, B, C int }

type S20 struct{ V [20]int }

//go:noinline
func f3(s S3) int { return s.A + s.B + s.C }

//go:noinline
func f20(s S20) int { return s.V[0] + s.V[9] + s.V[16] + s.V[19] }

func main() { println(f3(S3{1, 2, 3}), f20(S20{})) }
