package main

import "fmt"

func main() {
	defer fmt.Println("第一个登记")
	defer fmt.Println("第二个登记")
	fmt.Println("正常流程")
}
