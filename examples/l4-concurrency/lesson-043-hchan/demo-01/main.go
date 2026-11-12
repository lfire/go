package main

import "fmt"

// ring 用与 hchan 相同的思路模拟一个有界环形缓冲
type ring struct {
	buf      []int
	dataqsiz int
	qcount   int
	sendx    int // 下一个写入位置
	recvx    int // 下一个读取位置
}

func newRing(n int) *ring { return &ring{buf: make([]int, n), dataqsiz: n} }

func (r *ring) send(v int) bool {
	if r.qcount == r.dataqsiz {
		return false // 满：真实 hchan 会把发送者挂进 sendq
	}
	r.buf[r.sendx] = v
	r.sendx = (r.sendx + 1) % r.dataqsiz // 回绕，避免搬移数据
	r.qcount++
	return true
}

func (r *ring) recv() (int, bool) {
	if r.qcount == 0 {
		return 0, false // 空：真实 hchan 会把接收者挂进 recvq
	}
	v := r.buf[r.recvx]
	r.recvx = (r.recvx + 1) % r.dataqsiz
	r.qcount--
	return v, true
}

func main() {
	r := newRing(3)
	for _, v := range []int{10, 20, 30} {
		fmt.Println("发送", v, "成功:", r.send(v))
	}
	fmt.Println("缓冲区满后再发送:", r.send(40))
	for i := 0; i < 3; i++ {
		v, _ := r.recv()
		fmt.Println("接收:", v)
	}
	fmt.Println("sendx =", r.sendx, "recvx =", r.recvx, "qcount =", r.qcount)
}
