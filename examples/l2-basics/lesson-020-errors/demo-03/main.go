package main

import (
	"errors"
	"fmt"
)

// ErrInvalid 是代表「输入非法」这一类别的根因哨兵。
var ErrInvalid = errors.New("非法输入")

// ValidationError 携带字段名、说明与根因的自定义错误类型。
type ValidationError struct {
	Field string
	Msg   string
	Cause error
}

// 实现 error 接口：唯一要求就是 Error() string。
func (e *ValidationError) Error() string {
	return fmt.Sprintf("字段 %s 校验失败: %s", e.Field, e.Msg)
}

// 实现 Unwrap，让 errors.Is / errors.As 能继续往链下走。
func (e *ValidationError) Unwrap() error { return e.Cause }

func check(age int) error {
	if age < 0 {
		return &ValidationError{Field: "age", Msg: "不能为负", Cause: ErrInvalid}
	}
	return nil
}

func main() {
	err := check(-1)
	fmt.Println(err != nil) // true

	var ve *ValidationError
	fmt.Println(errors.As(err, &ve))        // true：取回自定义类型
	fmt.Println(ve.Field)                   // age
	fmt.Println(errors.Is(err, ErrInvalid)) // true：穿透自定义类型找到根因
}
