package main

import (
	"fmt"
	"math/bits"
)

const (
	ctrlEmpty   = uint8(0b10000000)
	ctrlDeleted = uint8(0b11111110)
	bitsetLSB   = uint64(0x0101010101010101)
	bitsetMSB   = uint64(0x8080808080808080)
	groupSlots  = 8
	maxLoad     = 7
)

// 对应 Go 1.24 internal/runtime/maps/map.go 的 h1 / h2
func h1(h uint64) uint64 { return h >> 7 }
func h2(h uint64) uint64 { return h & 0x7f }

// 对应 group.go 的 ctrlGroupMatchH2 可移植实现
func matchH2(g uint64, h uint64) uint64 {
	v := g ^ (bitsetLSB * h)
	return ((v - bitsetLSB) &^ v) & bitsetMSB
}

// 对应 table.go 的 probeSeq：p(i) = (i^2+i)/2 + h1 (mod mask+1)
func probe(h uint64, mask uint64, steps int) []uint64 {
	off := h & mask
	out := []uint64{off}
	for i := 1; i <= steps; i++ {
		off = (off + uint64(i)) & mask
		out = append(out, off)
	}
	return out
}

// 对应 table.go 的 resetGrowthLeft
func growthLeft(capacity int) int {
	if capacity <= groupSlots {
		return capacity - 1
	}
	return capacity * maxLoad / groupSlots
}

func main() {
	var g uint64
	ctrls := []uint8{0x2a, ctrlEmpty, 0x2a, 0x2a, 0x05, ctrlDeleted, 0x2a, ctrlEmpty}
	for i, c := range ctrls {
		g |= uint64(c) << (8 * uint(i))
	}
	m := matchH2(g, 0x2a)
	fmt.Printf("ctrlGroup=%016x  h2=0x2a  matchH2=%016x\n", g, m)
	fmt.Print("命中槽位:")
	for i := 0; i < groupSlots; i++ {
		if m&(uint64(0x80)<<(8*uint(i))) != 0 {
			fmt.Printf(" %d", i)
		}
	}
	fmt.Printf("  首个=%d\n", bits.TrailingZeros64(m)>>3)

	for _, cap := range []int{8, 16, 64, 1024} {
		fmt.Printf("capacity=%4d growthLeft=%4d 负载上限=%.3f\n",
			cap, growthLeft(cap), float64(growthLeft(cap))/float64(cap))
	}

	h := uint64(0x9e3779b97f4a7c15)
	fmt.Printf("hash=0x%016x h1=0x%013x h2=0x%02x\n", h, h1(h), h2(h))
	for _, mask := range []uint64{3, 7} {
		fmt.Printf("groups=%d 探测序列(整周期)=%v\n", mask+1, probe(h1(h), mask, int(mask)))
	}
}
