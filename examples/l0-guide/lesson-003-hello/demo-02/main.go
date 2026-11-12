package main

import "fmt"

func main() {
	name := "Gopher"
	fmt.Println(name)                   // Println：打印后自动换行
	fmt.Printf("长度是 %d\n", len(name)) // Printf：按格式符格式化，换行要自己写
	fmt.Print("A", "B", "\n")           // Print：不自动换行，手动补一个
}
