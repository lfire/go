package main

import "fmt"

// 领域层只定义接口，不 import 任何具体存储实现。
type OrderRepo interface {
	Save(id string, amount int) error
}

// 基础设施层实现接口。
type MemRepo struct{ data map[string]int }

func (m *MemRepo) Save(id string, amount int) error {
	m.data[id] = amount
	return nil
}

// 应用层依赖抽象，而非具体实现。
type Service struct{ repo OrderRepo }

func (s *Service) Create(id string, amount int) error {
	return s.repo.Save(id, amount)
}

func main() {
	svc := Service{repo: &MemRepo{data: map[string]int{}}}
	if err := svc.Create("o-1", 99); err != nil {
		panic(err)
	}
	fmt.Println("依赖倒置装配完成")
}
