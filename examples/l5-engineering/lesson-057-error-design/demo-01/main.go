// 业务错误码：让调用方按类别判定，而不是比字符串
package main

import (
	"errors"
	"fmt"
)

// Code 是稳定的对外错误类别
type Code int

const (
	CodeOK       Code = 0
	CodeNotFound Code = 1001
	CodeInvalid  Code = 1002
	CodeInternal Code = 1500
)

// AppError 携带错误码与可对外展示的消息
type AppError struct {
	Code  Code
	Msg   string
	cause error
}

func (e *AppError) Error() string { return e.Msg }
func (e *AppError) Unwrap() error { return e.cause }

// Wrap 在底层错误之外附上一层类别与上下文
func Wrap(c Code, msg string, cause error) error {
	return &AppError{Code: c, Msg: msg, cause: cause}
}

// CodeOf 沿错误链找出最近的 AppError 类别
func CodeOf(err error) Code {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return CodeInternal
}

func main() {
	base := errors.New("sql: no rows")
	err := Wrap(CodeNotFound, "订单不存在", base)
	fmt.Println("错误码:", CodeOf(err))
	fmt.Println("仍可判定原始错误:", errors.Is(err, base))
}
