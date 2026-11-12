// cmd/api/main.go 的等价可运行示例：装配根
package main

import (
	"fmt"
)

// OrderRepository 是领域层定义的抽象，属于业务规则，不依赖任何数据库。
type OrderRepository interface {
	AmountOf(id string) (int, error)
}

// OrderService 只依赖抽象接口，实现可以被自由替换。
type OrderService struct {
	repo OrderRepository
}

func NewOrderService(r OrderRepository) *OrderService {
	return &OrderService{repo: r}
}

func (s *OrderService) Total(id string) (int, error) {
	return s.repo.AmountOf(id)
}

// memoryRepo 是放在 internal 下的具体实现，外部包无法直接 import。
type memoryRepo struct {
	data map[string]int
}

func (m *memoryRepo) AmountOf(id string) (int, error) {
	v, ok := m.data[id]
	if !ok {
		return 0, fmt.Errorf("订单 %s 不存在", id)
	}
	return v, nil
}

func main() {
	// 唯一知道「谁实现谁」的地方：入口装配
	svc := NewOrderService(&memoryRepo{data: map[string]int{"A-1001": 280}})
	total, err := svc.Total("A-1001")
	if err != nil {
		fmt.Println("查询失败:", err)
		return
	}
	fmt.Println("订单金额:", total)
}
