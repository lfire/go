package main

import (
	"fmt"
	"runtime"
	"sync"
)

type Buffer struct{ data []byte }

var pool = sync.Pool{
	New: func() any { // 池空时兜底创建
		fmt.Println("调用 New 新建 Buffer")
		return &Buffer{data: make([]byte, 0, 1024)}
	},
}

func main() {
	b := pool.Get().(*Buffer) // 第一次 Get 触发 New
	b.data = append(b.data, 1)
	pool.Put(b)

	b2 := pool.Get().(*Buffer) // 未 GC 通常拿到刚归还的
	fmt.Println("复用对象，长度 =", len(b2.data))
	b2.data = b2.data[:0] // 归还前必须重置
	pool.Put(b2)

	runtime.GC()   // 第一次 GC：本地缓存降为 victim，仍可能被命中
	runtime.GC()   // 第二次 GC：victim 才真正丢弃
	_ = pool.Get() // 池空，只能再调 New
	fmt.Println("两次 GC 后，Pool 已被清空")
}
