// stackpath.go
package main

import (
	"fmt"
	"runtime/debug"
)

func f() {
	defer func() { fmt.Print(string(debug.Stack())) }()
	panic("boom")
}

func main() { f() }
