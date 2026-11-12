// 文件：contract.go —— 秒杀契约：窄接口与领域错误
package main

import (
	"errors"
	"fmt"
)

var (
	ErrSoldOut   = errors.New("seckill: sold out")
	ErrNoTicket  = errors.New("seckill: no ticket")
	ErrDuplicate = errors.New("seckill: duplicate")
)

// 三个窄接口把限流配额、原子预扣、可靠落库解耦，便于分别替换实现。
type Quota interface {
	Take() bool
}
type Stock interface {
	Deduct(userID string) (bool, error)
	Refund(userID string) error
	Left() int
}
type TicketSink interface {
	Enqueue(userID string) bool
	Result(userID string) string
}

// OrderStatus 是排队结果的状态机，轮询接口只返回终态或处理中。
type OrderStatus string

const (
	StatusQueued   OrderStatus = "QUEUED"
	StatusPaid     OrderStatus = "PAID"
	StatusReleased OrderStatus = "RELEASED"
)

func main() {
	var s OrderStatus = StatusQueued
	fmt.Printf("initial=%s\n", s)
}
