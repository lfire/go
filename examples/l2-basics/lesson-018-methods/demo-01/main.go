package main

import "fmt"

type Counter struct {
	N int
}

// 值接收者：拿到副本
func (c Counter) AddValue(delta int) {
	c.N += delta
}

// 指针接收者：改到原值
func (c *Counter) Add(delta int) {
	c.N += delta
}

func main() {
	c := Counter{N: 1}
	c.AddValue(10)
	fmt.Println("值接收者后 N =", c.N)
	c.Add(10)
	fmt.Println("指针接收者后 N =", c.N)

	p := &Counter{N: 1}
	p.AddValue(5) // 指针也能调值方法
	fmt.Println("指针调值方法后 N =", p.N)
}
