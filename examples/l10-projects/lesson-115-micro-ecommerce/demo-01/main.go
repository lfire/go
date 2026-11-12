// 文件：contract_iface.go —— 内部窄接口与状态机跃迁校验
package main

import (
	"errors"
	"fmt"
)

var ErrIllegalTransition = errors.New("ecom: illegal status transition")

type OrderStatus string

const (
	StatusCreated  OrderStatus = "CREATED"
	StatusReserved OrderStatus = "RESERVED"
	StatusPaid     OrderStatus = "PAID"
	StatusCanceled OrderStatus = "CANCELED"
)

// Inventory 与 Payment 是订单服务仅有的两处外部依赖，
// 保持窄接口才能随时替换 gRPC 实现或塞入 fake。
type Inventory interface {
	Reserve(idemKey, sku string, qty int) (string, error)
	Release(idemKey string) error
}
type Payment interface {
	Charge(idemKey, orderID string, amountFen int64) (string, error)
	Refund(idemKey, orderID string) error
}

// allowed 是一张显式状态机：禁止从 PAID 回退，也禁止对
// CANCELED 再补偿，挡住乱序补偿覆盖已支付订单。
func allowed(from, to OrderStatus) error {
	legal := map[OrderStatus]map[OrderStatus]bool{
		StatusCreated:  {StatusReserved: true, StatusCanceled: true},
		StatusReserved: {StatusPaid: true, StatusCanceled: true},
		StatusPaid:     {},
		StatusCanceled: {},
	}
	if !legal[from][to] {
		return fmt.Errorf("%w: %s to %s", ErrIllegalTransition, from, to)
	}
	return nil
}

func main() {
	fmt.Println("CREATED to PAID:", allowed(StatusCreated, StatusPaid))
	fmt.Println("RESERVED to PAID:", allowed(StatusReserved, StatusPaid))
	fmt.Println("PAID to CREATED:", allowed(StatusPaid, StatusCreated))
}
