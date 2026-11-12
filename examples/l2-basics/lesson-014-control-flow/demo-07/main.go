package main

import (
	"fmt"
	"strconv"
)

func main() {
	for _, day := range []int{1, 2, 3} {
		switch day {
		case 1:
			fmt.Print("一 ")
		case 2:
			fmt.Print("二 ")
		default:
			fmt.Print("其它 ")
		}
	}
	fmt.Println()

	switch n := 1; n {
	case 1:
		fmt.Print("一 ")
		fallthrough
	case 99:
		fmt.Print("无条件进入 ")
	}
	fmt.Println()

	if n, err := strconv.Atoi("42"); err == nil {
		fmt.Println("if 链内 n =", n)
	} else {
		fmt.Println("else 里同样可见 n =", n)
	}

FOUND:
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if i == 1 && j == 2 {
				fmt.Println("标签 break 跳出于", i, j)
				break FOUND
			}
		}
	}
}
