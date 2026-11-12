package main

import (
	"errors"
	"fmt"
)

type Account struct {
	Balance int
}

// Withdraw 用指针接收者改状态；余额不足或接收者为 nil 都返回明确错误。
func (a *Account) Withdraw(amount int) error {
	if a == nil { // 指针接收者可能为 nil，先挡住
		return errors.New("账户为空")
	}
	if amount <= 0 || amount > a.Balance {
		return fmt.Errorf("取款 %d 不合法", amount)
	}
	a.Balance -= amount
	return nil
}

func main() {
	a := &Account{Balance: 100}
	if err := a.Withdraw(30); err == nil {
		fmt.Println("取款后余额 =", a.Balance)
	}
	fmt.Println("超额被拒 =", a.Withdraw(1000) != nil)

	var empty *Account
	fmt.Println("nil 接收者被挡住 =", empty.Withdraw(1) != nil)
}
