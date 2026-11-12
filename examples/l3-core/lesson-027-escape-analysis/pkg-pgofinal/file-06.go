// lib.go
package pgofinal

// 30 条独立语句把成本推到 97：大于 80、小于 2000
func helper(x int) int {
	s := x
	s += 1
	s += 2
	s += 3
	s += 4
	s += 5
	s += 6
	s += 7
	s += 8
	s += 9
	s += 10
	s += 11
	s += 12
	s += 13
	s += 14
	s += 15
	s += 16
	s += 17
	s += 18
	s += 19
	s += 20
	s += 21
	s += 22
	s += 23
	s += 24
	s += 25
	s += 26
	s += 27
	s += 28
	s += 29
	s += 30
	return s
}

//go:noinline
func Hot(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += helper(i)
	}
	return s
}
