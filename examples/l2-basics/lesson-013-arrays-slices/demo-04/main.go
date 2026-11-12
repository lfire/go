package main

import (
	"errors"
	"fmt"
)

// clonePrefix 复制 src 的前 n 个元素，返回互不影响的独立切片。
// 三个真实边界：n 越界要报错、copy 可能静默截断、nil 输入要安全返回。
func clonePrefix(src []int, n int) ([]int, error) {
	if n < 0 || n > len(src) {
		return nil, fmt.Errorf("前缀长度 %d 超出 0~%d", n, len(src))
	}
	if src == nil {
		return nil, nil // nil 进，nil 出，调用方 range/append 都安全
	}
	out := make([]int, n)
	if copied := copy(out, src[:n]); copied != n {
		return nil, errors.New("复制数量不足")
	}
	return out, nil
}

func main() {
	ok, err := clonePrefix([]int{1, 2, 3}, 2)
	fmt.Println("克隆 =", ok, "出错 =", err != nil)
	ok[0] = 99
	fmt.Println("改克隆 =", ok)

	_, err = clonePrefix([]int{1}, 5)
	fmt.Println("越界提示 =", err)

	empty, err := clonePrefix(nil, 0)
	fmt.Println("nil 输入得到 =", empty, "出错 =", err != nil, "append 后 =", append(empty, 7))

	// copy 只搬 min(len(dst), len(src)) 个，dst 偏小会静默截断
	small := make([]int, 2)
	n := copy(small, []int{10, 20, 30})
	fmt.Println("只搬了", n, "个 =", small)
}
