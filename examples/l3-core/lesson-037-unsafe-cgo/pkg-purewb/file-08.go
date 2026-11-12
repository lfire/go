// wb.go
package purewb

type Node struct {
	Next *Node
	Val  int64
}

var Hold []Node

func PtrStores(n int) {
	buf := make([]Node, n)
	var prev *Node
	for i := range buf {
		buf[i].Next = prev // 每次都是一次堆指针写
		buf[i].Val = int64(i)
		prev = &buf[i]
	}
	Hold = buf
}
