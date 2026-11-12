package main

import "fmt"

func main() {
	name := "Gopher"
	fmt.Printf("编号是 %d\n", name) // 故意用错格式符：能编译，但 vet 会报
}
