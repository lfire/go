package main

import "fmt"

type Node struct {
	Value int
	Next  *Node
}

// nil 接收者可被直接调用
func (n *Node) Sum() int {
	if n == nil {
		return 0
	}
	return n.Value + n.Next.Sum()
}

func main() {
	list := &Node{1, &Node{2, &Node{3, nil}}}
	fmt.Println("链表和 =", list.Sum())

	var empty *Node
	fmt.Println("nil 接收者 =", empty.Sum())
}
