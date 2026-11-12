package main

import "fmt"

// Step 是一个正向步骤及其对应的补偿步骤
type Step struct {
	Name       string
	Forward    func() error
	Compensate func()
}

// Saga 顺序执行正向步骤；任一步失败，逆序补偿已成功的步骤。
// 这就是「中途失败会怎样」的答案：不留部分成功，靠补偿回到一致。
func Saga(steps []Step) error {
	done := make([]Step, 0, len(steps))
	for _, s := range steps {
		if err := s.Forward(); err != nil {
			fmt.Println("正向失败于:", s.Name, "->", err)
			for i := len(done) - 1; i >= 0; i-- {
				fmt.Println("补偿:", done[i].Name)
				done[i].Compensate()
			}
			return err
		}
		done = append(done, s)
		fmt.Println("完成:", s.Name)
	}
	return nil
}

func main() {
	state := map[string]bool{"创建订单": false, "扣库存": false, "冻结余额": false}
	steps := []Step{
		{"创建订单", func() error { state["创建订单"] = true; return nil },
			func() { state["创建订单"] = false; fmt.Println("  撤销订单") }},
		{"扣库存", func() error { state["扣库存"] = true; return nil },
			func() { state["扣库存"] = false; fmt.Println("  归还库存") }},
		{"冻结余额", func() error { return fmt.Errorf("账户服务超时") },
			func() { state["冻结余额"] = false }},
	}
	err := Saga(steps)
	fmt.Println("Saga 结果:", err)
	fmt.Println("最终状态 创建订单:", state["创建订单"], "扣库存:", state["扣库存"], "冻结余额:", state["冻结余额"])
}
