package main

import "fmt"

func main() {
	v := 1
	f := func() { fmt.Println("闭包读到 v =", v) }
	v = 9
	f()

	var fs []func()
	for _, x := range []int{10, 20, 30} {
		fs = append(fs, func() { fmt.Printf("%d ", x) })
	}
	for _, g := range fs {
		g()
	}
	fmt.Println()

	var ps []*int
	for _, x := range []int{4, 5, 6} {
		ps = append(ps, &x)
	}
	for _, p := range ps {
		fmt.Print(*p, " ")
	}
	fmt.Println()
}
