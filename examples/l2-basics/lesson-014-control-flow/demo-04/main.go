package main

import "fmt"

func main() {
	found := false
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if i == 1 && j == 2 {
				found = true
				goto DONE // 一次跳出两层循环
			}
		}
	}
DONE:
	fmt.Println("结束，found =", found)
}
