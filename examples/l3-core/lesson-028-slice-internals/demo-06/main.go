package main

//go:noinline
func makePtr(n int) []*int {
	s := make([]*int, 0, n)
	for i := 0; i < n; i++ {
		s = append(s, new(int))
	}
	return s
}

//go:noinline
func makeVal(n int) []int {
	s := make([]int, 0, n)
	for i := 0; i < n; i++ {
		s = append(s, i)
	}
	return s
}

func main() {
	a, b := makePtr(64), makeVal(64)
	println(len(a), len(b))
}
