// gen.go（//go:build ignore，不参与包编译）
package main

import (
	"fmt"
	"os"
)

func main() {
	for _, k := range []string{"GOFILE", "GOPACKAGE", "GOLINE", "GOOS", "GOARCH", "DOLLAR", "GOWORK"} {
		fmt.Printf("  %s=%s\n", k, os.Getenv(k))
	}
	fmt.Println("  args =", os.Args[1:])
}
