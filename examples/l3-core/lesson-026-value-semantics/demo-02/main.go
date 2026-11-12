package main

import "fmt"

type Big struct{ data [4]int }

func byValue(b Big)    { b.data[0] = 9 } // 改的是副本
func byPtr(b *Big)     { b.data[0] = 9 } // 改的是原对象
func byArray(a [4]int) { a[0] = 9 }      // 改的是数组副本
func bySlice(s []int)  { s[0] = 9 }      // 改的是共享底层数组

func main() {
	b, a := Big{}, [4]int{}
	s := []int{0, 0, 0, 0}
	byValue(b)
	fmt.Println("byValue 后:", b.data[0]) // 0
	byArray(a)
	fmt.Println("byArray 后:", a[0]) // 0
	bySlice(s)
	fmt.Println("bySlice 后:", s[0]) // 9
	byPtr(&b)
	fmt.Println("byPtr 后:", b.data[0]) // 9
}
