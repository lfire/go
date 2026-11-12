package main

import "fmt"

// 对应 runtime/iface.go 的 itabHashFunc 与 find 的二次探测
func findProbe(interHash, typHash uintptr, size uintptr) []uintptr {
	mask := size - 1
	h := (interHash ^ typHash) & mask
	out := []uintptr{h}
	for i := uintptr(1); i <= 6; i++ {
		h += i
		h &= mask
		out = append(out, h)
	}
	return out
}

// 对应 itabAdd 的 75% 扩容阈值与 mallocgc 请求字节
func growAt(size uintptr) (uintptr, uintptr) {
	return 3 * (size / 4), (2 + 2*size) * 8
}

func main() {
	for _, size := range []uintptr{512, 1024, 2048} {
		count, bytes := growAt(size)
		fmt.Printf("size=%4d 扩容阈值 count=%4d 新表 mallocgc=%6d B\n", size, count, bytes)
	}
	interHash := uintptr(0x9e3779b97f4a7c15)
	typHash := uintptr(0x243f6a8885a308d3)
	fmt.Printf("interHash ^ typHash = %#x\n", interHash^typHash)
	fmt.Printf("size=512 二次探测序列: %v\n", findProbe(interHash, typHash, 512))
	fmt.Println("itab 头 24 B：inter(8)+_type(8)+hash(4)+pad(4)；fun[0] 偏移 24")
}
