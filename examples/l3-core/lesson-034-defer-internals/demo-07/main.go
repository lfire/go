// defbits.go
package main

import (
	"fmt"
	"math/bits"
)

// 对应 runtime/panic.go 的 _panic.nextDefer：取最高置位即 LIFO
func main() {
	deferBits := uint8(0b10110010) // 第 7、5、4、1 条已注册
	for deferBits != 0 {
		i := 7 - bits.LeadingZeros8(deferBits)
		fmt.Printf("执行第 %d 条 defer，剩余位图 %08b\n", i, deferBits&^(1<<i))
		deferBits &^= 1 << i
	}
}
