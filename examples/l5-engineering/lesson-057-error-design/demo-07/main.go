package main

import (
	"errors"
	"fmt"
)

var marker = errors.New("marker")

type custom struct{ tag string }

func (c *custom) Error() string { return "custom-" + c.tag }
func (c *custom) Is(target error) bool {
	fmt.Printf("  custom.Is 被调用: tag=%s target=%v -> %v\n", c.tag, target, target == marker)
	return target == marker
}

func main() {
	chain := fmt.Errorf("外层: %w", &custom{tag: "inner"})
	fmt.Println("errors.Is(chain, marker) =", errors.Is(chain, marker))
	fmt.Println("errors.Is(chain, errors.New(\"E1\")) =", errors.Is(chain, errors.New("E1")))
}
