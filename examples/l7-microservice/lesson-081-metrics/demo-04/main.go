package main

import "fmt"

func main() {
	services, methods, statuses, users := 10, 20, 5, 100000
	base := services * methods * statuses
	fmt.Printf("低基数标签 时序数=%d\n", base)
	fmt.Printf("误加 user_id 标签 时序数=%d\n", base*users)
}
