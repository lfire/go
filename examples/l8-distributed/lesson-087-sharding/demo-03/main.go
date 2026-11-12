package main

import "fmt"

// shardOf 按分片键取模，模拟按 tenant_id 分库
func shardOf(tenantID, n int) int {
	return tenantID % n
}

// spread 返回各分片承载量的最大值与最小值
func spread(per []int) (int, int) {
	max, min := per[0], per[0]
	for _, v := range per {
		if v > max {
			max = v
		}
		if v < min {
			min = v
		}
	}
	return max, min
}

func main() {
	const shards = 8
	// 模拟各租户订单量：一个超大客户占了绝大部分
	loads := map[int]int{1: 120, 2: 90, 3: 110, 4: 80, 5: 95, 6: 100, 7: 88, 8: 9000}
	per := make([]int, shards)
	for tenant, cnt := range loads {
		per[shardOf(tenant, shards)] += cnt
	}
	fmt.Println("按 tenant_id 取模，各分片承载量:", per)
	max, min := spread(per)
	fmt.Println("最热分片:", max, "最冷分片:", min, "出现严重倾斜:", max > min*4)

	// 把热点租户从公共取模里摘出去，单独划一个分片
	hot := per[shardOf(8, shards)]
	per[shardOf(8, shards)] -= hot
	fmt.Println("把热点租户迁到独立分片，腾出承载量:", hot)
}
