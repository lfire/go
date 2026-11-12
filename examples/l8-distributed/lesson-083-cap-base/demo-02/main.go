package main

import "fmt"

// Replica 表示一个副本，Reachable 是对网络故障的直接建模
type Replica struct {
	Name      string
	Reachable bool
	Version   int
}

// Write 在要求 W 个确认的前提下提交写入。
// 分区导致可达副本不足时，选择一致性的系统会拒绝写入。
func Write(replicas []Replica, w int) (bool, string) {
	reachable := 0
	for _, r := range replicas {
		if r.Reachable {
			reachable++
		}
	}
	if reachable < w {
		return false, "CP 取舍：拒绝写入，宁可不可用也不制造冲突"
	}
	return true, "写入成功"
}

func main() {
	normal := []Replica{{"n1", true, 1}, {"n2", true, 1}, {"n3", true, 1}}
	// 分区：只有 n1 可达，n2 与 n3 落在另一侧
	partition := []Replica{{"n1", true, 1}, {"n2", false, 1}, {"n3", false, 1}}
	ok, why := Write(normal, 2)
	fmt.Println("正常 W=2：", ok, why)
	ok, why = Write(partition, 2)
	fmt.Println("分区 W=2：", ok, why)
	// 把 W 降到 1 相当于选可用性，接受写入但另一侧会读到旧值
	ok, _ = Write(partition, 1)
	fmt.Println("分区降级 W=1：", ok, "（可用，但可能读到旧值）")
}
