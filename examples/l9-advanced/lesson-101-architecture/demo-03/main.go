package main

import "fmt"

type Price interface{ Get(sku string) int }

type OldPrice struct{}

func (OldPrice) Get(sku string) int { return len(sku) * 10 }

type NewPrice struct{}

func (NewPrice) Get(sku string) int { return len(sku) * 10 }

// 双写校验：重构中期同时调用新旧实现并逐条比对，确认等价后再放量。
func main() {
	old, next := OldPrice{}, NewPrice{}
	mismatch := 0
	for _, sku := range []string{"a", "bb", "ccc", "dddd"} {
		o, n := old.Get(sku), next.Get(sku)
		if o != n {
			mismatch++
		}
		fmt.Println(sku, o, n)
	}
	fmt.Println("不一致数 =", mismatch)
}
