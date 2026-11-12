package main

import (
	"errors"
	"fmt"
)

func main() {
	e1, e2 := errors.New("E1"), errors.New("E2")

	one := fmt.Errorf("one: %w", e1)
	many := fmt.Errorf("a=%w b=%w", e1, e2)
	joined := errors.Join(e1, e2)

	fmt.Printf("单 %%w 类型 = %T\n", one)
	fmt.Printf("多 %%w 类型 = %T\n", many)
	fmt.Printf("Join   类型 = %T\n", joined)

	fmt.Println("errors.Unwrap(one)  =", errors.Unwrap(one))
	fmt.Println("errors.Unwrap(many) =", errors.Unwrap(many)) // 只认 Unwrap() error
	if u, ok := many.(interface{ Unwrap() []error }); ok {
		fmt.Println("many 实现 Unwrap() []error，子节点数 =", len(u.Unwrap()))
	}
	fmt.Println("errors.Is(many, E1) =", errors.Is(many, e1), " errors.Is(many, E2) =", errors.Is(many, e2))
}
