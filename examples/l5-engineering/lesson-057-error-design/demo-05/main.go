package main

import (
	"errors"
	"fmt"
)

var errNotFound = errors.New("record not found")

type AppError struct {
	Code  int
	cause error
}

func (e *AppError) Error() string { return "app error" }
func (e *AppError) Unwrap() error { return e.cause }

func main() {
	base := errNotFound
	wrapped := fmt.Errorf("查询订单: %w", base)
	lost := fmt.Errorf("查询订单: %v", base)
	joined := errors.Join(base, fmt.Errorf("另一个错误"))

	fmt.Printf("wrapped 动态类型 = %T\n", wrapped)
	fmt.Printf("lost    动态类型 = %T\n", lost)
	_, ok := wrapped.(interface{ Unwrap() error })
	fmt.Println("wrapped 实现 Unwrap() error =", ok)
	fmt.Println("errors.Unwrap(wrapped) =", errors.Unwrap(wrapped))
	fmt.Println("errors.Unwrap(lost)    =", errors.Unwrap(lost))
	fmt.Println("errors.Is(wrapped, base) =", errors.Is(wrapped, base))
	fmt.Println("errors.Is(lost, base)    =", errors.Is(lost, base))
	fmt.Println("errors.Is(joined, base)  =", errors.Is(joined, base))

	var ae *AppError
	chain := fmt.Errorf("外层: %w", &AppError{Code: 1001, cause: base})
	fmt.Println("errors.As(chain, &ae) =", errors.As(chain, &ae), "code =", ae.Code)

	func() {
		defer func() { fmt.Println("As 第二参数传非指针:", recover()) }()
		_ = errors.As(chain, AppError{})
	}()
}
