// 文件：contract.go —— 领域模型与依赖倒置的窄接口
package main

import (
	"errors"
	"fmt"
	"time"
)

// 领域错误用哨兵值，上层可用 errors.Is 判定并映射成稳定的 HTTP 状态码。
var (
	ErrStockNotEnough = errors.New("ecom: stock not enough")
	ErrDuplicate      = errors.New("ecom: duplicate request")
	ErrBadRequest     = errors.New("ecom: bad request")
	ErrSagaAbort      = errors.New("ecom: saga aborted")
	ErrDownstream     = errors.New("ecom: downstream failure")
)

// OrderStatus 用显式状态机约束跃迁，禁止从 PAID 回退到 CREATED。
type OrderStatus string

const (
	StatusCreated  OrderStatus = "CREATED"
	StatusReserved OrderStatus = "RESERVED"
	StatusPaid     OrderStatus = "PAID"
	StatusCanceled OrderStatus = "CANCELED"
)

// Order 是订单聚合根。IdemKey 由网关透传，贯穿全链路用于去重；
// Version 是乐观锁，PriceFen 是下单瞬间的价格快照，不随商品改价变化。
type Order struct {
	ID        string
	UserID    string
	SKU       string
	Qty       int
	AmountFen int64
	Status    OrderStatus
	IdemKey   string
	Version   int
	CreatedAt time.Time
}

// 三张窄接口是订单服务对外部世界的全部依赖：可换成 gRPC 客户端，
// 也可在单测里塞入 fake，这是依赖倒置的核心。
type Inventory interface {
	Reserve(idemKey, sku string, qty int) (string, error)
	Release(idemKey string) error
}
type Payment interface {
	Charge(idemKey, orderID string, amountFen int64) (string, error)
	Refund(idemKey, orderID string) error
}
type OrderStore interface {
	Create(o Order) error
	UpdateStatus(orderID string, from, to OrderStatus, version int) error
}

func main() {
	o := Order{ID: "O-1001", SKU: "SKU-9", Qty: 2, AmountFen: 199900,
		Status: StatusCreated, IdemKey: "idem-abc", CreatedAt: time.Now()}
	fmt.Printf("order=%s status=%s amount=%d fen\n", o.ID, o.Status, o.AmountFen)
}
