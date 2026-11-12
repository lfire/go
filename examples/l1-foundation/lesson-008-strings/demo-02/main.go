package main

import (
	"fmt"
	"strings"
	"time"
)

const n = 20000

func main() {
	start := time.Now()
	s := ""
	for i := 0; i < n; i++ {
		s += "Go"
	}
	fmt.Println("加号耗时：", time.Since(start))

	start = time.Now()
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("Go")
	}
	_ = b.String()
	fmt.Println("Builder 耗时：", time.Since(start))
}
