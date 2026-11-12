package main

import (
	"errors"
	"fmt"
)

// Participant 模拟一个参与者：先预提交锁定资源，再真正提交
type Participant struct {
	Name      string
	prepared  bool
	committed bool
	failAt    string // 注入故障：prepare / commit / none
}

func (p *Participant) Prepare() error {
	if p.failAt == "prepare" {
		return errors.New(p.Name + " 在 Prepare 阶段宕机")
	}
	p.prepared = true
	return nil
}

func (p *Participant) Commit() error {
	if p.failAt == "commit" {
		return errors.New(p.Name + " 在 Commit 阶段宕机")
	}
	p.committed = true
	return nil
}

func (p *Participant) Rollback() {
	p.prepared = false
}

// TwoPC 返回是否最终提交，并把中途失败留下的状态打印出来
func TwoPC(ps []*Participant, coordinatorFails bool) bool {
	// 阶段一：询问所有参与者能否提交
	for _, p := range ps {
		if err := p.Prepare(); err != nil {
			fmt.Println("Prepare 失败，全局回滚:", err)
			for _, q := range ps {
				q.Rollback()
			}
			return false
		}
	}
	fmt.Println("全部 Prepare 成功，进入提交阶段")

	if coordinatorFails {
		fmt.Println("协调者在发出 Commit 前宕机：参与者已锁定资源却无人通知，只能阻塞等待")
		return false
	}
	// 阶段二：逐个提交，任何一处失败都无法整体回退
	for _, p := range ps {
		if err := p.Commit(); err != nil {
			fmt.Println("Commit 中途失败:", err, "，已提交的参与者无法回退，出现部分提交")
			return false
		}
	}
	return true
}

func main() {
	ok := TwoPC([]*Participant{{Name: "订单库"}, {Name: "库存库"}, {Name: "账户库"}}, false)
	fmt.Println("正常两阶段提交:", ok)

	fmt.Println("--- 注入故障：协调者宕机 ---")
	ps := []*Participant{{Name: "订单库"}, {Name: "库存库"}, {Name: "账户库"}}
	TwoPC(ps, true)
	fmt.Println("参与者 prepared 状态:", ps[0].prepared, ps[1].prepared, ps[2].prepared, "（资源被挂住）")
}
