package main

import "fmt"

func main() {
	for i := 0; i < 3; i++ {
		fmt.Print(i, " ")
	}
	fmt.Println()

	sum, n := 0, 1
	for n <= 5 {
		sum += n
		n++
	}
	fmt.Println("1 到 5 的和 =", sum)

	for {
		fmt.Println("执行一次就退出")
		break
	}

	for i := 0; i < 5; i++ {
		if i%2 == 0 {
			continue
		}
		fmt.Print(i, " ")
	}
	fmt.Println()
}
