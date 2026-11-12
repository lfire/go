package main

import "fmt"

// Node 保存一份带版本号的数据副本
type Node struct {
	Name    string
	Version int
	Value   string
}

// ReadRepair 读取全部可达副本，取版本最高者作为结果，并顺手补齐落后副本。
// 这就是最终一致系统把不一致的收敛推迟到读取时的典型做法。
func ReadRepair(nodes []*Node) string {
	latest := nodes[0]
	for _, n := range nodes {
		if n.Version > latest.Version {
			latest = n
		}
	}
	for _, n := range nodes {
		n.Version = latest.Version
		n.Value = latest.Value
	}
	return latest.Value
}

func main() {
	a := &Node{"n1", 3, "v3"}
	b := &Node{"n2", 2, "v2"} // 分区期间未收到更新
	c := &Node{"n3", 3, "v3"}
	fmt.Println("修复前 n2 =", b.Value)
	got := ReadRepair([]*Node{a, b, c})
	fmt.Println("读到 =", got, "，修复后 n2 =", b.Value)
}
